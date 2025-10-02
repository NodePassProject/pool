// Package pool 实现了一个高性能、可靠的网络连接池管理系统
package pool

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	defaultMinCap           = 1
	defaultMaxCap           = 1
	defaultMinIvl           = time.Second
	defaultMaxIvl           = time.Second
	readTimeout             = 20 * time.Second
	acceptRetryInterval     = 50 * time.Millisecond
	intervalAdjustStep      = 100 * time.Millisecond
	capacityAdjustLowRatio  = 0.2
	capacityAdjustHighRatio = 0.8
	intervalLowThreshold    = 0.2
	intervalHighThreshold   = 0.8
	drainBufferSize         = 2048
	activeCheckTimeout      = 20 * time.Millisecond
)

// Pool 连接池结构体，用于管理多个网络连接
type Pool struct {
	mu        sync.Mutex               // 互斥锁，保护共享资源访问
	conns     sync.Map                 // 存储连接的映射表
	idChan    chan string              // 可用ID通道
	tlsCode   string                   // TLS安全模式代码
	hostname  string                   // 主机名
	clientIP  string                   // 客户端IP
	tlsConfig *tls.Config              // TLS配置
	dialer    func() (net.Conn, error) // 创建连接的函数
	listener  net.Listener             // 监听器
	errCount  int                      // 错误计数
	capacity  int                      // 当前容量
	minCap    int                      // 最小容量
	maxCap    int                      // 最大容量
	interval  time.Duration            // 连接创建间隔
	minIvl    time.Duration            // 最小间隔
	maxIvl    time.Duration            // 最大间隔
	keepAlive time.Duration            // 保活间隔
	ctx       context.Context          // 上下文
	cancel    context.CancelFunc       // 取消函数
}

// NewClientPool 创建新的客户端连接池
func NewClientPool(
	minCap, maxCap int,
	minIvl, maxIvl time.Duration,
	keepAlive time.Duration,
	tlsCode string,
	hostname string,
	dialer func() (net.Conn, error),
) *Pool {
	if minCap <= 0 {
		minCap = defaultMinCap
	}
	if maxCap <= 0 {
		maxCap = defaultMaxCap
	}
	if minCap > maxCap {
		minCap, maxCap = maxCap, minCap
	}

	if minIvl <= 0 {
		minIvl = defaultMinIvl
	}
	if maxIvl <= 0 {
		maxIvl = defaultMaxIvl
	}
	if minIvl > maxIvl {
		minIvl, maxIvl = maxIvl, minIvl
	}

	return &Pool{
		conns:     sync.Map{},
		idChan:    make(chan string, maxCap),
		tlsCode:   tlsCode,
		hostname:  hostname,
		dialer:    dialer,
		capacity:  minCap,
		minCap:    minCap,
		maxCap:    maxCap,
		interval:  minIvl,
		minIvl:    minIvl,
		maxIvl:    maxIvl,
		keepAlive: keepAlive,
	}
}

// NewServerPool 创建新的服务器连接池
func NewServerPool(
	maxCap int,
	clientIP string,
	tlsConfig *tls.Config,
	listener net.Listener,
	keepAlive time.Duration,
) *Pool {
	if maxCap <= 0 {
		maxCap = defaultMaxCap
	}

	if listener == nil {
		return nil
	}

	return &Pool{
		conns:     sync.Map{},
		idChan:    make(chan string, maxCap),
		clientIP:  clientIP,
		tlsConfig: tlsConfig,
		listener:  listener,
		maxCap:    maxCap,
		keepAlive: keepAlive,
	}
}

// ClientManager 客户端连接池管理器，负责创建和维护客户端连接
func (p *Pool) ClientManager() {
	if p.cancel != nil {
		p.cancel()
	}
	p.ctx, p.cancel = context.WithCancel(context.Background())
	var mu sync.Mutex

	for {
		if p.ctx.Err() != nil {
			return
		}

		if !mu.TryLock() {
			continue
		}

		p.adjustInterval()
		created := 0

		// 填充连接池至目标容量
		for len(p.idChan) < p.capacity {
			conn, err := p.dialer()
			if err != nil {
				continue
			}

			conn.(*net.TCPConn).SetKeepAlive(true)
			conn.(*net.TCPConn).SetKeepAlivePeriod(p.keepAlive)

			var id string
			// 根据TLS代码应用不同级别的TLS安全
			switch p.tlsCode {
			case "0":
				// 不使用TLS
			case "1":
				// 使用自签名证书（不验证）
				tlsConn := tls.Client(conn, &tls.Config{
					InsecureSkipVerify: true,
					MinVersion:         tls.VersionTLS13,
				})
				if err := tlsConn.Handshake(); err != nil {
					conn.Close()
					continue
				}
				conn = tlsConn
			case "2":
				// 使用验证证书（安全模式）
				tlsConn := tls.Client(conn, &tls.Config{
					InsecureSkipVerify: false,
					MinVersion:         tls.VersionTLS13,
					ServerName:         p.hostname,
				})
				if err := tlsConn.Handshake(); err != nil {
					conn.Close()
					continue
				}
				conn = tlsConn
			}

			// 接收连接ID
			conn.SetReadDeadline(time.Now().Add(readTimeout))
			buf := make([]byte, 4)
			n, err := io.ReadFull(conn, buf)
			if err != nil || n != 4 {
				conn.Close()
				continue
			}
			id = hex.EncodeToString(buf)
			conn.SetReadDeadline(time.Time{})

			// 建立映射并存入通道
			p.conns.Store(id, conn)
			select {
			case p.idChan <- id:
				created++
			default:
				p.conns.Delete(id)
				conn.Close()
			}
		}

		p.adjustCapacity(created)
		mu.Unlock()
		time.Sleep(p.interval)
	}
}

// ServerManager 服务器连接池管理器，负责接受和管理新连接
func (p *Pool) ServerManager() {
	if p.cancel != nil {
		p.cancel()
	}
	p.ctx, p.cancel = context.WithCancel(context.Background())

	for {
		if p.ctx.Err() != nil {
			return
		}

		conn, err := p.listener.Accept()
		if err != nil {
			if p.ctx.Err() != nil || err == net.ErrClosed {
				return
			}

			select {
			case <-p.ctx.Done():
				return
			case <-time.After(acceptRetryInterval):
			}
			continue
		}

		conn.(*net.TCPConn).SetKeepAlive(true)
		conn.(*net.TCPConn).SetKeepAlivePeriod(p.keepAlive)

		// 验证客户端IP（如果指定）
		if p.clientIP != "" && conn.RemoteAddr().(*net.TCPAddr).IP.String() != p.clientIP {
			conn.Close()
			continue
		}

		// 应用TLS（如果配置）
		if p.tlsConfig != nil {
			tlsConn := tls.Server(conn, p.tlsConfig)
			if err := tlsConn.Handshake(); err != nil {
				conn.Close()
				continue
			}
			conn = tlsConn
		}

		// 生成连接ID
		rawID := make([]byte, 4)
		if _, err = rand.Read(rawID); err != nil {
			conn.Close()
			continue
		}
		id := hex.EncodeToString(rawID)

		// 防止重复连接ID
		if _, exist := p.conns.Load(id); exist {
			conn.Close()
			continue
		}

		// 发送连接ID原始字节
		if _, err = conn.Write(rawID); err != nil {
			conn.Close()
			continue
		}

		// 建立映射并存入通道
		p.conns.Store(id, conn)
		select {
		case p.idChan <- id:
		default:
			p.conns.Delete(id)
			conn.Close()
		}
	}
}

// ClientGet 获取指定ID的客户端连接
func (p *Pool) ClientGet(id string) (net.Conn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if conn, ok := p.conns.LoadAndDelete(id); ok {
		p.removeID(id)
		return conn.(net.Conn), nil
	}
	return nil, fmt.Errorf("pool connection not found")
}

// ServerGet 获取一个可用的服务器连接及其ID
func (p *Pool) ServerGet(timeout time.Duration) (string, net.Conn, error) {
	for {
		select {
		case <-p.ctx.Done():
			return "", nil, p.ctx.Err()
		case id := <-p.idChan:
			if conn, ok := p.conns.LoadAndDelete(id); ok {
				netConn := conn.(net.Conn)
				if p.isActive(netConn) {
					return id, netConn, nil
				}
				netConn.Close()
			}
		case <-time.After(timeout):
			return "", nil, fmt.Errorf("insufficient pool connections")
		}
	}
}

// Put 将连接放回连接池
func (p *Pool) Put(id string, conn net.Conn) {
	if id == "" || conn == nil {
		return
	}

	// 清空连接残留数据
	if !p.drainConn(conn) {
		conn.Close()
		return
	}

	// 防止重复放回
	if _, loaded := p.conns.LoadOrStore(id, conn); loaded {
		conn.Close()
		return
	}

	// 防止立即复用导致竞态
	go func() {
		select {
		case <-time.After(p.interval):
			select {
			case p.idChan <- id:
				// 成功放回连接池
			default:
				// 池满，除名并关闭连接
				p.conns.Delete(id)
				conn.Close()
			}
		case <-p.ctx.Done():
			p.conns.Delete(id)
			conn.Close()
			return
		}
	}()
}

// Clean 清理池连接中的闲置连接
func (p *Pool) Clean() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for {
		select {
		case id := <-p.idChan:
			if conn, ok := p.conns.LoadAndDelete(id); ok {
				if conn != nil {
					conn.(net.Conn).Close()
				}
			}
		default:
			return
		}
	}
}

// Flush 清空连接池中的所有连接
func (p *Pool) Flush() {
	p.mu.Lock()
	defer p.mu.Unlock()

	var wg sync.WaitGroup
	p.conns.Range(func(key, value any) bool {
		wg.Go(func() {
			if value != nil {
				value.(net.Conn).Close()
			}
		})
		return true
	})
	wg.Wait()

	p.conns = sync.Map{}
	p.idChan = make(chan string, p.maxCap)
}

// Close 关闭连接池并释放资源
func (p *Pool) Close() {
	if p.cancel != nil {
		p.cancel()
	}
	p.Flush()
}

// Ready 检查连接池是否已初始化
func (p *Pool) Ready() bool {
	return p.ctx != nil
}

// Active 获取当前活跃连接数
func (p *Pool) Active() int {
	return len(p.idChan)
}

// Capacity 获取当前连接池容量
func (p *Pool) Capacity() int {
	return p.capacity
}

// Interval 获取当前连接创建间隔
func (p *Pool) Interval() time.Duration {
	return p.interval
}

// AddError 增加错误计数
func (p *Pool) AddError() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.errCount++
}

// ErrorCount 获取错误计数
func (p *Pool) ErrorCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.errCount
}

// ResetError 重置错误计数
func (p *Pool) ResetError() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.errCount = 0
}

// removeID 从ID通道中移除指定ID
func (p *Pool) removeID(id string) {
	var wg sync.WaitGroup
	tmpChan := make(chan string, p.maxCap)

	for {
		select {
		case tmp := <-p.idChan:
			wg.Go(func() {
				if tmp != id {
					tmpChan <- tmp
				}
			})
		default:
			wg.Wait()
			p.idChan = tmpChan
			return
		}
	}
}

// adjustInterval 根据连接池使用情况动态调整连接创建间隔
func (p *Pool) adjustInterval() {
	idle := len(p.idChan)

	if idle < int(float64(p.capacity)*intervalLowThreshold) && p.interval > p.minIvl {
		p.interval -= intervalAdjustStep
		if p.interval < p.minIvl {
			p.interval = p.minIvl
		}
	}

	if idle > int(float64(p.capacity)*intervalHighThreshold) && p.interval < p.maxIvl {
		p.interval += intervalAdjustStep
		if p.interval > p.maxIvl {
			p.interval = p.maxIvl
		}
	}
}

// adjustCapacity 根据创建成功率动态调整连接池容量
func (p *Pool) adjustCapacity(created int) {
	ratio := float64(created) / float64(p.capacity)

	if ratio < capacityAdjustLowRatio && p.capacity > p.minCap {
		p.capacity--
	}

	if ratio > capacityAdjustHighRatio && p.capacity < p.maxCap {
		p.capacity++
	}
}

// drainConn 清空连接中的残留数据
func (p *Pool) drainConn(conn net.Conn) bool {
	conn.SetReadDeadline(time.Now().Add(p.keepAlive))
	defer conn.SetReadDeadline(time.Time{})

	buf := make([]byte, drainBufferSize)

	for {
		n, err := conn.Read(buf)
		if n == 0 || err != nil {
			if err == nil {
				return true
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				return true
			}
			return false
		}
	}
}

// isActive 检查连接是否处于活跃状态
func (p *Pool) isActive(conn net.Conn) bool {
	if err := conn.SetWriteDeadline(time.Now().Add(activeCheckTimeout)); err != nil {
		return false
	}

	if _, err := conn.Write([]byte{}); err != nil {
		return false
	}

	if err := conn.SetWriteDeadline(time.Time{}); err != nil {
		return false
	}

	return true
}

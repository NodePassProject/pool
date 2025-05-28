// Package pool 实现了一个高性能、可靠的网络连接池管理系统
package pool

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"net"
	"sync"
	"time"
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
		minCap = 1
	}
	if maxCap <= 0 {
		maxCap = 1
	}
	if minCap > maxCap {
		minCap, maxCap = maxCap, minCap
	}

	if minIvl <= 0 {
		minIvl = time.Second
	}
	if maxIvl <= 0 {
		maxIvl = time.Second
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
	clientIP string,
	tlsConfig *tls.Config,
	listener net.Listener,
	keepAlive time.Duration,
) *Pool {
	maxCap := 65536
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
		select {
		case <-p.ctx.Done():
			return
		default:
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
					err := tlsConn.Handshake()
					if err != nil {
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
					err := tlsConn.Handshake()
					if err != nil {
						conn.Close()
						continue
					}
					conn = tlsConn
				}

				// 读取连接ID
				buf := make([]byte, 8)
				n, err := conn.Read(buf)
				if err != nil || n != 8 {
					conn.Close()
					continue
				}

				conn.(*net.TCPConn).SetKeepAlive(true)
				conn.(*net.TCPConn).SetKeepAlivePeriod(p.keepAlive)

				id := string(buf[:n])
				select {
				case p.idChan <- id:
					p.conns.Store(id, conn)
					created++
				default:
					conn.Close()
				}
			}

			p.adjustCapacity(created)
			mu.Unlock()
			time.Sleep(p.interval)
		}
	}
}

// ServerManager 服务器连接池管理器，负责接受和管理新连接
func (p *Pool) ServerManager() {
	if p.cancel != nil {
		p.cancel()
	}
	p.ctx, p.cancel = context.WithCancel(context.Background())

	for {
		select {
		case <-p.ctx.Done():
			return
		default:
			conn, err := p.listener.Accept()
			if err != nil {
				continue
			}

			// 验证客户端IP（如果指定）
			if p.clientIP != "" && conn.RemoteAddr().(*net.TCPAddr).IP.String() != p.clientIP {
				conn.Close()
				continue
			}

			// 应用TLS（如果配置）
			if p.tlsConfig != nil {
				tlsConn := tls.Server(conn, p.tlsConfig)
				err := tlsConn.Handshake()
				if err != nil {
					conn.Close()
					continue
				}
				conn = tlsConn
			}

			// 生成并发送连接ID
			id := p.getID()
			if _, exist := p.conns.Load(id); exist {
				conn.Close()
				continue
			}

			_, err = conn.Write([]byte(id))
			if err != nil {
				conn.Close()
				continue
			}

			conn.(*net.TCPConn).SetKeepAlive(true)
			conn.(*net.TCPConn).SetKeepAlivePeriod(p.keepAlive)

			select {
			case p.idChan <- id:
				p.conns.Store(id, conn)
			default:
				conn.Close()
			}
		}
	}
}

// ClientGet 获取指定ID的客户端连接
func (p *Pool) ClientGet(id string) net.Conn {
	p.mu.Lock()
	defer p.mu.Unlock()

	if conn, ok := p.conns.LoadAndDelete(id); ok {
		p.removeID(id)
		return conn.(net.Conn)
	}
	return nil
}

// ServerGet 获取一个可用的服务器连接及其ID
func (p *Pool) ServerGet() (string, net.Conn) {
	for {
		select {
		case id := <-p.idChan:
			if conn, ok := p.conns.LoadAndDelete(id); ok {
				netConn := conn.(net.Conn)
				if p.isActive(netConn) {
					return id, netConn
				}
				netConn.Close()
			}
		case <-p.ctx.Done():
			return p.ctx.Err().Error(), nil
		}
	}
}

// Flush 清空连接池中的所有连接
func (p *Pool) Flush() {
	p.mu.Lock()
	defer p.mu.Unlock()

	var wg sync.WaitGroup
	p.conns.Range(func(key, value any) bool {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value.(net.Conn).Close()
		}()
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

// getID 生成唯一的连接ID
func (p *Pool) getID() string {
	bytes := make([]byte, 4)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// removeID 从ID通道中移除指定ID
func (p *Pool) removeID(id string) {
	var wg sync.WaitGroup
	tmpChan := make(chan string, p.maxCap)

	for {
		select {
		case tmp := <-p.idChan:
			wg.Add(1)
			go func() {
				defer wg.Done()
				if tmp != id {
					tmpChan <- tmp
				}
			}()
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

	if idle < p.capacity*2/10 && p.interval > p.minIvl {
		p.interval -= 100 * time.Millisecond
		if p.interval < p.minIvl {
			p.interval = p.minIvl
		}
	}

	if idle > p.capacity*8/10 && p.interval < p.maxIvl {
		p.interval += 100 * time.Millisecond
		if p.interval > p.maxIvl {
			p.interval = p.maxIvl
		}
	}
}

// adjustCapacity 根据创建成功率动态调整连接池容量
func (p *Pool) adjustCapacity(created int) {
	ratio := float64(created) / float64(p.capacity)

	if ratio < 0.2 && p.capacity > p.minCap {
		p.capacity--
	}

	if ratio > 0.8 && p.capacity < p.maxCap {
		p.capacity++
	}
}

// isActive 检查连接是否处于活跃状态
func (p *Pool) isActive(conn net.Conn) bool {
	if err := conn.SetReadDeadline(time.Now().Add(time.Millisecond)); err != nil {
		return false
	}

	_, err := conn.Read(make([]byte, 1))

	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return false
	}

	if err, ok := err.(net.Error); ok && err.Timeout() {
		return true
	}

	return false
}

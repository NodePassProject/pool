# Pool Package

[![Go Reference](https://pkg.go.dev/badge/github.com/NodePassProject/pool.svg)](https://pkg.go.dev/github.com/NodePassProject/pool)
[![License](https://img.shields.io/badge/License-BSD_3--Clause-blue.svg)](https://opensource.org/licenses/BSD-3-Clause)

A high-performance, reliable network connection pool management system for Go applications.

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Usage](#usage)
  - [Client Connection Pool](#client-connection-pool)
  - [Server Connection Pool](#server-connection-pool)
  - [Returning Connections](#returning-connections)
  - [Managing Pool Health](#managing-pool-health)
- [Security Features](#security-features)
  - [Client IP Restriction](#client-ip-restriction)
  - [TLS Security Modes](#tls-security-modes)
- [Connection Keep-Alive](#connection-keep-alive)
- [Dynamic Adjustment](#dynamic-adjustment)
- [Advanced Usage](#advanced-usage)
- [Performance Considerations](#performance-considerations)
- [Troubleshooting](#troubleshooting)
- [Best Practices](#best-practices)
  - [Pool Configuration](#1-pool-configuration)
  - [Connection Management](#2-connection-management)
  - [Error Handling and Monitoring](#3-error-handling-and-monitoring)
  - [Production Deployment](#4-production-deployment)
  - [Performance Optimization](#5-performance-optimization)
  - [Testing and Development](#6-testing-and-development)
- [License](#license)

## Features

- **Thread-safe connection management** with mutex protection
- **Support for both client and server connection pools**
- **Dynamic capacity adjustment** based on usage patterns
- **Automatic connection health monitoring**
- **Connection keep-alive management** for maintaining active connections
- **Multiple TLS security modes** (none, self-signed, verified)
- **Connection identification and tracking**
- **Graceful error handling and recovery**
- **Configurable connection creation intervals**
- **Auto-reconnection with exponential backoff**
- **Connection activity validation**

## Installation

```bash
go get github.com/NodePassProject/pool
```

## Quick Start

Here's a minimal example to get you started:

```go
package main

import (
    "net"
    "time"
    "github.com/NodePassProject/pool"
)

func main() {
    // Create a client pool
    dialer := func() (net.Conn, error) {
        return net.Dial("tcp", "example.com:8080")
    }
    
    pool := pool.NewClientPool(
        5, 20,                              // min/max capacity
        500*time.Millisecond, 5*time.Second, // min/max intervals
        30*time.Second,                     // keep-alive period
        "0",                                // TLS mode
        "example.com",                      // hostname
        dialer,
    )
    
    // Start the pool manager
    go pool.ClientManager()
    
    // Use the pool
    conn := pool.ClientGet("connection-id")
    if conn != nil {
        // Use connection...
        defer conn.Close()
    }
    
    // Clean up
    defer pool.Close()
}
```

## Usage

### Client Connection Pool

```go
package main

import (
    "net"
    "time"
    "github.com/NodePassProject/pool"
)

func main() {    // Create a dialer function
    dialer := func() (net.Conn, error) {
        return net.Dial("tcp", "example.com:8080")
    }
    // Create a new client pool with:
    // - Minimum capacity: 5 connections
    // - Maximum capacity: 20 connections
    // - Minimum interval: 500ms between connection attempts
    // - Maximum interval: 5s between connection attempts
    // - Keep-alive period: 30s for connection health monitoring
    // - TLS mode: "2" (verified certificates)
    // - Hostname for certificate verification: "example.com"
    clientPool := pool.NewClientPool(
        5, 20,
        500*time.Millisecond, 5*time.Second,
        30*time.Second,
        "2",
        "example.com",
        dialer,
    )
    
    // Start the client manager (usually in a goroutine)
    go clientPool.ClientManager()
    
    // Get a connection by ID (usually received from the server)
    conn := clientPool.ClientGet("connection-id")
    
    // Use the connection...
    
    // When finished with the pool
    clientPool.Close()
}
```

### Server Connection Pool

```go
package main

import (
    "crypto/tls"
    "net"
    "github.com/NodePassProject/pool"
)

func main() {
    // Create a listener
    listener, err := net.Listen("tcp", ":8080")
    if err != nil {
        panic(err)
    }
    
    // Optional: Create a TLS config
    tlsConfig := &tls.Config{
        // Configure TLS settings
        MinVersion: tls.VersionTLS13,    }
    
    // Create a new server pool
    // - Restrict to specific client IP (optional, "" for any IP, "192.168.1.10" to only allow that specific IP)
    // - Use TLS config (optional, nil for no TLS)
    // - Use the created listener
    // - Keep-alive period: 30s for connection health monitoring
    serverPool := pool.NewServerPool("192.168.1.10", tlsConfig, listener, 30*time.Second)
    
    // Start the server manager (usually in a goroutine)
    go serverPool.ServerManager()
    
    // Get a new connection from the pool (blocks until available)
    id, conn := serverPool.ServerGet(30 * time.Second)
    
    // Use the connection...
    
    // When finished with the pool
    serverPool.Close()
}
```

### Returning Connections

When you finish using a connection, you can return it to the pool using the `Put` method. This helps avoid connection leaks and maximizes reuse:

```go
// After using the connection
pool.Put(id, conn)
```
- `id` is the connection ID generated by the server.
- `conn` is the `net.Conn` object you want to return.

If the pool is full or the connection is already present, `Put` will close the connection automatically.

**Best Practice:** Always call `Put` (or `Close` if not reusing) after you are done with a connection to prevent resource leaks.

### Managing Pool Health

```go
// Check if the pool is ready
if clientPool.Ready() {
    // The pool is initialized and ready for use
}

// Get current active connection count
activeConnections := clientPool.Active()

// Get current capacity setting
capacity := clientPool.Capacity()

// Get current connection creation interval
interval := clientPool.Interval()

// Manually flush all connections (rarely needed)
clientPool.Flush()

// Record an error (increases internal error counter)
clientPool.AddError()

// Get the current error count
errorCount := clientPool.ErrorCount()

// Reset the error count to zero
clientPool.ResetError()
```

## Security Features

### Client IP Restriction

The `NewServerPool` function allows you to restrict incoming connections to a specific client IP address. The function signature is:

```go
func NewServerPool(
    maxCap int,
    clientIP string,
    tlsConfig *tls.Config,
    listener net.Listener,
    keepAlive time.Duration,
) *Pool
```

- `maxCap`: Maximum pool capacity.
- `clientIP`: Restrict allowed client IP ("" for any).
- `tlsConfig`: TLS configuration (can be nil).
- `listener`: TCP listener.
- `keepAlive`: Keep-alive period.

When the `clientIP` parameter is set:
- All connections from other IP addresses will be immediately closed.
- This provides an additional layer of security beyond network firewalls.
- Particularly useful for internal services or dedicated client-server applications.

To allow connections from any IP address, use an empty string:

```go
// Create a server pool that accepts connections from any IP
serverPool := pool.NewServerPool(20, "", tlsConfig, listener, 30*time.Second)
```

### TLS Security Modes

| Mode | Description | Security Level | Use Case |
|------|-------------|----------------|----------|
| `"0"` | No TLS (plain TCP) | None | Internal networks, maximum performance |
| `"1"` | Self-signed certificates | Medium | Development, testing environments |
| `"2"` | Verified certificates | High | Production, public networks |

#### Example Usage

```go
// No TLS - maximum performance
clientPool := pool.NewClientPool(5, 20, minIvl, maxIvl, keepAlive, "0", "example.com", dialer)

// Self-signed TLS - development/testing
clientPool := pool.NewClientPool(5, 20, minIvl, maxIvl, keepAlive, "1", "example.com", dialer)

// Verified TLS - production
clientPool := pool.NewClientPool(5, 20, minIvl, maxIvl, keepAlive, "2", "example.com", dialer)
```

---

**Implementation Details (from pool.go):**

- **Connection ID Generation:**
  - The server generates an 8-byte ID and sends it to the client after TLS handshake.
  - Connection IDs are used for tracking and managing individual connections.

- **Put Method:**
  - Prevents duplicate connections in the pool.
  - If the pool is full or the connection is already present, the connection is closed automatically.

- **Flush/Close:**
  - `Flush` closes all connections and resets the pool.
  - `Close` cancels the context and flushes the pool.

- **Dynamic Adjustment:**
  - `adjustInterval` and `adjustCapacity` are used internally for pool optimization based on usage and success rate.

- **isActive:**
  - Checks if a connection is alive by sending an empty write with a short deadline.

- **Error Handling:**
  - `AddError` and `ErrorCount` are thread-safe and use mutex protection.

## Connection Keep-Alive

The pool implements TCP keep-alive functionality to maintain connection health and detect broken connections:

### Keep-Alive Features

- **Automatic Keep-Alive**: All connections automatically enable TCP keep-alive
- **Configurable Period**: Set custom keep-alive periods for both client and server pools
- **Connection Health**: Helps detect and remove dead connections from the pool
- **Network Efficiency**: Reduces unnecessary connection overhead

### Usage Examples

```go
// Client pool with 30-second keep-alive
clientPool := pool.NewClientPool(
    5, 20,
    500*time.Millisecond, 5*time.Second,
    30*time.Second,  // Keep-alive period
    "2",             // TLS mode
    "example.com",   // hostname
    dialer,
)

// Server pool with 60-second keep-alive
serverPool := pool.NewServerPool(
    "192.168.1.10", 
    tlsConfig, 
    listener, 
    60*time.Second,  // Keep-alive period
)
```

### Keep-Alive Best Practices

| Period Range | Use Case | Pros | Cons |
|-------------|----------|------|------|
| 15-30s | High-frequency apps, real-time systems | Quick dead connection detection | Higher network overhead |
| 30-60s | General purpose applications | Balanced performance/overhead | Standard detection time |
| 60-120s | Low-frequency, batch processing | Minimal network overhead | Slower dead connection detection |

**Recommendations:**
- **Web applications**: 30-60 seconds
- **Real-time systems**: 15-30 seconds  
- **Batch processing**: 60-120 seconds
- **Behind NAT/Firewall**: Use shorter periods (15-30s)

## Dynamic Adjustment

The pool automatically adjusts:

- Connection creation intervals based on idle connection count (using `adjustInterval` method)
  - Decreases interval when pool is under-utilized (< 20% idle connections)
  - Increases interval when pool is over-utilized (> 80% idle connections)
  
- Connection capacity based on connection creation success rate (using `adjustCapacity` method)
  - Decreases capacity when success rate is low (< 20%)
  - Increases capacity when success rate is high (> 80%)

These adjustments ensure optimal resource usage:

```go
// Check current capacity and interval settings
currentCapacity := clientPool.Capacity()
currentInterval := clientPool.Interval()
```

## Advanced Usage

### Custom Error Handling

```go
package main

import (
    "log"
    "net"
    "time"
    "github.com/NodePassProject/pool"
    "github.com/NodePassProject/logs"
)

func main() {    logger := logs.NewLogger(logs.Info, true)
      clientPool := pool.NewClientPool(
        5, 20,
        500*time.Millisecond, 5*time.Second,
        30*time.Second,
        "2",
        "example.com",
        func() (net.Conn, error) {
            conn, err := net.Dial("tcp", "example.com:8080")
            if err != nil {
                // Log the error
                logger.Error("Connection failed: %v", err)
                
                // Record the error in the pool
                clientPool.AddError()
            }
            return conn, err
        },
    )
    
    go clientPool.ClientManager()
    
    // Your application logic...
}
```

### Working with Context

```go
package main

import (
    "context"
    "net"
    "time"
    "github.com/NodePassProject/pool"
)

func main() {
    // Create a context that can be cancelled    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
      clientPool := pool.NewClientPool(
        5, 20,
        500*time.Millisecond, 5*time.Second,
        30*time.Second,
        "2",
        "example.com",
        func() (net.Conn, error) {
            // Use context-aware dialer
            dialer := net.Dialer{Timeout: 5 * time.Second}
            return dialer.DialContext(ctx, "tcp", "example.com:8080")
        },
    )
    
    go clientPool.ClientManager()
    
    // When needed to stop the pool:
    // cancel()
    // clientPool.Close()
}
```

### Load Balancing with Multiple Pools

```go
package main

import (
    "net"
    "sync/atomic"
    "time"
    "github.com/NodePassProject/pool"
)

func main() {
    // Create pools for different servers
    serverAddresses := []string{
        "server1.example.com:8080",
        "server2.example.com:8080",
        "server3.example.com:8080",
    }
    
    pools := make([]*pool.Pool, len(serverAddresses))
      for i, addr := range serverAddresses {
        serverAddr := addr // Create local copy for closure
        pools[i] = pool.NewClientPool(
            5, 20,
            500*time.Millisecond, 5*time.Second,
            30*time.Second,
            "2",
            serverAddr[:len(serverAddr)-5], // Extract hostname
            func() (net.Conn, error) {
                return net.Dial("tcp", serverAddr)
            },
        )
        go pools[i].ClientManager()
    }
    
    // Simple round-robin load balancer
    var counter int32 = 0
    getNextPool := func() *pool.Pool {
        next := atomic.AddInt32(&counter, 1) % int32(len(pools))
        return pools[next]
    }
    
    // Usage
    id, conn := getNextPool().ServerGet(30 * time.Second)
    
    // Use connection...
    
    // When done with all pools
    for _, p := range pools {
        p.Close()
    }
}
```

## Performance Considerations

### Connection Pool Sizing

| Pool Size | Pros | Cons | Best For |
|-----------|------|------|----------|
| Too Small (< 5) | Low resource usage | Connection contention, delays | Low-traffic applications |
| Optimal (5-50) | Balanced performance | Requires monitoring | Most applications |
| Too Large (> 100) | No contention | Resource waste, server overload | High-traffic, many clients |

**Sizing Guidelines:**
- Start with `minCap = baseline_load` and `maxCap = peak_load × 1.5`
- Monitor connection usage with `pool.Active()` and `pool.Capacity()`
- Adjust based on observed patterns

### TLS Performance Impact

| Aspect | No TLS | Self-signed TLS | Verified TLS |
|--------|--------|-----------------|--------------|
| **Handshake Time** | ~1ms | ~10-50ms | ~50-100ms |
| **Memory Usage** | Low | Medium | High |
| **CPU Overhead** | Minimal | Medium | High |
| **Throughput** | Maximum | ~80% of max | ~60% of max |

### Connection Validation Overhead

The `isActive` method performs lightweight connection health checks:
- **Cost**: ~1ms per validation
- **Frequency**: On connection retrieval
- **Trade-off**: Reliability vs. slight performance overhead

For ultra-high-throughput systems, consider implementing custom validation strategies.

## Troubleshooting

### Common Issues

#### 1. Connection Timeout
**Symptoms:** Connections fail to establish  
**Solutions:**
- Check network connectivity to target host
- Verify server address and port are correct
- Increase connection timeout in dialer:
  ```go
  dialer := func() (net.Conn, error) {
      d := net.Dialer{Timeout: 10 * time.Second}
      return d.Dial("tcp", "example.com:8080")
  }
  ```

#### 2. TLS Handshake Failure
**Symptoms:** TLS connections fail with certificate errors  
**Solutions:**
- Verify certificate validity and expiration
- Check hostname matches certificate Common Name
- For testing, temporarily use TLS mode `"1"`:
  ```go  // Temporary workaround for testing
  pool := pool.NewClientPool(5, 20, minIvl, maxIvl, keepAlive, "1", hostname, dialer)
  ```

#### 3. Pool Exhaustion
**Symptoms:** `ServerGet()` blocks indefinitely or times out  
**Solutions:**
- Increase maximum capacity
- Reduce connection hold time in application code
- Check for connection leaks (ensure connections are properly closed)
- Monitor with `pool.Active()` and `pool.ErrorCount()`
- Use appropriate timeout values with `ServerGet(timeout)`

#### 4. High Error Rate
**Symptoms:** Frequent connection failures  
**Solutions:**
- Implement exponential backoff in dialer
- Monitor server-side issues
- Track errors with `pool.AddError()` and `pool.ErrorCount()`

### Debugging Checklist

- [ ] **Network connectivity**: Can you ping/telnet to the target?
- [ ] **Port availability**: Is the target port open and listening?
- [ ] **Certificate validity**: For TLS, are certificates valid and not expired?
- [ ] **Pool capacity**: Is `maxCap` sufficient for your load?
- [ ] **Connection leaks**: Are you properly closing connections?
- [ ] **Error monitoring**: Are you tracking `pool.ErrorCount()`?

### Debug Logging

Add logging at key points for better debugging:

```go
dialer := func() (net.Conn, error) {
    log.Printf("Attempting connection to %s", address)
    conn, err := net.Dial("tcp", address)
    if err != nil {
        log.Printf("Connection failed: %v", err)
        pool.AddError() // Track the error
    } else {
        log.Printf("Connection established successfully")
    }
    return conn, err
}
```

## Best Practices

### 1. Pool Configuration

#### Capacity Sizing
```go
// For most applications, start with these guidelines:
minCap := expectedConcurrentConnections
maxCap := peakConcurrentConnections * 1.5

// Example for a web service handling 100 concurrent requests
clientPool := pool.NewClientPool(
    100, 150,                           // min/max capacity based on load
    500*time.Millisecond, 2*time.Second, // connection intervals
    30*time.Second,                     // keep-alive
    "2",                                // verified TLS for production
    "api.example.com",                  // hostname
    dialer,
)
```

#### Interval Configuration
```go
// Aggressive (high-frequency applications)
minInterval := 100 * time.Millisecond
maxInterval := 1 * time.Second

// Balanced (general purpose)
minInterval := 500 * time.Millisecond
maxInterval := 5 * time.Second

// Conservative (low-frequency, batch processing)
minInterval := 2 * time.Second
maxInterval := 10 * time.Second
```

### 2. Connection Management

#### Always Return Connections
```go
// GOOD: Always return connections
id, conn := serverPool.ServerGet(30 * time.Second)
if conn != nil {
    defer func() {
        if err := processData(conn); err != nil {
            conn.Close() // Close on error
        } else {
            serverPool.Put(id, conn) // Return to pool on success
        }
    }()
    // Use connection...
}

// BAD: Forgetting to return connections leads to pool exhaustion
id, conn := serverPool.ServerGet(30 * time.Second)
// Missing Put() or Close() - causes connection leak!
```

#### Handle Timeouts Gracefully
```go
// Use reasonable timeouts for ServerGet
timeout := 10 * time.Second
id, conn := serverPool.ServerGet(timeout)
if conn == nil {
    // Handle timeout case
    log.Printf("Failed to get connection within %v", timeout)
    return errors.New("connection pool timeout")
}
```

### 3. Error Handling and Monitoring

#### Implement Comprehensive Error Tracking
```go
type PoolManager struct {
    pool        *pool.Pool
    metrics     *metrics.Registry
    logger      *log.Logger
}

func (pm *PoolManager) getConnectionWithRetry(maxRetries int) (string, net.Conn, error) {
    for i := 0; i < maxRetries; i++ {
        id, conn := pm.pool.ServerGet(5 * time.Second)
        if conn != nil {
            return id, conn, nil
        }
        
        // Log and track the error
        pm.logger.Printf("Connection attempt %d failed", i+1)
        pm.pool.AddError()
        
        // Exponential backoff
        time.Sleep(time.Duration(math.Pow(2, float64(i))) * time.Second)
    }
    
    return "", nil, errors.New("max retries exceeded")
}

// Monitor pool health periodically
func (pm *PoolManager) healthCheck() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        active := pm.pool.Active()
        capacity := pm.pool.Capacity()
        errors := pm.pool.ErrorCount()
        
        pm.logger.Printf("Pool health: %d/%d active, %d errors", active, capacity, errors)
        
        // Reset error count periodically
        if errors > 100 {
            pm.pool.ResetError()
        }
        
        // Alert if pool utilization is consistently high
        if float64(active)/float64(capacity) > 0.9 {
            pm.logger.Printf("WARNING: Pool utilization high (%d/%d)", active, capacity)
        }
    }
}
```

### 4. Production Deployment

#### Security Configuration
```go
// Production setup with proper TLS
func createProductionPool() *pool.Pool {
    return pool.NewClientPool(
        20, 100,                         // Production-scale capacity
        500*time.Millisecond, 5*time.Second,
        30*time.Second,
        "2",                            // Always use verified TLS in production
        "secure-api.company.com",       // Proper hostname for certificate verification
        createSecureDialer(),
    )
}

func createSecureDialer() func() (net.Conn, error) {
    return func() (net.Conn, error) {
        dialer := &net.Dialer{
            Timeout:   10 * time.Second,
            KeepAlive: 30 * time.Second,
        }
        return dialer.Dial("tcp", "secure-api.company.com:443")
    }
}
```

#### Graceful Shutdown
```go
func (app *Application) Shutdown(ctx context.Context) error {
    // Stop accepting new requests first
    app.server.Shutdown(ctx)
    
    // Allow existing connections to complete
    select {
    case <-time.After(30 * time.Second):
        app.logger.Println("Forcing pool shutdown after timeout")
    case <-ctx.Done():
    }
    
    // Close all pool connections
    app.clientPool.Close()
    app.serverPool.Close()
    
    return nil
}
```

### 5. Performance Optimization

#### Avoid Common Anti-patterns
```go
// ANTI-PATTERN: Creating pools repeatedly
func badHandler(w http.ResponseWriter, r *http.Request) {
    // DON'T: Create a new pool for each request
    pool := pool.NewClientPool(5, 10, time.Second, time.Second, 30*time.Second, "2", "api.com", dialer)
    defer pool.Close()
}

// GOOD PATTERN: Reuse pools
type Server struct {
    apiPool *pool.Pool // Shared pool instance
}

func (s *Server) goodHandler(w http.ResponseWriter, r *http.Request) {
    // DO: Reuse existing pool
    id, conn := s.apiPool.ServerGet(10 * time.Second)
    if conn != nil {
        defer s.apiPool.Put(id, conn)
        // Use connection...
    }
}
```

#### Optimize for Your Use Case
```go
// High-throughput, low-latency services
highThroughputPool := pool.NewClientPool(
    50, 200,                           // Large pool for many concurrent connections
    100*time.Millisecond, 1*time.Second, // Fast connection creation
    15*time.Second,                    // Short keep-alive for quick failure detection
    "2", "fast-api.com", dialer,
)

// Batch processing, memory-constrained services  
batchPool := pool.NewClientPool(
    5, 20,                             // Smaller pool to conserve memory
    2*time.Second, 10*time.Second,     // Slower connection creation
    60*time.Second,                    // Longer keep-alive for stable connections
    "2", "batch-api.com", dialer,
)
```

### 6. Testing and Development

#### Development Configuration
```go
// Development/testing setup
func createDevPool() *pool.Pool {
    return pool.NewClientPool(
        2, 5,                           // Smaller pool for development
        time.Second, 3*time.Second,
        30*time.Second,
        "1",                           // Self-signed TLS acceptable for dev
        "localhost",                   // Local development hostname
        createLocalDialer(),
    )
}
```

#### Unit Testing with Pools
```go
func TestPoolIntegration(t *testing.T) {
    // Create a test server
    listener, err := net.Listen("tcp", "localhost:0")
    require.NoError(t, err)
    defer listener.Close()
    
    // Create server pool
    serverPool := pool.NewServerPool(5, "", nil, listener, 10*time.Second)
    go serverPool.ServerManager()
    defer serverPool.Close()
    
    // Create client pool  
    addr := listener.Addr().String()
    clientPool := pool.NewClientPool(
        2, 5, time.Second, 3*time.Second, 10*time.Second,
        "0", // No TLS for testing
        strings.Split(addr, ":")[0],
        func() (net.Conn, error) { return net.Dial("tcp", addr) },
    )
    go clientPool.ClientManager()
    defer clientPool.Close()
    
    // Test connection flow
    // ... test logic
}
```

These best practices will help you get the most out of the pool package while maintaining reliability and performance in production environments.

## License

Copyright (c) 2025, NodePassProject. Licensed under the BSD 3-Clause License.
See the [LICENSE](LICENSE) file for details.
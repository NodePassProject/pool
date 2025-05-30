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
  - [Managing Pool Health](#managing-pool-health)
- [Security Features](#security-features)
  - [Client IP Restriction](#client-ip-restriction)
  - [TLS Security Modes](#tls-security-modes)
- [Connection Modes](#connection-modes)
- [Connection Keep-Alive](#connection-keep-alive)
- [Dynamic Adjustment](#dynamic-adjustment)
- [Advanced Usage](#advanced-usage)
- [Performance Considerations](#performance-considerations)
- [Troubleshooting](#troubleshooting)
- [License](#license)

## Features

- **Thread-safe connection management** with mutex protection
- **Support for both client and server connection pools**
- **Dynamic capacity adjustment** based on usage patterns
- **Automatic connection health monitoring**
- **Connection keep-alive management** for maintaining active connections
- **Multiple TLS security modes** (none, self-signed, verified)
- **Connection identification and tracking**
- **Single and multi-connection modes** for different use cases
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
        false,                              // isSingle mode
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
    // - Single mode: false (multi-connection mode)
    // - Hostname for certificate verification: "example.com"
    clientPool := pool.NewClientPool(
        5, 20,
        500*time.Millisecond, 5*time.Second,
        30*time.Second,
        "2",
        false,
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
    id, conn := serverPool.ServerGet()
    
    // Use the connection...
    
    // When finished with the pool
    serverPool.Close()
}
```

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
```

## Security Features

### Client IP Restriction

The `NewServerPool` function allows you to restrict incoming connections to a specific client IP address:

```go
// Create a server pool that only accepts connections from 192.168.1.10
serverPool := pool.NewServerPool("192.168.1.10", tlsConfig, listener, 30*time.Second)
```

When the `clientIP` parameter is set:
- All connections from other IP addresses will be immediately closed
- This provides an additional layer of security beyond network firewalls
- Particularly useful for internal services or dedicated client-server applications

To allow connections from any IP address, use an empty string:

```go
// Create a server pool that accepts connections from any IP
serverPool := pool.NewServerPool("", tlsConfig, listener, 30*time.Second)
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
clientPool := pool.NewClientPool(5, 20, minIvl, maxIvl, keepAlive, "0", false, "example.com", dialer)

// Self-signed TLS - development/testing
clientPool := pool.NewClientPool(5, 20, minIvl, maxIvl, keepAlive, "1", false, "example.com", dialer)

// Verified TLS - production
clientPool := pool.NewClientPool(5, 20, minIvl, maxIvl, keepAlive, "2", false, "example.com", dialer)
```

## Connection Modes

The pool supports two connection modes through the `isSingle` parameter:

### Multi-Connection Mode (`isSingle = false`)

In this mode, the pool manages multiple connections with server-generated IDs:

```go
// Multi-connection mode - server generates connection IDs
clientPool := pool.NewClientPool(
    5, 20,
    500*time.Millisecond, 5*time.Second,
    30*time.Second,
    "2",
    false,  // Multi-connection mode
    "example.com",
    dialer,
)

// Get connection by server-provided ID
conn := clientPool.ClientGet("server-provided-id")
```

**Features:**
- Server generates unique 8-byte connection IDs
- Client reads ID from connection after TLS handshake
- Ideal for load balancing and connection tracking
- Better for complex distributed systems

### Single-Connection Mode (`isSingle = true`)

In this mode, the pool generates its own IDs and manages connections independently:

```go
// Single-connection mode - client generates connection IDs
clientPool := pool.NewClientPool(
    5, 20,
    500*time.Millisecond, 5*time.Second,
    30*time.Second,
    "0",
    true,   // Single-connection mode
    "example.com",
    dialer,
)

// Get any available connection (no specific ID needed)
conn := clientPool.ClientGet("")
```

**Features:**
- Client generates its own connection IDs
- No dependency on server-side ID generation
- Simpler connection management
- Better for simple client-server applications

### Mode Comparison

| Aspect | Multi-Connection (`false`) | Single-Connection (`true`) |
|--------|---------------------------|---------------------------|
| **ID Generation** | Server-side | Client-side |
| **Connection Tracking** | Server-controlled | Client-controlled |
| **Complexity** | Higher | Lower |
| **Use Case** | Distributed systems | Simple applications |
| **Load Balancing** | Advanced | Basic |

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
    false,           // isSingle mode
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
        false,
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
        false,
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
        serverAddr := addr // Create local copy for closure        pools[i] = pool.NewClientPool(
            5, 20,
            500*time.Millisecond, 5*time.Second,
            30*time.Second,
            "2",
            false,
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
    id, conn := getNextPool().ServerGet()
    
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
  pool := pool.NewClientPool(5, 20, minIvl, maxIvl, keepAlive, "1", false, hostname, dialer)
  ```

#### 3. Pool Exhaustion
**Symptoms:** `ServerGet()` blocks indefinitely  
**Solutions:**
- Increase maximum capacity
- Reduce connection hold time in application code
- Check for connection leaks (ensure connections are properly closed)
- Monitor with `pool.Active()` and `pool.ErrorCount()`

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

## License

Copyright (c) 2025, NodePassProject. Licensed under the BSD 3-Clause License.
See the [LICENSE](LICENSE) file for details.
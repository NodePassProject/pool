# Pool Package

[![Go Reference](https://pkg.go.dev/badge/github.com/NodePassProject/pool.svg)](https://pkg.go.dev/github.com/NodePassProject/pool)
[![License](https://img.shields.io/badge/License-BSD_3--Clause-blue.svg)](https://opensource.org/licenses/BSD-3-Clause)

A high-performance, reliable network connection pool management system for Go applications.

## Features

- Thread-safe connection management with mutex protection
- Support for both client and server connection pools
- Dynamic capacity adjustment based on usage patterns
- Automatic connection health monitoring
- Multiple TLS security modes (none, self-signed, verified)
- Connection identification and tracking
- Graceful error handling and recovery
- Configurable connection creation intervals
- Auto-reconnection with exponential backoff
- Connection activity validation

## Installation

```bash
go get github.com/NodePassProject/pool
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

func main() {
    // Create a dialer function
    dialer := func() (net.Conn, error) {
        return net.Dial("tcp", "example.com:8080")
    }
    
    // Create a new client pool with:
    // - Minimum capacity: 5 connections
    // - Maximum capacity: 20 connections
    // - Minimum interval: 500ms between connection attempts
    // - Maximum interval: 5s between connection attempts
    // - TLS mode: "2" (verified certificates)
    // - Hostname for certificate verification: "example.com"
    clientPool := pool.NewClientPool(
        5, 20,
        500*time.Millisecond, 5*time.Second,
        "2", "example.com",
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
        MinVersion: tls.VersionTLS13,
    }
    
    // Create a new server pool
    // - Restrict to specific client IP (optional, "" for any IP, "192.168.1.10" to only allow that specific IP)
    // - Use TLS config (optional, nil for no TLS)
    // - Use the created listener
    serverPool := pool.NewServerPool("192.168.1.10", tlsConfig, listener)
    
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
```

## Security Features

### Client IP Restriction

The `NewServerPool` function allows you to restrict incoming connections to a specific client IP address:

```go
// Create a server pool that only accepts connections from 192.168.1.10
serverPool := pool.NewServerPool("192.168.1.10", tlsConfig, listener)
```

When the `clientIP` parameter is set:
- All connections from other IP addresses will be immediately closed
- This provides an additional layer of security beyond network firewalls
- Particularly useful for internal services or dedicated client-server applications

To allow connections from any IP address, use an empty string:

```go
// Create a server pool that accepts connections from any IP
serverPool := pool.NewServerPool("", tlsConfig, listener)
```

### TLS Security Modes

The pool supports three TLS security modes for client connections:

- `"0"`: No TLS (plain TCP connections)
- `"1"`: Self-signed certificates (InsecureSkipVerify=true)
- `"2"`: Verified certificates (proper certificate validation)

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
)

func main() {
    logger := log.New(log.Writer(), "POOL: ", log.LstdFlags)
    
    clientPool := pool.NewClientPool(
        5, 20,
        500*time.Millisecond, 5*time.Second,
        "2", "example.com",
        func() (net.Conn, error) {
            conn, err := net.Dial("tcp", "example.com:8080")
            if err != nil {
                // Log the error
                logger.Printf("Connection failed: %v", err)
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
    // Create a context that can be cancelled
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    clientPool := pool.NewClientPool(
        5, 20,
        500*time.Millisecond, 5*time.Second,
        "2", "example.com",
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

Note: The pool internally creates and manages its own context during operation. The context 
passed to dialers is useful for external control of connection attempts.

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
            "2", serverAddr[:len(serverAddr)-5], // Extract hostname
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

The ideal pool size depends on your application's workload:

- **Too small**: Can cause connection contention and delays
- **Too large**: Wastes resources and may overload the server

Start with a minimum capacity that can handle your baseline load and a maximum capacity that can handle peak loads. Monitor connection usage and adjust accordingly.

### TLS Overhead

TLS connections have additional overhead:

- **Handshake cost**: TLS handshakes are CPU-intensive
- **Memory usage**: TLS connections require more memory
- **Latency**: Initial connection setup is slower

If maximum performance is critical and the network is secure, consider using mode `"0"` (no TLS).

### Connection Validation

The `isActive` method checks connection health by setting a brief read deadline and attempting to read. This ensures connections in the pool are valid, but adds a small overhead. For extremely high-throughput systems, consider implementing a custom validation strategy.

## Troubleshooting

### Common Issues

1. **Connection Timeout**
   - Check network connectivity
   - Verify server address and port
   - Increase connection timeout in dialer

2. **TLS Handshake Failure**
   - Verify certificate validity
   - Check hostname configuration matches certificate
   - For testing, try TLS mode `"1"` (InsecureSkipVerify)

3. **Pool Exhaustion**
   - Increase max capacity
   - Decrease connection hold time
   - Check for connection leaks (connections not being released)
   - Monitor dynamic capacity adjustment with `Capacity()` method

4. **High Error Rate**
   - Implement backoff strategy in dialer
   - Consider server-side issues
   - Check connection validation with `isActive` method

### Debugging

Set log points at key locations:

- Before/after dialer calls
- When connections are added/removed from the pool
- When pool capacity is adjusted (`adjustCapacity` method) 
- When connection intervals are adjusted (`adjustInterval` method)
- When connections are validated with `isActive` method

## Context Management

The pool package uses Go's context package for proper connection lifecycle management. 
Both `ClientManager` and `ServerManager` create a context when started, and this context 
is cancelled when the pool is closed.

## License

Copyright (c) 2025, NodePassProject. Licensed under the BSD 3-Clause License.
See the [LICENSE](./LICENSE) file for details.
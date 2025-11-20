# HTTP Keep-Alive Connection Management Guide

## Table of Contents
1. [What is Keep-Alive](#what-is-keep-alive)
2. [Keep-Alive Headers Explained](#keep-alive-headers-explained)
3. [How Keep-Alive Works](#how-keep-alive-works)
4. [Connection Routing and Goroutines](#connection-routing-and-goroutines)
5. [Timeout and Connection Termination](#timeout-and-connection-termination)
6. [Performance Benefits](#performance-benefits)
7. [Implementation Details](#implementation-details)

---

## What is Keep-Alive

HTTP Keep-Alive is a feature that allows a single TCP connection to be reused for multiple HTTP requests/responses, rather than opening a new connection for each request.

### Traditional Approach (Without Keep-Alive)

```
Browser                                Server
   │                                      │
   │──── TCP Handshake (SYN/ACK) ────────│  100ms
   │──── Request #1: GET /page.html ─────│
   │────── Response: <html>... ──────────│
   │──── TCP Close (FIN/ACK) ────────────│  50ms
   │                                      │
   Total time: ~150ms overhead per request
   
   │──── TCP Handshake (SYN/ACK) ────────│  100ms (again!)
   │──── Request #2: GET /style.css ─────│
   │────── Response: .body {...} ────────│
   │──── TCP Close (FIN/ACK) ────────────│  50ms
   │                                      │
   Total overhead for 3 requests: 450ms ❌
```

---

### Keep-Alive Approach (Connection Reuse)

```
Browser                                Server
   │                                      │
   │──── TCP Handshake (SYN/ACK) ────────│  100ms (only once!)
   │──── Request #1: GET /page.html ─────│
   │────── Response: <html>... ──────────│
   │ (connection stays open)              │
   │──── Request #2: GET /style.css ─────│  0ms overhead! ✅
   │────── Response: .body {...} ────────│
   │ (connection stays open)              │
   │──── Request #3: GET /script.js ─────│  0ms overhead! ✅
   │────── Response: function() {...} ───│
   │                                      │
   Total overhead: 150ms (vs 450ms)
   Savings: 300ms (67% faster!)
```

---

## Keep-Alive Headers Explained

### Connection: keep-alive

```http
HTTP/1.1 200 OK
Connection: keep-alive
Keep-Alive: timeout=30, max=100
Content-Type: text/html

<html>...</html>
```

**Meaning:**
- `Connection: keep-alive` - "Don't close TCP connection after this response"
- Browser interprets this as: "I can reuse this connection for next request"

---

### Keep-Alive Parameters

```
Keep-Alive: timeout=30, max=100
```

#### `timeout=30` (seconds)
```
Purpose: Close idle connections after 30 seconds

Timeline:
t=0s:   Request arrives
t=0.1s: Response sent
t=0.1s - t=30.1s: Waiting for next request...
t=30.1s: No request → Timeout → Close connection

Why?
├─ Free up server resources
├─ File descriptor limit (usually ~1024)
├─ Memory for each connection (~32KB)
└─ Can't keep idle connections forever
```

#### `max=100` (requests)
```
Purpose: Prevent connection from being held too long

Example:
Request 1:  GET /page1.html
Request 2:  GET /style.css
Request 3:  GET /script.js
...
Request 99:  GET /image99.jpg
Request 100: GET /image100.jpg
Response:    200 OK
             Connection: close  ← Force close after 100 requests

Why?
├─ Prevent resource leaks
├─ Force connection refresh
├─ Balance load across connections
└─ HTTP/1.1 spec recommendation
```

---

## How Keep-Alive Works

### Without Keep-Alive (HTTP/1.0 Default)

```
User loads page with 5 resources:
1. index.html
2. style.css
3. script.js
4. logo.png
5. data.json

WITHOUT Keep-Alive:
┌────────────────────────────────────────┐
│ Connection 1 (new)                     │
│ ├─ TCP Handshake (100ms)              │
│ ├─ GET /index.html                     │
│ ├─ Response: <html>...                 │
│ └─ Connection closed                   │
├────────────────────────────────────────┤
│ Connection 2 (new)                     │
│ ├─ TCP Handshake (100ms)              │
│ ├─ GET /style.css                      │
│ ├─ Response: .body {...}               │
│ └─ Connection closed                   │
├────────────────────────────────────────┤
│ Connection 3 (new)                     │
│ ├─ TCP Handshake (100ms)              │
│ ├─ GET /script.js                      │
│ ├─ Response: function() {...}          │
│ └─ Connection closed                   │
├────────────────────────────────────────┤
│ Connection 4 (new)                     │
│ ├─ TCP Handshake (100ms)              │
│ ├─ GET /logo.png                       │
│ ├─ Response: [PNG data]                │
│ └─ Connection closed                   │
├────────────────────────────────────────┤
│ Connection 5 (new)                     │
│ ├─ TCP Handshake (100ms)              │
│ ├─ GET /data.json                      │
│ ├─ Response: {"data": ...}             │
│ └─ Connection closed                   │
└────────────────────────────────────────┘

Total time: 500ms (handshakes) + 250ms (requests) = 750ms
```

---

### With Keep-Alive (HTTP/1.1 Default)

```
WITH Keep-Alive:
┌────────────────────────────────────────┐
│ Connection 1 (reused)                  │
│ ├─ TCP Handshake (100ms, once)        │
│ ├─ GET /index.html                     │
│ ├─ Response: <html>...                 │
│ ├─ (connection stays open)             │
│ ├─ GET /style.css                      │
│ ├─ Response: .body {...}               │
│ ├─ (connection stays open)             │
│ ├─ GET /script.js                      │
│ ├─ Response: function() {...}          │
│ ├─ (connection stays open)             │
│ ├─ GET /logo.png                       │
│ ├─ Response: [PNG data]                │
│ ├─ (connection stays open)             │
│ ├─ GET /data.json                      │
│ ├─ Response: {"data": ...}             │
│ └─ Connection closed                   │
└────────────────────────────────────────┘

Total time: 100ms (handshake) + 250ms (requests) = 350ms

Speed improvement: 53% faster! ✅
```

---

## Connection Routing and Goroutines

### Key Architecture: One Goroutine Per Connection

```go
func (s *Server) Start() error {
    listener, err := tcp.Listen("tcp", s.addr)
    
    for {
        conn, err := listener.Accept()  // Accept new TCP connection
        
        // ONE goroutine handles ALL requests on this connection
        go s.handleConnection(tcpConn)
        //  ↑
        //  This goroutine stays alive for the entire connection lifetime
    }
}
```

---

### How Multiple Clients Are Handled

```
3 Browsers connect simultaneously:

Browser A (fd=5) ────┐
                     ├──> Server
Browser B (fd=6) ────┤
                     │
Browser C (fd=7) ────┘

Server creates 3 goroutines:

Goroutine 1: handleConnection(fd=5)
├─ for { wait for request from fd=5 }
├─ Handle Request 1 from Browser A
├─ Handle Request 2 from Browser A
└─ Handle Request 3 from Browser A

Goroutine 2: handleConnection(fd=6)
├─ for { wait for request from fd=6 }
├─ Handle Request 1 from Browser B
└─ Handle Request 2 from Browser B

Goroutine 3: handleConnection(fd=7)
├─ for { wait for request from fd=7 }
├─ Handle Request 1 from Browser C
├─ Handle Request 2 from Browser C
└─ Handle Request 3 from Browser C

All run concurrently! Each goroutine owns its connection.
```

---

### Visual: Connection Lifecycle

```
Browser A connects:
    ↓
TCP connection established (fd=5)
    ↓
go s.handleConnection(conn_fd5) ← Goroutine 1 created
    ↓
Goroutine 1 handles:
├─ Request 1: GET /index.html
├─ Request 2: GET /style.css
├─ Request 3: GET /script.js
├─ Request 4: GET /logo.png
└─ ... (keeps handling until timeout or close)
    ↓
30 seconds idle → Goroutine 1 exits


Browser B connects:
    ↓
TCP connection established (fd=6)
    ↓
go s.handleConnection(conn_fd6) ← Goroutine 2 created
    ↓
Goroutine 2 handles:
├─ Request 1: GET /about.html
├─ Request 2: GET /about.css
└─ ... (separate from Goroutine 1)
    ↓
Connection closed → Goroutine 2 exits
```

---

### How Requests Are Routed Without Handshake

**Key Insight:** TCP socket file descriptor (fd) uniquely identifies the connection!

```
TCP socket file descriptor uniquely identifies the connection:

Browser A's connection: fd=5
├─ All requests from Browser A arrive on fd=5
├─ Goroutine 1 reads from fd=5
└─ Only Goroutine 1 can read from fd=5

Browser B's connection: fd=6
├─ All requests from Browser B arrive on fd=6
├─ Goroutine 2 reads from fd=6
└─ Only Goroutine 2 can read from fd=6

Operating system routes data:
├─ Data from Browser A → fd=5 buffer
├─ Data from Browser B → fd=6 buffer
└─ Each goroutine reads from its own fd

No confusion! OS handles routing by fd.
```

---

### OS-Level Connection Management

```
Kernel maintains per-connection buffers:

┌─────────────────────────────────────┐
│         Kernel Space                │
│                                     │
│  fd=5 buffer: [GET /page.html...]  │ ← Browser A's data
│  fd=6 buffer: [GET /about.html...] │ ← Browser B's data
│  fd=7 buffer: [GET /api/data...]   │ ← Browser C's data
│                                     │
└─────────────────────────────────────┘
           ↓         ↓         ↓
┌─────────────────────────────────────┐
│        User Space (Your Server)     │
│                                     │
│  Goroutine 1: read(fd=5)           │
│  Goroutine 2: read(fd=6)           │
│  Goroutine 3: read(fd=7)           │
│                                     │
└─────────────────────────────────────┘

Each goroutine blocks on read(fd):
- read(fd=5) only gets Browser A's data
- read(fd=6) only gets Browser B's data
- read(fd=7) only gets Browser C's data

OS guarantees data goes to correct fd!
```

---

## Timeout and Connection Termination

### The handleConnection Function

```go
func (s *Server) handleConnection(conn *tcp.TCPConn) {
    defer conn.Close()  // Ensures connection closes when function returns

    // Set initial timeout: wait 30 seconds for first request
    conn.SetReadDeadline(time.Now().Add(30 * time.Second))

    // INFINITE LOOP: keeps reading requests on SAME connection
    for {
        // Read next request from THIS connection
        request, err := protocol.ParseRequest(conn)
        //                                     ↑
        // This blocks until:
        // 1. Browser sends next request, OR
        // 2. 30 seconds pass (timeout), OR
        // 3. Browser closes connection
        
        if err != nil {
            return  // Exit goroutine (connection closed or timeout)
        }
        
        // Determine keep-alive
        keepAlive := /* ... check headers ... */
        
        // Handle request
        if s.handler.NeedsStreaming(request) {
            err = s.handler.HandleStream(request, conn)
        } else {
            response := s.handler.Handle(request)
            
            if keepAlive {
                response.Headers["Connection"] = "keep-alive"
                response.Headers["Keep-Alive"] = "timeout=30, max=100"
            } else {
                response.Headers["Connection"] = "close"
            }
            
            err = protocol.WriteResponse(conn, response)
        }
        
        if err != nil {
            return  // Exit goroutine
        }
        
        // Should we close?
        if !keepAlive {
            return  // Exit goroutine (closes connection)
        }
        
        // Reset timeout for NEXT request on SAME connection
        conn.SetReadDeadline(time.Now().Add(30 * time.Second))
        //                                  ↑
        // "I'll wait another 30 seconds for your next request"
    }
    
    // Loop continues → waits for next request on SAME connection
    // Same goroutine handles ALL requests from this client
}
```

---

### Complete Timeout Timeline

```
t=0s: Browser connects
      ├─ TCP handshake
      ├─ Connection established (fd=5)
      └─ go handleConnection(fd5) created

t=0.1s: First request arrives
      ├─ for { request = ParseRequest(fd5) }  ← Blocks, then receives
      ├─ Request: GET /index.html
      ├─ Response: 200 OK + HTML
      ├─ Headers: Connection: keep-alive
      └─ SetReadDeadline(now + 30s) → Deadline = t=30.1s

t=0.5s: Second request arrives (SAME connection)
      ├─ for { request = ParseRequest(fd5) }  ← Blocks, then receives
      ├─ Request: GET /style.css
      ├─ Response: 200 OK + CSS
      ├─ Headers: Connection: keep-alive
      └─ SetReadDeadline(now + 30s) → Deadline = t=30.5s

t=1.0s: Third request arrives (SAME connection)
      ├─ for { request = ParseRequest(fd5) }  ← Blocks, then receives
      ├─ Request: GET /script.js
      ├─ Response: 200 OK + JS
      ├─ Headers: Connection: keep-alive
      └─ SetReadDeadline(now + 30s) → Deadline = t=31.0s

t=1.5s - t=31.5s: Idle (no requests)
      ├─ for { request = ParseRequest(fd5) }  ← Blocks, waiting
      └─ Timeout deadline: t=31.5s

t=31.5s: Read timeout (30 seconds idle)
      ├─ ParseRequest returns error (timeout)
      ├─ if err != nil { return }
      ├─ defer conn.Close() executes
      └─ Goroutine exits

Connection closed ✅
```

---

### How ParseRequest Detects Timeout

```go
func ParseRequest(conn *tcp.TCPConn) (*Request, error) {
    // Read from connection with timeout
    buffer := make([]byte, 4096)
    
    // This blocks until:
    // 1. Data arrives, OR
    // 2. Deadline reached (timeout)
    n, err := conn.Read(buffer)
    //            ↑
    // conn.Read() respects SetReadDeadline()
    
    if err != nil {
        // If timeout occurred:
        // err = "i/o timeout" (from OS)
        return nil, err
        //         ↑
        // This error propagates back to handleConnection
    }
    
    // Parse request from buffer
    // ...
    
    return request, nil
}
```

---

### Error Propagation on Timeout

```
┌─────────────────────────────────────────────────────┐
│              Operating System                       │
│                                                     │
│  fd=5 buffer: [empty]                              │
│  Deadline: t=30s                                   │
│  Current time: t=30s                               │
│  → TIMEOUT! Return ETIMEDOUT                       │
└─────────────────────────────────────────────────────┘
                    ↓ error
┌─────────────────────────────────────────────────────┐
│         syscall.Read(fd=5, buffer)                  │
│                                                     │
│  Returns: (0, ETIMEDOUT)                           │
└─────────────────────────────────────────────────────┘
                    ↓ error
┌─────────────────────────────────────────────────────┐
│         TCPConn.Read(buffer)                        │
│                                                     │
│  Returns: (0, "i/o timeout")                       │
└─────────────────────────────────────────────────────┘
                    ↓ error
┌─────────────────────────────────────────────────────┐
│         ParseRequest(conn)                          │
│                                                     │
│  Returns: (nil, "i/o timeout")                     │
└─────────────────────────────────────────────────────┘
                    ↓ error
┌─────────────────────────────────────────────────────┐
│         handleConnection(conn)                      │
│                                                     │
│  if err != nil {                                   │
│      return  // Exit function                      │
│  }                                                 │
│                                                     │
│  defer conn.Close() ← Executes here                │
└─────────────────────────────────────────────────────┘
                    ↓
        Connection closed ✅
        Goroutine exits ✅
```

---

### Other Ways Connection Can Terminate

#### 1. Client Closes Connection

```go
for {
    request, err := protocol.ParseRequest(conn)
    if err != nil {
        // err = "connection reset by peer" or "EOF"
        return  // Exit, close connection
    }
    // ...
}
```

**When:**
- User closes browser tab
- Browser navigates away
- Network disconnected

---

#### 2. Client Sends `Connection: close`

```go
for {
    request, err := protocol.ParseRequest(conn)
    // ... handle request ...
    
    keepAlive := false
    if connHeader, ok := request.Headers["Connection"]; ok {
        keepAlive = strings.ToLower(connHeader) != "close"
    }
    
    if !keepAlive {
        return  // Client requested close, exit
    }
}
```

**When:**
- HTTP/1.0 client (no keep-alive)
- Client explicitly sends `Connection: close`

---

#### 3. Max Requests Reached (100)

```go
// Implementation example:
func (s *Server) handleConnection(conn *tcp.TCPConn) {
    defer conn.Close()
    
    maxRequests := 100
    requestCount := 0
    
    for {
        request, err := protocol.ParseRequest(conn)
        if err != nil {
            return
        }
        
        requestCount++
        
        // ... handle request ...
        
        if requestCount >= maxRequests {
            // Send final response with Connection: close
            response.Headers["Connection"] = "close"
            protocol.WriteResponse(conn, response)
            return  // Exit after 100 requests
        }
    }
}
```

---

#### 4. Server Error

```go
for {
    request, err := protocol.ParseRequest(conn)
    if err != nil {
        return  // Parse error, exit
    }
    
    response := s.handler.Handle(request)
    err = protocol.WriteResponse(conn, response)
    if err != nil {
        return  // Write error, exit
    }
}
```

**When:**
- Network error during write
- Malformed request
- Handler panic/error

---

## Performance Benefits

### 1. Latency Reduction

```
Without Keep-Alive (3 requests):
├─ TCP Handshake 1: 100ms
├─ Request/Response 1: 50ms
├─ TCP Close 1: 50ms
├─ TCP Handshake 2: 100ms
├─ Request/Response 2: 50ms
├─ TCP Close 2: 50ms
├─ TCP Handshake 3: 100ms
├─ Request/Response 3: 50ms
└─ TCP Close 3: 50ms
Total: 600ms

With Keep-Alive (3 requests):
├─ TCP Handshake: 100ms (once)
├─ Request/Response 1: 50ms
├─ Request/Response 2: 50ms
├─ Request/Response 3: 50ms
└─ TCP Close: 50ms (once)
Total: 300ms

Improvement: 50% faster! ✅
```

---

### 2. Reduced Server Load

```
Without Keep-Alive:
├─ 1000 requests = 1000 TCP handshakes
├─ 1000 socket creations
├─ 1000 goroutine creations
└─ 1000 socket destructions

With Keep-Alive:
├─ 1000 requests = 100 TCP handshakes (10 req/conn)
├─ 100 socket creations
├─ 100 goroutine creations
└─ 100 socket destructions

CPU saved: 90% less work! ✅
```

---

### 3. Better Resource Usage

```
File Descriptor Usage:

Without Keep-Alive:
├─ Peak connections: 100
├─ File descriptors: 100
└─ Each request needs new fd briefly

With Keep-Alive:
├─ Peak connections: 10 (reused)
├─ File descriptors: 10
└─ Each fd handles multiple requests

FD pressure: 90% reduction! ✅
```

---

### 4. Bandwidth Savings

```
Each TCP connection overhead:
├─ SYN: 60 bytes
├─ SYN-ACK: 60 bytes
├─ ACK: 52 bytes
├─ FIN: 52 bytes
├─ FIN-ACK: 52 bytes
└─ Total: 276 bytes per connection

1000 requests without Keep-Alive:
├─ 1000 connections × 276 bytes = 276KB overhead

1000 requests with Keep-Alive (100 connections):
├─ 100 connections × 276 bytes = 27.6KB overhead

Bandwidth saved: 90% less overhead! ✅
```

---

## Implementation Details

### Server Implementation

```go
package server

import (
    "strings"
    "time"
    "webserver/internal/protocol"
    "webserver/internal/tcp"
)

func (s *Server) handleConnection(conn *tcp.TCPConn) {
    defer conn.Close()

    // Set initial read deadline (30 seconds)
    conn.SetReadDeadline(time.Now().Add(30 * time.Second))

    for {
        // Parse incoming request
        request, err := protocol.ParseRequest(conn)
        if err != nil {
            // Timeout, connection closed, or parse error
            return
        }

        // Determine if connection should be kept alive
        keepAlive := false
        if s.config.Version == protocol.HTTP10 {
            // HTTP/1.0 does not support keep-alive by default
            keepAlive = false
        } else {
            // HTTP/1.1 defaults to keep-alive
            if connHeader, ok := request.Headers["Connection"]; ok {
                keepAlive = strings.ToLower(connHeader) != "close"
            } else {
                keepAlive = true
            }
        }

        // Handle request and generate response
        response := s.handler.Handle(request)
        response.Version = s.config.Version

        // Set response Connection header
        if keepAlive {
            response.Headers["Connection"] = "keep-alive"
            response.Headers["Keep-Alive"] = "timeout=30, max=100"
        } else {
            response.Headers["Connection"] = "close"
        }

        // Write response
        err = protocol.WriteResponse(conn, response)
        if err != nil {
            return
        }

        // Close connection if not keep-alive
        if !keepAlive {
            return
        }

        // Reset read deadline for next request
        conn.SetReadDeadline(time.Now().Add(30 * time.Second))
    }
}
```

---

### HTTP/1.0 vs HTTP/1.1 Behavior

```go
// HTTP/1.0: No keep-alive by default
if s.config.Version == protocol.HTTP10 {
    keepAlive = false
    response.Headers["Connection"] = "close"
}

// HTTP/1.1: Keep-alive by default
if s.config.Version == protocol.HTTP11 {
    keepAlive = true
    response.Headers["Connection"] = "keep-alive"
    response.Headers["Keep-Alive"] = "timeout=30, max=100"
}
```

---

### Client Request Control

Client can override keep-alive behavior:

```http
GET /page.html HTTP/1.1
Host: localhost:8080
Connection: close  ← Client requests connection close

Response:
HTTP/1.1 200 OK
Connection: close  ← Server honors client's request
```

---

## Summary

### Key Points

| Aspect | Without Keep-Alive | With Keep-Alive |
|--------|-------------------|-----------------|
| **TCP Handshakes** | 1 per request | 1 per connection |
| **Connection Overhead** | 200-300ms per request | 0ms (after first) |
| **File Descriptors** | 1 per request | 1 per connection (reused) |
| **Goroutines** | 1 per request | 1 per connection (reused) |
| **Memory Usage** | High (constant churn) | Low (stable) |
| **Page Load Time** | Slow (many handshakes) | Fast (one handshake) |
| **Server Load** | High CPU for handshakes | Low CPU usage |
| **Bandwidth** | High overhead | Low overhead |

---

### Connection Management

```
1 Client = 1 TCP Connection = 1 File Descriptor = 1 Goroutine

All requests from that client:
└─ Same goroutine
   └─ Same for loop  
      └─ Same ParseRequest(conn) call
         └─ Reads from same fd
            └─ OS ensures data routing by fd

Timeout handling:
└─ SetReadDeadline() tells OS
   └─ OS enforces deadline
      └─ Read() returns error on timeout
         └─ if err != nil { return }
            └─ defer conn.Close()
               └─ Clean shutdown ✅
```

---

### Best Practices

1. **Always set read deadlines** - Prevent goroutine leaks from idle connections
2. **Honor client's Connection header** - Respect client's keep-alive preferences
3. **Implement max request limits** - Prevent single connection from monopolizing resources
4. **Handle errors gracefully** - Clean up connections on any error
5. **Use defer for cleanup** - Ensure connections always close
6. **Log connection events** - Monitor connection lifecycle for debugging

---

### Configuration Recommendations

```go
// Production settings
timeout := 30 * time.Second  // 30s idle timeout
maxRequests := 100           // 100 requests per connection

// High-traffic settings
timeout := 15 * time.Second  // Shorter timeout
maxRequests := 50            // Fewer requests per connection

// Low-traffic settings
timeout := 60 * time.Second  // Longer timeout
maxRequests := 200           // More requests per connection
```

---

## Conclusion

HTTP Keep-Alive is a crucial feature for modern web servers that:
- Reduces latency by 50-70% for multi-resource pages
- Decreases server load by 90% (fewer handshakes)
- Improves resource efficiency (CPU, memory, file descriptors)
- Provides better user experience with faster page loads

The implementation using one goroutine per connection is elegant, efficient, and scalable, allowing thousands of concurrent connections with minimal overhead.

# TCP Socket Timeout Implementation Guide

## Overview

This document explains how socket-level timeouts work in our custom TCP implementation, specifically focusing on read and write deadlines.

## Table of Contents

1. [What Are Socket Timeouts?](#what-are-socket-timeouts)
2. [Implementation Details](#implementation-details)
3. [How It Works](#how-it-works)
4. [Real-World Examples](#real-world-examples)
5. [Why OS-Level Timeouts Are Better](#why-os-level-timeouts-are-better)
6. [Common Pitfalls](#common-pitfalls)

---

## What Are Socket Timeouts?

Socket timeouts are **operating system-level** mechanisms that automatically terminate blocking I/O operations (like `read()` or `write()`) if they take too long.

### Key Concepts

- **Deadline**: Absolute time when operation should timeout
- **Timeout Duration**: Relative time (e.g., 30 seconds from now)
- **Socket Options**: OS kernel settings that control socket behavior
  - `SO_RCVTIMEO`: Receive (read) timeout
  - `SO_SNDTIMEO`: Send (write) timeout

---

## Implementation Details

### File: `internal/tcp/conn.go`

```go
type TCPConn struct {
    fd            int         // Socket file descriptor
    laddr         *TCPAddr    // Local address
    raddr         *TCPAddr    // Remote address
    readDeadline  time.Time   // When read should timeout
    writeDeadline time.Time   // When write should timeout
}
```

### Setting Read Deadline

```go
func (c *TCPConn) SetReadDeadline(t time.Time) error {
    c.readDeadline = t

    var timeout time.Duration
    if t.IsZero() {
        // No timeout (infinite wait)
        timeout = 0
    } else {
        timeout = time.Until(t)
        if timeout < 0 {
            timeout = 0 // Already expired
        }
    }

    // Convert to OS timeval structure
    tv := syscall.NsecToTimeval(timeout.Nanoseconds())
    
    // Tell OS kernel to enforce this timeout
    return syscall.SetsockoptTimeval(c.fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)
}
```

### What This Does

1. **Stores the deadline** in `c.readDeadline` for reference
2. **Calculates timeout duration** from current time to deadline
3. **Converts to timeval** structure (seconds + microseconds)
4. **Sets socket option** via syscall to OS kernel

### Setting Write Deadline

```go
func (c *TCPConn) SetWriteDeadline(t time.Time) error {
    c.writeDeadline = t

    var timeout time.Duration
    if t.IsZero() {
        timeout = 0
    } else {
        timeout = time.Until(t)
        if timeout < 0 {
            timeout = 0
        }
    }

    tv := syscall.NsecToTimeval(timeout.Nanoseconds())
    return syscall.SetsockoptTimeval(c.fd, syscall.SOL_SOCKET, syscall.SO_SNDTIMEO, &tv)
}
```

### Setting Both Deadlines

```go
func (c *TCPConn) SetDeadline(t time.Time) error {
    if err := c.SetReadDeadline(t); err != nil {
        return err
    }
    return c.SetWriteDeadline(t)
}
```

---

## How It Works

### Step-by-Step Flow

#### 1. Server Sets Deadline

```go
// Server wants to timeout after 30 seconds
conn.SetReadDeadline(time.Now().Add(30 * time.Second))
```

**What happens:**
```
Application                    OS Kernel
    |                              |
    |--SetReadDeadline(now+30s)--->|
    |                              |
    |                              | Stores: SO_RCVTIMEO = 30s
    |<------ Success --------------|
```

#### 2. Application Calls Read()

```go
buf := make([]byte, 4096)
n, err := conn.Read(buf)
```

**What happens:**
```
Application                    OS Kernel                    Network
    |                              |                            |
    |-------- Read() ------------->|                            |
    |                              |                            |
    |                              |-- Start 30s timer          |
    |                              |                            |
    |                              |<--- Data arrives? ---------|
    |                              |                            |
    |                              |   YES: Return data         |
    |<------ Data (or error) ------|                            |
    |                              |   NO (30s passed):         |
    |<------ ETIMEDOUT error ------|   Return timeout error     |
```

#### 3. OS Kernel Decision Tree

```
┌─────────────────────────────────────────────┐
│ Application calls Read()                    │
└──────────────────┬──────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────┐
│ Kernel: Check socket receive buffer         │
└──────────────────┬──────────────────────────┘
                   │
        ┌──────────┴──────────┐
        │                     │
        ▼ Data available      ▼ No data
┌──────────────┐    ┌─────────────────────────┐
│ Return data  │    │ Start timer (SO_RCVTIMEO)│
│ immediately  │    │ Block process, wait      │
└──────────────┘    └──────────┬───────────────┘
                                │
                    ┌───────────┴──────────┐
                    │                      │
                    ▼ Data arrives         ▼ Timeout expires
            ┌───────────────┐      ┌──────────────────┐
            │ Wake process  │      │ Wake process     │
            │ Return data   │      │ Return ETIMEDOUT │
            └───────────────┘      └──────────────────┘
```

### 4. No Application-Level Checking

**Important:** Our `Read()` method is simple:

```go
func (c *TCPConn) Read(b []byte) (int, error) {
    // NO timeout checking here!
    // OS handles everything automatically
    n, err := syscall.Read(c.fd, b)
    return n, err
}
```

**Why?** The OS kernel automatically enforces the timeout we set with `SO_RCVTIMEO`.

---

## Real-World Examples

### Example 1: Fast Client (Data Arrives Quickly)

```go
// Server sets 30-second timeout
conn.SetReadDeadline(time.Now().Add(30 * time.Second))

// Client sends HTTP request immediately
// Timeline:
// t=0s:    Read() called
// t=0.1s:  HTTP request arrives
// t=0.1s:  Read() returns data
// Result:  ✅ Success, read 256 bytes

n, err := conn.Read(buf)
// n = 256, err = nil
```

### Example 2: Slow Client (Timeout Occurs)

```go
// Server sets 5-second timeout
conn.SetReadDeadline(time.Now().Add(5 * time.Second))

// Client is very slow or network is congested
// Timeline:
// t=0s:  Read() called, kernel starts timer
// t=1s:  Still waiting for data...
// t=2s:  Still waiting...
// t=3s:  Still waiting...
// t=4s:  Still waiting...
// t=5s:  ⏰ TIMEOUT! Kernel wakes up the blocked Read()
// Result: ❌ Error, connection will be closed

n, err := conn.Read(buf)
// n = 0, err = syscall.ETIMEDOUT
```

### Example 3: Keep-Alive with Multiple Requests

```go
func (s *Server) handleConnection(conn *tcp.TCPConn) {
    defer conn.Close()
    
    // Set initial 30-second timeout
    conn.SetReadDeadline(time.Now().Add(30 * time.Second))
    
    for {
        // Read request (OS enforces 30s timeout)
        request, err := protocol.ParseRequest(conn)
        if err != nil {
            return // Timeout or error, close connection
        }
        
        // Process request
        response := s.handler.Handle(request)
        
        // Write response
        protocol.WriteResponse(conn, response)
        
        // If keep-alive, reset timeout for next request
        if keepAlive {
            conn.SetReadDeadline(time.Now().Add(30 * time.Second))
            continue // Wait for next request
        }
        
        return // Close connection
    }
}
```

**Timeline for 3 requests:**
```
Request 1:
t=0s:     Set deadline (now + 30s)
t=1s:     Request arrives, processed
t=1.5s:   Response sent
t=1.5s:   Set new deadline (now + 30s)

Request 2:
t=10s:    Request arrives, processed
t=10.5s:  Response sent
t=10.5s:  Set new deadline (now + 30s)

Request 3:
t=35s:    Client is idle, no data
t=40.5s:  ⏰ 30s timeout expires
t=40.5s:  Connection closed
```

---

## Why OS-Level Timeouts Are Better

### Comparison: Application-Level vs OS-Level

| Aspect | Application-Level Check | OS-Level Timeout (Our Approach) |
|--------|------------------------|----------------------------------|
| **Implementation** | Check `time.Now()` before each read | Set socket option once |
| **Efficiency** | Multiple time checks, CPU overhead | Kernel handles timing |
| **Accuracy** | Race condition: time passes between check and read | Atomic: kernel enforces precisely |
| **Blocking Behavior** | Must poll or use timers | True blocking with timeout |
| **Code Complexity** | More code, error-prone | Simple, clean |
| **Resource Usage** | More CPU for time checks | Kernel timer, no CPU waste |

### Example of Bad Application-Level Approach

```go
// ❌ Bad: Application-level checking
func (c *TCPConn) Read(b []byte) (int, error) {
    // Check if deadline passed
    if !c.readDeadline.IsZero() && time.Now().After(c.readDeadline) {
        return 0, syscall.ETIMEDOUT
    }
    
    // But what if time passes HERE? Race condition!
    // Read might still block forever!
    n, err := syscall.Read(c.fd, b)
    return n, err
}
```

**Problems:**
1. **Race condition**: Time can pass between check and actual read
2. **Still blocks**: Even if we check time, `syscall.Read()` can block indefinitely
3. **Inefficient**: Every read call does time calculation
4. **Incomplete**: Doesn't actually prevent blocking

### Example of Good OS-Level Approach

```go
// ✅ Good: OS-level timeout
func (c *TCPConn) SetReadDeadline(t time.Time) error {
    timeout := time.Until(t)
    tv := syscall.NsecToTimeval(timeout.Nanoseconds())
    return syscall.SetsockoptTimeval(c.fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)
}

func (c *TCPConn) Read(b []byte) (int, error) {
    // Simple! OS handles timeout automatically
    n, err := syscall.Read(c.fd, b)
    return n, err
}
```

**Benefits:**
1. **No race condition**: Kernel enforces atomically
2. **True timeout**: Kernel interrupts blocked read
3. **Efficient**: No application-level checking
4. **Complete**: Handles all timeout scenarios

---

## Common Pitfalls

### 1. Forgetting to Reset Deadline for Keep-Alive

❌ **Wrong:**
```go
// Set deadline once
conn.SetReadDeadline(time.Now().Add(30 * time.Second))

for {
    // First request: OK (within 30s of initial deadline)
    // Second request: TIMEOUT! (deadline not reset)
    request, err := protocol.ParseRequest(conn)
}
```

✅ **Correct:**
```go
for {
    // Reset deadline for each request
    conn.SetReadDeadline(time.Now().Add(30 * time.Second))
    request, err := protocol.ParseRequest(conn)
}
```

### 2. Not Handling Timeout Errors

❌ **Wrong:**
```go
n, err := conn.Read(buf)
if err != nil {
    log.Printf("Error: %v", err) // Just log, don't close
    // Connection stays open, client hangs!
}
```

✅ **Correct:**
```go
n, err := conn.Read(buf)
if err != nil {
    if err == syscall.ETIMEDOUT {
        log.Printf("Client timeout, closing connection")
    }
    conn.Close()
    return
}
```

### 3. Setting Zero Timeout (Infinite Wait)

```go
// Zero time = no timeout (wait forever)
conn.SetReadDeadline(time.Time{})

// This makes the server vulnerable to slowloris attacks!
```

**Always set a reasonable timeout for production servers.**

### 4. Timeout Too Short

```go
// 1 second might be too short for slow networks
conn.SetReadDeadline(time.Now().Add(1 * time.Second))
```

**Typical values:**
- **30 seconds**: Standard for HTTP servers
- **5 seconds**: For fast internal services
- **2 minutes**: For long-polling or SSE connections

---

## Under the Hood: System Call Details

### What `syscall.SetsockoptTimeval` Does

```go
tv := syscall.NsecToTimeval(timeout.Nanoseconds())
syscall.SetsockoptTimeval(c.fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)
```

**Equivalent C code:**
```c
struct timeval tv;
tv.tv_sec = 30;   // 30 seconds
tv.tv_usec = 0;   // 0 microseconds

// Set receive timeout on socket file descriptor
setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof(tv));
```

### Kernel Behavior

When `read()` or `recv()` is called on this socket:

```c
// Inside Linux kernel (simplified)
int sock_read(int fd, void *buf, size_t len) {
    struct socket *sock = get_socket(fd);
    struct timeval *timeout = &sock->so_rcvtimeo;
    
    if (timeout->tv_sec > 0) {
        // Set up timer
        setup_timer(timeout);
    }
    
    // Wait for data or timeout
    while (!data_available(sock)) {
        if (timer_expired()) {
            return -ETIMEDOUT; // Return timeout error
        }
        sleep_interruptible(); // Block process
    }
    
    // Data arrived, copy to buffer
    return copy_to_user(buf, sock->rx_buffer, len);
}
```

---

## Performance Considerations

### CPU Usage

- **Setting deadline**: ~1-2 µs (one syscall)
- **OS timeout enforcement**: Zero CPU (kernel timer)
- **vs Application checking**: ~0.1 µs per check, but called many times

### Memory Usage

- **Per connection**: +16 bytes (two time.Time fields)
- **Kernel**: ~64 bytes per socket for timer structure

### Scalability

With 10,000 concurrent connections:
- **Memory**: +160 KB for deadline fields
- **CPU**: 0% (kernel handles all timing)
- **vs Application approach**: Would require constant time.Now() calls

---

## Testing Timeout Behavior

### Test 1: Normal Operation

```bash
# Start server
go run cmd/main.go

# Fast client (completes within timeout)
curl http://127.0.0.1:8080/
# ✅ Should return response immediately
```

### Test 2: Slow Client Simulation

```bash
# Use netcat to send incomplete request
nc 127.0.0.1 8080
GET / HTTP/1.1
Host: localhost
# Don't press Enter, wait 30+ seconds
# ⏰ Server should close connection after 30s
```

### Test 3: Keep-Alive

```bash
# Use telnet to send multiple requests
telnet 127.0.0.1 8080
GET / HTTP/1.1
Host: localhost
Connection: keep-alive

# Send another request within 30s
GET /hello HTTP/1.1
Host: localhost
Connection: keep-alive

# Connection stays open for multiple requests
```

---

## Summary

### Key Takeaways

1. **Socket timeouts are OS-level** - The kernel enforces them, not our application
2. **Set once, works automatically** - No need to check on every read/write
3. **Prevents hanging connections** - Essential for production servers
4. **Supports keep-alive** - Reset deadline for each request in persistent connections
5. **Efficient and reliable** - Kernel-level implementation is optimal

### Best Practices

- ✅ Always set read deadlines on server connections
- ✅ Reset deadlines for keep-alive connections
- ✅ Handle timeout errors gracefully
- ✅ Use reasonable timeout values (30s for HTTP)
- ✅ Test timeout behavior in development

### Related Files

- [`internal/tcp/conn.go`](internal/tcp/conn.go) - TCPConn implementation
- [`internal/server/server.go`](internal/server/server.go) - Server timeout usage
- [`internal/protocol/request.go`](internal/protocol/request.go) - Request parsing with timeouts

---

## Further Reading

- [Linux Socket Options](https://man7.org/linux/man-pages/man7/socket.7.html)
- [Go net.Conn Interface](https://pkg.go.dev/net#Conn)
- [TCP Keep-Alive](https://tldp.org/HOWTO/TCP-Keepalive-HOWTO/overview.html)
- [Slowloris Attack Prevention](https://en.wikipedia.org/wiki/Slowloris_(computer_security))

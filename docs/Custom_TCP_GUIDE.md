# Custom TCP Implementation - Learning Guide

This guide provides a structured approach to building a custom TCP stack in Go using low-level syscalls, replacing the standard `net` package with your own implementation.

---

## Table of Contents

1. [Understanding TCP Socket Programming](#phase-1-understanding-tcp-socket-programming)
2. [Implementation Roadmap](#implementation-roadmap)
3. [Syscall Examples](#syscall-examples)
4. [Testing Guide](#testing-guide)
5. [Integration](#integration)

---

## Phase 1: Understanding TCP Socket Programming

### What is a Socket?

A **socket** is an endpoint for sending and receiving data across a network:
- Represented as a **file descriptor** in Unix/Linux systems
- Uniquely identified by: **IP Address + Port Number**
- Acts as the interface between your application and the kernel's network stack

### TCP Socket Lifecycle

```
┌─────────────────────────────────────────────────────┐
│  1. socket()  →  Create a socket                    │
│  2. bind()    →  Attach socket to address:port      │
│  3. listen()  →  Mark socket as passive (server)    │
│  4. accept()  →  Wait for & accept connections      │
│  5. read()    →  Receive data from connection       │
│  6. write()   →  Send data to connection            │
│  7. close()   →  Close connection & free resources  │
└─────────────────────────────────────────────────────┘
```

### Key Constants

**Address Family:**
- `AF_INET` — IPv4 addressing
- `AF_INET6` — IPv6 addressing

**Socket Type:**
- `SOCK_STREAM` — TCP (reliable, connection-oriented, ordered delivery)
- `SOCK_DGRAM` — UDP (unreliable, connectionless, no ordering)

**Protocol:**
- `IPPROTO_TCP` — TCP protocol
- `IPPROTO_UDP` — UDP protocol

---

## Implementation Roadmap

### File: `internal/tcp/addr.go`

**Purpose:** Represent a TCP endpoint (IP + Port)

#### TCPAddr Struct

```go
type TCPAddr struct {
    IP   string  // e.g., "127.0.0.1"
    Port int     // e.g., 8080
    Zone string  // for IPv6, usually empty for IPv4
}
```

#### Methods to Implement

| Method | Signature | Description |
|--------|-----------|-------------|
| `Network()` | `string` | Returns `"tcp"` |
| `String()` | `string` | Returns `"IP:Port"` format (e.g., `"127.0.0.1:8080"`) |
| `ResolveTCPAddr()` | `(network, address string) (*TCPAddr, error)` | Parses `"127.0.0.1:8080"` into TCPAddr struct |

---

### File: `internal/tcp/conn.go`

**Purpose:** Represent an active TCP connection

#### TCPConn Struct

```go
type TCPConn struct {
    fd    int       // file descriptor from syscall.Socket/Accept
    laddr *TCPAddr  // local address
    raddr *TCPAddr  // remote address
}
```

#### Methods to Implement (net.Conn Interface)

| Method | Signature | Description |
|--------|-----------|-------------|
| `Read()` | `(b []byte) (int, error)` | Read data using `syscall.Read(fd, b)` |
| `Write()` | `(b []byte) (int, error)` | Write data using `syscall.Write(fd, b)` |
| `Close()` | `() error` | Close connection using `syscall.Close(fd)` |
| `LocalAddr()` | `() net.Addr` | Return local address (`laddr`) |
| `RemoteAddr()` | `() net.Addr` | Return remote address (`raddr`) |
| `SetDeadline()` | `(t time.Time) error` | Set read and write deadline |
| `SetReadDeadline()` | `(t time.Time) error` | Set timeout for read operations |
| `SetWriteDeadline()` | `(t time.Time) error` | Set timeout for write operations |

---

### File: `internal/tcp/listener.go`

**Purpose:** Listen for and accept incoming TCP connections

#### TCPListener Struct

```go
type TCPListener struct {
    fd    int       // listening socket file descriptor
    laddr *TCPAddr  // local address
}
```

#### Methods to Implement

| Method | Signature | Description |
|--------|-----------|-------------|
| `Accept()` | `() (net.Conn, error)` | Accept connection using `syscall.Accept(fd)` |
| `Close()` | `() error` | Close listener using `syscall.Close(fd)` |
| `Addr()` | `() net.Addr` | Return local address (`laddr`) |

---

### File: `internal/tcp/socket.go`

**Purpose:** Low-level socket operations using syscalls

#### Functions to Implement

##### 1. `createSocket() (int, error)`
Create a new TCP socket.
```go
syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
```
**Returns:** File descriptor for the socket

##### 2. `bindSocket(fd int, addr *TCPAddr) error`
Bind socket to a specific address and port.
```go
// Convert TCPAddr to syscall.SockaddrInet4
// Call syscall.Bind(fd, sockaddr)
```

##### 3. `listenSocket(fd int, backlog int) error`
Mark socket as passive listener.
```go
syscall.Listen(fd, backlog)
```
**Note:** `backlog` = maximum queue length for pending connections

##### 4. `acceptSocket(fd int) (int, *TCPAddr, error)`
Accept an incoming connection.
```go
syscall.Accept(fd)
```
**Returns:** New connection file descriptor and remote address

##### 5. `setSocketOptions(fd int) error`
Configure socket options using `syscall.SetsockoptInt()`:
- `SO_REUSEADDR` — Allow address reuse (prevents "address already in use" errors)
- `SO_KEEPALIVE` — Enable TCP keepalive probes

---

### File: `internal/tcp/tcp.go`

**Purpose:** High-level TCP API (similar to standard `net` package)

#### Functions to Implement

##### 1. `Listen(network, address string) (*TCPListener, error)`

Create a listening TCP socket.

**Steps:**
1. Parse address using `ResolveTCPAddr()`
2. Create socket using `createSocket()`
3. Set socket options using `setSocketOptions()`
4. Bind socket using `bindSocket()`
5. Listen on socket using `listenSocket()`
6. Return `*TCPListener`

##### 2. `Dial(network, address string) (*TCPConn, error)`

Connect to a remote TCP server.

**Steps:**
1. Parse address using `ResolveTCPAddr()`
2. Create socket using `createSocket()`
3. Connect using `syscall.Connect()`
4. Return `*TCPConn`

---

## Syscall Examples

### 1. Create Socket
```go
fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
if err != nil {
    log.Fatal("Socket creation failed:", err)
}
```

### 2. Bind Socket
```go
addr := &syscall.SockaddrInet4{Port: 8080}
copy(addr.Addr[:], net.ParseIP("127.0.0.1").To4())
err := syscall.Bind(fd, addr)
```

### 3. Listen
```go
err := syscall.Listen(fd, 128)  // backlog = 128
```

### 4. Accept
```go
nfd, sa, err := syscall.Accept(fd)
// nfd = new connection file descriptor
// sa  = remote socket address
```

### 5. Read Data
```go
buf := make([]byte, 1024)
n, err := syscall.Read(fd, buf)
if err != nil {
    log.Printf("Read error: %v", err)
}
data := buf[:n]  // actual data read
```

### 6. Write Data
```go
message := []byte("HTTP/1.1 200 OK\r\n\r\nHello, World!")
n, err := syscall.Write(fd, message)
if err != nil {
    log.Printf("Write error: %v", err)
}
```

### 7. Close Socket
```go
err := syscall.Close(fd)
```

---

## Testing Guide

### Step-by-Step Testing

#### 1. Test Socket Creation
```go
fd, err := createSocket()
if err != nil {
    log.Fatal("Failed to create socket:", err)
}
log.Printf("✓ Created socket with fd: %d", fd)
```

#### 2. Test Bind
```go
addr := &TCPAddr{IP: "127.0.0.1", Port: 8080}
err := bindSocket(fd, addr)
if err != nil {
    log.Fatal("Failed to bind:", err)
}
log.Printf("✓ Bound to %s", addr.String())
```

#### 3. Test Listen
```go
err := listenSocket(fd, 10)
if err != nil {
    log.Fatal("Failed to listen:", err)
}
log.Printf("✓ Listening with backlog: 10")
```

#### 4. Test Accept
```go
log.Println("Waiting for connection...")
connFd, remoteAddr, err := acceptSocket(fd)
if err != nil {
    log.Fatal("Failed to accept:", err)
}
log.Printf("✓ Accepted connection from %s (fd: %d)", remoteAddr.String(), connFd)
```

#### 5. Test Read/Write
```go
// Write to client
message := []byte("Hello from custom TCP!")
n, err := syscall.Write(connFd, message)
log.Printf("✓ Wrote %d bytes", n)

// Read from client
buf := make([]byte, 1024)
n, err = syscall.Read(connFd, buf)
log.Printf("✓ Read %d bytes: %s", n, string(buf[:n]))
```

---

## Integration

### Replace Standard Library

Once your custom TCP implementation is complete, you can replace the standard `net` package in your server:

**Before:**
```go
import "net"

listener, err := net.Listen("tcp", "127.0.0.1:8080")
conn, err := listener.Accept()
```

**After:**
```go
import "yourproject/internal/tcp"

listener, err := tcp.Listen("tcp", "127.0.0.1:8080")
conn, err := listener.Accept()
```

### Benefits

✅ **No changes to existing protocol layer** — Your HTTP parsing and routing code works unchanged  
✅ **Drop-in replacement** — Implements `net.Conn` interface  
✅ **Educational** — Full understanding of TCP stack internals  
✅ **Control** — Fine-tune socket options and behavior  

---

## Next Steps

1. **Start with `addr.go`** — Implement address parsing
2. **Build `socket.go`** — Low-level syscall wrappers
3. **Create `listener.go`** — Listening socket logic
4. **Implement `conn.go`** — Connection management
5. **Write `tcp.go`** — High-level API
6. **Test incrementally** — Use the testing guide above
7. **Integrate** — Replace `net.Listen()` in your server

---

## Additional Resources

- **Man pages:** `man 2 socket`, `man 2 bind`, `man 2 listen`, `man 2 accept`
- **Go syscall package:** `godoc golang.org/x/sys/unix`
- **TCP RFC:** RFC 793 (Transmission Control Protocol)

---

**Good luck building your custom TCP stack! 🚀**
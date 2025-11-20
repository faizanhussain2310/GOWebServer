# Why Open Connections Consume Memory and File Descriptors

## Overview

Every time your server accepts a client connection, the connection consumes **two critical system resources**:

1. **File Descriptor (fd)** - Managed by the OS kernel
2. **Memory** - Managed by both the kernel and your Go application

Without proper timeout handling, idle or malicious connections can exhaust these resources, leading to server crashes or denial of service.

---

## Table of Contents

1. [File Descriptor Consumption](#1-file-descriptor-consumption)
2. [Memory Consumption](#2-memory-consumption)
3. [Resource Limits](#resource-limits)
4. [Attack Scenarios](#attack-scenarios)
5. [Why Timeouts Are Essential](#why-timeouts-are-essential)

---

## 1. File Descriptor Consumption 🎫

### What is a File Descriptor?

A **File Descriptor (fd)** is a small, non-negative integer that the operating system's kernel uses as an index into a table of open files/resources belonging to your running process.

In Unix-like systems, "everything is a file," including:
- Regular files
- Network sockets (our case)
- Pipes
- Devices

### How FDs Are Consumed

```go
// When this happens:
conn, err := listener.Accept()

// The kernel:
// 1. Creates a new connected socket
// 2. Assigns it a unique fd (e.g., fd=7)
// 3. Adds it to the process's fd table
```

**Visualization:**

```
┌──────────────────────────────────────────┐
│ Process File Descriptor Table            │
├──────────────────────────────────────────┤
│ fd 0  → stdin                            │
│ fd 1  → stdout                           │
│ fd 2  → stderr                           │
│ fd 3  → listening socket                 │
│ fd 4  → client connection 1              │
│ fd 5  → client connection 2              │
│ fd 6  → client connection 3              │
│ fd 7  → client connection 4              │
│ ...   → ...                              │
│ fd 1023 → client connection 1020         │
│ fd 1024 → ❌ LIMIT REACHED!              │
└──────────────────────────────────────────┘
```

### System Limits

Every operating system imposes limits on file descriptors:

| Limit Type | Typical Default | Adjustable? |
|-----------|-----------------|-------------|
| **Soft Limit** | 1,024 | Yes (ulimit) |
| **Hard Limit** | 4,096 - 1,048,576 | Yes (root) |

**Check your limits:**
```bash
# Current limits
ulimit -n

# Soft and hard limits
ulimit -Sn  # Soft limit
ulimit -Hn  # Hard limit

# See all process limits
cat /proc/<pid>/limits
```

### What Happens When FDs Run Out?

```go
// Attempt to accept new connection
conn, err := listener.Accept()
if err != nil {
    // Error: "too many open files"
    log.Printf("Accept failed: %v", err)
    // ❌ Server cannot accept new clients!
}
```

**Impact:**
- ❌ New legitimate clients are rejected
- ❌ Server appears "down" to new users
- ❌ Existing connections may still work
- ❌ Denial of Service (DoS) condition

### Example: FD Exhaustion

```
Server with fd limit: 1024

Time    Event                           FDs Used
────────────────────────────────────────────────
t=0     Server starts                   3 (std in/out/err)
t=1     Listening socket created        4
t=10    100 clients connect             104
t=30    500 clients connect             504
t=60    1000 clients connect            1004
t=90    1020 clients connect            1024 ✅ Limit reached
t=91    New client tries to connect     ❌ REJECTED
                                        Error: "too many open files"
```

---

## 2. Memory Consumption 🧠

An open connection consumes memory in **two main places**: kernel space and application space.

### A. Kernel Memory (Socket State)

The kernel allocates memory to manage the internal state of each TCP connection.

#### Components of Kernel Memory

##### 1. TCP Control Block (TCB)

```
┌─────────────────────────────────────────┐
│ TCP Control Block (TCB)                 │
├─────────────────────────────────────────┤
│ • Source IP address                     │
│ • Source port number                    │
│ • Destination IP address                │
│ • Destination port number               │
│ • Connection state (ESTABLISHED, etc.)  │
│ • Sequence numbers (send/receive)       │
│ • Acknowledgment numbers                │
│ • Congestion window                     │
│ • Retransmission timers                 │
│ • Round-trip time estimates             │
│ • TCP options                           │
│                                         │
│ Size: ~1-2 KB per connection            │
└─────────────────────────────────────────┘
```

##### 2. Send/Receive Buffers (The Big Memory Consumer)

```
┌─────────────────────────────────────────┐
│ Kernel Send Buffer (SO_SNDBUF)          │
│ Default size: ~16-64 KB                 │
│                                         │
│ Holds data waiting to be:              │
│ • Acknowledged by receiver              │
│ • Retransmitted if lost                 │
│ • Sent over the network                 │
└─────────────────────────────────────────┘

┌─────────────────────────────────────────┐
│ Kernel Receive Buffer (SO_RCVBUF)      │
│ Default size: ~16-64 KB                 │
│                                         │
│ Holds data that has been:              │
│ • Received from network                 │
│ • Not yet read by application           │
│ • Waiting for Read() call               │
└─────────────────────────────────────────┘
```

**Memory per connection (kernel):**
```
TCB:              ~1-2 KB
Send Buffer:      ~16-64 KB
Receive Buffer:   ~16-64 KB
Other metadata:   ~1-2 KB
─────────────────────────────
Total per conn:   ~34-132 KB
```

**Impact at scale:**

| Connections | Memory (Conservative) | Memory (Typical) |
|-------------|----------------------|------------------|
| 100 | 3.4 MB | 6.6 MB |
| 1,000 | 34 MB | 66 MB |
| 10,000 | 340 MB | 660 MB |
| 100,000 | 3.4 GB | 6.6 GB |

### B. Application Memory (Go Goroutines)

Your Go application consumes additional memory for each connection handler.

#### Components of Application Memory

##### 1. Goroutine Stack

```go
go s.handleConnection(conn)  // Starts a new goroutine
```

**Goroutine Memory:**

```
┌─────────────────────────────────────────┐
│ Goroutine Stack                         │
├─────────────────────────────────────────┤
│ Initial size:    2-8 KB                 │
│ Can grow to:     Several MB             │
│                                         │
│ Contains:                               │
│ • Local variables                       │
│ • Function call stack                   │
│ • Return addresses                      │
│ • Function parameters                   │
└─────────────────────────────────────────┘
```

##### 2. Read/Write Buffers

```go
func (s *Server) handleConnection(conn *tcp.TCPConn) {
    buf := make([]byte, 4096)  // 4 KB allocation
    // ...
}
```

**Buffer Memory:**
- **Per connection**: 4 KB (in our implementation)
- **Lifetime**: Exists while goroutine is alive
- **Garbage collection**: Freed when goroutine exits

#### Total Application Memory per Connection

```
Goroutine stack:  ~2-8 KB  (grows as needed)
Read buffer:      ~4 KB
Local variables:  ~0.5-1 KB
─────────────────────────────
Total per conn:   ~6.5-13 KB
```

**Impact at scale:**

| Connections | Goroutine Stacks | Read Buffers | Total App Memory |
|-------------|------------------|--------------|------------------|
| 100 | 0.8 MB | 0.4 MB | 1.2 MB |
| 1,000 | 8 MB | 4 MB | 12 MB |
| 10,000 | 80 MB | 40 MB | 120 MB |
| 100,000 | 800 MB | 400 MB | 1.2 GB |

### C. Combined Memory Usage

```
Total Memory per Connection = Kernel Memory + Application Memory
                            = ~40-145 KB per connection

For 10,000 connections:
  Kernel:         ~340-660 MB
  Application:    ~120 MB
  ───────────────────────────
  Total:          ~460-780 MB
```

---

## Resource Limits

### System-Level Limits

```bash
# Check current limits
ulimit -a

# Common limits affecting servers:
ulimit -n     # Max file descriptors (open files)
ulimit -u     # Max user processes
ulimit -v     # Max virtual memory
ulimit -m     # Max resident set size
```

### Increasing Limits

#### Temporary (Current Session)

```bash
# Increase fd limit to 65535
ulimit -n 65535
```

#### Permanent (System-Wide)

Edit `/etc/security/limits.conf`:
```
# <domain>  <type>  <item>   <value>
*           soft    nofile   65535
*           hard    nofile   1048576
```

Edit `/etc/sysctl.conf`:
```
# Maximum number of open files
fs.file-max = 2097152

# TCP settings
net.ipv4.tcp_max_syn_backlog = 4096
net.core.somaxconn = 4096
```

Apply changes:
```bash
sudo sysctl -p
```

---

## Attack Scenarios

### 1. Slowloris Attack

**Attack:** Open many connections but send data very slowly.

```
Attacker opens 1000 connections:
Connection 1: GET / HTTP/1.1\r\n [waits 60s] Host: ...
Connection 2: GET / HTTP/1.1\r\n [waits 60s] Host: ...
...
Connection 1000: GET / HTTP/1.1\r\n [waits 60s] Host: ...

Result without timeouts:
  ❌ 1000 fds consumed
  ❌ ~460 MB memory consumed
  ❌ No fds left for legitimate clients
```

**Defense with timeouts:**
```go
conn.SetReadDeadline(time.Now().Add(30 * time.Second))

// After 30s of inactivity:
// ✅ Connection closed
// ✅ FD released
// ✅ Memory freed
```

### 2. Connection Exhaustion

**Attack:** Open maximum connections and hold them.

```
Without timeouts:
  FD limit: 1024
  Attacker opens: 1020 connections
  Keeps connections alive forever
  
  Result:
    ❌ Only 4 fds available for real users
    ❌ Server effectively down
```

### 3. Memory Exhaustion

**Attack:** Open connections to consume all server memory.

```
Server with 4 GB RAM:
  Each connection: ~50 KB
  Max connections: ~80,000
  
  Attacker opens 80,000 connections:
    ❌ All memory consumed
    ❌ Server becomes unresponsive
    ❌ OOM killer may terminate process
```

---

## Why Timeouts Are Essential

### The Resource Lifecycle

```
Without Timeout:
┌────────────────┐
│ Accept()       │ → FD allocated, memory allocated
│      ↓         │
│ Block forever  │ → ❌ Resources never freed
│      ↓         │    ❌ Connection hangs
│   (never)      │    ❌ Server vulnerable
└────────────────┘

With Timeout:
┌────────────────┐
│ Accept()       │ → FD allocated, memory allocated
│      ↓         │
│ Read(timeout)  │ → ⏰ 30 seconds max
│      ↓         │
│ Timeout!       │ → ✅ Error returned
│      ↓         │
│ conn.Close()   │ → ✅ FD released
│                │   ✅ Memory freed
└────────────────┘
```

### Key Points

1. **File Descriptors are Limited**
   - Operating systems have hard limits
   - Each connection consumes one FD
   - Running out = server cannot accept new connections

2. **Memory is Limited**
   - Kernel allocates buffers per connection (~34-132 KB)
   - Application allocates goroutine stacks (~6-13 KB)
   - 10K idle connections = ~500 MB wasted memory

3. **Timeouts Trigger Cleanup**
   - `conn.Close()` is the **only** way to release resources
   - Timeouts automatically trigger `conn.Close()`
   - Without timeouts, connections hang forever

4. **Protection Against Attacks**
   - Slowloris attacks defeated by read timeouts
   - Connection exhaustion prevented
   - Memory exhaustion prevented

### Best Practices

```go
// ✅ Always set read deadline
conn.SetReadDeadline(time.Now().Add(30 * time.Second))

// ✅ Always defer close
defer conn.Close()

// ✅ Reset deadline for keep-alive
if keepAlive {
    conn.SetReadDeadline(time.Now().Add(30 * time.Second))
}

// ✅ Handle timeout errors
if err == syscall.ETIMEDOUT {
    log.Printf("Client timeout, closing connection")
    return
}
```

---

## Summary

### Resources Consumed per Connection

| Resource | Location | Size | Freed By |
|----------|----------|------|----------|
| **File Descriptor** | OS Kernel | 1 fd | `conn.Close()` |
| **TCP Control Block** | Kernel | 1-2 KB | `conn.Close()` |
| **Send Buffer** | Kernel | 16-64 KB | `conn.Close()` |
| **Receive Buffer** | Kernel | 16-64 KB | `conn.Close()` |
| **Goroutine Stack** | Application | 2-8 KB | Goroutine exit |
| **Read Buffer** | Application | 4 KB | Goroutine exit |
| **Total** | - | **~40-145 KB** | **Timeout → Close** |

### Critical Insights

1. ⚠️ **Every connection consumes scarce resources**
2. ⚠️ **Resources are NOT freed automatically**
3. ⚠️ **Only `conn.Close()` releases resources**
4. ⚠️ **Timeouts are the trigger for `conn.Close()`**
5. ✅ **Timeouts = Protection against resource exhaustion**

---

## Related Documentation

- [TCP_TIMEOUT_GUIDE.md](TCP_TIMEOUT_GUIDE.md) - Socket timeout implementation
- [KERNEL_TIMEOUT_ATOMICITY.md](KERNEL_TIMEOUT_ATOMICITY.md) - Kernel-level timeout guarantees
- [internal/tcp/conn.go](internal/tcp/conn.go) - TCP connection implementation
- [internal/server/server.go](internal/server/server.go) - Server with timeout handling
# Kernel Send and Receive Buffers

## Overview

Every TCP connection has **two separate buffers** in kernel space: one for incoming data (receive) and one for outgoing data (send).

---

## The Two Buffers

```
┌─────────────────────────────────────────────────────────┐
│           Kernel Space (Per TCP Connection)             │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  ┌─────────────────────────┐  ┌─────────────────────┐ │
│  │   RECEIVE BUFFER        │  │   SEND BUFFER       │ │
│  │   (SO_RCVBUF)           │  │   (SO_SNDBUF)       │ │
│  │                         │  │                     │ │
│  │   Default: 64 KB        │  │   Default: 64 KB    │ │
│  │                         │  │                     │ │
│  │   Network → Buffer      │  │   Buffer → Network  │ │
│  │   Buffer → Read()       │  │   Write() → Buffer  │ │
│  └─────────────────────────┘  └─────────────────────┘ │
│            ↓                            ↑               │
│     Your conn.Read()           Your conn.Write()       │
└─────────────────────────────────────────────────────────┘
```

---

## Receive Buffer (SO_RCVBUF)

### Purpose
Holds **incoming data** from the network until your application reads it.

### Data Flow
```
Client → Network → NIC → Kernel → Receive Buffer → conn.Read() → Your buf
```

### Example
```
Client sends: "GET /hello HTTP/1.1\r\n\r\n" (26 bytes)

Receive Buffer:
┌──────────────────────────────────┐
│ "GET /hello HTTP/1.1\r\n\r\n"   │ ← Waiting for Read()
│ 26 / 65536 bytes                 │
└──────────────────────────────────┘

Your code: conn.Read(buf)

Receive Buffer (after Read):
┌──────────────────────────────────┐
│ [empty]                          │ ← Data moved to your buf
│ 0 / 65536 bytes                  │
└──────────────────────────────────┘
```

### When Full
```
Buffer: [████████████████] 65536/65536 FULL!
         ↓
Kernel sends: TCP Window Size = 0
         ↓
Client STOPS sending (blocked)
         ↓
You call Read() → Space freed
         ↓
Kernel sends: TCP Window Size > 0
         ↓
Client RESUMES sending
```

---

## Send Buffer (SO_SNDBUF)

### Purpose
Holds **outgoing data** that you've written but hasn't been acknowledged by the receiver yet.

### Data Flow
```
Your buf → conn.Write() → Send Buffer → Kernel → NIC → Network → Client
```

### Example
```go
conn.Write([]byte("HTTP/1.1 200 OK\r\n\r\nHello"))
```

```
Send Buffer (after Write):
┌──────────────────────────────────┐
│ "HTTP/1.1 200 OK\r\n\r\nHello"  │ ← Waiting to send
│ 28 / 65536 bytes                 │
└──────────────────────────────────┘
         ↓
Kernel sends packet to network
         ↓
Send Buffer (still there!):
┌──────────────────────────────────┐
│ "HTTP/1.1 200 OK\r\n\r\nHello"  │ ← Waiting for ACK
│ 28 / 65536 bytes                 │
└──────────────────────────────────┘
         ↓
Client sends ACK
         ↓
Send Buffer (ACK received):
┌──────────────────────────────────┐
│ [empty]                          │ ← Safe to remove
│ 0 / 65536 bytes                  │
└──────────────────────────────────┘
```

**Why data stays?** For retransmission if packet is lost!

### When Full
```
Buffer: [████████████████] 65536/65536 FULL!
         ↓
conn.Write() BLOCKS!
         ↓
Client sends ACK → Space freed
         ↓
Write() continues
```

---

## Complete Flow: Both Buffers

```
REQUEST (Client → Server):
─────────────────────────────────────────────────────────

Client                           Server
  │                                │
  │ Write("GET /hello")            │
  │ ↓                              │
  │ [Client Send Buffer]           │
  │ ↓                              │
  │ ────── Network Packet ────→    │
  │                              ↓ │
  │                 [Server Receive Buffer]
  │                              ↓ │
  │                         conn.Read()
  │                                │

RESPONSE (Server → Client):
─────────────────────────────────────────────────────────

Server                          Client
  │                               │
  │ conn.Write("HTTP/1.1 200")   │
  │ ↓                             │
  │ [Server Send Buffer]          │
  │ ↓                             │
  │ ────── Network Packet ────→   │
  │                             ↓ │
  │                [Client Receive Buffer]
  │                             ↓ │
  │                        Read()
  │                               │
```

---

## Key Differences

| Aspect | Receive Buffer | Send Buffer |
|--------|----------------|-------------|
| **Direction** | Incoming (Network → You) | Outgoing (You → Network) |
| **Filled by** | Network packets | Your Write() calls |
| **Emptied by** | Your Read() calls | Network ACKs |
| **When full** | Sender blocked by TCP flow control | Your Write() blocks |
| **Purpose** | Store data until you're ready | Enable retransmission if packet lost |

---

## Memory Cost

```
Per Connection:
├─ Receive Buffer:   64 KB
├─ Send Buffer:      64 KB
├─ Socket metadata:  ~3 KB
└─ Total:           ~131 KB

Scale:
  1,000 connections   = 131 MB
 10,000 connections   = 1.31 GB
100,000 connections   = 13.1 GB
```

---

## TCP Flow Control (Visual)

### Receive Buffer Flow Control

```
Time   Server Receive Buffer    TCP Window    Client State
───────────────────────────────────────────────────────────
t=0    [empty] 0/64KB            64 KB         Sending
t=1    [████] 32/64KB            32 KB         Sending
t=2    [████████] 64/64KB        0 KB          BLOCKED!
t=3    conn.Read() called
t=3.1  [empty] 0/64KB            64 KB         UNBLOCKED
```

### Send Buffer Flow Control

```
Time   Server Send Buffer       Network       Write() State
──────────────────────────────────────────────────────────
t=0    [empty] 0/64KB           Ready         Returns immediately
t=1    [████] 32/64KB           Sending       Returns immediately
t=2    [████████] 64/64KB       Slow ACKs     BLOCKS!
t=3    ACK received
t=3.1  [████] 32/64KB           Ready         UNBLOCKED
```

---

## Why Two Buffers?

### 1. **Decoupling**
```
Your application speed ≠ Network speed

Fast app, slow network:
  Send buffer accumulates data, sends when ready

Slow app, fast network:
  Receive buffer stores data, app reads when ready
```

### 2. **Reliability**
```
Send buffer keeps data until ACKed:
  Packet lost? → Retransmit from buffer ✅
  
Without buffer:
  Packet lost? → Data gone forever ❌
```

### 3. **Performance**
```
Your Write() returns immediately:
  Data copied to Send buffer (fast)
  Actual network transmission happens asynchronously
  
Your Read() doesn't wait for network:
  Data already in Receive buffer (instant)
```

---

## Configuring Buffer Sizes

### Check Current Size

```go
// Get receive buffer size
size, err := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF)
fmt.Printf("Receive buffer: %d bytes\n", size)

// Get send buffer size
size, err = syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_SNDBUF)
fmt.Printf("Send buffer: %d bytes\n", size)
```

### Set Custom Size

```go
// Set receive buffer to 128 KB
syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF, 128*1024)

// Set send buffer to 128 KB
syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_SNDBUF, 128*1024)
```

### When to Change?

```
Larger buffers (128 KB - 2 MB):
✅ High-throughput applications
✅ Long-distance connections (high latency)
✅ Bulk data transfer

Smaller buffers (16 KB - 32 KB):
✅ Low-latency applications
✅ Memory-constrained systems
✅ Many simultaneous connections
```

---

## Common Issues

### Issue 1: Slow Reader

```
Problem:
  Client sends data faster than server reads
  Receive buffer fills up
  Client blocked indefinitely

Solution:
  Set read timeout: SO_RCVTIMEO
  Read data promptly in application
```

### Issue 2: Slow Writer

```
Problem:
  Server writes faster than network/client can handle
  Send buffer fills up
  Write() blocks forever

Solution:
  Set write timeout: SO_SNDTIMEO
  Implement backpressure in application
  Close slow clients
```

### Issue 3: Memory Exhaustion

```
Problem:
  10,000 connections × 131 KB = 1.3 GB!
  Server runs out of memory

Solution:
  Reduce buffer sizes for short-lived connections
  Close idle connections aggressively
  Use connection pooling
```

---

## Summary

### Core Concepts

```
1. Two Buffers:
   ├─ Receive Buffer: Network → You
   └─ Send Buffer: You → Network

2. Automatic Flow Control:
   ├─ Buffer full → TCP stops sender
   └─ Buffer empty → TCP resumes sender

3. Reliability:
   └─ Send buffer keeps data until ACKed

4. Memory Cost:
   └─ ~131 KB per connection (both buffers)
```

### Critical Points

1. ✅ **Buffers are per-connection** (not shared)
2. ✅ **Automatically managed by kernel** (you just Read/Write)
3. ✅ **Flow control is built-in** (prevents overflow)
4. ✅ **Data persists until consumed** (Receive) or ACKed (Send)
5. ✅ **Configurable sizes** (tune for your use case)

### Visual Summary

```
┌────────────────────────────────────────────────────┐
│            Kernel Buffers (Per Connection)         │
├────────────────────────────────────────────────────┤
│                                                    │
│  RECEIVE (64 KB)          SEND (64 KB)            │
│  ┌──────────────┐         ┌──────────────┐        │
│  │ Network →    │         │ → Network    │        │
│  │    ↓         │         │    ↑         │        │
│  │ Your Read()  │         │ Your Write() │        │
│  └──────────────┘         └──────────────┘        │
│                                                    │
│  Filled by:               Filled by:              │
│  - Network packets        - Write() calls         │
│                                                    │
│  Emptied by:              Emptied by:             │
│  - Read() calls           - ACKs from peer        │
│                                                    │
│  When full:               When full:              │
│  - Sender blocked         - Write() blocks        │
│                                                    │
└────────────────────────────────────────────────────┘
```

---

## Related Documentation

- [KERNEL_FD_MANAGEMENT.md](KERNEL_FD_MANAGEMENT.md) - FD lifecycle and routing
- [BUFFER_AND_TCP_FLOW.md](BUFFER_AND_TCP_FLOW.md) - Application buffer vs kernel buffer
- [TCP_TIMEOUT_GUIDE.md](TCP_TIMEOUT_GUIDE.md) - Timeout handling
- [OPEN_CONNECTION_CONSUMES_MEMORY_AND_FD.md](OPEN_CONNECTION_CONSUMES_MEMORY_AND_FD.md) - Resource usage
- [internal/tcp/socket.go](internal/tcp/socket.go) - Buffer configuration

# Understanding Buffers and TCP Data Flow

## Overview

This document explains how data flows from the network through the OS kernel's TCP buffer into your application buffer, and why buffer reuse is safe and efficient.

---

## Table of Contents

1. [Two Different Buffers](#two-different-buffers)
2. [How Data Flow Works](#how-data-flow-works)
3. [Step-by-Step Data Flow Example](#step-by-step-data-flow-example)
4. [What If Kernel Buffer Fills Up?](#what-if-kernel-buffer-fills-up)
5. [Visual Timeline](#visual-timeline)
6. [Key Questions Answered](#key-questions-answered)
7. [Code Examples](#code-examples)
8. [Summary](#summary)

---

## Two Different Buffers

### 1. Your Application Buffer (buf)

```go
buf := make([]byte, 4096)  // Your local buffer in Go program
```

**Characteristics:**
- **Size:** 4KB (you control this)
- **Location:** Your Go program's memory
- **Lifetime:** Exists as long as variable is in scope
- **Management:** You control when to read/write
- **Reusable:** Overwritten on each `Read()` call

### 2. OS Kernel TCP Receive Buffer (Socket Buffer)

```
Hidden from you, managed by OS kernel
```

**Characteristics:**
- **Size:** Usually 64KB - 2MB (configurable via SO_RCVBUF)
- **Location:** Kernel memory space (not accessible directly)
- **Lifetime:** Exists while socket is open
- **Management:** OS automatically manages
- **Persistent:** Data stays until you read it

---

## How Data Flow Works

```
┌──────────┐    ┌─────────────┐    ┌──────────┐    ┌──────────┐
│ Internet │ -> │ Network     │ -> │  Kernel  │ -> │ Your buf │ -> allData
│          │    │ Card (NIC)  │    │   TCP    │    │  (4KB)   │    (storage)
│          │    │             │    │  Buffer  │    │          │
└──────────┘    └─────────────┘    └──────────┘    └──────────┘
    ↓                ↓                   ↓               ↓           ↓
Hardware         Hardware           OS Managed      Your Go Code  Permanent
                                                                   Storage
```

**Key Point:** Data goes through multiple layers before reaching your code!

---

## Step-by-Step Data Flow Example

Let's trace how 10KB of data flows through the system.

### Initial State

```
Kernel TCP Buffer (OS):          Your buf:                 allData:
┌─────────────────────┐         ┌──────────┐              []
│ [empty]             │         │ [empty]  │
│ 0 / 64KB used       │         │ 4KB cap  │
└─────────────────────┘         └──────────┘
```

---

### Step 1: Client Sends 10KB of Data

**What happens:**
1. Network card receives packets
2. Hardware interrupt triggered
3. OS kernel copies data into TCP receive buffer
4. Your program doesn't know yet!

```
Network packet arrives (10KB):
"POST /upload HTTP/1.1\r\nContent-Length: 10240\r\n\r\n{large JSON payload}"

Kernel TCP Buffer (OS):          Your buf:                 allData:
┌─────────────────────┐         ┌──────────┐              []
│ ████████████        │         │          │
│ [10KB of data]      │         │ [empty]  │
│ 10KB / 64KB used    │         │          │
└─────────────────────┘         └──────────┘
       ↑
   Data stored here
   by OS automatically
   (not in your program yet!)
```

**Important:** Data sits in kernel buffer until you call `Read()`!

---

### Step 2: You Call conn.Read(buf) - First Read

```go
n, err := conn.Read(buf)  // Reads UP TO 4096 bytes
// n = 4096 (read maximum possible)
```

**What happens:**
1. Kernel **copies** 4KB from its buffer to your `buf`
2. Kernel **removes** that 4KB from its buffer (frees space)
3. You **append** `buf[:n]` to `allData` (permanent storage)

```
Kernel TCP Buffer (OS):          Your buf:                 allData:
┌─────────────────────┐         ┌──────────┐              []
│ ██████              │  ─────> │ ████████ │  ───────>   [████]
│ [6KB remaining]     │  copy   │ [4KB]    │   append    [4KB]
│ 6KB / 64KB used     │         │ FULL!    │
└─────────────────────┘         └──────────┘
       ↑                              ↑                      ↑
   Kernel freed                  Buffer is               Permanent
   4KB (now 6KB left)            "full" but              storage
                                 will be reused
```

**Code execution:**
```go
n, err := conn.Read(buf)               // n = 4096
allData = append(allData, buf[:n]...)  // Copy to permanent storage
// buf can now be reused safely!
```

---

### Step 3: You Call conn.Read(buf) Again - Second Read

```go
n, err := conn.Read(buf)  // buf is REUSED!
// n = 4096 (read another 4KB)
```

**What happens:**
1. Kernel copies another 4KB to your `buf`
2. **Old data in buf is OVERWRITTEN**
3. You append new data to `allData`

```
Kernel TCP Buffer (OS):          Your buf:                 allData:
┌─────────────────────┐         ┌──────────┐              [████]
│ ██                  │  ─────> │ ████████ │  ───────>   [████████]
│ [2KB remaining]     │  copy   │ [4KB]    │   append    [4KB+4KB]
│ 2KB / 64KB used     │         │ NEW DATA!│             [8KB total]
└─────────────────────┘         └──────────┘
                                      ↑
                              Previous data
                              OVERWRITTEN!
                              (but safe, already
                              in allData)
```

**Code execution:**
```go
n, err := conn.Read(buf)               // n = 4096
allData = append(allData, buf[:n]...)  // Append new 4KB
// allData now has 8KB total
```

---

### Step 4: Third Read (Last Chunk)

```go
n, err := conn.Read(buf)  // Only 2KB left
// n = 2048 (partial buffer fill)
```

**What happens:**
1. Kernel copies last 2KB to your `buf`
2. Only first 2KB of `buf` is used
3. Rest of `buf` contains old data (ignored via `buf[:n]`)

```
Kernel TCP Buffer (OS):          Your buf:                 allData:
┌─────────────────────┐         ┌──────────┐              [████████]
│                     │  ─────> │ ██______ │  ───────>   [██████████]
│ [empty]             │  copy   │ [2KB]    │   append    [4+4+2 = 10KB]
│ 0KB / 64KB used     │         │ [2KB unused]            Complete!
└─────────────────────┘         └──────────┘
                                      ↑
                              buf[:2048] used
                              buf[2048:] ignored
                              (contains old data)
```

**Code execution:**
```go
n, err := conn.Read(buf)               // n = 2048
allData = append(allData, buf[:n]...)  // Only append buf[:2048]
// allData now has 10KB - complete!
```

---

## What If Kernel Buffer Fills Up?

### Scenario: Client Sends Data Faster Than You Read

This is where **TCP Flow Control** comes into play.

#### Time 0s: Client Sends 64KB (fills kernel buffer)

```
Client sends data rapidly...

Kernel TCP Buffer (OS):
┌─────────────────────────────────┐
│ ████████████████████████████████│
│ [64KB FULL!]                    │
│ 64KB / 64KB used (100%)         │
└─────────────────────────────────┘
```

#### Time 1s: Client Tries to Send More...

```
Client attempts: send(more_data, ...)

Kernel TCP Buffer (OS):
┌─────────────────────────────────┐
│ ████████████████████████████████│  ← No space!
│ [64KB FULL!]                    │
│ Can't accept more data!         │
└─────────────────────────────────┘
        ↓
TCP Flow Control Kicks In:
```

**What happens:**

```
1. Kernel sends TCP packet to client:
   ┌──────────────────────────┐
   │ TCP Window Size = 0      │  ← "Stop sending!"
   └──────────────────────────┘

2. Client's send() call BLOCKS:
   - Client process sleeps
   - Waits for window size > 0

3. Your program calls Read():
   - Frees space in kernel buffer
   - Kernel sends update to client:
   ┌──────────────────────────┐
   │ TCP Window Size = 32KB   │  ← "You can send 32KB now"
   └──────────────────────────┘

4. Client's send() unblocks:
   - Client resumes sending
   - System maintains balance
```

### Visual Timeline of Flow Control

```
Time    Kernel Buffer    TCP Window    Client State       Your Action
──────────────────────────────────────────────────────────────────────
0s      [64KB] 100%      0 bytes       BLOCKED on send()  (slow reader)
        
1s      [64KB] 100%      0 bytes       BLOCKED on send()  (still slow)
        
2s      [64KB] 100%      0 bytes       BLOCKED on send()  Read() called!
        ↓
2s      [60KB] 93%       4KB           send() unblocked   Read() freed 4KB
                                       Sends 4KB
        ↓
3s      [64KB] 100%      0 bytes       BLOCKED again      (slow again)
        
4s      [48KB] 75%       16KB          Sending...         Read() called!
                                                          Read() again!
                                                          Reading faster!
```

**Key Insight:** TCP Flow Control prevents data loss by automatically throttling the sender when receiver is slow!

---

## Visual Timeline

Complete timeline showing all three buffers:

```
Time  Kernel Buffer         Your buf              allData           What happens
─────────────────────────────────────────────────────────────────────────────────
0s    [empty]               [empty]               []                Client connects
      0KB / 64KB            0KB / 4KB             0KB

1s    [████████████]        [empty]               []                Client sends 10KB
      10KB / 64KB           0KB / 4KB             0KB               Data arrives in kernel!
      
2s    [████████████]        [empty]               []                You call Read()
      10KB / 64KB           ↓
      ↓                     
2s    [██████]              [████]                []                Kernel→buf copy (4KB)
      6KB / 64KB            4KB / 4KB             0KB               
      ↓                     ↓
      
2s    [██████]              [████]                [████]            buf→allData append
      6KB / 64KB            4KB / 4KB             4KB               buf[:4096] copied
                            ↓ REUSABLE
                            
3s    [██████]              [empty]               [████]            You call Read() again
      6KB / 64KB            0KB / 4KB             4KB
      ↓
      
3s    [██]                  [████]                [████]            Kernel→buf copy (4KB)
      2KB / 64KB            4KB / 4KB             4KB               
      ↓                     ↓ OVERWRITES!
      
3s    [██]                  [████]                [████████]        buf→allData append
      2KB / 64KB            4KB / 4KB             8KB               Another 4KB added
                            ↓ REUSABLE
                            
4s    [██]                  [empty]               [████████]        You call Read() again
      2KB / 64KB            0KB / 4KB             8KB
      ↓
      
4s    [empty]               [██__]                [████████]        Kernel→buf copy (2KB)
      0KB / 64KB            2KB / 4KB             8KB               Only 2KB copied
      ↓                     ↓ PARTIAL
      
4s    [empty]               [██__]                [██████████]      buf[:2048]→allData
      0KB / 64KB            2KB / 4KB             10KB              Complete!
                                                  ↑
                                                Done reading!
```

---

## Key Questions Answered

### Q1: "Will data persist in buffer?"

#### Your `buf`: NO! Overwritten each `Read()` call

```go
buf := make([]byte, 4096)

// First read
n, _ := conn.Read(buf)  // buf = "Hello___" (+ garbage)
fmt.Println(string(buf[:n]))  // "Hello"

// Second read - OVERWRITES buf!
n, _ = conn.Read(buf)  // buf = "World___" (+ garbage)
fmt.Println(string(buf[:n]))  // "World"

// "Hello" is GONE from buf!
// But if you saved it to allData, it's safe there
```

**Timeline:**
```
After first read:  buf = ['H','e','l','l','o', ?, ?, ?, ...]
After second read: buf = ['W','o','r','l','d', ?, ?, ?, ...]
                               ↑
                        Old data overwritten!
```

#### Kernel Buffer: YES! Data persists until you read it

```go
// Data arrives at time=0
// You wait 10 seconds...
time.Sleep(10 * time.Second)

// Data is STILL in kernel buffer!
n, _ := conn.Read(buf)  // Gets data that arrived 10s ago ✅
```

**Why?** Kernel buffer is managed by OS, persists independently of your program's actions.

---

### Q2: "If buffer is full and we haven't retrieved data?"

#### Your `buf` can't be "full" between reads

You control when to call `Read()`, so `buf` is never "stuck" full.

```go
buf := make([]byte, 4096)

n, _ := conn.Read(buf)          // Fills buf
allData = append(allData, buf[:n]...)  // Save data

// buf is now "free" - you can reuse it immediately
n, _ := conn.Read(buf)          // Overwrites buf ✅
```

#### Kernel buffer CAN be full

When kernel buffer fills:

```
Scenario: 10,000 clients sending data simultaneously

Kernel Buffer State:
┌─────────────────────────────────┐
│ ████████████████████████████████│
│ FULL! (64KB)                    │
└─────────────────────────────────┘
        ↓
TCP Window = 0 sent to ALL clients
        ↓
All 10,000 clients BLOCKED on send()
        ↓
You call Read() repeatedly
        ↓
Space freed in kernel buffer
        ↓
TCP Window > 0 sent to clients
        ↓
Clients resume sending
```

**Protection mechanism:** TCP flow control prevents overflow!

---

### Q3: "How will next installment be put in buffer?"

#### Your `buf`: Reused every `Read()` call (you control timing)

```go
// First installment
n1, _ := conn.Read(buf)  // buf = [data1]
save(buf[:n1])

// Second installment - same buf, new data
n2, _ := conn.Read(buf)  // buf = [data2] (overwrites data1)
save(buf[:n2])

// Third installment - same buf, new data
n3, _ := conn.Read(buf)  // buf = [data3] (overwrites data2)
save(buf[:n3])
```

#### Kernel Buffer: Automatically managed by OS

```
Network packets arrive continuously:
Packet 1 (1KB)  ──> Kernel buffer [1KB]
Packet 2 (2KB)  ──> Kernel buffer [3KB]
You Read(4KB)   <── Kernel buffer [0KB] (3KB returned, 1KB space left)
Packet 3 (500B) ──> Kernel buffer [500B]
Packet 4 (500B) ──> Kernel buffer [1KB]
You Read(4KB)   <── Kernel buffer [0KB] (1KB returned)
```

**Flow control ensures kernel buffer never overflows:**
- If buffer fills → TCP Window = 0 → Sender stops
- If buffer empties → TCP Window > 0 → Sender resumes

---

## Code Examples

### Example 1: Basic Buffer Reuse

```go
func readData(conn *tcp.TCPConn) ([]byte, error) {
    var allData []byte
    buf := make([]byte, 4096)  // Single buffer, reused
    
    for {
        n, err := conn.Read(buf)
        if err != nil {
            if err == io.EOF {
                break  // Connection closed
            }
            return nil, err
        }
        
        // CRITICAL: Copy data before next read!
        allData = append(allData, buf[:n]...)
        
        // buf will be overwritten on next iteration
    }
    
    return allData, nil
}
```

**Why copy?**
```go
// ❌ WRONG: Don't save reference to buf
allData := buf  // Dangerous! allData points to buf
n, _ := conn.Read(buf)  // buf changed → allData changed!

// ✅ CORRECT: Copy data out
allData := make([]byte, len(buf[:n]))
copy(allData, buf[:n])  // Independent copy

// ✅ CORRECT: Use append (makes copy)
allData = append(allData, buf[:n]...)  // Copies data
```

---

### Example 2: Demonstrating Buffer Overwrite

```go
func demonstrateOverwrite() {
    buf := make([]byte, 10)
    
    // Simulate first read
    copy(buf, []byte("HELLO_____"))
    fmt.Printf("After first read:  %s\n", string(buf))
    // Output: "HELLO_____"
    
    // Simulate second read (overwrites)
    copy(buf, []byte("WORLD_____"))
    fmt.Printf("After second read: %s\n", string(buf))
    // Output: "WORLD_____"
    
    // "HELLO" is gone!
}
```

---

### Example 3: Kernel Buffer Visualization

```go
func monitorKernelBuffer(conn *tcp.TCPConn) {
    // Get kernel buffer size
    var bufSize int
    // (syscall to get SO_RCVBUF)
    
    fmt.Printf("Kernel buffer size: %d bytes\n", bufSize)
    
    buf := make([]byte, 4096)
    
    for {
        // Check how much data is waiting
        // (ioctl FIONREAD or similar)
        var bytesAvailable int
        fmt.Printf("Bytes in kernel buffer: %d\n", bytesAvailable)
        
        n, err := conn.Read(buf)
        if err != nil {
            break
        }
        
        fmt.Printf("Read %d bytes from kernel buffer\n", n)
    }
}
```

**Sample output:**
```
Kernel buffer size: 65536 bytes
Bytes in kernel buffer: 10240
Read 4096 bytes from kernel buffer
Bytes in kernel buffer: 6144
Read 4096 bytes from kernel buffer
Bytes in kernel buffer: 2048
Read 2048 bytes from kernel buffer
Bytes in kernel buffer: 0
```

---

### Example 4: Handling Slow Reader (Flow Control)

```go
func slowReader(conn *tcp.TCPConn) {
    buf := make([]byte, 4096)
    
    for {
        fmt.Println("Reading from connection...")
        n, err := conn.Read(buf)
        if err != nil {
            break
        }
        
        fmt.Printf("Read %d bytes\n", n)
        
        // Simulate slow processing
        time.Sleep(5 * time.Second)  // ← Slow!
        
        // During this sleep:
        // - More data arrives in kernel buffer
        // - Kernel buffer might fill up
        // - TCP flow control kicks in
        // - Sender is throttled automatically
    }
}
```

**What happens on sender side:**
```go
// Sender (client)
func sendLargeData(conn net.Conn) {
    data := make([]byte, 1000000)  // 1MB
    
    // This call may BLOCK if receiver is slow!
    n, err := conn.Write(data)
    
    // Write() returns when:
    // 1. Data copied to kernel buffer, OR
    // 2. Partial write if buffer full
    
    fmt.Printf("Sent %d bytes\n", n)
}
```

---

## Why This Design?

### 1. Memory Efficiency

```go
// ✅ Good: Reuse single 4KB buffer
buf := make([]byte, 4096)
for {
    n, _ := conn.Read(buf)
    allData = append(allData, buf[:n]...)
}
// Memory: 4KB buffer + growing allData

// ❌ Bad: Allocate new buffer each time
for {
    buf := make([]byte, 4096)  // New allocation!
    n, _ := conn.Read(buf)
    allData = append(allData, buf[:n]...)
}
// Memory: 4KB × iterations + growing allData
// Wasteful! Causes GC pressure
```

---

### 2. Performance

```go
// Kernel buffer acts as staging area
// - Network packets arrive at hardware speed
// - Kernel buffer absorbs bursts
// - Your program reads at its own pace
// - No packet loss due to timing

Without kernel buffer:
Network packet → Your program (must be ready!)
                 ↓
              If busy, packet dropped! ❌

With kernel buffer:
Network packet → Kernel buffer (always ready)
                 ↓
                 Your program (reads when ready) ✅
```

---

### 3. Simplicity

```go
// You don't need to worry about:
// - Network packet arrival timing
// - Packet fragmentation
// - TCP retransmissions
// - Flow control
// - Congestion control

// OS handles all of this!
// You just call Read() and get data ✅
```

---

## Summary

### Key Concepts

1. **Two buffers exist:**
   - **Kernel TCP buffer:** 64KB+, managed by OS, persistent until read
   - **Your `buf`:** 4KB, managed by you, overwritten each read

2. **Your `buf` is temporary:**
   - Each `Read()` overwrites it
   - Must copy data to permanent storage (`allData`)
   - Safe to reuse immediately after copying

3. **Kernel buffer is persistent:**
   - Data waits until you read it
   - Can hold data for seconds/minutes
   - TCP flow control prevents overflow

4. **Data flow:**
   ```
   Network → Kernel Buffer → Your buf (temporary) → allData (permanent)
   ```

5. **If you're slow to read:**
   - Kernel buffer fills up
   - TCP sends "Window Size = 0" to sender
   - Sender blocks on `send()`
   - No data loss! ✅

6. **Buffer reuse is safe:**
   ```go
   buf := make([]byte, 4096)  // Allocate once
   for {
       n, _ := conn.Read(buf)           // Overwrites buf
       allData = append(allData, buf[:n]...)  // Copies out
   }
   // buf overwritten each iteration
   // allData preserves all data ✅
   ```

---

### Memory Layout Summary

```
┌─────────────────────────────────────────────────────────┐
│                    System Memory                        │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  Kernel Space (Protected):                             │
│  ┌──────────────────────────────┐                      │
│  │ TCP Receive Buffer (64KB)    │ ← Persistent         │
│  │ - Managed by OS              │                      │
│  │ - Not directly accessible    │                      │
│  │ - Survives across Read() calls│                     │
│  └──────────────────────────────┘                      │
│                                                         │
│  User Space (Your Program):                            │
│  ┌──────────────────────────────┐                      │
│  │ buf (4KB)                    │ ← Temporary          │
│  │ - Managed by you             │                      │
│  │ - Overwritten each Read()    │                      │
│  └──────────────────────────────┘                      │
│                                                         │
│  ┌──────────────────────────────┐                      │
│  │ allData (grows)              │ ← Permanent          │
│  │ - Managed by you             │                      │
│  │ - Accumulates all data       │                      │
│  │ - Never overwritten          │                      │
│  └──────────────────────────────┘                      │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

---

### Critical Rules

1. ✅ **Always copy data from `buf` before next `Read()`**
   ```go
   allData = append(allData, buf[:n]...)  // Makes copy
   ```

2. ✅ **Reuse `buf` - don't reallocate**
   ```go
   buf := make([]byte, 4096)  // Once, outside loop
   for { conn.Read(buf) }      // Reuse each iteration
   ```

3. ✅ **Trust TCP flow control**
   - Don't worry about kernel buffer overflow
   - OS handles backpressure automatically

4. ✅ **Use `buf[:n]`, not `buf`**
   ```go
   n, _ := conn.Read(buf)
   allData = append(allData, buf[:n]...)  // Only actual data
   ```

5. ❌ **Don't save references to `buf`**
   ```go
   saved := buf       // ❌ Points to same memory
   saved := buf[:n]   // ❌ Still points to buf
   saved := append([]byte{}, buf[:n]...)  // ✅ Independent copy
   ```

---

## Common Misconceptions

### Misconception 1: "Buffer size must be 4096 bytes"

**Reality:** Buffer size is a trade-off, not a requirement.

```go
// All of these work:
buf := make([]byte, 512)   // ✅ Works (more syscalls)
buf := make([]byte, 1024)  // ✅ Works (your current choice)
buf := make([]byte, 4096)  // ✅ Works (recommended - OS page aligned)
buf := make([]byte, 8192)  // ✅ Works (fewer syscalls)
```

**Recommendation:** Use 4096 bytes (4KB)
- Aligns with OS memory page size
- Reduces number of syscalls
- Standard practice in most servers

**Common buffer sizes in the wild:**
```go
// Go's net/http (standard library)
const defaultBufSize = 4096  // 4KB

// io.Copy (standard library)
const copyBufSize = 32 * 1024  // 32KB

// nginx (web server)
const clientBodyBufferSize = 16 * 1024  // 16KB

// Linux kernel default pipe buffer
const pipeBuffSize = 65536  // 64KB
```

---

### Misconception 2: "Only copy to allData when buffer is full"

**Reality:** Must copy after EVERY Read() call!

```go
// ❌ WRONG:
n, _ := conn.Read(buf)
if n == len(buf) {  // Only when full?
    allData = append(allData, buf...)  // ❌ Data loss!
}

// ✅ CORRECT:
n, _ := conn.Read(buf)
if n > 0 {  // After every read!
    allData = append(allData, buf[:n]...)  // ✅ Safe!
}
```

**Why?**
1. Read() returns variable amounts (not always full buffer)
2. Next Read() overwrites buffer
3. Must preserve data immediately

**Visual Example:**

```
Receiving 2.5KB with 1KB buffer:

Iteration 1:
buf = [empty, 1024 bytes capacity]
    ↓
n = 1024 (buffer FULL)
    ↓
allData = append(allData, buf[:1024]...)  ← Copy happens!
allData = [1024 bytes]

Iteration 2:
buf = [old data, will be overwritten]
    ↓
n = 1024 (buffer FULL)
    ↓
allData = append(allData, buf[:1024]...)  ← Copy happens!
allData = [2048 bytes total]

Iteration 3:
buf = [old data, will be overwritten]
    ↓
n = 512 (buffer NOT FULL! Only 512 bytes left)
    ↓
allData = append(allData, buf[:512]...)  ← Copy happens! (Only 512)
allData = [2560 bytes total]

Result:
- 3 Read() calls
- 3 copy operations (every time!)
- Buffer was full: 2 times
- Buffer was partial: 1 time
- Every read triggered a copy! ✅
```

---

### Misconception 3: "Read() waits until buffer is full"

**Reality:** Read() returns as soon as ANY data is available!

```go
buf := make([]byte, 1024)
n, _ := conn.Read(buf)

// n could be:
// - 1 byte (if only 1 byte in kernel buffer)
// - 50 bytes (if 50 bytes in kernel buffer)
// - 1024 bytes (if ≥1024 bytes in kernel buffer)
// - It does NOT wait for 1024 bytes!
```

**Read() behavior:**
```
If kernel buffer has:
├─ 0 bytes → Read() BLOCKS (waits for data)
├─ 1-1023 bytes → Read() returns immediately with n < 1024
└─ ≥1024 bytes → Read() returns immediately with n = 1024
```

**Real-world example (slow network):**
```
Client sends data slowly over slow network:

Time 0ms:   Kernel buffer: [100 bytes]
            ↓
            Read(buf) → n = 100 (buffer NOT full)
            ↓
            allData = append(allData, buf[:100]...)  ← Must copy now!

Time 500ms: Kernel buffer: [200 bytes]
            ↓
            Read(buf) → n = 200 (buffer NOT full)
            ↓
            allData = append(allData, buf[:200]...)  ← Must copy now!

Time 1s:    Kernel buffer: [50 bytes]
            ↓
            Read(buf) → n = 50 (buffer NOT full)
            ↓
            allData = append(allData, buf[:50]...)  ← Must copy now!

Time 1.5s:  Kernel buffer: [150 bytes]
            ↓
            Read(buf) → n = 150 (buffer NOT full)
            ↓
            allData = append(allData, buf[:150]...)  ← Must copy now!

Total: 500 bytes received
Buffer was NEVER full!
But we copied after EVERY read! ✅
```

---

## Buffer Size Performance Comparison

### For 100KB HTTP Request

| Buffer Size | Read() Calls | Syscalls | Relative Performance |
|-------------|-------------|----------|----------------------|
| 512 bytes   | ~200        | 200      | 1.0x (baseline) |
| 1KB         | ~100        | 100      | 2.0x faster |
| 4KB         | ~25         | 25       | 8.0x faster ⭐ |
| 8KB         | ~13         | 13       | 15.4x faster |
| 32KB        | ~4          | 4        | 50x faster |

**Sweet spot:** 4KB (balances performance and memory usage)

**Why 4KB is optimal:**
1. **OS Page Size:** Modern OS uses 4KB memory pages
   ```
   Memory is allocated in 4KB pages
   Using 4KB buffer = 1 page (efficient)
   Using 1KB buffer = 1/4 page (wastes alignment)
   ```

2. **Fewer Syscalls:** Syscalls are expensive (context switch to kernel)
   ```
   Each Read() call:
   - Switch from user mode → kernel mode
   - Copy data from kernel buffer → user buffer
   - Switch from kernel mode → user mode
   
   Time: ~1-5 microseconds per syscall
   With 1KB: 100 syscalls = 100-500µs
   With 4KB: 25 syscalls = 25-125µs
   ```

3. **Memory is Cheap:** 1KB vs 4KB per connection
   ```
   1000 concurrent connections:
   - 1KB buffer: 1MB total
   - 4KB buffer: 4MB total
   
   Difference: 3MB (negligible on modern hardware)
   ```

---

## Recommended Buffer Sizes by Use Case

| Use Case | Buffer Size | Reason |
|----------|-------------|--------|
| **HTTP headers** | 4KB | Headers typically < 8KB |
| **API responses** | 4-8KB | JSON/XML payloads |
| **File uploads** | 32KB | Bulk data transfer |
| **Video streaming** | 32-64KB | High throughput |
| **WebSocket messages** | 4KB | Small frequent messages |
| **Embedded systems** | 512B-1KB | Limited memory |
| **General purpose** | 4KB | ⭐ Best default |

---

## When Read() Returns Less Than Buffer Size

### Example: Receiving "Hello" (5 bytes)

```go
buf := make([]byte, 1024)  // 1KB buffer

n, _ := conn.Read(buf)  // n = 5 (only 5 bytes available)

// Buffer state:
buf = ['H', 'e', 'l', 'l', 'o', ???, ???, ..., ???]
       ↑─────── n=5 ──────↑     ↑───── garbage ────↑
       (valid data)            (uninitialized memory)

// MUST use buf[:n], NOT buf!
allData = append(allData, buf[:5]...)  // ✅ Correct
allData = append(allData, buf...)      // ❌ Wrong! Includes garbage
```

### Visual: buf[:n] vs buf

```
After Read(buf) with n=5:

buf[:5] =  ['H', 'e', 'l', 'l', 'o']              ← What you want
           
buf =      ['H', 'e', 'l', 'l', 'o', 0, 0, ..., 0] ← Includes garbage
           ↑─────── valid ──────↑  ↑──── waste ────↑
                  (5 bytes)           (1019 bytes)
```

**Always use `buf[:n]`!**

---

## Code Example: Correct Buffer Handling

```go
func ParseRequest(conn net.Conn) (*Request, error) {
    var allData []byte
    
    // Allocate buffer ONCE outside loop
    buf := make([]byte, 4096)  // 4KB recommended
    
    for {
        // Read up to 4KB (or whatever is available)
        n, err := conn.Read(buf)
        
        // Copy data immediately if any was read
        if n > 0 {
            // CRITICAL: Copy buf[:n], not buf
            // This copies only the actual data received
            allData = append(allData, buf[:n]...)
            
            // buf will be overwritten on next Read()
            // That's OK - we already copied the data!
        }
        
        // Handle errors
        if err != nil {
            if err == io.EOF {
                break  // Connection closed cleanly
            }
            return nil, err
        }
        
        // Check if we have complete request
        if bytes.Contains(allData, []byte("\r\n\r\n")) {
            break  // Headers complete
        }
    }
    
    return parseHTTPRequest(allData)
}
```

### Why This Works

```
Iteration 1: n=1500
buf = [1500 bytes of data]
    ↓
allData = append(allData, buf[:1500]...)  // Copy 1500 bytes
allData = [1500 bytes]

Iteration 2: n=2000
buf = [2000 bytes of NEW data] ← buf was overwritten!
    ↓
allData = append(allData, buf[:2000]...)  // Copy 2000 bytes
allData = [3500 bytes total]  ← Previous 1500 bytes still safe!

Iteration 3: n=800
buf = [800 bytes of NEW data] ← buf was overwritten again!
    ↓
allData = append(allData, buf[:800]...)  // Copy 800 bytes
allData = [4300 bytes total]  ← All previous data still safe!
```

**Key insight:** `append()` creates a **copy** of the data, so overwriting `buf` doesn't affect `allData`.

---

## Common Mistake: Not Copying Immediately

```go
// ❌ WRONG: Trying to delay copying
var allData []byte
buf := make([]byte, 1024)

n1, _ := conn.Read(buf)  // n1 = 500
// ❌ BUG: Not copying yet, waiting to "optimize"

n2, _ := conn.Read(buf)  // n2 = 524
// ❌ BUG: buf was overwritten! First 500 bytes are LOST!

allData = append(allData, buf[:n1+n2]...)  // ❌ CORRUPTED DATA!
```

**Correct approach:**
```go
// ✅ CORRECT: Copy immediately after every read
var allData []byte
buf := make([]byte, 1024)

n1, _ := conn.Read(buf)  // n1 = 500
allData = append(allData, buf[:n1]...)  // ✅ Copy now!

n2, _ := conn.Read(buf)  // n2 = 524 (overwrites buf)
allData = append(allData, buf[:n2]...)  // ✅ Copy now!

// allData now has 1024 bytes correctly ✅
```

---

## Related Documentation

- [TCP_TIMEOUT_GUIDE.md](TCP_TIMEOUT_GUIDE.md) - Socket timeout implementation
- [KERNEL_TIMEOUT_ATOMICITY.md](KERNEL_TIMEOUT_ATOMICITY.md) - Kernel-level guarantees
- [OPEN_CONNECTION_CONSUMES_MEMORY_AND_FD.md](OPEN_CONNECTION_CONSUMES_MEMORY_AND_FD.md) - Resource consumption
- [internal/protocol/request.go](internal/protocol/request.go) - Request parsing with buffer reuse
- [internal/tcp/conn.go](internal/tcp/conn.go) - TCP connection implementation

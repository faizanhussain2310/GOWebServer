# Video Streaming with HTTP Range Requests - Complete Guide

## Table of Contents
1. [Why Videos Download Instead of Playing](#why-videos-download)
2. [Browser Caching Behavior](#browser-caching)
3. [Request-Response Pattern](#request-response)
4. [Implementation Guide](#implementation)

---

## Why Videos Download Instead of Playing {#why-videos-download}

### Current Behavior vs Expected Behavior

#### What Happens Now (Download)
```
Browser: "GET /static/video.mp4"
         ↓
Server:  "HTTP/1.1 200 OK
          Content-Type: video/mp4
          Content-Length: 41943040
          [entire 40MB video data]"
         ↓
Browser: "This is a big file, let me download it"
         → Downloads to disk
```

#### What Should Happen (Play in Browser)
```
Browser: "GET /static/video.mp4
          Range: bytes=0-"  ← Asking for partial content
         ↓
Server:  "HTTP/1.1 206 Partial Content
          Content-Type: video/mp4
          Content-Range: bytes 0-1048575/41943040
          Accept-Ranges: bytes
          [first 1MB chunk]"
         ↓
Browser: "I can play this!"
         → Plays video inline with seek bar
```

---

### Why Images Work But Videos Don't?

#### Images (Small, Loads Fully)
```
Image: 500KB
Browser:
├─ Downloads entire 500KB quickly
├─ Renders in <img> tag immediately
└─ No need for partial loading

Result: ✅ Shows in browser
```

#### Videos (Large, Needs Streaming)
```
Video: 40MB
Browser:
├─ Sees 40MB size
├─ Server doesn't support Range requests
├─ Must download entire 40MB before playing
└─ Browser treats it as "file to download"

Result: ❌ Downloads instead of playing
```

---

### The Missing Feature: HTTP Range Requests

Modern browsers **require** Range support for video playback to:
1. **Seek** (jump to any part of video)
2. **Stream** (start playing before full download)
3. **Save bandwidth** (only load visible portion)

---

## Browser Caching Behavior {#browser-caching}

### Does Browser Re-download the Same Range?

**Answer: NO!** Browser caches downloaded ranges.

### Browser Memory Structure for Video

```
┌─────────────────────────────────────────────────┐
│          Browser Video Cache                    │
├─────────────────────────────────────────────────┤
│                                                 │
│  Video: /static/video.mp4 (40MB total)         │
│                                                 │
│  Downloaded Ranges (in memory):                 │
│  ┌───────────────────────────────────────┐     │
│  │ Range 1: bytes 0-1048575 (1MB)        │     │
│  │ Range 2: bytes 5242880-6291455 (1MB)  │     │
│  │ Range 3: bytes 20971520-21495807 (512KB) │  │
│  └───────────────────────────────────────┘     │
│                                                 │
│  Total cached: 2.5MB                            │
│                                                 │
└─────────────────────────────────────────────────┘
```

---

### Timeline: User Seeks Back and Forth

```
40MB Video: [0MB ─── 10MB ─── 20MB ─── 30MB ─── 40MB]

t=0s: User starts video
├─ Request: Range: bytes=0-1048575
├─ Downloaded: [0-1MB]
└─ Cached: [0-1MB] ✅

t=10s: User seeks to 50% (20MB)
├─ Check cache: Not present ❌
├─ Request: Range: bytes=20971520-21495807
├─ Downloaded: [20-20.5MB]
└─ Cached: [0-1MB] + [20-20.5MB] ✅

t=15s: User seeks BACK to 10% (4MB)
├─ Request: Range: bytes=4194304-5242879
├─ Downloaded: [4-5MB]
└─ Cached: [0-1MB] + [4-5MB] + [20-20.5MB] ✅

t=20s: User seeks BACK to 50% (20MB) AGAIN!
├─ Check cache: [20-20.5MB] present ✅
├─ Request: NONE! Uses cached data
└─ Cached: [0-1MB] + [4-5MB] + [20-20.5MB] (no change)

Result: NO network request for already-downloaded range!
```

---

### Chrome DevTools Network Tab Example

```
# First time seeking to 50%:
Request #3:
GET /static/video.mp4
Range: bytes=20971520-21495807
Status: 206 Partial Content
Size: 512 KB
Time: 0.3s
From: Network 🌐

# Seeking back to 50% later:
Request: (none)
Status: (from memory cache) 💾
Size: (cached)
Time: 0 ms
From: Memory Cache ✅
```

---

## What Happens When Cache Becomes Full?

### Browser Cache Management Strategy

Browsers use **LRU (Least Recently Used)** eviction:

```
┌─────────────────────────────────────────────────┐
│       Browser Video Cache (Limited Memory)      │
├─────────────────────────────────────────────────┤
│  Max Size: ~50-100MB for video buffers          │
│                                                 │
│  When cache full:                               │
│  1. Keep currently playing range                │
│  2. Keep ahead buffer (next 5-10MB)             │
│  3. Evict oldest/least recently used ranges     │
│                                                 │
└─────────────────────────────────────────────────┘
```

---

### Cache Full Scenario: Step-by-Step

```
User watching 100MB video:

t=0s: Cache empty
├─ Download: [0-5MB]
└─ Cache: [0-5MB] (5MB used)

t=30s: User at 20% (20MB position)
├─ Download: [20-25MB]
├─ Evict: [0-5MB] (too old, unlikely to reuse)
└─ Cache: [20-25MB] (5MB used)

t=60s: User at 50% (50MB position)
├─ Download: [50-55MB]
├─ Keep: [50-55MB] (currently playing)
├─ Keep: [55-60MB] (ahead buffer)
├─ Evict: [20-25MB] (old, far from current position)
└─ Cache: [50-60MB] (10MB used)

t=90s: User seeks BACK to 20% (20MB)
├─ Check cache: [20-25MB] not present ❌ (was evicted!)
├─ Download AGAIN: [20-25MB]
└─ Cache: [20-25MB] + [50-55MB] (evicted [55-60MB])

Summary:
✅ Recently played ranges: Kept in cache
❌ Old ranges far from current position: Evicted
🔄 If user seeks to evicted range: Re-downloaded
```

---

### Browser Cache Priority

```
Priority Levels (High to Low):

1. Currently Playing Range
   └─ bytes 50000000-50524287 (where video is NOW)
   
2. Ahead Buffer (next 5-10MB)
   └─ bytes 50524288-55524287 (for smooth playback)
   
3. Recently Played Ranges
   └─ bytes 45000000-50000000 (in case user rewinds)
   
4. Distant Past Ranges (first to evict)
   └─ bytes 0-10000000 (played long ago)
```

---

### Visual Cache Behavior

```
Video Timeline: [0 ─── 25% ─── 50% ─── 75% ─── 100%]

Downloaded Ranges (green = in cache):
t=0s:   [████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░] 0-10%

t=10s:  [████░░░░░░░░░░██░░░░░░░░░░░░░░░░░░] 0-10%, 50-55%
        User seeks to 50%

t=15s:  [░░░░░░░░░░░░░░██████░░░░░░░░░░░░░░] 50-65%
        Evicted 0-10% (too old)

t=20s:  [░░░░░░░░░░░░░░██████░░░░░░░░░░░░░░] 50-65%
        User seeks back to 50%
        Uses cached data! ✅ No download

t=25s:  [████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░] 0-10%
        User seeks to 5%
        Re-downloads (was evicted) 🔄
```

---

## Request-Response Pattern {#request-response}

### Do We Poll for Range Requests?

**Answer: NO polling!** It's request-driven (on-demand).

### How Range Requests Work: Event-Driven

```
┌─────────────────────────────────────────────────┐
│              Request-Response Flow              │
│            (No Polling, No Websockets)          │
├─────────────────────────────────────────────────┤
│                                                 │
│  Browser Event → HTTP Request → Server Response │
│                                                 │
│  Each user action creates NEW HTTP request      │
│                                                 │
└─────────────────────────────────────────────────┘
```

---

### Detailed Flow: User Interactions

#### Scenario 1: Video Starts Playing

```
User clicks play button
    ↓
Browser JavaScript: video.play()
    ↓
Browser checks buffer
    ↓
Buffer empty! Need data!
    ↓
Browser sends HTTP request:
┌────────────────────────────────────┐
│ GET /static/video.mp4 HTTP/1.1     │
│ Range: bytes=0-1048575             │
└────────────────────────────────────┘
    ↓
Server responds immediately:
┌────────────────────────────────────┐
│ HTTP/1.1 206 Partial Content       │
│ Content-Range: bytes 0-1048575/... │
│ [1MB of data]                      │
└────────────────────────────────────┘
    ↓
Connection CLOSES
    ↓
Browser buffers data
    ↓
Video plays
    ↓
No more requests until buffer runs low
```

**Key Point:** ONE request → ONE response → Connection closes

---

#### Scenario 2: Buffer Running Low (Automatic)

```
Video playing at 5 seconds
    ↓
Browser monitors buffer
    ↓
Buffer has <2 seconds left
    ↓
Browser automatically sends NEW request:
┌────────────────────────────────────┐
│ GET /static/video.mp4 HTTP/1.1     │
│ Range: bytes=1048576-2097151       │
└────────────────────────────────────┘
    ↓
Server responds
    ↓
Connection CLOSES
    ↓
Browser continues playing smoothly
```

**No polling!** Browser decides when to request based on buffer level.

---

#### Scenario 3: User Seeks

```
User drags seek bar to 50%
    ↓
Browser JavaScript: video.currentTime = 300
    ↓
Browser checks: Do I have data for position 50%?
    ↓
Cache miss!
    ↓
Browser sends NEW request:
┌────────────────────────────────────┐
│ GET /static/video.mp4 HTTP/1.1     │
│ Range: bytes=20971520-21495807     │
└────────────────────────────────────┘
    ↓
Server responds
    ↓
Connection CLOSES
    ↓
Video jumps to 50% and plays
```

**User action triggers request!** Not polling.

---

### No Continuous Connection Required

```
Traditional Polling (NOT used here):
┌────────────────────────────────────┐
│ Browser → Server: "Any new data?"  │
│ Server → Browser: "Nope"           │
│ (wait 100ms)                       │
│ Browser → Server: "Any new data?"  │
│ Server → Browser: "Nope"           │
│ (wait 100ms)                       │
│ Browser → Server: "Any new data?"  │
│ Server → Browser: "Yes! Here!"     │
└────────────────────────────────────┘
❌ Wasteful, continuous requests

Range Request (What we use):
┌────────────────────────────────────┐
│ Browser: I need data               │
│     ↓                              │
│ Request → Response                 │
│     ↓                              │
│ Connection closes                  │
│     ↓                              │
│ (silence... no network activity)   │
│     ↓                              │
│ Browser: I need more data          │
│     ↓                              │
│ Request → Response                 │
│     ↓                              │
│ Connection closes                  │
└────────────────────────────────────┘
✅ Efficient, on-demand only
```

---

### Timeline of Network Requests (No Polling)

```
Timeline of Network Requests:

t=0s    Request #1: bytes=0-1MB          ← Initial load
        ↓
        Response received
        ↓
        (silence for 5 seconds)
        ↓
t=5s    Request #2: bytes=1-2MB          ← Buffer low
        ↓
        Response received
        ↓
        (silence for 3 seconds)
        ↓
t=8s    [User seeks to 50%]
        Request #3: bytes=20-21MB         ← User action
        ↓
        Response received
        ↓
        (silence for 4 seconds)
        ↓
t=12s   Request #4: bytes=21-22MB        ← Buffer low
        ↓
        (etc...)

Each request is INDEPENDENT
No continuous connection
No polling loop
Just on-demand requests!
```

---

## Implementation Guide {#implementation}

### Progressive Download Strategy

```
Browser doesn't download sequentially!

Instead:
┌─────────────────────────────────────────┐
│  Smart Buffering Strategy               │
├─────────────────────────────────────────┤
│                                         │
│  1. Download initial chunk (1-2MB)      │
│     → Start playback immediately        │
│                                         │
│  2. Download "ahead buffer" (next 5MB)  │
│     → Smooth playback without pauses    │
│                                         │
│  3. User seeks? Stop current download   │
│     → Request new position via Range    │
│                                         │
│  4. Repeat for new position             │
│                                         │
└─────────────────────────────────────────┘
```

---

### Seek Operation (Detailed)

```
40MB Video File Layout:
[0MB ─── 10MB ─── 20MB ─── 30MB ─── 40MB]
   ↑              ↑
 Playing      User seeks here


Step 1: User drags seek bar to 50%
─────────────────────────────────────
Browser calculates byte position:
50% of 40MB = 20MB = byte 20971520


Step 2: Browser makes NEW request
─────────────────────────────────────
GET /static/video.mp4 HTTP/1.1
Range: bytes=20971520-
       ↑
  "Give me data starting from byte 20971520"


Step 3: Server responds with THAT part
─────────────────────────────────────
HTTP/1.1 206 Partial Content
Content-Range: bytes 20971520-21495807/41943040
                     ↑         ↑         ↑
                   Start      End    Total size
                   (20MB)   (20.5MB)  (40MB)

[512KB of data from byte 20971520 onwards]


Step 4: Browser receives and plays
─────────────────────────────────────
✅ Video plays from 50% mark
✅ Only downloaded 512KB for this seek
✅ Previous 0-20MB data discarded/not needed
```

---

### What Browser Has Downloaded Over Time

```
User watches video:

t=0s (Start playing):
Downloaded: [0-1MB]
Cached in memory: 1MB

t=10s (Still watching from start):
Downloaded: [0-1MB, 1-2MB, 2-3MB, 3-4MB, 4-5MB]
Cached in memory: 5MB
Buffer ahead: 2MB

t=15s (User seeks to 50% = 20MB mark):
Downloaded: [0-5MB] + [20-21MB]  ← Notice the gap!
                        ↑
                  Downloaded on-demand for seek
Cached in memory: 6MB total
Bytes 5-20MB: NEVER downloaded!

t=30s (User closes video):
Total downloaded: 6MB
File size: 40MB
Saved: 34MB of bandwidth! 🎉
```

---

### Current Code (Full File Download)

```go
// Problem: Always sends ENTIRE file
func (fs *FileServer) serveLargeFile(...) error {
    file, _ := os.Open(filePath)
    
    // Sends ENTIRE file (all 40MB)
    io.Copy(conn, file)
    //       ↑     ↑
    //      All bytes from file
    
    return err
}
```

**Problems:**
- No check for `Range` header
- Always sends from byte 0 to EOF
- Browser receives full 40MB
- Browser sees "must download all before seeking"
- Result: Download instead of inline playback

---

### Solution: Add Range Request Support

```go
// With Range support:
func (fs *FileServer) serveLargeFile(conn *tcp.TCPConn, filePath string,
    fileSize int64, version protocol.HTTPVersion, req *protocol.Request) error {
    
    file, _ := os.Open(filePath)
    defer file.Close()
    
    // Check for Range header
    rangeHeader := req.Headers["Range"]
    
    if rangeHeader == "" {
        // No range: Send all (first request)
        return fs.sendFullFile(conn, file, filePath, fileSize, version)
    } else {
        // Range request: Send ONLY requested bytes
        return fs.sendRangeFile(conn, file, filePath, fileSize, version, rangeHeader)
    }
}
```

---

### Implementation: Send Full File

```go
// sendFullFile sends entire file (200 OK)
func (fs *FileServer) sendFullFile(conn *tcp.TCPConn, file *os.File, 
    filePath string, fileSize int64, version protocol.HTTPVersion) error {
    
    resp := protocol.NewResponse(200, "OK", version, "")
    
    resp.Headers["Content-Type"] = getContentType(filePath)
    resp.Headers["Content-Length"] = fmt.Sprintf("%d", fileSize)
    resp.Headers["Accept-Ranges"] = "bytes"  // ← Tell browser we support ranges
    resp.Headers["Cache-Control"] = "public, max-age=3600"
    resp.Headers["Date"] = time.Now().UTC().Format(time.RFC1123)
    resp.Headers["Server"] = "GoWebServer/1.0"

    // Send headers
    headersStr := fmt.Sprintf("%s %d %s\r\n", resp.Version, resp.StatusCode, resp.Status)
    for key, value := range resp.Headers {
        headersStr += fmt.Sprintf("%s: %s\r\n", key, value)
    }
    headersStr += "\r\n"

    if _, err := conn.Write([]byte(headersStr)); err != nil {
        return err
    }

    // Stream entire file
    _, err := io.Copy(conn, file)
    return err
}
```

**Key:** `Accept-Ranges: bytes` header tells browser we support Range requests

---

### Implementation: Send Range File

```go
// sendRangeFile sends partial content (206 Partial Content)
func (fs *FileServer) sendRangeFile(conn *tcp.TCPConn, file *os.File,
    filePath string, fileSize int64, version protocol.HTTPVersion, 
    rangeHeader string) error {
    
    // Parse Range header: "bytes=0-1023" or "bytes=1024-"
    start, end, err := parseRangeHeader(rangeHeader, fileSize)
    if err != nil {
        return fs.sendError(conn, 416, "Range Not Satisfiable", version)
    }

    // Calculate content length for this range
    contentLength := end - start + 1

    // Seek to start position
    if _, err := file.Seek(start, 0); err != nil {
        return fs.sendError(conn, 500, "Error seeking file", version)
    }

    // Build response
    resp := protocol.NewResponse(206, "Partial Content", version, "")
    
    resp.Headers["Content-Type"] = getContentType(filePath)
    resp.Headers["Content-Length"] = fmt.Sprintf("%d", contentLength)
    resp.Headers["Content-Range"] = fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize)
    resp.Headers["Accept-Ranges"] = "bytes"
    resp.Headers["Cache-Control"] = "public, max-age=3600"
    resp.Headers["Date"] = time.Now().UTC().Format(time.RFC1123)
    resp.Headers["Server"] = "GoWebServer/1.0"

    // Send headers
    headersStr := fmt.Sprintf("%s %d %s\r\n", resp.Version, resp.StatusCode, resp.Status)
    for key, value := range resp.Headers {
        headersStr += fmt.Sprintf("%s: %s\r\n", key, value)
    }
    headersStr += "\r\n"

    if _, err := conn.Write([]byte(headersStr)); err != nil {
        return err
    }

    // Stream only the requested range
    _, err = io.CopyN(conn, file, contentLength)
    return err
}
```

**Key:** 
- Status: `206 Partial Content`
- Header: `Content-Range: bytes start-end/total`
- Uses `io.CopyN()` to send only requested bytes

---

### Implementation: Parse Range Header

```go
// parseRangeHeader parses "bytes=start-end" format
func parseRangeHeader(rangeHeader string, fileSize int64) (start, end int64, err error) {
    // Remove "bytes=" prefix
    if !strings.HasPrefix(rangeHeader, "bytes=") {
        return 0, 0, fmt.Errorf("invalid range header")
    }
    
    rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
    parts := strings.Split(rangeSpec, "-")
    
    if len(parts) != 2 {
        return 0, 0, fmt.Errorf("invalid range format")
    }

    // Parse start
    if parts[0] != "" {
        start, err = strconv.ParseInt(parts[0], 10, 64)
        if err != nil {
            return 0, 0, err
        }
    }

    // Parse end
    if parts[1] != "" {
        end, err = strconv.ParseInt(parts[1], 10, 64)
        if err != nil {
            return 0, 0, err
        }
    } else {
        // If end not specified, use file size
        end = fileSize - 1
    }

    // Validate range
    if start < 0 || end >= fileSize || start > end {
        return 0, 0, fmt.Errorf("invalid range values")
    }

    return start, end, nil
}
```

**Handles:**
- `Range: bytes=0-1023` → start=0, end=1023
- `Range: bytes=1024-` → start=1024, end=fileSize-1
- `Range: bytes=-1024` → Invalid (not implemented)

---

## Summary Table

| Question | Answer |
|----------|--------|
| **Why video downloads?** | Server doesn't support Range requests |
| **How to fix?** | Add Range request support (206 status, Content-Range header) |
| **Re-download same range?** | ❌ NO - Browser caches it |
| **Cache location?** | Browser memory (RAM) |
| **Cache size limit?** | ~50-100MB for video buffers |
| **Cache full behavior?** | Evicts old ranges (LRU) |
| **Seek to evicted range?** | Re-downloads automatically |
| **Continuous polling?** | ❌ NO - Event-driven requests |
| **Connection type?** | Request → Response → Close (stateless) |
| **Who initiates request?** | Browser (when needed) |
| **Server maintains state?** | ❌ NO - Each request independent |
| **Required HTTP status?** | 200 OK (full file) or 206 Partial Content (range) |
| **Required headers?** | `Accept-Ranges: bytes`, `Content-Range` (for 206) |

---

## What Enables Video Playback

### Required Server Capabilities

1. ✅ **Accept-Ranges header** - Tell browser we support ranges
2. ✅ **206 Partial Content status** - For range requests
3. ✅ **Content-Range header** - Specify which bytes we're sending
4. ✅ **Correct Content-Type** - `video/mp4`, `video/webm`, etc.
5. ✅ **Efficient seeking** - `file.Seek()` to jump to byte position

### Browser Behavior with Range Support

```
Browser sees: Accept-Ranges: bytes
    ↓
Browser thinks: "Great! I can request specific ranges!"
    ↓
Browser behavior:
├─ Request initial 1-2MB only
├─ Start playback immediately
├─ Show seek bar (user can jump anywhere)
├─ Request more data on-demand
├─ Cache downloaded ranges
└─ Reuse cached data when possible

Result: ✅ Smooth video playback with seek support
```

---

## Performance Benefits

### Without Range Support (Current)
- User experience: Wait for full download
- Bandwidth: Full 40MB downloaded
- Seek: Not possible until 100% downloaded
- Memory: 40MB in browser memory
- Time to play: 10+ seconds

### With Range Support (Proposed)
- User experience: Instant playback
- Bandwidth: ~2-5MB for typical viewing
- Seek: Instant to any position
- Memory: ~5-10MB in browser cache
- Time to play: <1 second

**Bandwidth savings: Up to 90%!** 🚀

---

## Key Insights

1. **Video streaming is on-demand, not sequential**
   - Browser requests only what it needs
   - Seeks don't require downloading intermediate data

2. **Caching is automatic and intelligent**
   - Browser manages cache using LRU
   - Recently used data kept, old data evicted
   - No server-side cache management needed

3. **Stateless HTTP is sufficient**
   - No WebSockets or long-polling needed
   - Each request is independent
   - Server just responds to Range requests

4. **Range support is mandatory for modern video**
   - Without it: Downloads instead of streaming
   - With it: Instant playback with seek support

5. **Implementation is straightforward**
   - Parse Range header
   - Use file.Seek() to jump to position
   - Send only requested bytes with 206 status
   - Add proper headers (Content-Range, Accept-Ranges)

---

## Next Steps

To enable video streaming in your web server:

1. ✅ Understand Range requests (this document)
2. 🔨 Implement `parseRangeHeader()` function
3. 🔨 Implement `sendFullFile()` method (with Accept-Ranges header)
4. 🔨 Implement `sendRangeFile()` method (206 status)
5. 🔨 Update `serveLargeFile()` to check Range header
6. ✅ Test with video file in browser
7. ✅ Verify seek bar works
8. ✅ Check network tab for 206 responses

Your video will play inline with full seek support! 🎬

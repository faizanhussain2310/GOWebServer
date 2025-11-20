# Gzip Compression in HTTP

## Overview

Gzip compression is a widely-used HTTP feature that reduces the size of text-based content before transmission. This document explains how gzip compression works in the GoServer implementation, including server-side compression and browser-side decompression.

## What is Gzip?

Gzip (GNU Zip) is a compression algorithm based on the DEFLATE algorithm, which combines LZ77 (dictionary-based compression) and Huffman coding. It is particularly effective for text-based content that contains repeated patterns.

### Compression Effectiveness

| Content Type | Typical Compression Ratio | Example |
|--------------|--------------------------|---------|
| HTML | 70-80% reduction | 100KB → 20KB |
| CSS | 70-85% reduction | 50KB → 10KB |
| JavaScript | 60-75% reduction | 200KB → 50KB |
| JSON | 80-90% reduction | 100KB → 10KB |
| XML | 75-85% reduction | 80KB → 15KB |
| SVG | 70-80% reduction | 50KB → 10KB |
| Plain Text | 60-70% reduction | 20KB → 6KB |

## When to Use Gzip Compression

### ✅ Should Compress

**Text-Based Content:**
- HTML documents
- CSS stylesheets
- JavaScript files
- JSON API responses
- XML documents
- SVG images
- Plain text files

**Criteria:**
- Content type is text-based
- File size is at least 1KB (smaller files don't benefit)
- Client supports gzip (sends `Accept-Encoding: gzip` header)

### ❌ Should NOT Compress

**Already Compressed Content:**
- JPEG images (`.jpg`, `.jpeg`)
- PNG images (`.png`)
- GIF images (`.gif`)
- WebP images (`.webp`)
- MP4 videos (`.mp4`)
- WebM videos (`.webm`)
- MP3 audio (`.mp3`)
- AAC audio (`.aac`)
- ZIP archives (`.zip`)
- GZIP archives (`.gz`)
- WOFF fonts (`.woff`, `.woff2`)
- PDF documents (`.pdf`)

**Reason:** These formats are already compressed using specialized algorithms. Attempting to gzip them may actually increase the file size or provide negligible benefits while wasting CPU cycles.

## HTTP Gzip Flow

### Complete Request-Response Cycle

```
1. Browser Request:
   ┌──────────────────────────────────────────┐
   │ GET /app.js HTTP/1.1                     │
   │ Host: localhost:8080                     │
   │ Accept-Encoding: gzip, deflate, br       │ ← Browser advertises support
   └──────────────────────────────────────────┘

2. Server Processing:
   ┌──────────────────────────────────────────┐
   │ Check compression criteria:               │
   │ ✓ Client accepts gzip                    │
   │ ✓ Content-Type: application/javascript   │
   │ ✓ File size: 200KB (> 1KB threshold)    │
   │                                          │
   │ Compress:                                │
   │ Original: 200KB                          │
   │ Compressed: 50KB (75% reduction)         │
   └──────────────────────────────────────────┘

3. Server Response:
   ┌──────────────────────────────────────────┐
   │ HTTP/1.1 200 OK                          │
   │ Content-Type: application/javascript     │
   │ Content-Encoding: gzip                   │ ← Indicates compression
   │ Content-Length: 51200                    │ ← Compressed size
   │ Vary: Accept-Encoding                    │ ← Cache instruction
   │                                          │
   │ [gzipped binary data]                    │
   └──────────────────────────────────────────┘

4. Browser Processing:
   ┌──────────────────────────────────────────┐
   │ Receive response                         │
   │ Detect: Content-Encoding: gzip           │
   │ Decompress automatically                 │
   │ Execute decompressed JavaScript          │
   └──────────────────────────────────────────┘
```

## Server-Side Implementation

### Compression Decision Process

```go
// Pseudo-code for compression logic
func shouldCompressResponse(request, response) bool {
    // 1. Check if client accepts gzip
    if !request.Headers["Accept-Encoding"].contains("gzip") {
        return false
    }
    
    // 2. Check if content is already compressed
    if response.Headers["Content-Encoding"] != "" {
        return false
    }
    
    // 3. Check if content type is compressible
    contentType := response.Headers["Content-Type"]
    if !isTextBased(contentType) {
        return false
    }
    
    // 4. Check if content is large enough
    if len(response.Body) < 1024 {  // 1KB
        return false
    }
    
    return true
}
```

### Compression Implementation

The GoServer uses a middleware approach for compression:

```go
// CompressResponse middleware
func CompressResponse(resp *protocol.Response, req *protocol.Request) {
    // Skip if no body
    if resp.Body == "" {
        return
    }
    
    // Skip if already compressed
    if resp.Headers["Content-Encoding"] != "" {
        return
    }
    
    // Check compression criteria
    contentType := resp.Headers["Content-Type"]
    bodyBytes := []byte(resp.Body)
    
    shouldGzip := acceptsGzip(req.Headers["Accept-Encoding"]) &&
                  shouldCompress(contentType) &&
                  len(bodyBytes) >= minSizeForCompression
    
    if !shouldGzip {
        if shouldCompress(contentType) {
            resp.Headers["Vary"] = "Accept-Encoding"
        }
        return
    }
    
    // Compress the content
    compressed, err := compressContent(bodyBytes)
    if err != nil || len(compressed) >= len(bodyBytes) {
        resp.Headers["Vary"] = "Accept-Encoding"
        return
    }
    
    // Update response with compressed content
    resp.Body = string(compressed)
    resp.Headers["Content-Encoding"] = "gzip"
    resp.Headers["Content-Length"] = fmt.Sprintf("%d", len(compressed))
    resp.Headers["Vary"] = "Accept-Encoding"
}
```

### Usage in Handlers

```go
func handleHome(req *protocol.Request) *protocol.Response {
    htmlBytes, _ := os.ReadFile("templates/home.html")
    
    resp := protocol.NewResponse(200, "OK", req.Version, string(htmlBytes))
    resp.Headers["Content-Type"] = "text/html; charset=utf-8"
    
    // Apply compression middleware
    CompressResponse(resp, req)
    
    return resp
}
```

## Browser-Side Decompression

### Automatic Decompression

Browsers automatically handle gzip decompression transparently:

1. **Detection**: Browser checks `Content-Encoding: gzip` header
2. **Decompression**: Browser decompresses the response body
3. **Usage**: Application code receives decompressed content

```javascript
// Browser JavaScript - No manual decompression needed!
fetch('/api/users')
    .then(response => response.json())  // Automatically decompressed
    .then(data => console.log(data));   // Regular JSON object

// Browser automatically:
// 1. Received gzipped response
// 2. Detected Content-Encoding: gzip
// 3. Decompressed the data
// 4. Parsed JSON from decompressed text
```

### Browser Developer Tools View

```
Network Tab:
┌─────────────────────────────────────────────────────┐
│ Name     │ Status │ Type       │ Size  │ Time       │
├─────────────────────────────────────────────────────┤
│ app.js   │ 200    │ javascript │ 50KB  │ 45ms       │
│          │        │            │ 200KB │            │
└─────────────────────────────────────────────────────┘
           ↑                      ↑       ↑
           Status                 │       Decompressed size (what JS engine uses)
                                  │
                                  Transferred size (what went over network)
```

### What Happens Behind the Scenes

```
Step 1: Browser sends request
   HTTP Request Headers:
   Accept-Encoding: gzip, deflate, br

Step 2: Server responds
   HTTP Response Headers:
   Content-Encoding: gzip
   Content-Length: 51200
   
   Response Body:
   [Binary gzipped data: 50KB]

Step 3: Browser network layer
   ┌─────────────────────────────────┐
   │ Network Stack                   │
   │ ├─ Receive 50KB gzipped data    │
   │ ├─ Detect Content-Encoding      │
   │ └─ Decompress to 200KB          │
   └─────────────────────────────────┘

Step 4: Browser application layer
   ┌─────────────────────────────────┐
   │ JavaScript Engine               │
   │ ├─ Receives 200KB text          │
   │ ├─ Parse as JavaScript          │
   │ └─ Execute code                 │
   └─────────────────────────────────┘
```

## Important HTTP Headers

### Content-Encoding

```http
Content-Encoding: gzip
```

**Purpose:** Tells the browser that the response body is compressed using gzip.

**Browser Action:** Automatically decompress the body before passing it to the application.

### Accept-Encoding

```http
Accept-Encoding: gzip, deflate, br
```

**Purpose:** Browser advertises which compression algorithms it supports.

**Common Values:**
- `gzip` - GNU Zip compression
- `deflate` - DEFLATE compression
- `br` - Brotli compression (newer, better compression)
- `identity` - No compression

### Vary

```http
Vary: Accept-Encoding
```

**Purpose:** Instructs caches to store separate versions based on the `Accept-Encoding` header.

**Why Important:** Prevents serving gzipped content to clients that don't support it.

**Example:**
```
Without Vary:
User A (gzip support)   → Cache stores gzipped version
User B (no gzip)        → Cache serves gzipped version ❌ Browser breaks!

With Vary:
User A (gzip support)   → Cache stores: key="/app.js+gzip"
User B (no gzip)        → Cache stores: key="/app.js+identity"
Both users get correct version ✅
```

### Content-Length

```http
Content-Length: 51200
```

**Purpose:** Indicates the size of the compressed body (not the original size).

**Important:** Must be updated after compression to reflect the actual transmitted bytes.

## Performance Benefits

### Bandwidth Savings

```
Example: Serving 200KB JavaScript file to 1000 users

Without Gzip:
├─ Transfer per user: 200KB
├─ Total bandwidth: 200MB
└─ Cost (at $0.10/GB): $0.02

With Gzip (75% compression):
├─ Transfer per user: 50KB
├─ Total bandwidth: 50MB
├─ Savings: 150MB (75%)
└─ Cost (at $0.10/GB): $0.005 (75% savings)
```

### Load Time Improvements

```
User with 10 Mbps connection:

Without Gzip:
├─ Download 200KB JavaScript
└─ Time: 160ms

With Gzip:
├─ Download 50KB (compressed)
├─ Network time: 40ms
├─ Decompression time: 5ms
└─ Total time: 45ms

Improvement: 72% faster! (160ms → 45ms)
```

### Mobile Performance

Mobile connections benefit even more from compression:

```
User with 3G connection (2 Mbps):

Without Gzip:
├─ Download 200KB JavaScript
└─ Time: 800ms

With Gzip:
├─ Download 50KB (compressed)
├─ Network time: 200ms
├─ Decompression time: 5ms
└─ Total time: 205ms

Improvement: 74% faster! (800ms → 205ms)
```

## Compression Levels

Gzip supports compression levels from 1 (fastest) to 9 (best compression):

```go
// Default compression (level 6)
gz := gzip.NewWriter(&buf)

// Fast compression (level 1)
gz, _ := gzip.NewWriterLevel(&buf, gzip.BestSpeed)

// Best compression (level 9)
gz, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
```

### Compression Level Trade-offs

| Level | Speed | Compression Ratio | CPU Usage | Use Case |
|-------|-------|------------------|-----------|----------|
| 1 | Fastest | 50-60% | Low | High-traffic sites, real-time |
| 6 | Balanced | 65-75% | Medium | General purpose (default) |
| 9 | Slowest | 70-80% | High | Static files, pre-compression |

**GoServer uses default level (6)** - Good balance between speed and compression ratio.

## Testing Gzip Compression

### Test 1: Verify Compression is Working

```bash
# Request with gzip support
curl -I http://localhost:8080/api/users \
  -H "Accept-Encoding: gzip"

# Expected response:
HTTP/1.1 200 OK
Content-Type: application/json
Content-Encoding: gzip          ← Compression active
Content-Length: 45              ← Compressed size
Vary: Accept-Encoding           ← Cache instruction
```

### Test 2: Compare Sizes

```bash
# Without compression
curl http://localhost:8080/api/users \
  -o uncompressed.json

# With compression (still readable after automatic decompression)
curl http://localhost:8080/api/users \
  -H "Accept-Encoding: gzip" \
  -o compressed.json

# Get raw compressed data
curl http://localhost:8080/api/users \
  -H "Accept-Encoding: gzip" \
  --compressed -o raw-compressed.gz

# Compare sizes
ls -lh uncompressed.json    # e.g., 120 bytes
ls -lh raw-compressed.gz    # e.g., 45 bytes (62% smaller)
```

### Test 3: Verify Content is Identical

```bash
# Download with compression
curl http://localhost:8080/api/users \
  -H "Accept-Encoding: gzip" \
  -o with-gzip.json

# Download without compression
curl http://localhost:8080/api/users \
  -o without-gzip.json

# Compare (should be identical after decompression)
diff with-gzip.json without-gzip.json
# (no output = files are identical)
```

### Test 4: Check Response Headers

```bash
# Verbose output to see all headers
curl -v http://localhost:8080/static/test.html \
  -H "Accept-Encoding: gzip"

# Look for:
# < Content-Encoding: gzip       ← Compression used
# < Vary: Accept-Encoding        ← Cache instruction
# < Content-Length: 250          ← Compressed size
```

## Common Issues and Solutions

### Issue 1: Content-Length Mismatch

**Problem:**
```
Content-Length header shows original size, not compressed size
Browser shows progress bar incorrectly
```

**Solution:**
```go
// Always update Content-Length after compression
resp.Headers["Content-Length"] = fmt.Sprintf("%d", len(compressed))
```

### Issue 2: Double Compression

**Problem:**
```
Content compressed twice, resulting in corrupted data
```

**Solution:**
```go
// Check if already compressed
if resp.Headers["Content-Encoding"] != "" {
    return  // Skip compression
}
```

### Issue 3: Missing Vary Header

**Problem:**
```
Cache serves gzipped content to clients without gzip support
Browser can't decompress, shows binary data
```

**Solution:**
```go
// Always add Vary header for compressible content
resp.Headers["Vary"] = "Accept-Encoding"
```

### Issue 4: Compressing Already-Compressed Files

**Problem:**
```
Trying to gzip images/videos
Wasting CPU cycles
Sometimes increasing file size
```

**Solution:**
```go
// Check content type before compressing
if !shouldCompress(contentType) {
    return  // Don't compress images, videos, etc.
}
```

## Best Practices

### 1. Always Check Accept-Encoding

```go
// Don't compress if client doesn't support it
if !acceptsGzip(req.Headers["Accept-Encoding"]) {
    return
}
```

### 2. Set Minimum Size Threshold

```go
// Don't compress very small files (overhead not worth it)
const minSizeForCompression = 1024  // 1KB
if len(content) < minSizeForCompression {
    return
}
```

### 3. Only Compress Text-Based Content

```go
// Check content type
compressibleTypes := []string{
    "text/html",
    "text/css",
    "application/json",
    "application/javascript",
    // ... other text types
}
```

### 4. Always Add Vary Header

```go
// Even if not compressing, add Vary for cache correctness
if shouldCompress(contentType) {
    resp.Headers["Vary"] = "Accept-Encoding"
}
```

### 5. Update Content-Length

```go
// After compression, update the length
resp.Headers["Content-Length"] = fmt.Sprintf("%d", len(compressed))
```

### 6. Handle Compression Errors

```go
// If compression fails or doesn't reduce size, send original
compressed, err := compressContent(content)
if err != nil || len(compressed) >= len(content) {
    // Send uncompressed
    return
}
```

## Pre-Compression Strategy

For static files that don't change, consider pre-compressing:

```bash
# Pre-compress static files
gzip -k -9 public/static/app.js
# Creates: public/static/app.js.gz

gzip -k -9 public/static/style.css
# Creates: public/static/style.css.gz
```

**Server logic:**
```go
// Check for pre-compressed version
gzPath := filePath + ".gz"
if _, err := os.Stat(gzPath); err == nil && acceptsGzip(req) {
    // Serve pre-compressed file
    return servePrecompressedFile(gzPath, contentType, conn, req)
}
```

**Benefits:**
- Zero CPU overhead (no compression at request time)
- Best compression (can use level 9)
- Faster response times

**Drawbacks:**
- Need to regenerate on file changes
- Double disk space usage

## Summary

### Key Points

1. **Gzip compresses text-based content** (HTML, CSS, JS, JSON) by 60-90%
2. **Server compresses** based on `Accept-Encoding` header
3. **Browser decompresses** automatically and transparently
4. **Critical headers:**
   - `Content-Encoding: gzip` (indicates compression)
   - `Vary: Accept-Encoding` (for cache correctness)
   - `Content-Length` (compressed size)
5. **Don't compress** already-compressed files (images, videos)
6. **Minimum size threshold** (1KB) to avoid unnecessary overhead
7. **Performance benefits:** Faster load times, reduced bandwidth costs

### Compression Checklist

- ✅ Check `Accept-Encoding` header
- ✅ Verify content type is compressible
- ✅ Ensure content is at least 1KB
- ✅ Compress only if it reduces size
- ✅ Update `Content-Encoding` header
- ✅ Update `Content-Length` header
- ✅ Add `Vary: Accept-Encoding` header
- ✅ Don't compress already-compressed formats
- ✅ Handle compression errors gracefully

### Performance Impact

```
Typical web page:
├─ HTML: 50KB → 10KB (80% savings)
├─ CSS: 30KB → 6KB (80% savings)
├─ JavaScript: 200KB → 50KB (75% savings)
└─ Total: 280KB → 66KB (76% savings)

Result: Page loads 3-4x faster on typical connections!
```

Gzip compression is one of the most effective performance optimizations for web servers, providing significant bandwidth savings and improved user experience with minimal implementation complexity.

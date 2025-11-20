package handler

import (
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"webserver/internal/protocol"
	"webserver/internal/tcp"
)

// MaxInMemorySize is the threshold for switching between in-memory and streaming
// Files larger than 1MB will be streamed to save memory
const MaxInMemorySize = 1024 * 1024 // 1MB

// FileServer serves static files from a directory
type FileServer struct {
	root string // Root directory for static files
}

// NewFileServer creates a new file server with the given root directory
func NewFileServer(root string) *FileServer {
	return &FileServer{
		root: root,
	}
}

// ServeFile serves a static file (used for small files via Response object)
// This method is kept as a fallback for the current router architecture
// which expects handlers to return *protocol.Response objects
func (fs *FileServer) ServeFile(req *protocol.Request) *protocol.Response {
	// Remove any query parameters from the path
	path := strings.Split(req.Path, "?")[0]

	// Clean the path to prevent directory traversal attacks
	cleanPath := filepath.Clean(path)

	// Prevent directory traversal outside root
	if strings.Contains(cleanPath, "..") {
		return protocol.NewResponse(403, "Forbidden", req.Version, "403 - Forbidden")
	}

	// Build full file path
	filePath := filepath.Join(fs.root, cleanPath)

	// Check if file exists
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return protocol.NewResponse(404, "Not Found", req.Version, "404 - File Not Found")
		}
		return protocol.NewResponse(500, "Internal Server Error", req.Version, "500 - Internal Server Error")
	}

	// If it's a directory, try to serve index.html
	if fileInfo.IsDir() {
		indexPath := filepath.Join(filePath, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			filePath = indexPath
		} else {
			// Directory listing disabled for security
			return protocol.NewResponse(403, "Forbidden", req.Version, "403 - Directory listing disabled")
		}
	}

	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return protocol.NewResponse(500, "Internal Server Error", req.Version, "500 - Error reading file")
	}

	// Create response
	resp := protocol.NewResponse(200, "OK", req.Version, string(content))

	// Set Content-Type based on file extension
	contentType := getContentType(filePath)
	resp.Headers["Content-Type"] = contentType

	// Add cache control headers
	resp.Headers["Cache-Control"] = "public, max-age=3600"

	return resp
}

// ServeFileStream serves a file with intelligent streaming based on size
// Small files (<1MB): Loaded in memory for speed
// Large files (>1MB): Streamed to save memory
func (fs *FileServer) ServeFileStream(req *protocol.Request, conn *tcp.TCPConn, keepAlive bool, remainingRequests int) error {
	// Remove any query parameters from the path
	path := strings.Split(req.Path, "?")[0]

	// Clean the path to prevent directory traversal attacks
	cleanPath := filepath.Clean(path)

	// Prevent directory traversal outside root
	if strings.Contains(cleanPath, "..") {
		return fs.sendError(conn, 403, "Forbidden", req.Version, keepAlive, remainingRequests)
	}

	// Build full file path
	filePath := filepath.Join(fs.root, cleanPath)

	// Check if file exists
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fs.sendError(conn, 404, "Not Found", req.Version, keepAlive, remainingRequests)
		}
		return fs.sendError(conn, 500, "Internal Server Error", req.Version, keepAlive, remainingRequests)
	}

	// If it's a directory, try to serve index.html
	if fileInfo.IsDir() {
		indexPath := filepath.Join(filePath, "index.html")
		if newInfo, err := os.Stat(indexPath); err == nil {
			filePath = indexPath
			fileInfo = newInfo
		} else {
			return fs.sendError(conn, 403, "Directory listing disabled", req.Version, keepAlive, remainingRequests)
		}
	}

	// Check If-Modified-Since header for caching (304 Not Modified)
	modTime := fileInfo.ModTime()
	if !modTime.IsZero() {
		if ifModifiedSince := req.Headers["If-Modified-Since"]; ifModifiedSince != "" {
			// Parse client's cached date
			ifModTime, err := parseHTTPTime(ifModifiedSince)
			if err == nil {
				// Truncate to seconds (HTTP time format precision)
				modTime = modTime.Truncate(time.Second)
				ifModTime = ifModTime.Truncate(time.Second)

				// If file hasn't been modified since client's cached version
				if !modTime.After(ifModTime) {
					// Return 304 Not Modified (no body - saves bandwidth!)
					return fs.sendNotModified(conn, modTime, req.Version, keepAlive, remainingRequests)
				}
			}
		}
	}

	fileSize := fileInfo.Size()

	// Decision: Small file (load in memory) or large file (stream)?
	if fileSize <= MaxInMemorySize {
		// Small file: Use in-memory approach (fast for small files)
		return fs.serveSmallFile(conn, filePath, fileSize, modTime, req.Version, req, keepAlive, remainingRequests)
	} else {
		// Large file: Use streaming with Range support (memory-efficient)
		return fs.serveLargeFile(req, conn, filePath, fileSize, modTime, req.Version, keepAlive, remainingRequests)
	}
}

// serveSmallFile loads entire file in memory (fast for small files <1MB)
// Supports gzip compression for text-based content types via CompressResponse middleware
func (fs *FileServer) serveSmallFile(conn *tcp.TCPConn, filePath string, fileSize int64, modTime time.Time, version protocol.HTTPVersion, req *protocol.Request, keepAlive bool, remainingRequests int) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fs.sendError(conn, 500, "Error reading file", version, keepAlive, remainingRequests)
	}

	contentType := getContentType(filePath)

	// Create response object
	resp := protocol.NewResponse(200, "OK", version, string(content))
	resp.Headers["Content-Type"] = contentType
	resp.Headers["Accept-Ranges"] = "bytes"                            // Critical: tells browser Range requests are supported
	resp.Headers["Last-Modified"] = modTime.UTC().Format(time.RFC1123) // Enable caching
	resp.Headers["Cache-Control"] = "public, max-age=3600"
	resp.Headers["Date"] = time.Now().UTC().Format(time.RFC1123)
	resp.Headers["Server"] = "GoWebServer/1.0"

	// Set Connection headers for keep-alive
	if keepAlive {
		resp.Headers["Connection"] = "keep-alive"
		resp.Headers["Keep-Alive"] = fmt.Sprintf("timeout=30, max=%d", remainingRequests)
	} else {
		resp.Headers["Connection"] = "close"
	}

	// Apply gzip compression using middleware (handles all compression logic)
	CompressResponse(resp, req)

	// Convert response object to string and send
	responseStr := fmt.Sprintf("%s %d %s\r\n", resp.Version, resp.StatusCode, resp.Status)
	for key, value := range resp.Headers {
		responseStr += fmt.Sprintf("%s: %s\r\n", key, value)
	}
	responseStr += "\r\n" + resp.Body

	_, err = conn.Write([]byte(responseStr))
	return err
}

// serveLargeFile streams file directly to connection with Range request support
// Supports partial content delivery (206) for video/audio seeking and resume downloads
// Works for ALL file types, not just video/audio
func (fs *FileServer) serveLargeFile(req *protocol.Request, conn *tcp.TCPConn, filePath string, fileSize int64, modTime time.Time, version protocol.HTTPVersion, keepAlive bool, remainingRequests int) error {
	// Open file (doesn't load into memory!)
	file, err := os.Open(filePath)
	if err != nil {
		return fs.sendError(conn, 500, "Error opening file", version, keepAlive, remainingRequests)
	}
	defer file.Close()

	// Check if client requests a specific range
	rangeHeader := req.Headers["Range"]
	if rangeHeader == "" {
		// No Range header - send full file with Accept-Ranges header
		return fs.sendFullFile(file, filePath, fileSize, modTime, version, conn, keepAlive, remainingRequests)
	}

	// Parse Range header (e.g., "bytes=0-1023")
	start, end, err := parseRangeHeader(rangeHeader, fileSize)
	if err != nil {
		// Invalid range - send 416 Range Not Satisfiable
		resp := protocol.NewResponse(416, "Range Not Satisfiable", version, "")
		resp.Headers["Content-Range"] = fmt.Sprintf("bytes */%d", fileSize)
		resp.Headers["Date"] = time.Now().UTC().Format(time.RFC1123)
		resp.Headers["Server"] = "GoWebServer/1.0"

		// Set Connection headers
		if keepAlive {
			resp.Headers["Connection"] = "keep-alive"
			resp.Headers["Keep-Alive"] = fmt.Sprintf("timeout=30, max=%d", remainingRequests)
		} else {
			resp.Headers["Connection"] = "close"
		}

		headersStr := fmt.Sprintf("%s %d %s\r\n", resp.Version, resp.StatusCode, resp.Status)
		for key, value := range resp.Headers {
			headersStr += fmt.Sprintf("%s: %s\r\n", key, value)
		}
		headersStr += "\r\n"

		if _, writeErr := conn.Write([]byte(headersStr)); writeErr != nil {
			return writeErr
		}
		return err
	}

	// Send requested range (206 Partial Content)
	return fs.sendRangeFile(file, start, end, filePath, fileSize, modTime, version, conn, keepAlive, remainingRequests)
}

// parseRangeHeader parses HTTP Range header (e.g., "bytes=0-1023")
// Returns start and end byte positions (inclusive)
func parseRangeHeader(rangeHeader string, fileSize int64) (int64, int64, error) {
	// Expected format: "bytes=start-end" or "bytes=start-"
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return 0, 0, fmt.Errorf("invalid range header format")
	}

	// Remove "bytes=" prefix
	rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")

	// Split on '-'
	parts := strings.Split(rangeSpec, "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid range specification")
	}

	// Parse start
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid start value")
	}

	// Parse end (or use fileSize-1 if not specified)
	var end int64
	if parts[1] == "" {
		end = fileSize - 1
	} else {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid end value")
		}
	}

	// Validate range
	if start < 0 || end >= fileSize || start > end {
		return 0, 0, fmt.Errorf("range out of bounds")
	}

	return start, end, nil
}

// sendFullFile sends the complete file with Accept-Ranges header
func (fs *FileServer) sendFullFile(file *os.File, filePath string, fileSize int64, modTime time.Time, version protocol.HTTPVersion, conn *tcp.TCPConn, keepAlive bool, remainingRequests int) error {
	resp := protocol.NewResponse(200, "OK", version, "")

	// Set headers
	resp.Headers["Content-Type"] = getContentType(filePath)
	resp.Headers["Content-Length"] = fmt.Sprintf("%d", fileSize)
	resp.Headers["Accept-Ranges"] = "bytes"                            // Critical: tells browser Range requests are supported
	resp.Headers["Last-Modified"] = modTime.UTC().Format(time.RFC1123) // Enable caching
	resp.Headers["Cache-Control"] = "public, max-age=3600"
	resp.Headers["Date"] = time.Now().UTC().Format(time.RFC1123)
	resp.Headers["Server"] = "GoWebServer/1.0"

	// Set Connection headers for keep-alive
	if keepAlive {
		resp.Headers["Connection"] = "keep-alive"
		resp.Headers["Keep-Alive"] = fmt.Sprintf("timeout=30, max=%d", remainingRequests)
	} else {
		resp.Headers["Connection"] = "close"
	}

	// Build and send headers
	headersStr := fmt.Sprintf("%s %d %s\r\n", resp.Version, resp.StatusCode, resp.Status)
	for key, value := range resp.Headers {
		headersStr += fmt.Sprintf("%s: %s\r\n", key, value)
	}
	headersStr += "\r\n"

	if _, err := conn.Write([]byte(headersStr)); err != nil {
		return err
	}

	// Stream full file content
	_, err := io.Copy(conn, file)
	return err
}

// sendRangeFile sends a partial file content (206 Partial Content)
// Used for video seeking, audio playback, and resume downloads
func (fs *FileServer) sendRangeFile(file *os.File, start, end int64, filePath string, fileSize int64, modTime time.Time, version protocol.HTTPVersion, conn *tcp.TCPConn, keepAlive bool, remainingRequests int) error {
	// Seek to start position
	if _, err := file.Seek(start, 0); err != nil {
		return err
	}

	contentLength := end - start + 1

	// Create 206 Partial Content response
	resp := protocol.NewResponse(206, "Partial Content", version, "")

	// Set headers
	resp.Headers["Content-Type"] = getContentType(filePath)
	resp.Headers["Content-Length"] = fmt.Sprintf("%d", contentLength)
	resp.Headers["Content-Range"] = fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize)
	resp.Headers["Accept-Ranges"] = "bytes"
	resp.Headers["Last-Modified"] = modTime.UTC().Format(time.RFC1123) // Enable caching
	resp.Headers["Cache-Control"] = "public, max-age=3600"
	resp.Headers["Date"] = time.Now().UTC().Format(time.RFC1123)
	resp.Headers["Server"] = "GoWebServer/1.0"

	// Set Connection headers for keep-alive
	if keepAlive {
		resp.Headers["Connection"] = "keep-alive"
		resp.Headers["Keep-Alive"] = fmt.Sprintf("timeout=30, max=%d", remainingRequests)
	} else {
		resp.Headers["Connection"] = "close"
	}

	// Build and send headers
	headersStr := fmt.Sprintf("%s %d %s\r\n", resp.Version, resp.StatusCode, resp.Status)
	for key, value := range resp.Headers {
		headersStr += fmt.Sprintf("%s: %s\r\n", key, value)
	}
	headersStr += "\r\n"

	if _, err := conn.Write([]byte(headersStr)); err != nil {
		return err
	}

	// Stream only the requested range using io.CopyN
	// This reads exactly contentLength bytes from the file
	_, err := io.CopyN(conn, file, contentLength)
	return err
}

// sendError sends an error response
func (fs *FileServer) sendError(conn *tcp.TCPConn, code int, status string, version protocol.HTTPVersion, keepAlive bool, remainingRequests int) error {
	body := fmt.Sprintf("%d - %s", code, status)

	// Create response object
	resp := protocol.NewResponse(code, status, version, body)

	// Set headers
	resp.Headers["Content-Type"] = "text/plain"
	resp.Headers["Content-Length"] = fmt.Sprintf("%d", len(body))
	resp.Headers["Date"] = time.Now().UTC().Format(time.RFC1123)
	resp.Headers["Server"] = "GoWebServer/1.0"

	// Set Connection headers for keep-alive
	if keepAlive {
		resp.Headers["Connection"] = "keep-alive"
		resp.Headers["Keep-Alive"] = fmt.Sprintf("timeout=30, max=%d", remainingRequests)
	} else {
		resp.Headers["Connection"] = "close"
	}

	// Convert response object to string and send
	responseStr := fmt.Sprintf("%s %d %s\r\n", resp.Version, resp.StatusCode, resp.Status)
	for key, value := range resp.Headers {
		responseStr += fmt.Sprintf("%s: %s\r\n", key, value)
	}
	responseStr += "\r\n" + resp.Body

	_, err := conn.Write([]byte(responseStr))
	return err
}

// sendNotModified sends a 304 Not Modified response (no body)
func (fs *FileServer) sendNotModified(conn *tcp.TCPConn, modTime time.Time, version protocol.HTTPVersion, keepAlive bool, remainingRequests int) error {
	resp := protocol.NewResponse(304, "Not Modified", version, "")

	// Set headers (no Content-Length or Content-Type for 304)
	resp.Headers["Last-Modified"] = modTime.UTC().Format(time.RFC1123)
	resp.Headers["Cache-Control"] = "public, max-age=3600"
	resp.Headers["Date"] = time.Now().UTC().Format(time.RFC1123)
	resp.Headers["Server"] = "GoWebServer/1.0"

	// Set Connection headers for keep-alive
	if keepAlive {
		resp.Headers["Connection"] = "keep-alive"
		resp.Headers["Keep-Alive"] = fmt.Sprintf("timeout=30, max=%d", remainingRequests)
	} else {
		resp.Headers["Connection"] = "close"
	}

	// Build and send headers (no body!)
	headersStr := fmt.Sprintf("%s %d %s\r\n", resp.Version, resp.StatusCode, resp.Status)
	for key, value := range resp.Headers {
		headersStr += fmt.Sprintf("%s: %s\r\n", key, value)
	}
	headersStr += "\r\n" // No body follows

	_, err := conn.Write([]byte(headersStr))
	return err
}

// parseHTTPTime parses HTTP date formats (RFC1123, RFC850, ANSI C)
func parseHTTPTime(timeStr string) (time.Time, error) {
	// Try RFC1123 (most common): "Mon, 02 Jan 2006 15:04:05 GMT"
	if t, err := time.Parse(time.RFC1123, timeStr); err == nil {
		return t, nil
	}

	// Try RFC850: "Monday, 02-Jan-06 15:04:05 GMT"
	if t, err := time.Parse(time.RFC850, timeStr); err == nil {
		return t, nil
	}

	// Try ANSI C: "Mon Jan  2 15:04:05 2006"
	if t, err := time.Parse(time.ANSIC, timeStr); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("invalid time format")
}

// getContentType returns the MIME type based on file extension
func getContentType(filePath string) string {
	ext := filepath.Ext(filePath)

	// Use Go's standard library to detect MIME type
	contentType := mime.TypeByExtension(ext)

	// If no type found, default to binary data
	if contentType == "" {
		return "application/octet-stream"
	}

	return contentType
}

// HandleStaticFile creates a handler function for serving static files
// This is kept for backward compatibility with response-based routing
func HandleStaticFile(rootDir string) func(*protocol.Request) *protocol.Response {
	fs := NewFileServer(rootDir)
	return func(req *protocol.Request) *protocol.Response {
		return fs.ServeFile(req)
	}
}

// HandleStaticFileStream creates a streaming handler function for serving static files
// Uses intelligent size-based routing: small files (<1MB) loaded in memory, large files (>1MB) streamed
func HandleStaticFileStream(rootDir string) func(*protocol.Request, *tcp.TCPConn, bool, int) error {
	fs := NewFileServer(rootDir)
	return func(req *protocol.Request, conn *tcp.TCPConn, keepAlive bool, remainingRequests int) error {
		return fs.ServeFileStream(req, conn, keepAlive, remainingRequests)
	}
}

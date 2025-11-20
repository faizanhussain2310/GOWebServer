package handler

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"strings"
	"webserver/internal/protocol"
)

// minSizeForCompression is the minimum size to bother compressing
// Files smaller than this won't benefit much from compression
const minSizeForCompression = 1024 // 1KB

// shouldCompress checks if content type should be compressed
// Returns true for text-based content types that benefit from compression
func shouldCompress(contentType string) bool {
	compressibleTypes := []string{
		"text/html",
		"text/css",
		"text/javascript",
		"text/plain",
		"text/xml",
		"application/json",
		"application/javascript",
		"application/xml",
		"application/xml+rss",
		"application/xhtml+xml",
		"image/svg+xml",
	}

	// Normalize content type (remove charset and whitespace)
	ct := strings.ToLower(strings.Split(contentType, ";")[0])
	ct = strings.TrimSpace(ct)

	for _, compressible := range compressibleTypes {
		if ct == compressible {
			return true
		}
	}

	return false
}

// acceptsGzip checks if client accepts gzip encoding
// Parses the Accept-Encoding header to look for "gzip"
func acceptsGzip(acceptEncoding string) bool {
	if acceptEncoding == "" {
		return false
	}

	// Accept-Encoding: gzip, deflate, br
	encodings := strings.Split(strings.ToLower(acceptEncoding), ",")
	for _, encoding := range encodings {
		if strings.TrimSpace(encoding) == "gzip" || strings.HasPrefix(strings.TrimSpace(encoding), "gzip;") {
			return true
		}
	}

	return false
}

// compressContent compresses content using gzip
// Returns the compressed bytes or an error
func compressContent(content []byte) ([]byte, error) {
	var buf bytes.Buffer

	// Create gzip writer with default compression level
	gz := gzip.NewWriter(&buf)

	// Write content
	if _, err := gz.Write(content); err != nil {
		gz.Close()
		return nil, err
	}

	// Close to flush remaining data
	if err := gz.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// CompressResponse applies gzip compression to a response if appropriate
// This is a reusable middleware-style function that:
// 1. Checks if client accepts gzip
// 2. Checks if content type is compressible
// 3. Checks if content is large enough to benefit from compression
// 4. Compresses and updates the response headers
//
// Usage:
//
//	resp := protocol.NewResponse(200, "OK", req.Version, body)
//	resp.Headers["Content-Type"] = "application/json"
//	CompressResponse(resp, req)  // Automatically compresses if beneficial
//	return resp
func CompressResponse(resp *protocol.Response, req *protocol.Request) {
	// Skip if no body
	if resp.Body == "" {
		return
	}

	// Skip if already compressed
	if resp.Headers["Content-Encoding"] != "" {
		return
	}

	// Get content type (default to text/plain if not set)
	contentType := resp.Headers["Content-Type"]
	if contentType == "" {
		contentType = "text/plain"
	}

	// Check compression criteria
	bodyBytes := []byte(resp.Body)
	shouldGzip := acceptsGzip(req.Headers["Accept-Encoding"]) &&
		shouldCompress(contentType) &&
		len(bodyBytes) >= minSizeForCompression

	if !shouldGzip {
		// Set Vary header even if not compressing (for cache correctness)
		if shouldCompress(contentType) {
			resp.Headers["Vary"] = "Accept-Encoding"
		}
		return
	}

	// Compress the content
	compressed, err := compressContent(bodyBytes)
	if err != nil || len(compressed) >= len(bodyBytes) {
		// Compression failed or didn't reduce size
		// Set Vary header for cache correctness
		resp.Headers["Vary"] = "Accept-Encoding"
		return
	}

	// Update response with compressed content
	resp.Body = string(compressed)
	resp.Headers["Content-Encoding"] = "gzip"
	resp.Headers["Content-Length"] = fmt.Sprintf("%d", len(compressed))
	resp.Headers["Vary"] = "Accept-Encoding"
}

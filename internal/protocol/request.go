package protocol

import (
	"bytes"
	"fmt"
	"strings"
	"webserver/internal/tcp"
)

const (
	MaxRequestSize = 1048576 // 1MB total request limit
	MaxHeaderSize  = 16384   // 16KB header limit
)

type Request struct {
	Method  string
	Path    string
	Version HTTPVersion
	Headers map[string]string
	Body    string
}

func ParseRequest(conn *tcp.TCPConn) (*Request, error) {
	var allData []byte
	buf := make([]byte, 4096)

	// Read until we have at least headers
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return nil, err
		}
		allData = append(allData, buf[:n]...)

		// SECURITY CHECK: Limit total request size
		if len(allData) > MaxRequestSize {
			return nil, fmt.Errorf("request size exceeded limit")
		}

		// SECURITY CHECK: Limit header section size
		// We check this before the header terminator is found
		if len(allData) > MaxHeaderSize && !bytes.Contains(allData, []byte("\r\n\r\n")) {
			return nil, fmt.Errorf("request header size exceeded limit")
		}

		// EFFICIENT CHECK: Use bytes.Contains()
		// Check if we have complete headers (double CRLF)
		if bytes.Contains(allData, []byte("\r\n\r\n")) {
			break
		}

		// Also check for single LF (some clients)
		if bytes.Contains(allData, []byte("\n\n")) {
			break
		}
	}

	// Split headers and body using byte slices
	var headerSectionBytes, bodySectionBytes []byte
	if idx := bytes.Index(allData, []byte("\r\n\r\n")); idx != -1 {
		headerSectionBytes = allData[:idx]
		bodySectionBytes = allData[idx+4:]
	} else if idx := bytes.Index(allData, []byte("\n\n")); idx != -1 {
		headerSectionBytes = allData[:idx]
		bodySectionBytes = allData[idx+2:]
	} else {
		return nil, fmt.Errorf("invalid request format")
	}

	// Convert header bytes to string once, then split
	headerSection := string(headerSectionBytes)
	lines := strings.Split(headerSection, "\n")
	if len(lines) < 1 {
		return nil, fmt.Errorf("invalid request")
	}

	// Parse request line
	requestLine := strings.TrimSpace(lines[0])
	parts := strings.Fields(requestLine)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid request line")
	}

	req := &Request{
		Method:  parts[0],
		Path:    parts[1],
		Version: HTTPVersion(parts[2]),
		Headers: make(map[string]string),
	}

	// Parse headers
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		headerParts := strings.SplitN(line, ":", 2)
		if len(headerParts) == 2 {
			req.Headers[strings.TrimSpace(headerParts[0])] = strings.TrimSpace(headerParts[1])
		}
	}

	// Handle request body
	if contentLength, ok := req.Headers["Content-Length"]; ok {
		var expectedLength int
		fmt.Sscanf(contentLength, "%d", &expectedLength)

		if expectedLength > 0 {
			// Calculate how many bytes we've already read from the header read
			currentBodyBytes := bodySectionBytes

			// Read loop to fetch remaining body data
			for len(currentBodyBytes) < expectedLength {
				n, err := conn.Read(buf)
				if err != nil {
					return nil, err
				}
				currentBodyBytes = append(currentBodyBytes, buf[:n]...)
			}

			// Final assignment: Trim the body to the exact expected length
			req.Body = string(currentBodyBytes[:expectedLength])
		}
	} else {
		// No Content-Length, use what we have
		req.Body = string(bodySectionBytes)
	}

	return req, nil
}

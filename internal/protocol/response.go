package protocol

import (
	"fmt"
	"time"
	"webserver/internal/tcp"
)

type Response struct {
	Version    HTTPVersion
	StatusCode int
	Status     string
	Headers    map[string]string
	Body       string
}

func NewResponse(statusCode int, status string, version HTTPVersion, body string) *Response {
    resp := &Response{
        Version:    version,
        StatusCode: statusCode,
        Status:     status,
        Headers:    make(map[string]string),
		Body:       body,
    }
    return resp
}

func WriteResponse(conn *tcp.TCPConn, resp *Response) error {
	response := fmt.Sprintf("%s %d %s\r\n", resp.Version, resp.StatusCode, resp.Status)

	resp.Headers["Content-Length"] = fmt.Sprintf("%d", len(resp.Body))
	resp.Headers["Date"] = time.Now().UTC().Format(time.RFC1123)
	resp.Headers["Server"] = "GoWebServer/1.0"

	for key, value := range resp.Headers {
		response += fmt.Sprintf("%s: %s\r\n", key, value)
	}

	response += "\r\n" + resp.Body

	_, err := conn.Write([]byte(response))
	return err
}

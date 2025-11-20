package router

import (
	"strings"
	"webserver/internal/protocol"
	"webserver/internal/tcp"
)

type HandlerFunc func(*protocol.Request) *protocol.Response
type StreamHandlerFunc func(*protocol.Request, *tcp.TCPConn, bool, int) error

type Router struct {
	routes        map[string]HandlerFunc // Key: "METHOD:PATH" for exact matches
	streamHandler StreamHandlerFunc      // Single streaming handler for static files
}

func NewRouter() *Router {
	return &Router{
		routes: make(map[string]HandlerFunc),
	}
}

func (r *Router) RegisterRoute(method, path string, handler HandlerFunc) {
	key := method + ":" + path
	r.routes[key] = handler
}

// SetStreamHandler sets the streaming handler for static files (/static/*)
// Streaming handlers write directly to TCP connection for memory efficiency
func (r *Router) SetStreamHandler(handler StreamHandlerFunc) {
	r.streamHandler = handler
}

func (r *Router) Route(req *protocol.Request) *protocol.Response {
	key := req.Method + ":" + req.Path

	// Try exact match
	if handler, found := r.routes[key]; found {
		return handler(req)
	}

	// Not found
	resp := protocol.NewResponse(404, "Not Found", req.Version, "404 - Page Not Found")
	resp.Headers["Content-Type"] = "text/plain"
	return resp
}

// NeedsStreaming checks if the request is for static files
// Static files (GET /static/*) use streaming for memory efficiency
func (r *Router) NeedsStreaming(req *protocol.Request) bool {
	return (req.Method == "GET" || req.Method == "HEAD") && strings.HasPrefix(req.Path, "/static/")
}

// RouteStream routes request to the streaming handler for static files
func (r *Router) RouteStream(req *protocol.Request, conn *tcp.TCPConn, keepAlive bool, remainingRequests int) error {
	if r.streamHandler != nil {
		return r.streamHandler(req, conn, keepAlive, remainingRequests)
	}

	// No stream handler configured - send error
	resp := protocol.NewResponse(500, "Internal Server Error", req.Version, "500 - Stream handler not configured")
	resp.Headers["Content-Type"] = "text/plain"
	return protocol.WriteResponse(conn, resp)
}

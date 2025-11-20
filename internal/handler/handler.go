package handler

import (
	"os"
	"webserver/internal/protocol"
	"webserver/internal/router"
	"webserver/internal/tcp"
)

type HTTPHandler struct {
	router *router.Router
}

func NewHTTPHandler() *HTTPHandler {
	r := router.NewRouter()

	// Register API routes (exact matches)
	r.RegisterRoute("GET", "/", handleHome)
	r.RegisterRoute("GET", "/hello", handleHello)
	r.RegisterRoute("POST", "/echo", handleEcho)
	r.RegisterRoute("GET", "/api/users", handleGetUsers)
	r.RegisterRoute("GET", "/version", handleVersion)

	// Set streaming handler for static files (GET /static/*)
	// Automatically uses in-memory for small files (<1MB) and streaming for large files (>1MB)
	r.SetStreamHandler(HandleStaticFileStream("./public"))

	return &HTTPHandler{
		router: r,
	}
}

func (h *HTTPHandler) Handle(req *protocol.Request) *protocol.Response {
	return h.router.Route(req)
}

func (h *HTTPHandler) NeedsStreaming(req *protocol.Request) bool {
	return h.router.NeedsStreaming(req)
}

func (h *HTTPHandler) HandleStream(req *protocol.Request, conn *tcp.TCPConn) error {
	return h.router.RouteStream(req, conn)
}

func handleHome(req *protocol.Request) *protocol.Response {
	// Read homepage template
	htmlBytes, err := os.ReadFile("templates/home.html")
	if err != nil {
		// Fallback if template file not found
		resp := protocol.NewResponse(500, "Internal Server Error", req.Version, "Error loading homepage template")
		resp.Headers["Content-Type"] = "text/plain"
		return resp
	}

	resp := protocol.NewResponse(200, "OK", req.Version, string(htmlBytes))
	resp.Headers["Content-Type"] = "text/html; charset=utf-8"

	// Apply gzip compression if beneficial
	CompressResponse(resp, req)

	return resp
}

func handleHello(req *protocol.Request) *protocol.Response {
	resp := protocol.NewResponse(200, "OK", req.Version, "Hello from Go Web Server!")
	resp.Headers["Content-Type"] = "text/plain"

	// Apply gzip compression if beneficial
	CompressResponse(resp, req)

	return resp
}

func handleEcho(req *protocol.Request) *protocol.Response {
	body := `{"message":"` + req.Body + `"}`
	resp := protocol.NewResponse(200, "OK", req.Version, body)
	resp.Headers["Content-Type"] = "application/json"

	// Apply gzip compression if beneficial
	CompressResponse(resp, req)

	return resp
}

func handleGetUsers(req *protocol.Request) *protocol.Response {
	resp := protocol.NewResponse(200, "OK", req.Version, `[{"id":1,"name":"Faizan"},{"id":2,"name":"Hussain"}]`)
	resp.Headers["Content-Type"] = "application/json"

	// Apply gzip compression if beneficial
	CompressResponse(resp, req)

	return resp
}

func handleVersion(req *protocol.Request) *protocol.Response {
	body := `{"protocol":"` + string(req.Version) + `","server":"GoWebServer/1.0"}`
	resp := protocol.NewResponse(200, "OK", req.Version, body)
	resp.Headers["Content-Type"] = "application/json"

	// Apply gzip compression if beneficial
	CompressResponse(resp, req)

	return resp
}

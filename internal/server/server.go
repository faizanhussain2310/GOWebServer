package server

import (
	"fmt"
	"log"
	"strings"
	"time"
	"webserver/internal/handler"
	"webserver/internal/protocol"
	"webserver/internal/tcp"
)

type Server struct {
	addr    string
	handler *handler.HTTPHandler
	config  *protocol.ProtocolConfig
}

func NewServer(addr string) *Server {
	return &Server{
		addr:    addr,
		handler: handler.NewHTTPHandler(),
		config:  protocol.NewHTTP11Config(),
	}
}

func NewServerWithVersion(addr string, config *protocol.ProtocolConfig) *Server {
	return &Server{
		addr:    addr,
		handler: handler.NewHTTPHandler(),
		config:  config,
	}
}

func (s *Server) Start() error {
	listener, err := tcp.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}
	defer listener.Close()

	log.Printf("Server running with %s protocol", s.config.Version)

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		tcpConn := conn.(*tcp.TCPConn)
		go s.handleConnection(tcpConn)
	}
}

func (s *Server) handleConnection(conn *tcp.TCPConn) {
	defer conn.Close()

	// Set initial read deadline
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	for {
		request, err := protocol.ParseRequest(conn)
		if err != nil {
			return
		}

		// Determine if connection should be kept alive
		keepAlive := false
		if s.config.Version == protocol.HTTP10 {
			// HTTP/1.0 does not support keep-alive
			keepAlive = false
		} else {
			// HTTP/1.1 and HTTP/2+ default to keep-alive unless client says close
			if connHeader, ok := request.Headers["Connection"]; ok {
				keepAlive = strings.ToLower(connHeader) != "close"
			} else {
				keepAlive = true
			}
		}

		// Check if route needs streaming (for large files)
		if s.handler.NeedsStreaming(request) {
			// Use streaming handler (writes directly to connection)
			err = s.handler.HandleStream(request, conn)
			if err != nil {
				return
			}
		} else {
			// Use regular handler (returns Response object)
			response := s.handler.Handle(request)
			response.Version = s.config.Version

			// Set response Connection header
			if keepAlive {
				response.Headers["Connection"] = "keep-alive"
				response.Headers["Keep-Alive"] = "timeout=30, max=100"
			} else {
				response.Headers["Connection"] = "close"
			}

			err = protocol.WriteResponse(conn, response)
			if err != nil {
				return
			}
		}

		// Close connection if not keep-alive
		if !keepAlive {
			return
		}

		// Reset read deadline for next request
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	}
}

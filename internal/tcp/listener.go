package tcp

import (
	"fmt"
	"net"
	"syscall"
)

// TCPListener represents a TCP network listener that waits for incoming connections.
// It wraps a listening socket file descriptor and provides methods to accept connections.
// This struct implements the net.Listener interface for compatibility with standard Go networking.
type TCPListener struct {
	fd    int      // Listening socket file descriptor
	laddr *TCPAddr // Local address (IP and port the listener is bound to)
}

// Accept waits for and returns the next incoming connection.
// This is a blocking operation that waits until a client connects.
//
// Returns:
//   - net.Conn: A new connection to the client (implements net.Conn interface)
//   - error: Error if accept fails (e.g., listener closed, system error)
//
// Each call to Accept returns a new connection to a different client.
// The returned connection should be closed when done to free resources.
//
// Example:
//
//	listener, _ := tcp.Listen("tcp", ":8080")
//	for {
//	    conn, err := listener.Accept()
//	    if err != nil {
//	        log.Printf("Accept error: %v", err)
//	        continue
//	    }
//	    go handleConnection(conn)  // Handle in goroutine for concurrency
//	}
//
// The function performs:
//  1. Waits for incoming connection (blocks until client connects)
//  2. Accepts the connection (completes TCP handshake)
//  3. Gets local and remote addresses
//  4. Creates a TCPConn representing the established connection
func (l *TCPListener) Accept() (net.Conn, error) {
	nfd, raddr, err := acceptSocket(l.fd)
	if err != nil {
		return nil, err
	}

	sa, err := syscall.Getsockname(nfd)
	if err != nil {
		syscall.Close(nfd)
		return nil, fmt.Errorf("failed to get local address: %w", err)
	}

	var laddr *TCPAddr
	switch v := sa.(type) {
	case *syscall.SockaddrInet4:
		laddr = &TCPAddr{
			IP:   net.IP(v.Addr[:]).String(),
			Port: v.Port,
		}
	case *syscall.SockaddrInet6:
		laddr = &TCPAddr{
			IP:   net.IP(v.Addr[:]).String(),
			Port: v.Port,
		}
	default:
		syscall.Close(nfd)
		return nil, fmt.Errorf("unexpected socket address type")
	}

	conn := &TCPConn{
		fd:    nfd,
		laddr: laddr,
		raddr: raddr,
	}

	return conn, nil
}

// Close closes the listening socket, stopping it from accepting new connections.
// Any blocked Accept operations will return an error.
//
// Returns:
//   - error: Error if the close operation fails
//
// After calling Close, the listener cannot accept new connections.
// Existing connections are not affected and continue to work normally.
//
// Example:
//
//	listener, _ := tcp.Listen("tcp", ":8080")
//	defer listener.Close()  // Ensure cleanup
//
//	// ... accept and handle connections ...
//
//	// When shutting down:
//	listener.Close()  // Stop accepting new connections
func (l *TCPListener) Close() error {
	err := syscall.Close(l.fd)
	if err != nil {
		return err
	}
	return nil
}

// Addr returns the listener's network address.
// This is the address the listener is bound to and listening on.
//
// Returns:
//   - net.Addr: The local address implementing the net.Addr interface
//
// Example:
//
//	listener, _ := tcp.Listen("tcp", ":8080")
//	addr := listener.Addr()
//	fmt.Println("Listening on:", addr.String())  // e.g., "0.0.0.0:8080"
func (l *TCPListener) Addr() net.Addr {
	return l.laddr
}

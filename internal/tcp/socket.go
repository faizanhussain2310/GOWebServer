package tcp

import (
	"fmt"
	"net"
	"syscall"
)

// createSocket creates a new TCP socket and returns its file descriptor.
// This is a low-level function that directly calls the socket() system call.
//
// Returns:
//   - int: Socket file descriptor (>= 0 on success)
//   - error: Error if socket creation fails
//
// The created socket is:
//   - AF_INET: IPv4 address family
//   - SOCK_STREAM: TCP stream socket (reliable, ordered, connection-oriented)
//   - IPPROTO_TCP: TCP protocol
//
// The file descriptor should be closed when no longer needed to free system resources.
func createSocket() (int, error) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
	if err != nil {
		return -1, fmt.Errorf("failed to create socket: %w", err)
	}
	return fd, nil
}

// bindSocket binds a socket to a specific local address (IP and port).
// This associates the socket with a specific port so it can receive connections on that port.
//
// Parameters:
//   - fd: Socket file descriptor to bind
//   - addr: TCP address containing IP and port to bind to
//
// Returns:
//   - error: Error if binding fails (e.g., port already in use, permission denied)
//
// Common bind errors:
//   - "address already in use": Port is already bound by another process
//   - "permission denied": Ports < 1024 require root/admin privileges
//
// Example addresses:
//   - "0.0.0.0:8080" - Listen on all network interfaces, port 8080
//   - "127.0.0.1:8080" - Listen only on localhost, port 8080
//   - "192.168.1.100:3000" - Listen on specific IP, port 3000
func bindSocket(fd int, addr *TCPAddr) error {
	sa := &syscall.SockaddrInet4{
		Port: addr.Port,
	}

	ip := net.ParseIP(addr.IP)
	if ip == nil {
		return fmt.Errorf("invalid IP address: %s", addr.IP)
	}

	ip4 := ip.To4()
	if ip4 == nil {
		return fmt.Errorf("not an IPv4 address: %s", addr.IP)
	}

	copy(sa.Addr[:], ip4)

	err := syscall.Bind(fd, sa)
	if err != nil {
		return fmt.Errorf("failed to bind socket: %w", err)
	}

	return nil
}

// listenSocket marks a socket as a listening socket, ready to accept incoming connections.
// After calling this, the socket can receive connection requests from clients.
//
// Parameters:
//   - fd: Socket file descriptor to mark as listening
//   - backlog: Maximum length of the queue of pending connections (typically 128)
//
// Returns:
//   - error: Error if listen fails
//
// The backlog parameter specifies how many connection requests can be queued
// before the system starts refusing new connections. A typical value is 128.
//
// After listen(), use accept() to retrieve connections from the queue.
func listenSocket(fd int, backlog int) error {
	err := syscall.Listen(fd, backlog)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %w", err)
	}
	return nil
}

// acceptSocket accepts a new incoming connection from the listening socket.
// This is a blocking call that waits until a client connects.
//
// Parameters:
//   - fd: Listening socket file descriptor
//
// Returns:
//   - int: New socket file descriptor for the accepted connection
//   - *TCPAddr: Remote address (IP and port) of the connected client
//   - error: Error if accept fails
//
// Each call returns a new connection. The listening socket remains open
// and can continue accepting more connections.
//
// The returned file descriptor is a new socket specifically for communicating
// with that client. It should be closed when done to free resources.
//
// This function:
//  1. Waits for a client to connect (blocks)
//  2. Completes the TCP handshake
//  3. Creates a new socket for the connection
//  4. Returns the new socket and client's address
func acceptSocket(fd int) (int, *TCPAddr, error) {
	nfd, sa, err := syscall.Accept(fd)
	if err != nil {
		return -1, nil, fmt.Errorf("failed to accept connection: %w", err)
	}

	addr := &TCPAddr{}

	switch v := sa.(type) {
	case *syscall.SockaddrInet4:
		addr.IP = net.IP(v.Addr[:]).String()
		addr.Port = v.Port
	case *syscall.SockaddrInet6:
		addr.IP = net.IP(v.Addr[:]).String()
		addr.Port = v.Port
		addr.Zone = ""
	default:
		syscall.Close(nfd)
		return -1, nil, fmt.Errorf("unexpected socket address type")
	}

	return nfd, addr, nil
}

// setSocketOptions configures socket options for optimal server operation.
// This function sets important options that affect socket behavior.
//
// Parameters:
//   - fd: Socket file descriptor to configure
//
// Returns:
//   - error: Error if any option fails to set
//
// Options configured:
//
//  1. SO_REUSEADDR: Allows reusing the address immediately after server restart.
//     Without this, you'd have to wait ~60 seconds before restarting on the same port.
//
//  2. SO_KEEPALIVE: Enables TCP keepalive probes to detect dead connections.
//     Helps detect when client crashes or network connection is lost.
//
//  3. Blocking mode: Ensures socket operations block until complete.
//     This is the default mode suitable for most server applications.
//
// These options are essential for a production-ready server:
//   - Quick server restarts (SO_REUSEADDR)
//   - Dead connection detection (SO_KEEPALIVE)
//   - Predictable I/O behavior (blocking mode)
func setSocketOptions(fd int) error {
	// Set SO_REUSEADDR to allow address reuse
	err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	if err != nil {
		return fmt.Errorf("failed to set SO_REUSEADDR: %w", err)
	}

	// Set SO_KEEPALIVE for TCP keepalive
	err = syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_KEEPALIVE, 1)
	if err != nil {
		return fmt.Errorf("failed to set SO_KEEPALIVE: %w", err)
	}

	// Set socket to non-blocking mode for better error handling
	err = syscall.SetNonblock(fd, false)
	if err != nil {
		return fmt.Errorf("failed to set blocking mode: %w", err)
	}

	return nil
}

package tcp

import (
	"fmt"
	"net"
	"syscall"
)

// Listen creates a TCP listener that waits for incoming connections on the specified address.
// This is the server-side function that binds to a port and listens for client connections.
//
// Parameters:
//   - network: Must be "tcp", "tcp4", or "tcp6"
//   - address: Host and port in format "host:port" (e.g., "127.0.0.1:8080" or ":8080")
//
// Returns:
//   - *TCPListener: A listener ready to accept incoming connections
//   - error: Any error that occurred during socket creation, binding, or listening
//
// Example:
//
//	listener, err := tcp.Listen("tcp", ":8080")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer listener.Close()
//
// The function performs the following steps:
//  1. Resolves the TCP address (host and port)
//  2. Creates a socket file descriptor
//  3. Sets socket options (SO_REUSEADDR, SO_KEEPALIVE)
//  4. Binds the socket to the specified address
//  5. Marks the socket as listening with backlog of 128 connections
func Listen(network, address string) (*TCPListener, error) {
	addr, err := ResolveTCPAddr(network, address)
	if err != nil {
		return nil, err
	}

	fd, err := createSocket()
	if err != nil {
		return nil, err
	}

	err = setSocketOptions(fd)
	if err != nil {
		syscall.Close(fd)
		return nil, err
	}

	err = bindSocket(fd, addr)
	if err != nil {
		syscall.Close(fd)
		return nil, err
	}

	err = listenSocket(fd, 128)
	if err != nil {
		syscall.Close(fd)
		return nil, err
	}

	listener := &TCPListener{
		fd:    fd,
		laddr: addr,
	}

	return listener, nil
}

// Dial creates an outgoing TCP connection to the specified address.
// This is the client-side function that initiates a connection to a remote server.
//
// Parameters:
//   - network: Must be "tcp", "tcp4", or "tcp6"
//   - address: Remote host and port in format "host:port" (e.g., "example.com:80")
//
// Returns:
//   - *TCPConn: An established connection ready for reading and writing
//   - error: Any error that occurred during connection establishment
//
// Example:
//
//	conn, err := tcp.Dial("tcp", "api.example.com:80")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer conn.Close()
//
// The function performs the following steps:
//  1. Resolves the TCP address (converts hostname to IP if needed)
//  2. Creates a socket file descriptor
//  3. Initiates TCP 3-way handshake (SYN, SYN-ACK, ACK)
//  4. Gets the local address assigned by the OS
//  5. Returns a connected TCPConn ready for I/O operations
//
// Use cases:
//   - Making HTTP requests to external APIs
//   - Connecting to databases or microservices
//   - Implementing proxy or load balancer functionality
func Dial(network, address string) (*TCPConn, error) {
	addr, err := ResolveTCPAddr(network, address)
	if err != nil {
		return nil, err
	}

	fd, err := createSocket()
	if err != nil {
		return nil, err
	}

	sa := &syscall.SockaddrInet4{
		Port: addr.Port,
	}

	ip := net.ParseIP(addr.IP)
	if ip == nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("invalid IP address: %s", addr.IP)
	}

	ip4 := ip.To4()
	if ip4 == nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("not an IPv4 address: %s", addr.IP)
	}

	copy(sa.Addr[:], ip4)

	err = syscall.Connect(fd, sa)
	if err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	localSa, err := syscall.Getsockname(fd)
	if err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("failed to get local address: %w", err)
	}

	var laddr *TCPAddr
	switch v := localSa.(type) {
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
		syscall.Close(fd)
		return nil, fmt.Errorf("unexpected socket address type")
	}

	conn := &TCPConn{
		fd:    fd,
		laddr: laddr,
		raddr: addr,
	}

	return conn, nil
}

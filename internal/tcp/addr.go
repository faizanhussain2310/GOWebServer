package tcp

import (
	"fmt"
	"strconv"
	"strings"
)

// TCPAddr represents a TCP network address consisting of an IP address and port number.
// It implements the net.Addr interface for compatibility with Go's standard networking.
type TCPAddr struct {
	IP   string // IP address (e.g., "127.0.0.1" or "192.168.1.1")
	Port int    // Port number (e.g., 8080)
	Zone string // IPv6 zone identifier (unused for IPv4)
}

// Network returns the network type, which is always "tcp" for TCP addresses.
// This method is required by the net.Addr interface.
func (a *TCPAddr) Network() string {
	return "tcp"
}

// String returns the string representation of the TCP address in "IP:Port" format.
// Returns "<nil>" if the address is nil.
//
// Example outputs:
//   - "127.0.0.1:8080"
//   - "192.168.1.100:3000"
//   - "<nil>" (if address is nil)
func (a *TCPAddr) String() string {
	if a == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s:%d", a.IP, a.Port)
}

// ResolveTCPAddr parses a network address string and returns a TCPAddr.
//
// Parameters:
//   - network: Must be "tcp", "tcp4", or "tcp6"
//   - address: Address string in format "host:port" or ":port"
//
// Returns:
//   - *TCPAddr: Parsed address with IP and port
//   - error: Error if network type is invalid, port is missing/invalid, or format is incorrect
//
// Examples:
//   - ResolveTCPAddr("tcp", "127.0.0.1:8080") → {IP: "127.0.0.1", Port: 8080}
//   - ResolveTCPAddr("tcp", ":8080") → {IP: "0.0.0.0", Port: 8080}
//   - ResolveTCPAddr("tcp", "localhost:3000") → {IP: "localhost", Port: 3000}
//
// Note: If host is empty, defaults to "0.0.0.0" (all interfaces)
func ResolveTCPAddr(network, address string) (*TCPAddr, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("unsupported network: %s", network)
	}

	host, portStr, err := splitHostPort(address)
	if err != nil {
		return nil, err
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid port: %s", portStr)
	}

	if host == "" {
		host = "0.0.0.0"
	}

	return &TCPAddr{
		IP:   host,
		Port: port,
	}, nil
}

// splitHostPort separates a network address into host and port components.
//
// Parameters:
//   - address: Address string in format "host:port"
//
// Returns:
//   - host: The host part (IP address or hostname)
//   - port: The port part as a string
//   - err: Error if no colon is found (missing port)
//
// Examples:
//   - splitHostPort("127.0.0.1:8080") → ("127.0.0.1", "8080", nil)
//   - splitHostPort(":8080") → ("0.0.0.0", "8080", nil)
//   - splitHostPort("localhost:3000") → ("localhost", "3000", nil)
//
// Note: Empty host defaults to "0.0.0.0"
func splitHostPort(address string) (host, port string, err error) {
	lastColon := strings.LastIndex(address, ":")
	if lastColon == -1 {
		return "", "", fmt.Errorf("missing port in address")
	}

	host = address[:lastColon]
	port = address[lastColon+1:]

	if host == "" {
		host = "0.0.0.0"
	}

	return host, port, nil
}

package tcp

import (
	"net"
	"syscall"
	"time"
)

// TCPConn represents an established TCP connection.
// It wraps a file descriptor and provides read/write operations with timeout support.
// This struct implements the net.Conn interface for compatibility with standard Go networking.
type TCPConn struct {
	fd            int       // Socket file descriptor for I/O operations
	laddr         *TCPAddr  // Local address (this machine's IP and port)
	raddr         *TCPAddr  // Remote address (connected peer's IP and port)
	readDeadline  time.Time // Deadline for read operations (zero = no timeout)
	writeDeadline time.Time // Deadline for write operations (zero = no timeout)
}

// Read reads data from the TCP connection into the provided byte slice.
//
// Parameters:
//   - b: Byte slice to read data into
//
// Returns:
//   - int: Number of bytes read (0 to len(b))
//   - error: io.EOF when connection is closed, or other errors on failure
//
// This is a blocking operation that waits for data to arrive.
// The actual number of bytes read may be less than len(b).
//
// Example:
//
//	buf := make([]byte, 4096)
//	n, err := conn.Read(buf)
//	if err != nil {
//	    if err == io.EOF {
//	        // Connection closed
//	    }
//	    return err
//	}
//	data := buf[:n]  // Use only the bytes actually read
func (c *TCPConn) Read(b []byte) (int, error) {
	n, err := syscall.Read(c.fd, b)
	return n, err
}

// Write writes data from the byte slice to the TCP connection.
//
// Parameters:
//   - b: Byte slice containing data to write
//
// Returns:
//   - int: Number of bytes written
//   - error: Error if write fails or connection is closed
//
// This is a blocking operation that may not write all bytes at once
// for very large buffers, though in practice it usually writes all bytes.
//
// Example:
//
//	data := []byte("HTTP/1.1 200 OK\r\n\r\n")
//	n, err := conn.Write(data)
//	if err != nil {
//	    return err
//	}
//	if n != len(data) {
//	    // Handle partial write (rare)
//	}
func (c *TCPConn) Write(b []byte) (int, error) {
	n, err := syscall.Write(c.fd, b)
	return n, err
}

// Close closes the TCP connection, releasing the file descriptor.
// After calling Close, the connection cannot be used for further I/O.
//
// Returns:
//   - error: Error if the close operation fails
//
// It's safe to call Close multiple times, though subsequent calls will return an error.
// Best practice is to use defer to ensure connections are always closed:
//
//	conn, err := listener.Accept()
//	if err != nil {
//	    return err
//	}
//	defer conn.Close()  // Ensures connection is closed even if errors occur
func (c *TCPConn) Close() error {
	return syscall.Close(c.fd)
}

// LocalAddr returns the local network address of this connection.
// This is the address of the local machine (your server).
//
// Returns:
//   - net.Addr: Local address implementing the net.Addr interface
//
// Example:
//
//	addr := conn.LocalAddr()
//	fmt.Println("Local address:", addr.String())  // e.g., "127.0.0.1:8080"
func (c *TCPConn) LocalAddr() net.Addr {
	return c.laddr
}

// RemoteAddr returns the remote network address of this connection.
// This is the address of the connected peer (the client or remote server).
//
// Returns:
//   - net.Addr: Remote address implementing the net.Addr interface
//
// Example:
//
//	addr := conn.RemoteAddr()
//	fmt.Println("Remote address:", addr.String())  // e.g., "192.168.1.100:54321"
func (c *TCPConn) RemoteAddr() net.Addr {
	return c.raddr
}

// SetDeadline sets both read and write deadlines for the connection.
// A deadline is an absolute time after which I/O operations will fail with a timeout error.
//
// Parameters:
//   - t: Absolute deadline time (use time.Time{} or time.IsZero() for no deadline)
//
// Returns:
//   - error: Error if setting the deadline fails
//
// Example:
//
//	// Set 30-second timeout for all operations
//	deadline := time.Now().Add(30 * time.Second)
//	err := conn.SetDeadline(deadline)
//
//	// Remove deadline (infinite timeout)
//	err := conn.SetDeadline(time.Time{})
func (c *TCPConn) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}

// SetReadDeadline sets the deadline for read operations on the connection.
// After the deadline, Read operations will return a timeout error.
//
// Parameters:
//   - t: Absolute deadline time (use time.Time{} for no deadline)
//
// Returns:
//   - error: Error if setting the deadline fails
//
// The deadline applies to all future Read calls and any currently-blocked Read.
// A zero value for t (time.Time{}) means Read will not time out.
//
// Example:
//
//	// Read with 10-second timeout
//	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
//	buf := make([]byte, 4096)
//	n, err := conn.Read(buf)
//	if err != nil {
//	    // Check if it's a timeout
//	    if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
//	        fmt.Println("Read timeout")
//	    }
//	}
func (c *TCPConn) SetReadDeadline(t time.Time) error {
	c.readDeadline = t

	var timeout time.Duration
	if t.IsZero() {
		// No timeout (infinite)
		timeout = 0
	} else {
		timeout = time.Until(t)
		if timeout < 0 {
			timeout = 0 // Already expired
		}
	}

	tv := syscall.NsecToTimeval(timeout.Nanoseconds())
	return syscall.SetsockoptTimeval(c.fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)
}

// SetWriteDeadline sets the deadline for write operations on the connection.
// After the deadline, Write operations will return a timeout error.
//
// Parameters:
//   - t: Absolute deadline time (use time.Time{} for no deadline)
//
// Returns:
//   - error: Error if setting the deadline fails
//
// The deadline applies to all future Write calls and any currently-blocked Write.
// A zero value for t (time.Time{}) means Write will not time out.
//
// Example:
//
//	// Write with 5-second timeout
//	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
//	data := []byte("Hello, World!")
//	n, err := conn.Write(data)
//	if err != nil {
//	    if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
//	        fmt.Println("Write timeout")
//	    }
//	}
func (c *TCPConn) SetWriteDeadline(t time.Time) error {
	c.writeDeadline = t

	var timeout time.Duration
	if t.IsZero() {
		timeout = 0
	} else {
		timeout = time.Until(t)
		if timeout < 0 {
			timeout = 0
		}
	}

	tv := syscall.NsecToTimeval(timeout.Nanoseconds())
	return syscall.SetsockoptTimeval(c.fd, syscall.SOL_SOCKET, syscall.SO_SNDTIMEO, &tv)
}

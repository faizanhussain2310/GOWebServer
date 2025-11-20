package protocol

type HTTPVersion string

const (
	HTTP10 HTTPVersion = "HTTP/1.0"
	HTTP11 HTTPVersion = "HTTP/1.1"
	HTTP2  HTTPVersion = "HTTP/2.0"
)

type ProtocolConfig struct {
	Version           HTTPVersion
	KeepAlive         bool
	MaxConnections    int
	ConnectionTimeout int
}

func NewHTTP10Config() *ProtocolConfig {
	return &ProtocolConfig{
		Version:           HTTP10,
		KeepAlive:         false,
		MaxConnections:    100,
		ConnectionTimeout: 30,
	}
}

func NewHTTP11Config() *ProtocolConfig {
	return &ProtocolConfig{
		Version:           HTTP11,
		KeepAlive:         true,
		MaxConnections:    1000,
		ConnectionTimeout: 60,
	}
}


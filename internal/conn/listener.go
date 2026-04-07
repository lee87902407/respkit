package conn

import (
	"crypto/tls"
	"net"
)

// Listener wraps network listener with consistent interface
type Listener interface {
	Accept() (net.Conn, error)
	Close() error
	Addr() net.Addr
}

// Listen creates a network listener for TCP or Unix domain sockets
// Supports: tcp, tcp4, tcp6, unix, unixpacket
func Listen(network, addr string) (net.Listener, error) {
	return net.Listen(network, addr)
}

// ListenTLS creates a TLS network listener for TCP networks
// Supports: tcp, tcp4, tcp6
func ListenTLS(network, addr string, config *tls.Config) (net.Listener, error) {
	ln, err := Listen(network, addr)
	if err != nil {
		return nil, err
	}

	return tls.NewListener(ln, config), nil
}

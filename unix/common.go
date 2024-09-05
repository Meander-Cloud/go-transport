package unix

import "net"

type Protocol interface {
	Close()
	ReadLoop(net.Conn)
}

type Options struct {
	// Unix Domain Socket to listen on or connect to
	SocketPath string

	// user installed protocol for processing connection
	Protocol Protocol

	// logging prefix
	LogPrefix string

	// enable verbose logging
	LogDebug bool
}

package tcp

import (
	"net"
	"time"
)

const (
	// defaults for when not provided in Options
	TcpKeepAliveInterval time.Duration = 47 * time.Second
	TcpKeepAliveCount    uint16        = 2
	TcpDialTimeout       time.Duration = 5 * time.Second
	TcpReconnectInterval time.Duration = 15 * time.Second
)

type Protocol interface {
	Close()
	ReadLoop(net.Conn)
}

type Options struct {
	// TCP Address to listen on or connect to
	Address string

	// client / server keep alive probe interval, sets both TCP_KEEPALIVE and TCP_KEEPINTVL
	KeepAliveInterval time.Duration

	// client / server keep alive probe unanswered count which would trigger disconnect
	KeepAliveCount uint16

	// client dial timeout
	DialTimeout time.Duration

	// client reconnect attempt interval
	ReconnectInterval time.Duration

	// user installed protocol for processing connection
	Protocol Protocol

	// logging prefix
	LogPrefix string

	// enable verbose logging
	LogDebug bool
}

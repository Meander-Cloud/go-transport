package tcp

import (
	"net"
	"time"
)

const (
	// defaults for when not provided in Options
	KeepAliveInterval time.Duration = time.Second * 47
	KeepAliveCount    uint16        = 2
	DialTimeout       time.Duration = time.Second * 5
	ReconnectInterval time.Duration = time.Second * 15
	ReconnectLogEvery uint32        = 120
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

	// client reconnect to log every nth attempt
	ReconnectLogEvery uint32

	// user installed protocol for processing connection
	Protocol Protocol

	// logging prefix
	LogPrefix string

	// enable verbose logging
	LogDebug bool
}

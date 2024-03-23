package transport

import (
	"net"
	"time"
)

const TcpKeepIntvl time.Duration = 47 * time.Second // prime number that I chose for keep alive
const TcpReconIntvl time.Duration = 15 * time.Second
const TcpDialTimeout time.Duration = 5 * time.Second

type Protocol interface {
	Name() string
	Close()
	ReadLoop(*net.Conn)
}

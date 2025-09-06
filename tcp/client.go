package tcp

import (
	"context"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type TcpClient struct {
	options *Options

	exitwg sync.WaitGroup
	exitch chan struct{}
}

func NewTcpClient(options *Options) (*TcpClient, error) {
	c := &TcpClient{
		options: options,

		exitwg: sync.WaitGroup{},
		exitch: make(chan struct{}, 1),
	}

	err := c.init()
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (c *TcpClient) Shutdown() {
	log.Printf("%s: synchronized shutdown starting", c.options.LogPrefix)

	select {
	case c.exitch <- struct{}{}:
	default:
		log.Printf("%s: exitch already signaled", c.options.LogPrefix)
	}

	c.exitwg.Wait()
	log.Printf("%s: synchronized shutdown done", c.options.LogPrefix)
}

func (c *TcpClient) init() error {
	var keepAliveInterval time.Duration
	var keepAliveCount uint16
	var dialTimeout time.Duration
	var reconnectInterval time.Duration
	var reconnectLogEvery uint32
	if c.options.KeepAliveInterval == 0 {
		keepAliveInterval = KeepAliveInterval
	} else {
		keepAliveInterval = c.options.KeepAliveInterval
	}
	if c.options.KeepAliveCount == 0 {
		keepAliveCount = KeepAliveCount
	} else {
		keepAliveCount = c.options.KeepAliveCount
	}
	if c.options.DialTimeout == 0 {
		dialTimeout = DialTimeout
	} else {
		dialTimeout = c.options.DialTimeout
	}
	if c.options.ReconnectInterval == 0 {
		reconnectInterval = ReconnectInterval
	} else {
		reconnectInterval = c.options.ReconnectInterval
	}
	if c.options.ReconnectLogEvery == 0 {
		reconnectLogEvery = ReconnectLogEvery
	} else {
		reconnectLogEvery = c.options.ReconnectLogEvery
	}

	dialer := &net.Dialer{
		Timeout: dialTimeout,
		KeepAliveConfig: net.KeepAliveConfig{
			Enable:   true,
			Idle:     keepAliveInterval,
			Interval: keepAliveInterval,
			Count:    int(keepAliveCount),
		},
	}

	rootContext, dialCancel := context.WithCancel(context.Background())

	inShutdown := atomic.Bool{}
	connectExitCh := make(chan struct{}, 1)
	connectExitedCh := make(chan struct{}, 1)

	activeConn := atomic.Pointer[net.Conn]{}

	log.Printf(
		"%s: connecting to TCP Address=%s, dialTimeout=%v, reconnectInterval=%v, keepAliveInterval=%v, keepAliveCount=%d",
		c.options.LogPrefix,
		c.options.Address,
		dialTimeout,
		reconnectInterval,
		keepAliveInterval,
		keepAliveCount,
	)

	c.exitwg.Add(2)

	// spawn lifecycle management goroutine
	go func() {
		log.Printf("%s: lifecycle management goroutine starting", c.options.LogPrefix)

		defer func() {
			log.Printf("%s: lifecycle management goroutine exiting", c.options.LogPrefix)
			c.exitwg.Done()
		}()

		<-c.exitch // wait
		log.Printf("%s: exitch received, proceeding to exit", c.options.LogPrefix)

		log.Printf("%s: marking atomic shutdown", c.options.LogPrefix)
		inShutdown.Store(true)

		log.Printf("%s: canceling dial context", c.options.LogPrefix)
		dialCancel()

		log.Printf("%s: signaling connectExitCh", c.options.LogPrefix)
		connectExitCh <- struct{}{}

		log.Printf("%s: closing protocol", c.options.LogPrefix)
		c.options.Protocol.Close()

		timer := time.NewTimer(time.Second)
		select {
		case <-timer.C: // wait
			pConn := activeConn.Load()
			if pConn != nil {
				log.Printf("%s: connect loop goroutine has not exited, closing active connection", c.options.LogPrefix)
				(*pConn).Close()
			} else {
				log.Printf("%s: connect loop goroutine has not exited, yet no active connection to close", c.options.LogPrefix)
			}
		case <-connectExitedCh: // wait
			timer.Stop()
			log.Printf("%s: connectExitedCh received, exiting", c.options.LogPrefix)
		}
	}()

	// spawn connect loop goroutine
	go func() {
		log.Printf("%s: connect loop goroutine starting", c.options.LogPrefix)

		defer func() {
			log.Printf("%s: signaling connectExitedCh", c.options.LogPrefix)
			connectExitedCh <- struct{}{}

			log.Printf("%s: connect loop goroutine exiting", c.options.LogPrefix)
			c.exitwg.Done()
		}()

		// loop to retry connection indefinitely
		var attempt uint32 = 1
		for {
			logAttempt := (attempt-1)%reconnectLogEvery == 0

			func() {
				if c.options.LogDebug && logAttempt {
					log.Printf(
						"%s: dialing TCP Address=%s at attempt=%d",
						c.options.LogPrefix,
						c.options.Address,
						attempt,
					)
				}

				dialContext, dialRelease := context.WithTimeout(rootContext, dialTimeout)

				conn, err := dialer.DialContext(dialContext, "tcp", c.options.Address)
				dialRelease()
				if err != nil {
					if logAttempt {
						log.Printf(
							"%s: failed to dial TCP Address=%s at attempt=%d, err=%s",
							c.options.LogPrefix,
							c.options.Address,
							attempt,
							err.Error(),
						)
					}
					return
				}

				if c.options.LogDebug {
					log.Printf(
						"%s: new %s connection %s at attempt=%d",
						c.options.LogPrefix,
						conn.RemoteAddr().Network(),
						conn.RemoteAddr().String(),
						attempt,
					)
				}

				connTCP, ok := conn.(*net.TCPConn)
				if !ok {
					log.Printf(
						"%s: connection %s is of transport %s and not tcp, closing",
						c.options.LogPrefix,
						conn.RemoteAddr().String(),
						conn.RemoteAddr().Network(),
					)
					conn.Close()
					return
				}

				// per https://news.ycombinator.com/item?id=34179426
				// by default go net implementation enables TCP_NODELAY / disables Nagle's Algorithm
				err = connTCP.SetNoDelay(true)
				if err != nil {
					log.Printf(
						"%s: connection %s failed to enable TCP_NODELAY, err=%s",
						c.options.LogPrefix,
						connTCP.RemoteAddr().String(),
						err.Error(),
					)
					// proceed
				}

				// cache reference
				activeConn.Store(&conn)

				// synchronously run ReadLoop
				c.options.Protocol.ReadLoop(conn) // wait

				// release reference
				activeConn.Store(nil)
			}()

			if inShutdown.Load() {
				log.Printf("%s: shutdown in progress, exiting", c.options.LogPrefix)
				return
			}

			// increment attempt count
			attempt += 1

			if c.options.LogDebug && logAttempt {
				log.Printf(
					"%s: scheduling reconnect in %v, next attempt=%d",
					c.options.LogPrefix,
					reconnectInterval,
					attempt,
				)
			}

			timer := time.NewTimer(reconnectInterval)
			select {
			case <-timer.C: // wait
				continue
			case <-connectExitCh: // wait
				timer.Stop()
				log.Printf("%s: connectExitCh received, exiting", c.options.LogPrefix)
				return
			}
		}
	}()

	return nil
}

func (c *TcpClient) Protocol() Protocol {
	return c.options.Protocol
}

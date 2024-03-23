package transport

import (
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type TcpClient struct {
	// only accessed in main, Init goroutines
	connectAddr string

	// may be accessed by multiple goroutines concurrently
	protocol *Protocol

	// only accessed in main, Init goroutines
	pinitwg    *sync.WaitGroup
	inShutdown atomic.Bool
	exitch     chan struct{}
}

func NewTcpClient(connectAddr string, protocol *Protocol, pinitwg *sync.WaitGroup) *TcpClient {
	return &TcpClient{
		connectAddr: connectAddr,
		protocol:    protocol,
		pinitwg:     pinitwg,
		exitch:      make(chan struct{}, 1),
	}
}

func (c *TcpClient) Protocol() *Protocol {
	return c.protocol
}

func (c *TcpClient) Shutdown() {
	log.Printf("%s: Shutdown starting\n", (*c.protocol).Name())

	// mark shutdown in progress
	c.inShutdown.Store(true)
	log.Printf("%s: Shutdown marked atomically\n", (*c.protocol).Name())

	func() {
		defer func() {
			err := recover()
			if err != nil {
				log.Printf("%s: Shutdown recovered from panic=%s\n", (*c.protocol).Name(), err)
			}
		}()

		select {
		case c.exitch <- struct{}{}:
			log.Printf("%s: Shutdown exit channel notified\n", (*c.protocol).Name())
		default:
			log.Printf("%s: Shutdown failed to send to exit channel\n", (*c.protocol).Name())
		}
	}()

	// explicitly invoke protocol close to break from synchronous ReadLoop
	(*c.protocol).Close()
	log.Printf("%s: Shutdown explicitly invoked protocol close\n", (*c.protocol).Name())
}

func (c *TcpClient) Init() {
	log.Printf("%s: Init starting\n", (*c.protocol).Name())
	c.pinitwg.Add(1)

	// do not access member variables (except protocol) in this caller goroutine beyond this point
	// member variables will be modified in Init goroutine below

	go func() {
		defer func() {
			log.Printf("%s: exit routine started\n", (*c.protocol).Name())

			(*c.protocol).Close()
			close(c.exitch)
			c.pinitwg.Done() // signal caller goroutine

			log.Printf("%s: exit routine completed, Init goroutine exiting\n", (*c.protocol).Name())
		}()

		// loop to retry connection indefinitely
		var attempt int64
		attempt = 1
		for {
			func() {
				dialer := net.Dialer{
					Timeout: TcpDialTimeout,
				}

				conn, err := dialer.Dial("tcp", c.connectAddr)
				if err != nil {
					log.Printf("%s: failed to dial TCP connectAddr=%s, err=%s, at attempt=%d\n", (*c.protocol).Name(), c.connectAddr, err.Error(), attempt)
					return
				}

				log.Printf(
					"%s: established %s connection to ip=%s, at attempt=%d\n",
					(*c.protocol).Name(),
					conn.RemoteAddr().Network(),
					conn.RemoteAddr().String(),
					attempt,
				)

				connTCP, ok := conn.(*net.TCPConn)
				if !ok {
					log.Printf("%s: connection %s is of transport %s and not tcp, closing and skipping\n", (*c.protocol).Name(), conn.RemoteAddr().String(), conn.RemoteAddr().Network())
					conn.Close()
					return
				}

				// per https://news.ycombinator.com/item?id=34179426
				// by default go net implementation enables TCP_NODELAY / disables Nagle's Algorithm
				err = connTCP.SetNoDelay(true)
				if err != nil {
					log.Printf("%s: connection %s failed to enable TCP_NODELAY, err=%s\n", (*c.protocol).Name(), connTCP.RemoteAddr().String(), err.Error())
					// proceed
				}

				// per https://github.com/golang/go/issues/48622 and https://github.com/golang/go/issues/23459
				// by default go net implementation enables TCP keepalive
				// and sets both tcp_keepalive_time and tcp_keepalive_intvl to 15 seconds
				// tcp_keepalive_probes is not set and inherits system setting, which on Linux is 9 probes
				// WIP to allow config https://github.com/golang/go/issues/62254
				err = connTCP.SetKeepAlive(true)
				if err != nil {
					log.Printf("%s: connection %s failed to enable SO_KEEPALIVE, err=%s\n", (*c.protocol).Name(), connTCP.RemoteAddr().String(), err.Error())
					// proceed
				}

				err = connTCP.SetKeepAlivePeriod(TcpKeepIntvl)
				if err != nil {
					log.Printf(
						"%s: connection %s failed to set TCP_KEEPIDLE/TCP_KEEPINTVL to %ds, err=%s\n",
						(*c.protocol).Name(),
						connTCP.RemoteAddr().String(),
						TcpKeepIntvl/time.Second,
						err.Error(),
					)
					// proceed
				}

				// synchronously run ReadLoop
				(*c.protocol).ReadLoop(&conn) // wait
			}()

			if c.inShutdown.Load() {
				log.Printf("%s: shutdown in progress, exiting\n", (*c.protocol).Name())
				return
			}

			// increment attempt count
			attempt += 1

			log.Printf("%s: scheduling reconnect in %ds, next attempt=%d\n", (*c.protocol).Name(), TcpReconIntvl/time.Second, attempt)
			timer := time.NewTimer(TcpReconIntvl)

			select {
			case t := <-timer.C: // wait
				log.Printf("%s: timer triggered at t=%v, current reconnect attempt=%d\n", (*c.protocol).Name(), t, attempt)
				continue
			case <-c.exitch: // wait
				timer.Stop()
				log.Printf("%s: exitch received, exiting\n", (*c.protocol).Name())
				return
			}
		}
	}()

	log.Printf("%s: Init started\n", (*c.protocol).Name())
}

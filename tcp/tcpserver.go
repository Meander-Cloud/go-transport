package tcp

import (
	"errors"
	"log"
	"net"
	"sync"
	"syscall"
	"time"
)

type TcpServer struct {
	// only accessed in main, Init, acceptLoop goroutines
	listenAddr string
	ln         net.Listener

	// may be accessed by multiple goroutines concurrently
	protocol *Protocol

	// only accessed in main, Init goroutines
	pinitwg *sync.WaitGroup
	exitch  chan struct{}
}

func NewTcpServer(listenAddr string, protocol *Protocol, pinitwg *sync.WaitGroup) *TcpServer {
	return &TcpServer{
		listenAddr: listenAddr,
		// ln
		protocol: protocol,
		pinitwg:  pinitwg,
		exitch:   make(chan struct{}, 1),
	}
}

func (s *TcpServer) Protocol() *Protocol {
	return s.protocol
}

func (s *TcpServer) Shutdown() {
	log.Printf("%s: Shutdown starting", (*s.protocol).Name())

	func() {
		defer func() {
			err := recover()
			if err != nil {
				log.Printf("%s: Shutdown recovered from panic=%s", (*s.protocol).Name(), err)
			}
		}()

		select {
		case s.exitch <- struct{}{}:
			log.Printf("%s: Shutdown exit channel notified", (*s.protocol).Name())
		default:
			log.Printf("%s: Shutdown failed to send to exit channel", (*s.protocol).Name())
		}
	}()
}

func (s *TcpServer) Init() {
	log.Printf("%s: Init starting", (*s.protocol).Name())
	s.pinitwg.Add(1)

	// do not access member variables (except protocol) in this caller goroutine beyond this point
	// member variables will be modified in Init goroutine below

	go func() {
		listening := false
		defer func() {
			log.Printf("%s: exit routine started", (*s.protocol).Name())

			if listening {
				s.ln.Close()
			}
			(*s.protocol).Close()
			close(s.exitch)
			s.pinitwg.Done() // signal caller goroutine

			log.Printf("%s: exit routine completed, Init goroutine exiting", (*s.protocol).Name())
		}()

		ln, err := net.Listen("tcp", s.listenAddr)
		if err != nil {
			log.Printf("%s: failed to listen on listenAddr=%s, err=%s", (*s.protocol).Name(), s.listenAddr, err.Error())
			return
		}

		log.Printf("%s: listening on listenAddr=%s", (*s.protocol).Name(), s.listenAddr)
		s.ln = ln
		listening = true

		// spawn acceptLoop goroutine to receive connections
		go s.acceptLoop()

		select {
		case <-s.exitch: // wait
			log.Printf("%s: exitch received, exiting", (*s.protocol).Name())
			return
		}
	}()

	log.Printf("%s: Init started", (*s.protocol).Name())
}

func (s *TcpServer) acceptLoop() {
	log.Printf("%s: acceptLoop starting", (*s.protocol).Name())
	defer func() {
		log.Printf("%s: acceptLoop goroutine exiting", (*s.protocol).Name())
	}()

	for {
		conn, err := s.ln.Accept()
		if err != nil {
			errOp, ok := err.(*net.OpError)
			if ok && errOp.Err == syscall.EINVAL {
				log.Printf("%s: listener port has closed, exiting acceptLoop, errOp=%s", (*s.protocol).Name(), errOp.Error())
				return
			}

			if errors.Is(err, net.ErrClosed) {
				log.Printf("%s: listener port has closed, exiting acceptLoop, err=%s", (*s.protocol).Name(), err.Error())
				return
			}

			log.Printf("%s: acceptLoop err=%s, skipping", (*s.protocol).Name(), err.Error())
			continue
		}

		log.Printf("%s: new %s connection %s", (*s.protocol).Name(), conn.RemoteAddr().Network(), conn.RemoteAddr().String())

		connTCP, ok := conn.(*net.TCPConn)
		if !ok {
			log.Printf("%s: connection %s is of transport %s and not tcp, closing and skipping", (*s.protocol).Name(), conn.RemoteAddr().String(), conn.RemoteAddr().Network())
			conn.Close()
			continue
		}

		// per https://news.ycombinator.com/item?id=34179426
		// by default go net implementation enables TCP_NODELAY / disables Nagle's Algorithm
		err = connTCP.SetNoDelay(true)
		if err != nil {
			log.Printf("%s: connection %s failed to enable TCP_NODELAY, err=%s", (*s.protocol).Name(), connTCP.RemoteAddr().String(), err.Error())
			// proceed
		}

		// per https://github.com/golang/go/issues/48622 and https://github.com/golang/go/issues/23459
		// by default go net implementation enables TCP keepalive
		// and sets both tcp_keepalive_time and tcp_keepalive_intvl to 15 seconds
		// tcp_keepalive_probes is not set and inherits system setting, which on Linux is 9 probes
		// WIP to allow config https://github.com/golang/go/issues/62254
		err = connTCP.SetKeepAlive(true)
		if err != nil {
			log.Printf("%s: connection %s failed to enable SO_KEEPALIVE, err=%s", (*s.protocol).Name(), connTCP.RemoteAddr().String(), err.Error())
			// proceed
		}

		err = connTCP.SetKeepAlivePeriod(TcpKeepIntvl)
		if err != nil {
			log.Printf(
				"%s: connection %s failed to set TCP_KEEPIDLE/TCP_KEEPINTVL to %ds, err=%s",
				(*s.protocol).Name(),
				connTCP.RemoteAddr().String(),
				TcpKeepIntvl/time.Second,
				err.Error(),
			)
			// proceed
		}

		// spawn ReadLoop goroutine for each established connection
		go (*s.protocol).ReadLoop(&conn)
	}
}

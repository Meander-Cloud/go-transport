package tcp

import (
	"context"
	"log"
	"net"
	"sync"
	"time"
)

type TcpServer struct {
	options *Options

	exitwg sync.WaitGroup
	exitch chan struct{}
}

func NewTcpServer(options *Options) (*TcpServer, error) {
	s := &TcpServer{
		options: options,

		exitwg: sync.WaitGroup{},
		exitch: make(chan struct{}, 1),
	}

	err := s.init()
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (s *TcpServer) Shutdown() {
	log.Printf("%s: synchronized shutdown starting", s.options.LogPrefix)
	s.exitch <- struct{}{}
	s.exitwg.Wait()
	log.Printf("%s: synchronized shutdown done", s.options.LogPrefix)
}

func (s *TcpServer) init() error {
	var keepAliveInterval time.Duration
	var keepAliveCount uint16
	if s.options.KeepAliveInterval == 0 {
		keepAliveInterval = TcpKeepAliveInterval
	} else {
		keepAliveInterval = s.options.KeepAliveInterval
	}
	if s.options.KeepAliveCount == 0 {
		keepAliveCount = TcpKeepAliveCount
	} else {
		keepAliveCount = s.options.KeepAliveCount
	}

	listenConfig := &net.ListenConfig{
		KeepAliveConfig: net.KeepAliveConfig{
			Enable:   true,
			Idle:     keepAliveInterval,
			Interval: keepAliveInterval,
			Count:    int(keepAliveCount),
		},
	}

	listener, err := listenConfig.Listen(context.Background(), "tcp", s.options.Address)
	if err != nil {
		log.Printf(
			"%s: failed to listen on TCP Address=%s, err=%s",
			s.options.LogPrefix,
			s.options.Address,
			err.Error(),
		)
		return err
	}

	log.Printf(
		"%s: listening on TCP Address=%s, keepAliveInterval=%v, keepAliveCount=%d",
		s.options.LogPrefix,
		s.options.Address,
		keepAliveInterval,
		keepAliveCount,
	)

	s.exitwg.Add(2)

	// spawn lifecycle management goroutine
	go func() {
		log.Printf("%s: lifecycle management goroutine starting", s.options.LogPrefix)

		defer func() {
			log.Printf("%s: lifecycle management goroutine exiting", s.options.LogPrefix)
			s.exitwg.Done()
		}()

		select {
		case <-s.exitch:
			log.Printf("%s: exitch received, proceeding to exit", s.options.LogPrefix)
		}

		log.Printf("%s: closing listener", s.options.LogPrefix)
		listener.Close()

		log.Printf("%s: closing protocol", s.options.LogPrefix)
		s.options.Protocol.Close()
	}()

	// spawn listener accept loop goroutine
	go func() {
		log.Printf("%s: listener accept loop goroutine starting", s.options.LogPrefix)

		defer func() {
			log.Printf("%s: listener accept loop goroutine exiting", s.options.LogPrefix)
			s.exitwg.Done()
		}()

		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("%s: listener accept err=%s, proceeding to exit", s.options.LogPrefix, err.Error())
				return
			}

			if s.options.LogDebug {
				log.Printf(
					"%s: new %s connection %s",
					s.options.LogPrefix,
					conn.RemoteAddr().Network(),
					conn.RemoteAddr().String(),
				)
			}

			connTCP, ok := conn.(*net.TCPConn)
			if !ok {
				log.Printf(
					"%s: connection %s is of transport %s and not tcp, closing",
					s.options.LogPrefix,
					conn.RemoteAddr().String(),
					conn.RemoteAddr().Network(),
				)
				conn.Close()
				continue
			}

			// per https://news.ycombinator.com/item?id=34179426
			// by default go net implementation enables TCP_NODELAY / disables Nagle's Algorithm
			err = connTCP.SetNoDelay(true)
			if err != nil {
				log.Printf(
					"%s: connection %s failed to enable TCP_NODELAY, err=%s",
					s.options.LogPrefix,
					connTCP.RemoteAddr().String(),
					err.Error(),
				)
				// proceed
			}

			// spawn ReadLoop goroutine for each established connection
			go s.options.Protocol.ReadLoop(conn)
		}
	}()

	return nil
}

func (s *TcpServer) Protocol() Protocol {
	return s.options.Protocol
}

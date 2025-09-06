package unix

import (
	"log"
	"net"
	"os"
	"sync"
)

type UnixServer struct {
	options *Options

	exitwg sync.WaitGroup
	exitch chan struct{}
}

func NewUnixServer(options *Options) (*UnixServer, error) {
	s := &UnixServer{
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

func (s *UnixServer) Shutdown() {
	log.Printf("%s: synchronized shutdown starting", s.options.LogPrefix)

	select {
	case s.exitch <- struct{}{}:
	default:
		log.Printf("%s: exitch already signaled", s.options.LogPrefix)
	}

	s.exitwg.Wait()
	log.Printf("%s: synchronized shutdown done", s.options.LogPrefix)
}

func (s *UnixServer) init() error {
	_, err := os.Stat(s.options.SocketPath)
	if err == nil {
		err = os.Remove(s.options.SocketPath)
		if err != nil {
			log.Printf(
				"%s: detected obsolete SocketPath=%s but failed to remove, err=%s",
				s.options.LogPrefix,
				s.options.SocketPath,
				err.Error(),
			)
			return err
		}
		log.Printf(
			"%s: removed obsolete SocketPath=%s",
			s.options.LogPrefix,
			s.options.SocketPath,
		)
	}

	listener, err := net.Listen("unix", s.options.SocketPath)
	if err != nil {
		log.Printf(
			"%s: failed to listen on Unix Domain SocketPath=%s, err=%s",
			s.options.LogPrefix,
			s.options.SocketPath,
			err.Error(),
		)
		return err
	}

	log.Printf("%s: listening on Unix Domain SocketPath=%s", s.options.LogPrefix, s.options.SocketPath)

	s.exitwg.Add(2)

	// spawn lifecycle management goroutine
	go func() {
		log.Printf("%s: lifecycle management goroutine starting", s.options.LogPrefix)

		defer func() {
			log.Printf("%s: lifecycle management goroutine exiting", s.options.LogPrefix)
			s.exitwg.Done()
		}()

		<-s.exitch // wait
		log.Printf("%s: exitch received, proceeding to exit", s.options.LogPrefix)

		log.Printf("%s: closing listener", s.options.LogPrefix)
		listener.Close()

		log.Printf("%s: closing protocol", s.options.LogPrefix)
		s.options.Protocol.Close()
	}()

	// spawn listener accept loop goroutine
	go func() {
		log.Printf("%s: listener accept loop goroutine starting", s.options.LogPrefix)

		defer func() {
			log.Printf("%s: removing SocketPath=%s", s.options.LogPrefix, s.options.SocketPath)
			os.Remove(s.options.SocketPath)

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

			// spawn ReadLoop goroutine for each established connection
			go s.options.Protocol.ReadLoop(conn)
		}
	}()

	return nil
}

func (s *UnixServer) Protocol() Protocol {
	return s.options.Protocol
}

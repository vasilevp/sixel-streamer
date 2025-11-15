package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func init() {
	// stdout is used for streaming, so use stderr for everything
	log.Logger = log.Output(zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.RFC3339,
	}).With().Caller().Logger()
}

const connectionReadTimeout = time.Minute * 2
const connectionWriteTimeout = time.Second * 10

type Client struct {
	Ready  bool
	Frames chan []byte
	Done   chan struct{}
}

type Server struct {
	clients  map[net.Conn]*Client
	mutex    sync.RWMutex
	shutdown chan struct{}
	wg       sync.WaitGroup
}

func NewServer() *Server {
	return &Server{
		clients:  make(map[net.Conn]*Client),
		shutdown: make(chan struct{}),
	}
}

func (s *Server) addClient(conn net.Conn) {
	client := Client{
		Ready:  false,
		Frames: make(chan []byte, 15),
		Done:   make(chan struct{}),
	}

	s.mutex.Lock()
	numclients := len(s.clients)
	s.clients[conn] = &client
	s.mutex.Unlock()

	log.Info().
		Str("addr", conn.RemoteAddr().String()).
		Int("total_clients", numclients).
		Msg("Client connected")

	conn.SetWriteDeadline(time.Now().Add(connectionWriteTimeout))
	defer conn.SetWriteDeadline(time.Time{})

	// hide cursor
	fmt.Fprint(conn, "\033[?25l")

	const message = "Welcome to Terminal Doom!\r\n" +
		"Controls:\r\n" +
		"\tMovement: WASD\r\n" +
		"\tTurn left/right: Q/E\r\n" +
		"\tFire: F\r\n" +
		"\tUse: SPACE\r\n" +
		"\tSprint: SHIFT (CAPSLOCK to toggle)\r\n\r\n" +
		"Notes:\r\n" +
		"\t- Do not hold input keys, tap them repeatedly instead!\r\n" +
		"\t  Otherwise, input will buffer and you're going to have a bad time.\r\n" +
		"\t- This is a shared session, so players may interfere with each other.\r\n" +
		"\t- The stream might stutter during the first ~5 seconds, (depending on\r\n" +
		"\t  your ping but it should catch up.\r\n" +
		"\r\n"

	fmt.Fprint(conn, message)

	switch numclients {
	case 0:
	case 1:
		fmt.Fprint(conn, "Note that someone is already connected")
	default:
		fmt.Fprintf(conn, "Note that %d people are already connected", numclients)
	}

	fmt.Fprint(conn, "Have fun!\r\n"+
		"Terminal Doom by pvas\r\n"+
		"Joining in ",
	)

	for i := range 5 {
		if i > 0 {
			fmt.Fprint(conn, "\b\b\b\b") // erase previous number
		}
		fmt.Fprintf(conn, "%d...", 5-i)
		time.Sleep(time.Second * 1)
	}

	client.Ready = true

	go func() {
		defer func() {
			client.Done <- struct{}{}
			close(client.Done)
		}()

		for data := range client.Frames {
			_, err := conn.Write(data)
			if err != nil {
				log.Error().
					Err(err).
					Str("addr", conn.RemoteAddr().String()).
					Msg("Error broadcasting to client")
				s.removeClient(conn, err.Error())
				return
			}
		}
	}()
}

func (s *Server) removeClient(conn net.Conn, message string) {
	s.mutex.Lock()
	client, ok := s.clients[conn]
	if !ok { // already removed, maybe by the broadcast handler
		s.mutex.Unlock()
		return
	}
	delete(s.clients, conn)
	close(client.Frames)
	s.mutex.Unlock()

	<-client.Done

	// show cursor, exit SIXEL mode, clear screen, and send goodbye message
	fmt.Fprint(conn, "\033[?25h\x1b\\\x1b[2J\x1b[H\r\n\r\n"+
		"Connection closed: "+message+".\r\n"+
		"Goodbye!\r\n")

	s.mutex.RLock()
	log.Info().
		Str("addr", conn.RemoteAddr().String()).
		Int("total_clients", len(s.clients)).
		Msg("Client disconnected")
	s.mutex.RUnlock()

	conn.Close()
}

func (s *Server) broadcast(data []byte) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	for _, client := range s.clients {
		if !client.Ready {
			continue
		}

		select {
		case client.Frames <- data:
		default:
			log.Warn().Msg("Frame dropped")
		}
	}
}

func (s *Server) handleESC(conn net.Conn) (string, error) {
	// set a short read timeout to check if more data is coming
	conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	// reset deadline
	defer conn.SetReadDeadline(time.Now().Add(connectionReadTimeout))

	nextBuf := make([]byte, 2)
	n, err := conn.Read(nextBuf)
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return "Escape", nil
	}

	if err != nil {
		return "", err
	}

	if n != 2 {
		return "Escape", nil
	}

	switch nextBuf[0] {
	case '[': // arrow keys
		switch nextBuf[1] {
		case 'A':
			return "Up", nil
		case 'B':
			return "Down", nil
		case 'C':
			return "Right", nil
		case 'D':
			return "Left", nil
		}
	case 79: // fn keys
	}

	log.Warn().
		Bytes("sequence", nextBuf[:n]).
		Msg("Unknown escape sequence")
	return "P", nil // i dont fucking know what else to do
}

func (s *Server) handleConnection(conn net.Conn) {
	s.wg.Add(1)
	defer s.wg.Done()

	log := log.With().Str("addr", conn.RemoteAddr().String()).Logger()

	go s.addClient(conn)
	defer s.removeClient(conn, "unknown error") // just in case

	// set initial read deadline
	conn.SetReadDeadline(time.Now().Add(connectionReadTimeout))

	// monitor shutdown signal and close connection if needed
	go func() {
		<-s.shutdown
		s.removeClient(conn, "server shutting down")
	}()

	buf := []byte{0}
	for {
		n, err := conn.Read(buf)
		if err != nil {
			// check if shutdown was triggered
			select {
			case <-s.shutdown:
				return
			default:
			}

			// check if it's a timeout error
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				log.Warn().Msg("Read timeout, disconnecting")
				s.removeClient(conn, "inactivity")
				return
			}
			log.Error().Err(err).Msg("Error reading from client")
			return
		}

		if n < len(buf) {
			log.Error().Msg("Error reading from client")
			return
		}

		var key string
		switch buf[0] {
		case 0x04: // EOF (CTRL+D)
			continue
		case 0x03: // CTRL+C
			s.removeClient(conn, "CTRL+C received")
			return
		case 0x1b: // ESC sequence
			key, err = s.handleESC(conn)
			if err != nil {
				log.Error().Err(err).Msg("Error reading escape sequence")
				return
			}
		case ' ':
			key = "space"
		case '\r', '\n': // Enter/Return
			key = "Return"
		case 'f', 'F':
			key = "control"
		default:
			key = string(buf[0])
		}

		fmt.Printf("keydown %s\nsleep 0.15\nkeyup %s\n", key, key)

		log.Debug().Bytes("data", buf[:n]).Msg("Received from client")
		conn.SetReadDeadline(time.Now().Add(connectionReadTimeout))
	}
}

func (s *Server) startBroadcaster(reader io.Reader) error {
	buf := make([]byte, 60000)
	for {
		n, err := reader.Read(buf)
		if err != nil {
			return err
		}

		if n > 0 {
			tmp := make([]byte, n)
			copy(tmp, buf[:n])
			s.broadcast(tmp[:n])
		}
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	log.Info().Msg("Initiating graceful shutdown")

	// signal all connections to close
	close(s.shutdown)

	// create a channel to signal when all goroutines are done
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Info().Msg("All connections closed gracefully")
		return nil
	case <-ctx.Done():
		log.Warn().Msg("Shutdown timeout exceeded, forcing close")
		return ctx.Err()
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: telfwd <port> [input-file]")
		fmt.Fprintln(os.Stderr, "  If input-file is not provided, reads from stdin")
		os.Exit(1)
	}

	port := os.Args[1]
	listenAddr := ":" + port

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to start listener")
	}
	defer listener.Close()

	log.Info().Str("addr", listenAddr).Msg("Server listening")

	server := NewServer()

	// Determine input source (file or stdin)
	var input io.Reader
	if len(os.Args) > 2 {
		// Read from file
		file, err := os.Open(os.Args[2])
		if err != nil {
			log.Fatal().Err(err).Str("file", os.Args[2]).Msg("Error opening file")
		}
		defer file.Close()
		input = file
		log.Info().Str("file", os.Args[2]).Msg("Reading broadcast messages from file")
	} else {
		// Read from stdin
		input = os.Stdin
		log.Info().Msg("Reading broadcast messages from stdin")
	}

	sigErr := make(chan struct{}, 1)
	// Start broadcaster that reads from input source
	go func() {
		err := server.startBroadcaster(input)
		if err != nil {
			log.Error().Err(err).Msg("Error reading broadcast input")
		}
		sigErr <- struct{}{}
	}()

	// Accept connections in a goroutine
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-server.shutdown:
					// Shutdown initiated, stop accepting silently
				default:
					log.Error().Err(err).Msg("Error accepting connection")
				}
				return
			}

			go server.handleConnection(conn)
		}
	}()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	select {
	case sig := <-sigChan:
		log.Info().Str("signal", sig.String()).Msg("Received signal")
	case <-sigErr:
		log.Warn().Msg("Shutting down due to an error")
	}

	// Stop accepting new connections
	listener.Close()

	// Graceful shutdown with 5 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("Shutdown error")
		os.Exit(1)
	}

	log.Info().Msg("Server stopped")
}

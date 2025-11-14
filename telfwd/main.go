package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"
)

func init() {
	// Route all log output to stderr
	log.SetOutput(os.Stderr)
}

const connectionReadTimeout = time.Minute * 2
const connectionWriteTimeout = time.Second * 10

type Client struct {
	Ready  bool
	Frames chan []byte
	Done   chan struct{}
}

type Server struct {
	clients map[net.Conn]*Client
	mutex   sync.RWMutex
}

func NewServer() *Server {
	return &Server{
		clients: make(map[net.Conn]*Client),
	}
}

func (s *Server) addClient(conn net.Conn) {
	client := Client{
		Ready:  false,
		Frames: make(chan []byte, 30),
		Done:   make(chan struct{}),
	}

	s.mutex.Lock()
	numclients := len(s.clients)
	s.clients[conn] = &client
	s.mutex.Unlock()

	log.Printf("Client connected: %s (total clients: %d)", conn.RemoteAddr(), numclients)

	conn.SetWriteDeadline(time.Now().Add(connectionWriteTimeout))
	defer conn.SetWriteDeadline(time.Time{})

	// Hide cursor
	fmt.Fprint(conn, "\033[?25l")

	const message = "Welcome to Terminal Doom!\r\n" +
		"Controls:\r\n" +
		"\tMovement: WASD\r\n" +
		"\tTurn left/right: Q/E\r\n" +
		"\tFire: F\r\n" +
		"\tUse: SPACE\r\n" +
		"\tSprint: SHIFT (CAPSLOCK to toggle)\r\n\r\n" +
		"This is a shared session, so players' inputs may interfere with each other.\r\n"

	fmt.Fprint(conn, message)

	switch numclients {
	case 0:
	case 1:
		fmt.Fprint(conn, "Note that someone is already connected")
	default:
		fmt.Fprintf(conn, "Note that %d people are already connected", numclients)
	}

	fmt.Fprint(conn, "Have fun!\r\n"+
		"Terminal Doom by pvas\r\n",
	)

	time.Sleep(time.Second * 3)

	client.Ready = true

	go func() {
		defer func() {
			client.Done <- struct{}{}
			close(client.Done)
		}()

		for data := range client.Frames {
			_, err := conn.Write(data)
			if err != nil {
				log.Printf("Error broadcasting to %s: %v", conn.RemoteAddr(), err)
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

	// Show cursor, exit SIXEL mode, clear screen, and send goodbye message
	fmt.Fprint(conn, "\033[?25h\x1b\\\x1b[2J\x1b[H\r\n\r\n"+
		"Connection closed: "+message+".\r\n"+
		"Goodbye!\r\n")

	s.mutex.RLock()
	log.Printf("Client disconnected: %s (total clients: %d)", conn.RemoteAddr(), len(s.clients))
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
			log.Printf("Frame dropped!")
		}
	}
}

func (s *Server) handleESC(conn net.Conn) (string, error) {
	// Set a short read timeout to check if more data is coming
	conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	// Reset deadline
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

	log.Printf("Unknown escape sequence: %v", nextBuf)
	return "P", nil // i dont fucking know what else to do
}

func (s *Server) handleConnection(conn net.Conn) {
	go s.addClient(conn)
	defer s.removeClient(conn, "unknown error") // just in case

	buf := []byte{0}
	for {
		n, err := conn.Read(buf)
		if err != nil {
			// Check if it's a timeout error
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				log.Printf("Read timeout for %s, disconnecting", conn.RemoteAddr())
				s.removeClient(conn, "inactivity")
				return
			}
			log.Printf("Error reading from %s: %v", conn.RemoteAddr(), err)
			return
		}

		if n < len(buf) {
			log.Printf("Error reading from %s", conn.RemoteAddr())
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
				log.Printf("Error reading from %s: %v", conn.RemoteAddr(), err)
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

		log.Printf("Received from %s: %v", conn.RemoteAddr(), buf)
		conn.SetReadDeadline(time.Now().Add(connectionReadTimeout))
	}
}

func (s *Server) startBroadcaster(reader io.Reader) {
	buf := make([]byte, 60000)
	for {
		n, err := reader.Read(buf)
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading broadcast input: %v", err)
			}
			return
		}

		if n > 0 {
			tmp := make([]byte, n)
			copy(tmp, buf[:n])
			s.broadcast(tmp[:n])
		}
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
		log.Fatal(err)
	}
	defer listener.Close()

	log.Printf("Server listening on %s", listenAddr)

	server := NewServer()

	// Determine input source (file or stdin)
	var input io.Reader
	if len(os.Args) > 2 {
		// Read from file
		file, err := os.Open(os.Args[2])
		if err != nil {
			log.Fatalf("Error opening file: %v", err)
		}
		defer file.Close()
		input = file
		log.Printf("Reading broadcast messages from file: %s", os.Args[2])
	} else {
		// Read from stdin
		input = os.Stdin
		log.Println("Reading broadcast messages from stdin")
	}

	// Start broadcaster that reads from input source
	go server.startBroadcaster(input)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Error accepting connection: %v", err)
			continue
		}

		go server.handleConnection(conn)
	}
}

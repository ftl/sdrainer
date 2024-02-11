package telnet

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"
)

const (
	newConnectionDeadline     = 100 * time.Millisecond
	connectionKeepAlivePeriod = 30 * time.Second
	readBufferSize            = 1024
	defaultSpotSilencePeriod  = 4 * time.Minute
)

type spotHash string

func newSpotHash(callsign string, frequency float64) spotHash {
	text := fmt.Sprintf("%s-%.0f", callsign, frequency/1000.0)
	hash := md5.Sum([]byte(text))
	return spotHash(hex.EncodeToString(hash[:]))
}

type Server struct {
	address  *net.TCPAddr
	listener *net.TCPListener
	mycall   string
	version  string

	connections []*Connection

	lastSpots     map[spotHash]time.Time
	silencePeriod time.Duration

	msg    chan []byte
	close  chan struct{}
	closed chan struct{}
}

func NewServer(address string, mycall string, version string) (*Server, error) {
	result := &Server{
		mycall:        mycall,
		version:       version,
		lastSpots:     make(map[spotHash]time.Time),
		silencePeriod: defaultSpotSilencePeriod,
		msg:           make(chan []byte, 1),
		close:         make(chan struct{}),
		closed:        make(chan struct{}),
	}

	localAddress, err := net.ResolveTCPAddr("tcp", address)
	if err != nil {
		log.Fatal(err)
	}
	result.address = localAddress

	listener, err := net.ListenTCP("tcp", result.address)
	if err != nil {
		return nil, err
	}
	result.listener = listener

	go result.run()

	return result, nil
}

func (s *Server) run() {
	defer close(s.closed)
	welcome := fmt.Sprintf("SDRainer Version %s\n", s.version)

	removeConnections := make([]int, 0, 10)
	for {
		select {
		case <-s.close:
			for _, conn := range s.connections {
				conn.Close()
			}
			return
		case bytes := <-s.msg:
			removeConnections = removeConnections[:0]
			for i, conn := range s.connections {
				_, err := conn.Write(bytes)
				if err != nil {
					log.Printf("found closed connection %s", conn.String())
					removeConnections = append(removeConnections, i)
				}
			}
			if len(s.connections) > 0 && len(s.connections)-len(removeConnections) <= 0 {
				log.Printf("clearing connections")
				clear(s.connections)
				s.connections = s.connections[:0]
				continue
			}

			for i, index := range removeConnections {
				s.removeConnection(index - i)
			}
		default:
			err := s.listener.SetDeadline(time.Now().Add(newConnectionDeadline))
			if err != nil {
				log.Fatalf("setting the listener deadline failed: %v", err)
			}
			conn, err := s.listener.AcceptTCP()
			if errors.Is(err, os.ErrDeadlineExceeded) {
				// ignore, nobody is calling
				continue
			} else if err != nil {
				log.Println(err)
				continue
			}

			log.Printf("new incoming connection: %v", conn.RemoteAddr())
			conn.SetKeepAlivePeriod(connectionKeepAlivePeriod)
			conn.SetKeepAlive(true)
			connection := NewConnection(conn, welcome)
			s.connections = append(s.connections, connection)
		}
	}
}

func (s *Server) removeConnection(index int) {
	if index < 0 || index >= len(s.connections) {
		return
	}
	log.Printf("removing connection %s", s.connections[index].String())
	last := len(s.connections) - 1
	if index < last {
		copy(s.connections[index:], s.connections[index+1:])
	}
	s.connections[last] = nil
	s.connections = s.connections[:last]
}

func (s *Server) Stop() {
	select {
	case <-s.closed:
		return
	default:
		close(s.close)
		<-s.closed
	}
}

func (s *Server) SetSilencePeriod(silencePeriod time.Duration) {
	s.silencePeriod = silencePeriod
}

func (s *Server) Spot(callsign string, frequency float64, msg string, timestamp time.Time) {
	hash := newSpotHash(callsign, frequency)
	if s.shouldAnnounce(hash, timestamp) {
		s.msg <- []byte(s.formatSpotMessage(callsign, frequency, msg, timestamp))
		s.registerSpot(hash, timestamp)
	}
}

func (s *Server) shouldAnnounce(hash spotHash, timestamp time.Time) bool {
	lastSpotTime, ok := s.lastSpots[hash]
	if !ok {
		return true
	}
	return timestamp.Sub(lastSpotTime) > s.silencePeriod
}

func (s *Server) registerSpot(hash spotHash, timestamp time.Time) {
	s.lastSpots[hash] = timestamp
}

func (s *Server) formatSpotMessage(callsign string, frequency float64, msg string, timestamp time.Time) string {
	prefix := fmt.Sprintf("DX de %s:", s.mycall)
	return fmt.Sprintf("%-16s% 6.1f  %-13s%-31s%-4sz\n", prefix, frequency/1000.0, callsign, msg, timestamp.Format("1504"))
}

var ErrClosed = errors.New("connection already closed")

type Prompt struct {
	Question string
	Answer   func(string) (string, *Prompt)
}

type Connection struct {
	conn  io.ReadWriteCloser
	msg   chan []byte
	input chan []byte

	currentPrompt *Prompt
	currentAnswer string

	close  chan struct{}
	closed chan struct{}

	user string
}

func NewConnection(conn io.ReadWriteCloser, welcome string) *Connection {
	result := &Connection{
		conn:  conn,
		msg:   make(chan []byte, 1),
		input: make(chan []byte, 1),

		currentAnswer: "",

		close:  make(chan struct{}),
		closed: make(chan struct{}),
	}

	result.writeAll([]byte(welcome))

	go result.run()
	go result.readLoop()

	return result
}

func (c *Connection) run() {
	defer close(c.closed)
	defer func() {
		err := c.conn.Close()
		if err != nil {
			log.Printf("close %s: %v", c.user, err)
		}
	}()

	ignoreInputPrompt := &Prompt{
		Question: "",
	}
	ignoreInputPrompt.Answer = func(string) (string, *Prompt) {
		return "\n", ignoreInputPrompt
	}
	inputCallsignPrompt := &Prompt{
		Question: "Enter your callsign: ",
		Answer: func(answer string) (string, *Prompt) {
			c.user = answer
			return fmt.Sprintf("welcome %s\n", c.user), ignoreInputPrompt
		},
	}

	err := c.startPrompt(inputCallsignPrompt)
	if err != nil {
		log.Printf("%s: %v", c.user, err)
	}

	for {
		select {
		case <-c.close:
			return
		case bytes := <-c.msg:
			err := c.writeAll(bytes)
			if err != nil {
				log.Printf("%s: %v", c.user, err)
				return
			}
		case bytes := <-c.input:
			for i := 0; i < len(bytes); i++ {
				response, nextPrompt := c.parseAnswerByte(bytes[i])
				if response != "" {
					err := c.writeAll([]byte(response))
					if err != nil {
						log.Printf("%s: %v", c.user, err)
						return
					}
				}

				err := c.startPrompt(nextPrompt)
				if err != nil {
					log.Printf("%s: %v", c.user, err)
					continue
				}
			}
		}
	}
}

func (c *Connection) readLoop() {
	readBuffer := make([]byte, readBufferSize)
	for {
		n, err := c.conn.Read(readBuffer)
		if errors.Is(err, os.ErrClosed) || errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
			return
		} else if err != nil {
			log.Printf("%s: %v", c.user, err)
			return
		}

		if n == 0 {
			continue
		}

		bytes := make([]byte, n)
		copy(bytes, readBuffer[:n])
		c.input <- bytes
	}
}

func (c *Connection) writeAll(bytes []byte) error {
	n := 0
	buffer := bytes
	for n < len(buffer) {
		var err error
		n, err = c.conn.Write(buffer)
		if err != nil {
			return err
		}
		if n < len(buffer) {
			buffer = buffer[n:]
		}
	}
	return nil
}

func (c *Connection) startPrompt(prompt *Prompt) error {
	if prompt == nil {
		return nil
	}
	c.currentPrompt = prompt
	return c.writeAll([]byte(prompt.Question))
}

func (c *Connection) parseAnswerByte(answerByte byte) (string, *Prompt) {
	switch answerByte {
	case '\n', '\r':
		response, nextPrompt := c.currentPrompt.Answer(c.currentAnswer)
		c.currentAnswer = ""
		return response, nextPrompt
	default:
		c.currentAnswer += string(answerByte)
		return "", nil
	}
}

func (c *Connection) Close() {
	select {
	case <-c.closed:
		return
	default:
		close(c.close)
		<-c.closed
	}
}

func (c *Connection) Write(bytes []byte) (int, error) {
	select {
	case <-c.closed:
		return 0, net.ErrClosed
	default:
		c.msg <- bytes
		return len(bytes), nil
	}
}

func (c *Connection) String() string {
	return c.user
}

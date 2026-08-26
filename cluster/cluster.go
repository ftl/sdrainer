package cluster

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ftl/sdrainer/dsp"
)

const (
	newConnectionDeadline     = 100 * time.Millisecond
	connectionKeepAlivePeriod = 30 * time.Second
	readBufferSize            = 1024
	defaultSpotSilencePeriod  = 4 * time.Minute
)

type spotHash string

func newSpotHash[F dsp.Number](callsign string, frequency F) spotHash {
	text := fmt.Sprintf("%s-%.0f", callsign, float64(frequency)/1000.0)
	hash := md5.Sum([]byte(text))
	return spotHash(hex.EncodeToString(hash[:]))
}

type Server[F dsp.Number] struct {
	address  *net.TCPAddr
	listener *net.TCPListener
	mycall   string
	version  string

	connections []*Connection

	lastSpots     map[spotHash]time.Time
	silencePeriod time.Duration

	// activeSpots holds the last spot of each station that still has a channel. A new connection
	// gets each of them, so that it sees the band as it is now and not only what happens from now
	// on.
	//
	// The pipeline reports the callsign of a running station one time, and again only when it finds
	// a better one, so a station that runs for one hour makes one spot. A list of the spots of the
	// last minutes would therefore hold exactly the stations that a new connection does not need.
	//
	// The map holds the callsign as its key. The pipeline gives one channel to one station, and two
	// channels with the same callsign at the same time would be the same station.
	//
	// The goroutine of the pipeline writes this map and the goroutine of a connection reads it, so
	// it needs the lock.
	spotsLock   sync.RWMutex
	activeSpots map[string]activeSpot

	msg    chan []byte
	close  chan struct{}
	closed chan struct{}
}

func NewServer[F dsp.Number](address string, mycall string, version string) (*Server[F], error) {
	result := &Server[F]{
		mycall:        mycall,
		version:       version,
		lastSpots:     make(map[spotHash]time.Time),
		activeSpots:   make(map[string]activeSpot),
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

func (s *Server[_]) run() {
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
			connection := NewConnection(conn, welcome, s.activeSpotMessages)
			s.connections = append(s.connections, connection)
		}
	}
}

func (s *Server[_]) removeConnection(index int) {
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

func (s *Server[_]) Stop() {
	select {
	case <-s.closed:
		return
	default:
		close(s.close)
		<-s.closed
	}
}

func (s *Server[_]) SetSilencePeriod(silencePeriod time.Duration) {
	s.silencePeriod = silencePeriod
}

func (s *Server[F]) Spot(callsign string, frequency F, msg string, timestamp time.Time) {
	message := s.formatSpotMessage(callsign, frequency, msg, timestamp)
	s.putActiveSpot(callsign, frequency, message)

	hash := newSpotHash(callsign, frequency)
	if s.shouldAnnounce(hash, timestamp) {
		s.msg <- []byte(message)
		s.registerSpot(hash, timestamp)
	}
}

// RemoveSpot takes the spot of a station away when its channel goes away.
//
// A channel that is IDLE keeps its spot: an operator makes pauses, and the station is still there.
// Only DeadTimeout ends a channel, and that is the moment at which the station is gone.
func (s *Server[F]) RemoveSpot(callsign string) {
	if callsign == "" {
		return
	}

	s.spotsLock.Lock()
	defer s.spotsLock.Unlock()
	delete(s.activeSpots, spotKey(callsign))
}

// activeSpot is the last spot of one station that still has a channel.
type activeSpot struct {
	frequency float64
	message   string
}

func (s *Server[F]) putActiveSpot(callsign string, frequency F, message string) {
	s.spotsLock.Lock()
	defer s.spotsLock.Unlock()
	s.activeSpots[spotKey(callsign)] = activeSpot{frequency: float64(frequency), message: message}
}

// spotKey makes the key of one station out of its callsign. Spot takes a string and
// ChannelDestroyed takes a callsign.Callsign, and the two do not always use the same case, so the
// key must not depend on it.
func spotKey(callsign string) string {
	return strings.ToUpper(callsign)
}

// activeSpotMessages gives the spot of each station that still has a channel, the lowest frequency
// first. A new connection gets this list after it gave its callsign.
func (s *Server[_]) activeSpotMessages() []string {
	s.spotsLock.RLock()
	defer s.spotsLock.RUnlock()

	spots := make([]activeSpot, 0, len(s.activeSpots))
	for _, spot := range s.activeSpots {
		spots = append(spots, spot)
	}
	sort.Slice(spots, func(i, j int) bool { return spots[i].frequency < spots[j].frequency })

	result := make([]string, 0, len(spots))
	for _, spot := range spots {
		result = append(result, spot.message)
	}
	return result
}

func (s *Server[_]) shouldAnnounce(hash spotHash, timestamp time.Time) bool {
	lastSpotTime, ok := s.lastSpots[hash]
	if !ok {
		return true
	}
	return timestamp.Sub(lastSpotTime) > s.silencePeriod
}

func (s *Server[_]) registerSpot(hash spotHash, timestamp time.Time) {
	s.lastSpots[hash] = timestamp
}

func (s *Server[F]) formatSpotMessage(callsign string, frequency F, msg string, timestamp time.Time) string {
	prefix := fmt.Sprintf("DX de %s:", s.mycall)
	return fmt.Sprintf("%-16s %7.1f  %-13s%-31s%-4sz\n", prefix, float64(frequency)/1000.0, callsign, msg, timestamp.UTC().Format("1504"))
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

	// onLogin gives the lines that the connection sends after the user gave a callsign. A DX
	// cluster sends its picture of the band at that moment, and not before: a client waits for the
	// prompt and it reads nothing until it answered.
	onLogin func() []string

	currentPrompt *Prompt
	currentAnswer string

	close  chan struct{}
	closed chan struct{}

	user string
}

func NewConnection(conn io.ReadWriteCloser, welcome string, onLogin func() []string) *Connection {
	result := &Connection{
		conn:    conn,
		msg:     make(chan []byte, 1),
		input:   make(chan []byte, 1),
		onLogin: onLogin,

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

			var response strings.Builder
			fmt.Fprintf(&response, "welcome %s\n", c.user)
			if c.onLogin != nil {
				for _, line := range c.onLogin() {
					response.WriteString(line)
				}
			}

			return response.String(), ignoreInputPrompt
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

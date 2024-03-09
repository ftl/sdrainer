package kiwi

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ftl/sdrainer/cli"
	"github.com/gorilla/websocket"
)

/*

Resources:
- https://github.com/strickyak/go-kiwisdr-client/blob/master/client/client.go
- https://github.com/hcab14/kiwiclient/blob/master/kiwi/client.py
- https://github.com/ftl/tci/blob/master/client/client.go

*/

const (
	defaultHostname = "localhost"
	defaultPort     = 8073
)

type kiwiTag string

const (
	msgTag kiwiTag = "MSG"
	sndTag kiwiTag = "SND"
	wfTag  kiwiTag = "W/F"
	extTag kiwiTag = "EXT"
)

type kiwiMode string

const (
	iqMode kiwiMode = "iq"
	cwMode kiwiMode = "cw"
)

type kiwiConfiguration map[string]string

const (
	tooBusyMessage     = "too_busy"
	badPasswordMessage = "badp"
	downMessage        = "down"
)

var (
	ErrTooBusy     = errors.New("kiwi too busy")
	ErrBadPassword = errors.New("bad password")
	ErrDown        = errors.New("kiwi down")
)

type clientConn interface {
	RemoteAddr() net.Addr
	Close() error
	WriteMessage(messageType int, data []byte) error
	ReadMessage() (messageType int, p []byte, err error)
}

type KiwiHandler interface {
	Connected(sampleRate int)
	IQData(sampleRate int, data []float32)
}

type Client struct {
	host *net.TCPAddr

	configuration kiwiConfiguration
	compression   bool
	audioRate     int
	connected     bool
	keepalive     bool

	iqBuffer    []float32
	kiwiHandler KiwiHandler

	out    chan string
	close  chan struct{}
	closed chan struct{}
}

func Open(host string, username string, password string, centerFrequency float64, bandwidth int, kiwiHandler KiwiHandler) (*Client, error) {
	client, err := newClient(host, true, kiwiHandler)
	if err != nil {
		return nil, err
	}

	conn, err := client.connect()
	if err != nil {
		return nil, err
	}
	log.Printf("connected to KiwiSDR %s", host)

	go client.readLoop(conn)
	go client.writeLoop(conn)

	client.sendAuthentication(username, password)
	client.sendSetup(
		"SET AR OK in=12000 out=48000",
		"SET squelch=0 max=0",
		"SET lms_autonotch=0",
		"SET getattn=0",
		"SET gen=0 mix=-1",
		"SET agc=0 hang=0 thresh=-100 slope=6 decay=1000 manGain=50",
		"SET compression=0",
	)

	lowCut := -(bandwidth / 2)
	highCut := bandwidth / 2

	client.setVFO(iqMode, lowCut, highCut, centerFrequency)

	return client, nil
}

func newClient(host string, keepalive bool, kiwiHandler KiwiHandler) (*Client, error) {
	tcpHost, err := cli.ParseTCPAddrArg(host, defaultHostname, defaultPort)
	if err != nil {
		return nil, fmt.Errorf("invalid Kiwi host: %v", err)
	}
	if tcpHost.Port == 0 {
		tcpHost.Port = defaultPort
	}

	result := &Client{
		host: tcpHost,

		configuration: make(kiwiConfiguration),

		kiwiHandler: kiwiHandler,

		keepalive: keepalive,

		out:    make(chan string),
		close:  make(chan struct{}),
		closed: make(chan struct{}),
	}

	return result, nil
}

func (c *Client) connect() (clientConn, error) {
	hostUrl, err := url.Parse(fmt.Sprintf("ws://%s:%d/%d/SND", c.host.IP.String(), c.host.Port, nextClientNumber()))
	if err != nil {
		return nil, fmt.Errorf("cannot build KiwiSDR URL: %v", err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(hostUrl.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("cannot dial KiwiSDR websocket: %v", err)
	}

	return conn, nil
}

func (c *Client) readLoop(conn clientConn) {
	defer conn.Close()
	for {
		select {
		case <-c.closed:
			return
		default:
			msgType, msgBytes, err := conn.ReadMessage()
			if err != nil {
				log.Printf("cannot read next message from websocket: %v", err)
				close(c.close)
				return
			}
			if msgType != websocket.BinaryMessage {
				log.Printf("received wrong message type from websocket: %d", msgType)
				continue
			}

			tag, payload, err := decodeKiwiMessage(msgBytes)
			if err != nil {
				log.Print(err)
				continue
			}

			switch tag {
			case msgTag:
				err = c.decodeConfigurationMessage(payload)
				if !c.connected && (c.audioRate != 0) && c.kiwiHandler != nil {
					c.connected = true
					c.kiwiHandler.Connected(c.audioRate)
				}
			case sndTag:
				if c.kiwiHandler == nil {
					err = fmt.Errorf("no KiwiHandler available")
				} else if c.audioRate == 0 {
					err = fmt.Errorf("received IQ data with unknown audio rate")
				} else {
					c.iqBuffer, err = decodeIQMessage(c.iqBuffer, payload)
				}
				if err == nil {
					c.kiwiHandler.IQData(c.audioRate, c.iqBuffer)
				}
			default:
				log.Printf("received message with unknown tag: %s %d bytes", tag, len(payload))
			}

			if err == ErrTooBusy || err == ErrBadPassword || err == ErrDown {
				log.Printf("error message: %s", payload)
				log.Print(err)
				close(c.close)
				return
			}

			if err != nil {
				log.Print(err)
			}
		}
	}
}

func decodeKiwiMessage(bytes []byte) (tag kiwiTag, payload []byte, err error) {
	if len(bytes) < 3 {
		return "", nil, fmt.Errorf("message too short: %v", bytes)
	}

	tag = kiwiTag(bytes[0:3])
	payload = bytes[3:]
	return
}

func (c *Client) decodeConfigurationMessage(payload []byte) error {
	parts := strings.Split(string(payload), " ")
	for _, part := range parts {
		equalIndex := strings.Index(part, "=")
		if equalIndex == -1 {
			c.configuration[part] = ""
			continue
		}

		key := strings.TrimSpace(part[0:equalIndex])
		value := strings.TrimSpace(part[equalIndex+1:])

		switch key {
		case tooBusyMessage:
			log.Printf("%s", part)
			if value == "1" {
				return ErrTooBusy
			}
		case badPasswordMessage:
			if value == "1" {
				return ErrBadPassword
			}
		case downMessage:
			if value == "1" {
				return ErrDown
			}
		}

		log.Printf("received configuration data: %s", key)

		var err error
		switch {
		case key == "audio_rate":
			c.audioRate, err = strconv.Atoi(value)
		case key == "compression":
			c.compression = (value == "1")
		case strings.HasPrefix(key, "load_"):
			value, err = url.QueryUnescape(value)
		}
		if err != nil {
			return err
		}

		c.configuration[key] = value
	}
	return nil
}

func decodeIQMessage(iqData []float32, payload []byte) ([]float32, error) {
	// decode header information
	// flags := payload[0]
	// sequenceNumber := binary.LittleEndian.Uint32(payload[1:5])
	// smeter := binary.BigEndian.Uint16(payload[5:7])
	// rssi := 0.1*float32(smeter) - 127
	// 10 more bytes of GPS information are ignored

	iqBytes := payload[17:]
	iqData = decodeIQBytes(iqData, iqBytes)

	return iqData, nil
}

func decodeIQBytes(iqData []float32, iqBytes []byte) []float32 {
	n := len(iqBytes) / 2
	if len(iqData) != n {
		iqData = make([]float32, n)
	}
	for i := 0; i < n; i++ {
		rawSample := binary.BigEndian.Uint16(iqBytes[2*i : 2*(i+1)])
		iqData[i] = float32(int16(rawSample)) / float32(math.MaxInt16)
	}
	return iqData
}

func (c *Client) writeLoop(conn clientConn) {
	defer close(c.closed)

	keepaliveMessage := []byte("SET keepalive")
	keepalive := time.NewTicker(5 * time.Second)
	defer keepalive.Stop()

	for {
		var err error
		select {
		case <-c.close:
			return
		case <-keepalive.C:
			if c.keepalive {
				err = conn.WriteMessage(websocket.TextMessage, keepaliveMessage)
			}
		case message := <-c.out:
			err = conn.WriteMessage(websocket.TextMessage, []byte(message))
		}
		if err != nil {
			log.Printf("cannot write message to websocket: %v", err)
			close(c.close)
			return
		}
	}

}

func (c *Client) sendAuthentication(username string, password string) error {
	err := c.send("SET auth t=kiwi p=%s", url.QueryEscape(password))
	if err != nil {
		return err
	}
	return c.send("SET ident_user=%s", url.QueryEscape(username))
}

func (c *Client) sendSetup(setup ...string) error {
	for _, line := range setup {
		err := c.send(line)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) setVFO(mode kiwiMode, lowCut int, highCut int, centerFrequency float64) error {
	return c.send("SET mod=%s low_cut=%d high_cut=%d freq=%.3f", mode, lowCut, highCut, centerFrequency/1000.0)
}

func (c *Client) send(format string, args ...any) error {
	c.out <- fmt.Sprintf(format, args...)
	return nil
}

func (c *Client) Close() {
	select {
	case <-c.close:
		return
	case <-c.closed:
		return
	default:
		close(c.close)
		<-c.closed
	}
}

func nextClientNumber() int64 {
	return time.Now().Unix()
}

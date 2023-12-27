package tci

import (
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	tci "github.com/ftl/tci/client"
)

const (
	defaultHostname = "localhost"
	defaultPort     = 40001
	timeout         = 10 * time.Second
)

type Process struct {
	client   *tci.Client
	listener *tciListener
	trx      []*trxHandler

	close  chan struct{}
	closed chan struct{}
}

func New(host string, trace bool) (*Process, error) {
	tcpHost, err := parseTCPAddrArg(host, defaultHostname, defaultPort)
	if err != nil {
		return nil, fmt.Errorf("invalid TCI host: %v", err)
	}
	if tcpHost.Port == 0 {
		tcpHost.Port = defaultPort
	}

	client := tci.KeepOpen(tcpHost, timeout, trace)

	result := &Process{
		client: client,
		close:  make(chan struct{}),
		closed: make(chan struct{}),
	}
	result.listener = &tciListener{process: result}
	result.trx = []*trxHandler{
		newTRXHandler(result, 0),
		newTRXHandler(result, 1),
	}
	go result.run()

	client.Notify(result.listener)

	return result, nil
}

func (p *Process) Close() {
	select {
	case <-p.close:
		return
	default:
		close(p.close)
		<-p.closed
	}
}

func (p *Process) run() {
	for {
		select {
		case <-p.close:
			p.client.StopIQ(0)
			p.trx[0].Close()
			p.trx[1].Close()
			close(p.closed)
			return
		}
	}
}

func (p *Process) onConnected(connected bool) {
	if !connected {
		return
	}

	p.client.StartIQ(0)
}

type tciListener struct {
	process *Process
}

func (l *tciListener) Connected(connected bool) {
	l.process.onConnected(connected)
}

func (l *tciListener) SetDDS(trx int, frequency int) {
	log.Printf("DDS: %d %d", trx, frequency)
}

func (l *tciListener) SetIF(trx int, vfo tci.VFO, frequency int) {
	log.Printf("IF: %d %d %d", trx, vfo, frequency)
}

func (l *tciListener) SetIFLimits(min, max int) {
	log.Printf("IF_LIMITS %d %d", min, max)
}

func (l *tciListener) SetIQSampleRate(sampleRate tci.IQSampleRate) {
	log.Printf("IQ_SAMPLERATE %d", sampleRate)
}

func (l *tciListener) IQData(trx int, sampleRate tci.IQSampleRate, data []float32) {
	l.process.trx[trx].IQData(sampleRate, data)
}

type trxHandler struct {
	process    *Process
	trx        int
	sampleRate float32
	in         chan []float32
}

func newTRXHandler(process *Process, trx int) *trxHandler {
	result := &trxHandler{
		process: process,
		trx:     trx,
		in:      make(chan []float32, 10),
	}
	go result.run()
	return result
}

func (h *trxHandler) run() {
	for frame := range h.in {
		log.Printf("incoming IQ frame with %d samples", len(frame))
	}
}

func (h *trxHandler) Close() {
	close(h.in)
}

func (h *trxHandler) IQData(sampleRate tci.IQSampleRate, data []float32) {
	if h.sampleRate == 0 {
		h.sampleRate = float32(sampleRate)
	} else if h.sampleRate != float32(sampleRate) {
		log.Printf("wrong incoming sample rate on trx %d: %d!", h.trx, sampleRate)
		return
	}

	select {
	case h.in <- data:
		return
	default:
		log.Printf("IQ data skipped on trx %d", h.trx)
	}
}

// TCP address handling

func parseTCPAddrArg(arg string, defaultHost string, defaultPort int) (*net.TCPAddr, error) {
	host, port := splitHostPort(arg)
	if host == "" {
		host = defaultHost
	}
	if port == "" {
		port = strconv.Itoa(defaultPort)
	}

	return net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%s", host, port))
}

func splitHostPort(hostport string) (host, port string) {
	host = hostport

	colon := strings.LastIndexByte(host, ':')
	if colon != -1 && validOptionalPort(host[colon:]) {
		host, port = host[:colon], host[colon+1:]
	}

	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}

	return
}

func validOptionalPort(port string) bool {
	if port == "" {
		return true
	}
	if port[0] != ':' {
		return false
	}
	for _, b := range port[1:] {
		if b < '0' || b > '9' {
			return false
		}
	}
	return true
}

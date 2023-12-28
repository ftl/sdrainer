package tci

import (
	"fmt"
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

	opAsync chan func()
	close   chan struct{}
	closed  chan struct{}
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
		client:  client,
		opAsync: make(chan func(), 10),
		close:   make(chan struct{}),
		closed:  make(chan struct{}),
	}
	result.listener = &tciListener{process: result}
	result.trx = []*trxHandler{
		newTRXHandler(result, 0),
		newTRXHandler(result, 1),
	}
	result.trx[1].Close() // TODO add full support for the second TRX
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
		case op := <-p.opAsync:
			op()
		case <-p.close:
			p.client.StopIQ(0)

			p.trx[0].Close()
			// p.trx[1].Close() // TODO add full support for the second TRX
			close(p.closed)
			return
		}
	}
}

func (p *Process) doAsync(f func()) {
	p.opAsync <- f
}

func (p *Process) onConnected(connected bool) {
	if !connected {
		return
	}

	p.client.StartIQ(0)
}

var peakColor tci.ARGB = tci.NewARGB(255, 255, 0, 0)

func (p *Process) showPeaks(peaks []peak) {
	// p.client.ClearSpots() // TODO this works only if there is only one TRX active
	for _, peak := range peaks {
		label := fmt.Sprintf("%d w%d", peak.CenterFrequency(), peak.Width())
		p.client.AddSpot(label, tci.ModeCW, peak.CenterFrequency(), peakColor, "SDRainer")
	}
}

type tciListener struct {
	process *Process
}

func (l *tciListener) Connected(connected bool) {
	l.process.onConnected(connected)
}

func (l *tciListener) SetDDS(trx int, frequency int) {
	l.process.trx[trx].SetCenterFrequency(frequency)
}

func (l *tciListener) SetIF(trx int, vfo tci.VFO, frequency int) {
	l.process.trx[trx].SetVFOOffset(vfo, frequency)
}

func (l *tciListener) IQData(trx int, sampleRate tci.IQSampleRate, data []float32) {
	const partCount = 4
	partLen := len(data) / partCount
	for i := 0; i < partCount; i++ {
		begin := i * partLen
		end := begin + partLen
		l.process.trx[trx].IQData(tci.IQSampleRate(sampleRate), data[begin:end])
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

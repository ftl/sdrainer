package tci

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	tci "github.com/ftl/tci/client"

	"github.com/ftl/sdrainer/trace"
)

const (
	defaultHostname = "localhost"
	defaultPort     = 40001
	timeout         = 10 * time.Second
)

type Mode string

const (
	VFOMode        Mode = "vfo"
	RandomPeakMode Mode = "random"
)

type Process struct {
	client   *tci.Client
	listener *tciListener
	trx      int
	receiver *Receiver

	opAsync chan func()
	close   chan struct{}
	closed  chan struct{}
}

func New(host string, trx int, mode Mode, traceTCI bool) (*Process, error) {
	tcpHost, err := parseTCPAddrArg(host, defaultHostname, defaultPort)
	if err != nil {
		return nil, fmt.Errorf("invalid TCI host: %v", err)
	}
	if tcpHost.Port == 0 {
		tcpHost.Port = defaultPort
	}

	client := tci.KeepOpen(tcpHost, timeout, traceTCI)

	result := &Process{
		client:  client,
		trx:     trx,
		opAsync: make(chan func(), 10),
		close:   make(chan struct{}),
		closed:  make(chan struct{}),
	}
	result.listener = &tciListener{process: result, trx: result.trx}
	result.receiver = NewReceiver(result, result.trx, mode)
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
			p.client.StopIQ(p.trx)
			p.receiver.Stop()
			close(p.closed)
			return
		}
	}
}

func (p *Process) doAsync(f func()) {
	p.opAsync <- f
}

func (p *Process) SetTracer(tracer trace.Tracer) {
	p.receiver.SetTracer(tracer)
}

func (p *Process) SetThreshold(threshold int) {
	p.receiver.SetPeakThreshold(float32(threshold))
}

func (p *Process) SetSignalDebounce(debounce int) {
	p.receiver.decoder.SetSignalDebounce(debounce)
}

func (p *Process) onConnected(connected bool) {
	if !connected {
		return
	}

	p.receiver.Start()
	p.client.SetIQSampleRate(48000)
	p.client.StartIQ(p.trx)
}

const (
	decodeLabel = "DECODE"
)

var (
	peakColor   tci.ARGB = tci.NewARGB(255, 255, 0, 0)
	decodeColor tci.ARGB = tci.NewARGB(255, 0, 255, 0)
)

func (p *Process) showPeaks(peaks []peak) {
	for _, peak := range peaks {
		label := fmt.Sprintf("%d w%d", peak.CenterFrequency(), peak.Width())
		p.client.AddSpot(label, tci.ModeCW, peak.CenterFrequency(), peakColor, "SDRainer")
	}
}

func (p *Process) showDecode(peak peak) {
	p.client.DeleteSpot(decodeLabel)
	p.client.AddSpot(decodeLabel, tci.ModeCW, peak.CenterFrequency(), decodeColor, "SDRainer")
	offset := peak.CenterFrequency() - p.receiver.CenterFrequency()
	p.client.SetIF(p.trx, tci.VFOA, offset)
}

func (p *Process) hideDecode() {
	p.client.DeleteSpot(decodeLabel)
}

type tciListener struct {
	process *Process
	trx     int
}

func (l *tciListener) Connected(connected bool) {
	l.process.onConnected(connected)
}

func (l *tciListener) SetDDS(trx int, frequency int) {
	if trx != l.trx {
		return
	}
	l.process.receiver.SetCenterFrequency(frequency)
}

func (l *tciListener) SetIF(trx int, vfo tci.VFO, frequency int) {
	if trx != l.trx {
		return
	}
	l.process.receiver.SetVFOOffset(vfo, frequency)
}

func (l *tciListener) IQData(trx int, sampleRate tci.IQSampleRate, data []float32) {
	if trx != l.trx {
		return
	}
	const partCount = 4
	partLen := len(data) / partCount
	for i := 0; i < partCount; i++ {
		begin := i * partLen
		end := begin + partLen
		l.process.receiver.IQData(tci.IQSampleRate(sampleRate), data[begin:end])
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

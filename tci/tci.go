package tci

import (
	"fmt"
	"time"

	tci "github.com/ftl/tci/client"

	"github.com/ftl/sdrainer/cli"
	"github.com/ftl/sdrainer/rx"
	"github.com/ftl/sdrainer/scope"
)

const (
	defaultHostname = "localhost"
	defaultPort     = 40001
	timeout         = 10 * time.Second
	partCount       = 4
)

type Spotter interface {
	Spot(callsign string, frequency float64, msg string, timestamp time.Time)
}

type Process struct {
	client   *tci.Client
	listener *tciListener
	trx      int
	receiver *rx.Receiver[float32, int]
	spotter  Spotter

	threshold         float32
	signalDebounce    int
	silenceTimeout    time.Duration
	attachmentTimeout time.Duration
	scope             scope.Scope
	showListeners     bool
	showSpots         bool

	opAsync chan func()
	close   chan struct{}
	closed  chan struct{}
}

func New(host string, trx int, mode rx.ReceiverMode, spotter Spotter, reporter rx.Reporter[int], traceTCI bool) (*Process, error) {
	tcpHost, err := cli.ParseTCPAddrArg(host, defaultHostname, defaultPort)
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
		spotter: spotter,
		opAsync: make(chan func(), 100),
		close:   make(chan struct{}),
		closed:  make(chan struct{}),
		scope:   scope.NewNullScope(),
	}
	result.listener = &tciListener{process: result, trx: result.trx}
	result.receiver = rx.NewReceiver[float32, int]("", mode, rx.WallClock)
	result.receiver.AddReporter(result)
	if reporter != nil {
		result.receiver.AddReporter(reporter)
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
	select {
	case <-p.closed:
		f()
	default:
		p.opAsync <- f
	}
}

func (p *Process) SetScope(scope scope.Scope) {
	p.scope = scope
	if p.client.Connected() {
		p.receiver.SetScope(scope)
	}
}

func (p *Process) SetShow(listeners bool, spots bool) {
	p.showListeners = listeners
	p.showSpots = spots
}

func (p *Process) SetThreshold(threshold int) {
	p.threshold = float32(threshold)
	if p.client.Connected() {
		p.receiver.SetPeakThreshold(float32(threshold))
	}
}

func (p *Process) SetSilenceTimeout(timeout time.Duration) {
	p.silenceTimeout = timeout
	if p.client.Connected() {
		p.receiver.SetSilenceTimeout(timeout)
	}
}

func (p *Process) SetAttachmentTimeout(timeout time.Duration) {
	p.attachmentTimeout = timeout
	if p.client.Connected() {
		p.receiver.SetAttachmentTimeout(timeout)
	}
}

func (p *Process) SetSignalDebounce(debounce int) {
	p.signalDebounce = debounce
	if p.client.Connected() {
		p.receiver.SetSignalDebounce(debounce)
	}
}

func (p *Process) onConnected(connected bool) {
	if !connected {
		return
	}

	bandwidth := -p.client.MinIFFrequency + p.client.MaxIFFrequency
	sampleRate := 48000
	blockSize := 2048 / partCount
	edgeWidth := int((float32(sampleRate-bandwidth) / 2.0) * (float32(blockSize) / float32(sampleRate)))
	p.receiver.SetEdgeWidth(edgeWidth)

	p.receiver.Start(sampleRate, blockSize)
	if p.threshold != 0 {
		p.receiver.SetPeakThreshold(p.threshold)
	}
	if p.signalDebounce != 0 {
		p.receiver.SetSignalDebounce(p.signalDebounce)
	}
	if p.silenceTimeout > 0 {
		p.receiver.SetSilenceTimeout(p.silenceTimeout)
	}
	if p.attachmentTimeout > 0 {
		p.receiver.SetAttachmentTimeout(p.attachmentTimeout)
	}
	p.receiver.SetScope(p.scope)

	p.client.SetIQSampleRate(48000)
	p.client.StartIQ(p.trx)
}

var (
	decodeColor tci.ARGB = tci.NewARGB(255, 0, 255, 0)
	spotColor   tci.ARGB = tci.NewARGB(255, 255, 255, 0)
)

func (p *Process) ListenerActivated(id string, frequency int) {
	p.doAsync(func() {
		p.showDecode(id, frequency)
	})
}

func (p *Process) showDecode(id string, frequency int) {
	if p.showListeners {
		p.client.DeleteSpot(id)
		p.client.AddSpot(id, tci.ModeCW, frequency, decodeColor, "SDRainer")
	}
}

func (p *Process) ListenerDeactivated(id string, _ int) {
	p.doAsync(func() { p.hideDecode(id) })
}

func (p *Process) hideDecode(id string) {
	p.client.DeleteSpot(id)
}

func (p *Process) CallsignDecoded(listener string, callsign string, frequency int, count int, weight int) {
	// ignore
}

func (p *Process) CallsignSpotted(_ string, callsign string, frequency int) {
	p.doAsync(func() {
		p.showSpot(callsign, frequency)
	})
}

func (p *Process) showSpot(callsign string, frequency int) {
	if p.showSpots {
		p.client.AddSpot(">"+callsign+"<", tci.ModeCW, frequency, spotColor, "SDRainer")
	}
	if p.spotter != nil {
		p.spotter.Spot(callsign, float64(frequency), "cw", time.Now().UTC())
	}
}

func (p *Process) SpotTimeout(_ string, callsign string, _ int) {
	p.doAsync(func() {
		p.hideSpot(callsign)
	})
}

func (p *Process) hideSpot(callsign string) {
	p.client.DeleteSpot(">" + callsign + "<")
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
	if vfo == tci.VFOB {
		return
	}

	l.process.receiver.SetVFOOffset(frequency)
}

func (l *tciListener) IQData(trx int, sampleRate tci.IQSampleRate, data []float32) {
	if trx != l.trx {
		return
	}

	partLen := len(data) / partCount
	for i := 0; i < partCount; i++ {
		begin := i * partLen
		end := begin + partLen
		l.process.receiver.IQData(int(sampleRate), data[begin:end])
	}
}

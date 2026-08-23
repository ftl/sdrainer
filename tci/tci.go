package tci

import (
	"fmt"
	"sort"
	"sync"
	"time"

	tci "github.com/ftl/tci/client"

	"github.com/ftl/sdrainer/cli"
	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/iq"
	"github.com/ftl/sdrainer/pipeline"
)

const (
	defaultHostname = "localhost"
	defaultPort     = 40001
	timeout         = 10 * time.Second

	// sampleRate is the rate of the IQ stream that we ask the TCI device for.
	sampleRate = 48000

	// maxTextLineLength is the length of one line of the text on the console.
	maxTextLineLength = 40
)

type (
	ChannelService = core.ChannelService[int]
	Spotter        = core.Spotter[int]
)

type Process struct {
	client   *tci.Client
	listener *tciListener
	trx      int

	// allTRX runs one pipeline for each receiver of the device. A TCI device holds more than one
	// receiver, and each of them gives its own IQ stream on its own frequency.
	allTRX bool

	peakThreshold  float64
	scope          core.ScopeService
	channelService ChannelService
	spotter        Spotter
	recorder       *iq.Writer
	showSpots      bool

	// The TCI device gives the center frequency with SetDDS, and that can happen before and after
	// the connection. The pipeline begins with the connection, and it takes each later value with
	// SetCenterFrequency.
	mutex     sync.Mutex
	receivers map[int]*receiver

	// callsigns holds the callsign that stands on the panorama of the TCI device for each channel,
	// so that the spot goes away with the channel.
	callsigns map[core.ChannelID]string

	opAsync chan func()
	close   chan struct{}
	closed  chan struct{}
}

// receiver holds the pipeline of one TRX and the frequency of its DDS. The frequency can arrive
// before the connection, thus before the pipeline exists.
type receiver struct {
	trx             int
	pipeline        *pipeline.Pipeline[float32, int]
	centerFrequency int
}

func New(host string, trx int, allTRX bool, peakThreshold float64, scope core.ScopeService, channelService ChannelService, spotter Spotter, recorder *iq.Writer, showSpots bool, traceTCI bool) (*Process, error) {
	tcpHost, err := cli.ParseTCPAddrArg(host, defaultHostname, defaultPort)
	if err != nil {
		return nil, fmt.Errorf("invalid TCI host: %v", err)
	}
	if tcpHost.Port == 0 {
		tcpHost.Port = defaultPort
	}
	if scope == nil {
		scope = &core.NullScopeService{}
	}
	if channelService == nil {
		channelService = &core.NullChannelService[int]{}
	}
	if spotter == nil {
		spotter = &core.NullSpotter[int]{}
	}

	client := tci.KeepOpen(tcpHost, timeout, traceTCI)

	result := &Process{
		client:         client,
		trx:            trx,
		allTRX:         allTRX,
		receivers:      make(map[int]*receiver),
		peakThreshold:  peakThreshold,
		scope:          scope,
		channelService: channelService,
		spotter:        spotter,
		recorder:       recorder,
		showSpots:      showSpots,
		callsigns:      make(map[core.ChannelID]string),
		opAsync:        make(chan func(), 100),
		close:          make(chan struct{}),
		closed:         make(chan struct{}),
	}
	result.listener = &tciListener{process: result, trx: result.trx}

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
			for _, trx := range p.runningTRX() {
				p.client.StopIQ(trx)
			}
			p.stopPipelines()
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

// onConnected builds one pipeline for each receiver and asks the TCI device for its IQ streams. The
// client calls this method again after each reconnection, and a pipeline that exists then stays as
// it is.
//
// The count of the receivers comes from the device. TCI sends it before the message that makes the
// client ready, and the client emits Connected only after that message, so the count is here.
func (p *Process) onConnected(connected bool) {
	if !connected {
		return
	}

	p.onConnectedWithoutClient(p.client.TRXCount)

	// the sample rate holds for each receiver of the device, so it needs one command
	p.client.SetIQSampleRate(sampleRate)
	for _, trx := range p.runningTRX() {
		p.client.StartIQ(trx)
	}
}

// onConnectedWithoutClient builds the pipelines. It is the part of onConnected that needs no client,
// so that a test can use it.
func (p *Process) onConnectedWithoutClient(trxCount int) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	for _, trx := range p.trxToRun(trxCount) {
		current := p.receiverOf(trx)
		if current.pipeline != nil {
			continue
		}
		current.pipeline = p.newPipeline(current)
	}
}

// trxToRun gives the receivers that this process runs. Without allTRX that is the one receiver of
// the flag, and with allTRX it is each receiver of the device.
//
// A device that names no count still gives one receiver: the count arrives with the connection, and
// a test builds the pipelines without a device.
func (p *Process) trxToRun(trxCount int) []int {
	if !p.allTRX {
		return []int{p.trx}
	}
	if trxCount < 1 {
		trxCount = 1
	}

	result := make([]int, trxCount)
	for i := range result {
		result[i] = i
	}
	return result
}

// firstTRX gives the receiver whose events go to the scope.
func (p *Process) firstTRX() int {
	if !p.allTRX {
		return p.trx
	}
	return 0
}

// newPipeline builds the pipeline of one receiver. The caller must hold the lock.
//
// **Only the first receiver gives its frames to the scope.** The scope shows one spectrum and one
// waterfall, and the frames of two receivers in one stream give a picture that no consumer can take
// apart.
//
// **Only the first receiver goes into a recording.** Two IQ streams in one file are not two streams
// any more: the samples stand one after the other and no consumer can separate them.
func (p *Process) newPipeline(current *receiver) *pipeline.Pipeline[float32, int] {
	config := pipeline.DefaultConfig(sampleRate, current.centerFrequency)
	config.PeakThreshold = p.peakThreshold

	scope := p.scope
	if current.trx != p.firstTRX() {
		scope = &core.NullScopeService{}
	}

	result := pipeline.New[float32, int](config, scope)
	if p.recorder != nil && current.trx == p.firstTRX() {
		// a nil pointer in the interface would not be nil
		result.SetRecorder(p.recorder)
	}
	result.SetSpotter(p.spotter)
	result.Notify(&trxListener{trx: current.trx, allTRX: p.allTRX, process: p, next: p.channelService})
	result.Start()

	return result
}

// receiverOf gives the receiver of one TRX and makes it if it does not exist. The caller must hold
// the lock.
func (p *Process) receiverOf(trx int) *receiver {
	current, ok := p.receivers[trx]
	if !ok {
		current = &receiver{trx: trx}
		p.receivers[trx] = current
	}
	return current
}

// runningTRX gives the receivers that hold a pipeline.
func (p *Process) runningTRX() []int {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	result := make([]int, 0, len(p.receivers))
	for trx, current := range p.receivers {
		if current.pipeline != nil {
			result = append(result, trx)
		}
	}
	sort.Ints(result)

	return result
}

func (p *Process) stopPipelines() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	for _, current := range p.receivers {
		if current.pipeline != nil {
			current.pipeline.Stop()
			current.pipeline = nil
		}
	}
}

func (p *Process) currentPipeline(trx int) *pipeline.Pipeline[float32, int] {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	current, ok := p.receivers[trx]
	if !ok {
		return nil
	}
	return current.pipeline
}

// IQData gives the samples of the TCI device to the pipeline. The STFT of the pipeline holds the
// samples that are left over, so the length of the data needs no block size.
func (p *Process) IQData(trx int, rate int, data []float32) {
	current := p.currentPipeline(trx)
	if current == nil {
		return
	}
	current.IQData(rate, data)
}

// setCenterFrequency takes the frequency of the DDS of the TCI device. The operator can turn the
// dial at any time, and the pipeline follows: a channel that is still inside the spectrum continues,
// and a channel outside it goes through IDLE to DEAD.
func (p *Process) setCenterFrequency(trx int, frequency int) {
	p.mutex.Lock()
	current := p.receiverOf(trx)
	current.centerFrequency = frequency
	running := current.pipeline
	p.mutex.Unlock()

	if running != nil {
		running.SetCenterFrequency(frequency)
	}
}

// handles tells if this process runs the given receiver. Without allTRX it is the one receiver of
// the flag, and with allTRX it is each receiver that the device has.
func (p *Process) handles(trx int) bool {
	return p.allTRX || trx == p.trx
}

/* the events of the pipeline */

var (
	decodeColor tci.ARGB = tci.NewARGB(255, 0, 255, 0)
	spotColor   tci.ARGB = tci.NewARGB(255, 255, 255, 0)
)

func (p *Process) ChannelDestroyed(channel core.Channel[int]) {
	callsign := p.callsigns[channel.ID]
	delete(p.callsigns, channel.ID)

	if callsign == "" {
		return
	}
	p.doAsync(func() {
		if callsign != "" {
			p.client.DeleteSpot(callsign)
		}
	})
}

// ChannelRunningCallsignDetected puts the callsign of a running station on the panorama of the TCI
// device. The pipeline gives the callsign to the spotter itself.
func (p *Process) ChannelRunningCallsignDetected(channel core.Channel[int]) {
	if !p.showSpots {
		return
	}

	name := channel.Callsign.String()
	previous := p.callsigns[channel.ID]
	p.callsigns[channel.ID] = name
	frequency := channel.Frequency

	p.doAsync(func() {
		if previous != "" && previous != name {
			p.client.DeleteSpot(previous)
		}
		p.client.AddSpot(name, tci.ModeCW, frequency, spotColor, "SDRainer")
	})
}

/* the listener of the TCI client */

type tciListener struct {
	process *Process
	trx     int
}

func (l *tciListener) Connected(connected bool) {
	l.process.onConnected(connected)
}

func (l *tciListener) SetDDS(trx int, frequency int) {
	if !l.process.handles(trx) {
		return
	}

	l.process.setCenterFrequency(trx, frequency)
}

func (l *tciListener) IQData(trx int, rate tci.IQSampleRate, data []float32) {
	if !l.process.handles(trx) {
		return
	}

	l.process.IQData(trx, int(rate), data)
}

/* the listener of one pipeline */

// trxListener takes the events of the pipeline of one receiver. It gives each event to the channel
// service and to the process.
//
// **It puts the number of the receiver in front of the id of each channel.** Each pipeline counts
// its channels from 1, so two receivers give the same id to two different stations. The id is the
// value with which a consumer holds a channel apart from each other channel, and the panorama of
// the TCI device uses it as well.
//
// The prefix comes only with allTRX: with one receiver there is nothing to hold apart, and the id
// then stays the id that each other source of SDRainer gives.
type trxListener struct {
	trx     int
	allTRX  bool
	process *Process
	next    ChannelService
}

func (l *trxListener) withTRX(channel core.Channel[int]) core.Channel[int] {
	if !l.allTRX {
		return channel
	}

	channel.ID = core.ChannelID(fmt.Sprintf("%d-%s", l.trx, channel.ID))
	return channel
}

func (l *trxListener) ChannelCreated(channel core.Channel[int]) {
	l.next.ChannelCreated(l.withTRX(channel))
}

func (l *trxListener) ChannelDestroyed(channel core.Channel[int]) {
	channel = l.withTRX(channel)
	l.next.ChannelDestroyed(channel)
	l.process.ChannelDestroyed(channel)
}

func (l *trxListener) ChannelStateChanged(channel core.Channel[int]) {
	l.next.ChannelStateChanged(l.withTRX(channel))
}

func (l *trxListener) ChannelCharacterReceived(channel core.Channel[int], character rune, offset int64) {
	l.next.ChannelCharacterReceived(l.withTRX(channel), character, offset)
}

func (l *trxListener) ChannelRunningCallsignDetected(channel core.Channel[int]) {
	channel = l.withTRX(channel)
	l.next.ChannelRunningCallsignDetected(channel)
	l.process.ChannelRunningCallsignDetected(channel)
}

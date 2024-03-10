package kiwi

import (
	"fmt"
	"log"
	"time"

	"github.com/ftl/sdrainer/rx"
	"github.com/ftl/sdrainer/trace"
)

const (
	blockSize    = 512 // blockSize is it the number of IQ samples, i.e. number of samples / 2
	maxBandwidth = 12_000
)

type Spotter interface {
	Spot(callsign string, frequency float64, msg string, timestamp time.Time)
}

type Process struct {
	client   *Client
	receiver *rx.Receiver[float32, int]
	spotter  Spotter

	threshold         float32
	centerFrequency   float64
	rxFrequency       float64
	signalDebounce    int
	silenceTimeout    time.Duration
	attachmentTimeout time.Duration
	tracer            trace.Tracer

	close chan struct{}
}

func New(host string, username string, password string, centerFrequency float64, bandwidth int, spotter Spotter) (*Process, error) {
	result := &Process{
		spotter:         spotter,
		centerFrequency: centerFrequency,
		rxFrequency:     centerFrequency,
		close:           make(chan struct{}),
	}
	result.receiver = rx.NewReceiver[float32, int]("", rx.StrainMode, rx.WallClock)

	edgeWidth := int(float32((maxBandwidth-bandwidth)/2) * (float32(blockSize) / float32(maxBandwidth)))
	result.receiver.SetEdgeWidth(edgeWidth)

	client, err := Open(host, username, password, centerFrequency, bandwidth, result)
	if err != nil {
		return nil, fmt.Errorf("cannot open KiwiSDR client: %v", err)
	}
	result.client = client

	return result, nil
}

func (p *Process) Close() {
	select {
	case <-p.close:
		return
	default:
		close(p.close)
		p.client.Close()
		p.receiver.Stop()
	}
}

func (p *Process) Connected(sampleRate int) {
	if sampleRate == 0 {
		log.Fatal("no audio rate!")
	}

	p.receiver.Start(sampleRate, blockSize)
	if p.threshold != 0 {
		p.receiver.SetPeakThreshold(p.threshold)
	}
	if p.rxFrequency != 0 {
		p.SetRXFrequency(p.rxFrequency)
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
	if p.tracer != nil {
		p.receiver.SetTracer(p.tracer)
		p.tracer.Start()
	}
}

func (p *Process) IQData(sampleRate int, data []float32) {
	const partSize = blockSize * 2
	if len(data)%partSize != 0 {
		panic(fmt.Errorf("data must be transferred in blocks of %d samples instead of %d", partSize, len(data)))
	}
	count := len(data) / partSize
	for i := 0; i < count; i++ {
		begin := i * partSize
		end := begin + partSize
		p.receiver.IQData(sampleRate, data[begin:end])
	}
}

func (p *Process) SetTracer(tracer trace.Tracer) {
	p.tracer = tracer
	p.receiver.SetTracer(tracer)
}

func (p *Process) SetThreshold(threshold int) {
	p.threshold = float32(threshold)
	p.receiver.SetPeakThreshold(float32(threshold))
}

func (p *Process) SetRXFrequency(frequency float64) {
	p.rxFrequency = frequency
	if frequency == 0 {
		return
	}

	delta := int(frequency - p.centerFrequency)
	p.receiver.SetVFOOffset(delta)
}

func (p *Process) SetSilenceTimeout(timeout time.Duration) {
	p.silenceTimeout = timeout
	p.receiver.SetSilenceTimeout(timeout)
}

func (p *Process) SetAttachmentTimeout(timeout time.Duration) {
	p.attachmentTimeout = timeout
	p.receiver.SetAttachmentTimeout(timeout)
}

func (p *Process) SetSignalDebounce(debounce int) {
	p.signalDebounce = debounce
	p.receiver.SetSignalDebounce(debounce)
}

func (p *Process) ListenerActivated(listener string, frequency int)   {}
func (p *Process) ListenerDeactivated(listener string, frequency int) {}
func (p *Process) CallsignDecoded(listener string, callsign string, frequency int, count int, weight int) {
}
func (p *Process) CallsignSpotted(listener string, callsign string, frequency int) {}
func (p *Process) SpotTimeout(listener string, callsign string, frequency int)     {}

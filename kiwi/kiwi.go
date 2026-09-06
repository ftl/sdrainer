package kiwi

import (
	"fmt"
	"sync"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/iq"
	"github.com/ftl/sdrainer/pipeline"
)

type (
	ChannelService = core.ChannelService[float64]
	Spotter        = core.Spotter[float64]
)

type Process struct {
	client *Client

	recorder        *iq.Writer
	centerFrequency float64
	peakThreshold   float64
	contest         string
	scope           core.ScopeService
	channelService  ChannelService
	spotter         Spotter

	// The KiwiSDR gives the sample rate with the Connected callback, and the pipeline needs that
	// rate at its construction. The pipeline therefore begins in Connected, which runs in the
	// goroutine of the client, and Close reads it from the goroutine of the caller.
	mutex    sync.Mutex
	pipeline *pipeline.Pipeline[float32, float64]

	close chan struct{}
}

// New connects to the given KiwiSDR and prepares the pipeline. It opens the client last, so that no
// callback of the client can arrive before the values above are complete.
func New(host string, username string, password string, centerFrequency float64, peakThreshold float64, contest string, scope core.ScopeService, channelService ChannelService, spotter Spotter, recorder *iq.Writer) (*Process, error) {
	if scope == nil {
		scope = &core.NullScopeService{}
	}
	if channelService == nil {
		channelService = &core.NullChannelService[float64]{}
	}
	if spotter == nil {
		spotter = &core.NullSpotter[float64]{}
	}

	result := &Process{
		recorder:        recorder,
		centerFrequency: centerFrequency,
		peakThreshold:   peakThreshold,
		contest:         contest,
		scope:           scope,
		channelService:  channelService,
		spotter:         spotter,
		close:           make(chan struct{}),
	}

	client, err := Open(host, username, password, centerFrequency, result)
	if err != nil {
		return nil, fmt.Errorf("cannot open KiwiSDR client: %v", err)
	}
	result.client = client

	return result, nil
}

// pipelineConfig gives the configuration of the pipeline for the given sample rate of the KiwiSDR.
func (p *Process) pipelineConfig(sampleRate int) pipeline.Config[float64] {
	result := pipeline.DefaultConfig(sampleRate, p.centerFrequency)
	result.PeakThreshold = p.peakThreshold
	result.Contest = p.contest
	return result
}

func (p *Process) Close() {
	select {
	case <-p.close:
		return
	default:
		close(p.close)
		if p.client != nil {
			p.client.Close()
		}

		p.mutex.Lock()
		defer p.mutex.Unlock()
		if p.pipeline != nil {
			p.pipeline.Stop()
			p.pipeline = nil
		}
	}
}

// Connected builds the pipeline with the sample rate of the KiwiSDR and starts it. The client calls
// this method one time, before the first call of IQData.
func (p *Process) Connected(sampleRate int) {
	if sampleRate == 0 {
		panic("no audio rate!")
	}

	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.pipeline != nil {
		return
	}

	result := pipeline.New[float32, float64](p.pipelineConfig(sampleRate), p.scope)
	if p.recorder != nil {
		// a nil pointer in the interface would not be nil, so the pipeline gets the recorder only
		// when it exists
		result.SetRecorder(p.recorder)
	}
	result.SetSpotter(p.spotter)
	result.Notify(p.channelService)
	result.Start()
	p.pipeline = result
}

// IQData gives the samples of the KiwiSDR to the pipeline. The STFT of the pipeline holds the
// samples that are left over, so the length of the data needs no block size.
func (p *Process) IQData(sampleRate int, data []float32) {
	p.mutex.Lock()
	current := p.pipeline
	p.mutex.Unlock()

	if current == nil {
		return
	}
	current.IQData(sampleRate, data)
}

func (p *Process) ListenerActivated(listener string, frequency int)   {}
func (p *Process) ListenerDeactivated(listener string, frequency int) {}
func (p *Process) CallsignDecoded(listener string, callsign string, frequency int, count int, weight int) {
}
func (p *Process) CallsignSpotted(listener string, callsign string, frequency int) {}
func (p *Process) SpotTimeout(listener string, callsign string, frequency int)     {}

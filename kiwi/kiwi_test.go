package kiwi

import (
	"sync"
	"testing"
	"time"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/pipeline"
	"github.com/ftl/sdrainer/pipeline/generator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testSampleRate = 12000 // the KiwiSDR gives this rate with the Connected callback
	testCenter     = 7028000.0
	testChunk      = 512 // IQ samples in one SND message of the KiwiSDR
	testSeconds    = 24
)

// testChannelService collects the events of the pipeline, as the gRPC service does.
type testChannelService struct {
	mutex     sync.Mutex
	created   []core.Channel[float64]
	text      []rune
	callsigns []string
	spots     []string
}

func (s *testChannelService) Active() bool { return true }

func (s *testChannelService) ChannelCreated(channel core.Channel[float64]) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.created = append(s.created, channel)
}

func (s *testChannelService) ChannelDestroyed(core.Channel[float64])    {}
func (s *testChannelService) ChannelStateChanged(core.Channel[float64]) {}

func (s *testChannelService) ChannelCharacterReceived(_ core.Channel[float64], character rune, _ int64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.text = append(s.text, character)
}

func (s *testChannelService) ChannelRunningCallsignDetected(channel core.Channel[float64]) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.callsigns = append(s.callsigns, channel.Callsign.String())
}

func (s *testChannelService) Spot(callsign string, _ float64, _ string, _ time.Time) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.spots = append(s.spots, callsign)
}

func (s *testChannelService) RemoveSpot(string) {}

func (s *testChannelService) result() ([]core.Channel[float64], string, []string, []string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.created, string(s.text), s.callsigns, s.spots
}

// newTestProcess makes a Process without a client. New opens the client, and a test has no
// KiwiSDR.
func newTestProcess(service *testChannelService) *Process {
	return &Process{
		centerFrequency: testCenter,
		peakThreshold:   pipeline.DefaultPeakThreshold,
		scope:           &core.NullScopeService{},
		channelService:  service,
		spotter:         service,
		close:           make(chan struct{}),
	}
}

// TestProcessDecodesTheStreamOfTheKiwiSDR gives the samples in the same way as the client: the
// Connected callback with the sample rate first, then the IQ data in the messages of the KiwiSDR.
func TestProcessDecodesTheStreamOfTheKiwiSDR(t *testing.T) {
	service := &testChannelService{}
	process := newTestProcess(service)

	process.Connected(testSampleRate)
	defer process.Close()

	source := generator.New[float32, float64](generator.GeneratorConfig[float64]{
		SampleRate:      testSampleRate,
		CenterFrequency: testCenter,
		NoiseLevel:      0.005,
		Seed:            1,
		Signals: []generator.CWSignal[float64]{
			{
				Frequency: testCenter + 1000, Text: "cq a1bc a1bc test", WPM: 30, Amplitude: 1.0,
				RiseTime: 5 * time.Millisecond,
			},
		},
	})

	data := make([]float32, 2*testChunk)
	for range testSeconds * testSampleRate / testChunk {
		source.Read(data)
		process.IQData(testSampleRate, data)
	}
	process.Close()

	created, text, callsigns, spots := service.result()
	require.NotEmptyf(t, created, "the stream must give a channel, text %q", text)
	assert.InDelta(t, testCenter+1000, created[0].Frequency, 30, "the frequency of the channel")
	assert.NotEmptyf(t, text, "the channel must give text")
	require.NotEmptyf(t, callsigns, "the running station must give its callsign, text %q", text)
	assert.Equal(t, "A1BC", callsigns[0])
	// The pipeline spots a callsign again when that callsign becomes valid, so the count of the
	// spots is not the count of the stations. Each spot must name the running station.
	require.NotEmpty(t, spots, "the running station must give a spot")
	for i, spot := range spots {
		assert.Equalf(t, "A1BC", spot, "the spot %d must name the running station", i)
	}
}

// TestProcessIgnoresDataBeforeConnected checks the order of the callbacks of the client. The
// pipeline needs the sample rate of Connected, so it does not exist before that callback.
func TestProcessIgnoresDataBeforeConnected(t *testing.T) {
	process := newTestProcess(&testChannelService{})
	defer process.Close()

	assert.NotPanics(t, func() {
		process.IQData(testSampleRate, make([]float32, 2*testChunk))
	})
}

// TestProcessUsesTheSampleRateOfTheKiwiSDR checks that the pipeline takes its sizes from the rate
// that the KiwiSDR reports. doc/architecture.md, section 8.2, holds these values.
func TestProcessUsesTheSampleRateOfTheKiwiSDR(t *testing.T) {
	process := newTestProcess(&testChannelService{})
	process.Connected(testSampleRate)
	defer process.Close()

	derived := process.pipeline.Derived()

	assert.Equal(t, 1024, derived.BlockSize)
	assert.Equal(t, 256, derived.Hop)
	assert.Equal(t, 256, derived.DecodeBlockSize)
	assert.Equal(t, 64, derived.DecodeHop)
	assert.InDelta(t, 11.72, derived.BinWidth, 0.01)
	assert.Equal(t, 10, derived.ConfirmCount)
}

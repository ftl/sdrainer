package tci

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/pipeline"
	"github.com/ftl/sdrainer/pipeline/generator"
)

// newAllTRXProcess makes a Process for each receiver of a device, without a client.
func newAllTRXProcess(service *testChannelService, scope core.ScopeService) *Process {
	if scope == nil {
		scope = &core.NullScopeService{}
	}

	return &Process{
		allTRX:         true,
		receivers:      make(map[int]*receiver),
		peakThreshold:  pipeline.DefaultPeakThreshold,
		scope:          scope,
		channelService: service,
		spotter:        service,
		callsigns:      make(map[core.ChannelID]string),
		opAsync:        make(chan func(), 100),
		close:          make(chan struct{}),
		closed:         make(chan struct{}),
	}
}

func TestAllTRXBuildsOnePipelineForEachReceiver(t *testing.T) {
	process := newAllTRXProcess(newTestChannelService(), nil)

	process.onConnectedWithoutClient(3)
	defer process.stopPipelines()

	assert.Equal(t, []int{0, 1, 2}, process.runningTRX())
	for trx := range 3 {
		assert.NotNilf(t, process.currentPipeline(trx), "the pipeline of the trx %d", trx)
	}
}

// TestOneTRXBuildsOnePipeline is the other side: without the flag the count of the device changes
// nothing, and only the receiver of --trx runs.
func TestOneTRXBuildsOnePipeline(t *testing.T) {
	process := newTestProcess(newTestChannelService())
	process.trx = 1

	process.onConnectedWithoutClient(3)
	defer process.stopPipelines()

	assert.Equal(t, []int{1}, process.runningTRX())
	assert.Nil(t, process.currentPipeline(0), "the other receivers of the device do not run")
}

// TestAllTRXTakesTheFrequencyOfEachReceiver covers the DDS: each receiver has its own frequency, and
// each of them can arrive before the connection.
func TestAllTRXTakesTheFrequencyOfEachReceiver(t *testing.T) {
	process := newAllTRXProcess(newTestChannelService(), nil)

	process.setCenterFrequency(0, 7028000)
	process.setCenterFrequency(1, 14028000)
	process.onConnectedWithoutClient(2)
	defer process.stopPipelines()

	assert.Equal(t, 7028000, process.currentPipeline(0).CenterFrequency())
	assert.Equal(t, 14028000, process.currentPipeline(1).CenterFrequency())

	process.setCenterFrequency(1, 21028000)
	assert.Equal(t, 21028000, process.currentPipeline(1).CenterFrequency(), "a frequency after the connection")
	assert.Equal(t, 7028000, process.currentPipeline(0).CenterFrequency(), "the other receiver stays")
}

// TestAllTRXIgnoresAReceiverThatDoesNotRun covers the IQ data of a receiver whose pipeline does not
// exist: the device can send it before the connection is complete.
func TestAllTRXIgnoresAReceiverThatDoesNotRun(t *testing.T) {
	process := newAllTRXProcess(newTestChannelService(), nil)
	process.onConnectedWithoutClient(1)
	defer process.stopPipelines()

	assert.NotPanics(t, func() {
		process.IQData(2, sampleRate, make([]float32, 2*testChunk))
	})
}

// countingScope counts the frames that reach it, so that a test can see which receiver writes to
// the scope.
type countingScope struct {
	spectral int
	timed    int
}

func (s *countingScope) Active() bool { return true }

func (s *countingScope) SendSpectralFrame(*core.SpectralFrame) { s.spectral++ }
func (s *countingScope) SendTimeFrame(*core.TimeFrame)         { s.timed++ }

// TestAllTRXGivesTheScopeOnlyToTheFirstReceiver holds the rule of the scope: it shows one spectrum,
// and the frames of two receivers in one stream give a picture that no consumer can take apart.
func TestAllTRXGivesTheScopeOnlyToTheFirstReceiver(t *testing.T) {
	scope := &countingScope{}
	process := newAllTRXProcess(newTestChannelService(), scope)
	process.onConnectedWithoutClient(2)
	defer process.stopPipelines()

	// only the second receiver gets samples, so each frame of the scope would come from it
	samples := make([]float32, 2*testChunk)
	for range 20 {
		process.IQData(1, sampleRate, samples)
	}
	process.stopPipelines()

	assert.Zero(t, scope.spectral, "the second receiver must write no spectral frame")
	assert.Zero(t, scope.timed, "the second receiver must write no time frame")
}

func TestAllTRXGivesTheScopeToTheFirstReceiver(t *testing.T) {
	scope := &countingScope{}
	process := newAllTRXProcess(newTestChannelService(), scope)
	process.onConnectedWithoutClient(2)
	defer process.stopPipelines()

	samples := make([]float32, 2*testChunk)
	for range 20 {
		process.IQData(0, sampleRate, samples)
	}
	process.stopPipelines()

	assert.NotZero(t, scope.spectral, "the first receiver writes the frames of the scope")
}

// TestAllTRXHoldsTheChannelsOfTheReceiversApart is the reason for the prefix of the id: each
// pipeline counts its channels from 1, so two receivers give the same id to two different stations.
func TestAllTRXHoldsTheChannelsOfTheReceiversApart(t *testing.T) {
	service := newTestChannelService()
	process := newAllTRXProcess(service, nil)
	process.setCenterFrequency(0, testCenter)
	process.setCenterFrequency(1, testCenter+1000000)
	process.onConnectedWithoutClient(2)
	defer process.stopPipelines()

	// the same scene on both receivers, so both give a channel with the same id of its pipeline
	for trx := range 2 {
		center := testCenter + trx*1000000
		source := generator.New[float32, float64](generator.GeneratorConfig[float64]{
			SampleRate: sampleRate, CenterFrequency: float64(center), NoiseLevel: 0.01, Seed: 1,
			Signals: []generator.CWSignal[float64]{
				{
					Frequency: float64(center + testOffset), Text: "cq a1bc a1bc test", WPM: 30,
					Amplitude: 1.0, RiseTime: 5 * time.Millisecond,
				},
			},
		})

		data := make([]float32, 2*testChunk)
		for range 12 * sampleRate / testChunk {
			source.Read(data)
			process.IQData(trx, sampleRate, data)
		}
	}
	process.stopPipelines()

	ids := service.channelIDs()
	require.NotEmpty(t, ids, "the receivers must give channels")

	var first, second int
	for _, id := range ids {
		switch {
		case strings.HasPrefix(string(id), "0-"):
			first++
		case strings.HasPrefix(string(id), "1-"):
			second++
		default:
			t.Errorf("the id %q holds no receiver", id)
		}
	}
	assert.NotZero(t, first, "the first receiver must give a channel")
	assert.NotZero(t, second, "the second receiver must give a channel")
}

// TestOneTRXKeepsThePlainChannelID holds the other side: with one receiver there is nothing to hold
// apart, and the id stays the id that each other source of SDRainer gives.
func TestOneTRXKeepsThePlainChannelID(t *testing.T) {
	service := newTestChannelService()
	process := newTestProcess(service)
	process.setCenterFrequency(0, testCenter)
	process.onConnectedWithoutClient(1)
	defer process.stopPipelines()

	source := generator.New[float32, float64](generator.GeneratorConfig[float64]{
		SampleRate: sampleRate, CenterFrequency: float64(testCenter), NoiseLevel: 0.01, Seed: 1,
		Signals: []generator.CWSignal[float64]{
			{
				Frequency: float64(testCenter + testOffset), Text: "cq a1bc a1bc test", WPM: 30,
				Amplitude: 1.0, RiseTime: 5 * time.Millisecond,
			},
		},
	})

	data := make([]float32, 2*testChunk)
	for range 12 * sampleRate / testChunk {
		source.Read(data)
		process.IQData(0, sampleRate, data)
	}
	process.stopPipelines()

	ids := service.channelIDs()
	require.NotEmpty(t, ids)
	for _, id := range ids {
		assert.NotContainsf(t, string(id), "-", "the id %q must hold no receiver", id)
	}
}

// TestAllTRXHandlesEachReceiver covers the filter of the listener: without the flag only the
// receiver of --trx counts, and with the flag each of them.
func TestAllTRXHandlesEachReceiver(t *testing.T) {
	one := newTestProcess(newTestChannelService())
	one.trx = 1
	assert.True(t, one.handles(1))
	assert.False(t, one.handles(0))

	all := newAllTRXProcess(newTestChannelService(), nil)
	assert.True(t, all.handles(0))
	assert.True(t, all.handles(3))
}

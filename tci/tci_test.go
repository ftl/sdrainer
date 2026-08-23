package tci

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
	testCenter  = 14_020_000
	testOffset  = 1000
	testChunk   = 2048 // IQ samples in one message of the TCI device
	testSeconds = 24
)

// testChannelService collects the events of the pipeline, as the gRPC service does.
type testChannelService struct {
	mutex     sync.Mutex
	frequency map[core.ChannelID]int
	text      map[core.ChannelID][]rune
	spots     []string
}

func newTestChannelService() *testChannelService {
	return &testChannelService{
		frequency: make(map[core.ChannelID]int),
		text:      make(map[core.ChannelID][]rune),
	}
}

func (s *testChannelService) Active() bool { return true }

func (s *testChannelService) ChannelCreated(channel core.Channel[int]) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.frequency[channel.ID] = channel.Frequency
}

func (s *testChannelService) ChannelDestroyed(core.Channel[int])    {}
func (s *testChannelService) ChannelStateChanged(core.Channel[int]) {}

func (s *testChannelService) ChannelCharacterReceived(channel core.Channel[int], character rune, _ int64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.text[channel.ID] = append(s.text[channel.ID], character)
	s.frequency[channel.ID] = channel.Frequency
}

func (s *testChannelService) ChannelRunningCallsignDetected(core.Channel[int]) {}

func (s *testChannelService) Spot(callsign string, _ int, _ string, _ time.Time) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.spots = append(s.spots, callsign)
}

func (s *testChannelService) RemoveSpot(string) {}

// channelIDs gives the id of each channel that the service saw.
func (s *testChannelService) channelIDs() []core.ChannelID {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	result := make([]core.ChannelID, 0, len(s.frequency))
	for id := range s.frequency {
		result = append(result, id)
	}
	return result
}

func (s *testChannelService) textNear(frequency int, tolerance int) (string, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for id, actual := range s.frequency {
		if actual-frequency < tolerance && frequency-actual < tolerance {
			return string(s.text[id]), true
		}
	}
	return "", false
}

// newTestProcess makes a Process without a client. New opens a client, and a test has no TCI
// device.
func newTestProcess(service *testChannelService) *Process {
	return &Process{
		receivers:      make(map[int]*receiver),
		peakThreshold:  pipeline.DefaultPeakThreshold,
		scope:          &core.NullScopeService{},
		channelService: service,
		spotter:        service,
		callsigns:      make(map[core.ChannelID]string),
		opAsync:        make(chan func(), 100),
		close:          make(chan struct{}),
		closed:         make(chan struct{}),
	}
}

// TestProcessDecodesTheStreamOfTheTCIDevice gives the samples in the same way as the client: the
// center frequency first, then the connection, then the IQ data.
func TestProcessDecodesTheStreamOfTheTCIDevice(t *testing.T) {
	service := newTestChannelService()
	process := newTestProcess(service)

	// the TCI device gives the frequency of its DDS before the connection is complete
	process.setCenterFrequency(0, testCenter)
	process.onConnectedWithoutClient(1)
	defer process.stopPipelines()

	source := generator.New[float32, float64](generator.GeneratorConfig[float64]{
		SampleRate:      sampleRate,
		CenterFrequency: float64(testCenter),
		NoiseLevel:      0.01,
		Seed:            1,
		Signals: []generator.CWSignal[float64]{
			{
				Frequency: float64(testCenter + testOffset), Text: "cq a1bc a1bc test", WPM: 30,
				Amplitude: 1.0, RiseTime: 5 * time.Millisecond,
			},
		},
	})

	data := make([]float32, 2*testChunk)
	for range testSeconds * sampleRate / testChunk {
		source.Read(data)
		process.IQData(0, sampleRate, data)
	}
	process.stopPipelines()

	text, found := service.textNear(testCenter+testOffset, 100)
	require.Truef(t, found, "the stream must give a channel at %d Hz", testCenter+testOffset)
	assert.Contains(t, text, "a1bc", "the text of the signal")
	// the pipeline spots a callsign again when that callsign becomes valid
	require.NotEmpty(t, service.spots, "the running station of the stream")
	for i, spot := range service.spots {
		assert.Equalf(t, "A1BC", spot, "the spot %d must name the running station", i)
	}
}

// TestProcessUsesTheFrequencyOfTheDDS checks that the pipeline takes the center frequency that the
// TCI device gives, also when that value comes before the connection.
func TestProcessUsesTheFrequencyOfTheDDS(t *testing.T) {
	process := newTestProcess(newTestChannelService())

	process.setCenterFrequency(0, testCenter)
	process.onConnectedWithoutClient(1)
	defer process.stopPipelines()

	derived := process.currentPipeline(0).Derived()
	assert.Equal(t, 4096, derived.BlockSize, "the block size at 48 kHz")
	assert.Equal(t, 1024, derived.Hop)
	assert.Equal(t, testCenter, process.receivers[0].centerFrequency)
}

// TestProcessFollowsTheDial checks that a frequency of the DDS that comes after the connection
// reaches the pipeline.
func TestProcessFollowsTheDial(t *testing.T) {
	process := newTestProcess(newTestChannelService())
	process.onConnectedWithoutClient(1)
	defer process.stopPipelines()

	process.setCenterFrequency(0, testCenter+5000)

	assert.Equal(t, testCenter+5000, process.currentPipeline(0).CenterFrequency())
}

func TestProcessIgnoresDataBeforeTheConnection(t *testing.T) {
	process := newTestProcess(newTestChannelService())

	assert.NotPanics(t, func() {
		process.IQData(0, sampleRate, make([]float32, 2*testChunk))
	})
}

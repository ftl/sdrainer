package replay

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/iq"
	"github.com/ftl/sdrainer/pipeline"
	"github.com/ftl/sdrainer/pipeline/generator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testSampleRate = 12000
	testOffset     = 1000.0
	testSeconds    = 24
)

type testChannelService struct {
	mutex     sync.Mutex
	frequency map[core.ChannelID]float64
	text      map[core.ChannelID][]rune
	spots     []string
}

func newTestChannelService() *testChannelService {
	return &testChannelService{
		frequency: make(map[core.ChannelID]float64),
		text:      make(map[core.ChannelID][]rune),
	}
}

func (s *testChannelService) Active() bool { return true }

func (s *testChannelService) ChannelCreated(channel core.Channel[float64]) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.frequency[channel.ID] = channel.Frequency
}

func (s *testChannelService) ChannelDestroyed(core.Channel[float64])    {}
func (s *testChannelService) ChannelStateChanged(core.Channel[float64]) {}

func (s *testChannelService) ChannelCharacterReceived(channel core.Channel[float64], character rune, _ int64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.text[channel.ID] = append(s.text[channel.ID], character)
	s.frequency[channel.ID] = channel.Frequency
}

func (s *testChannelService) ChannelRunningCallsignDetected(core.Channel[float64]) {}

func (s *testChannelService) Spot(callsign string, _ float64, _ string, _ time.Time) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.spots = append(s.spots, callsign)
}

func (s *testChannelService) RemoveSpot(string) {}

// textNear gives the text of the channel at the given frequency.
func (s *testChannelService) textNear(frequency float64, tolerance float64) (string, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for id, actual := range s.frequency {
		if actual-frequency < tolerance && frequency-actual < tolerance {
			return string(s.text[id]), true
		}
	}
	return "", false
}

// writeRecording makes a recording with one CW signal, as --record does.
func writeRecording(t *testing.T, filename string) {
	t.Helper()

	writer, err := iq.NewWriter(filename)
	require.NoError(t, err)

	source := generator.New[float32, float64](generator.GeneratorConfig[float64]{
		SampleRate:      testSampleRate,
		CenterFrequency: 0,
		NoiseLevel:      0.005,
		Seed:            1,
		Signals: []generator.CWSignal[float64]{
			{
				Frequency: testOffset, Text: "cq a1bc a1bc test", WPM: 30, Amplitude: 1.0,
				RiseTime: 5 * time.Millisecond,
			},
		},
	})

	chunk := make([]float32, 2*1024)
	for range testSeconds * testSampleRate / 1024 {
		source.Read(chunk)
		require.NoError(t, writer.RecordIQ(chunk))
	}
	require.NoError(t, writer.Close())
}

// TestRecordAndReplayGiveTheSameSignal is the loop that the tests of the decoder need: the samples
// of a source go into a file, and the pipeline gives the same result from that file.
func TestRecordAndReplayGiveTheSameSignal(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "test.iq")
	writeRecording(t, filename)

	service := newTestChannelService()
	err := Run(context.Background(), Options{
		Filename:      filename,
		SampleRate:    testSampleRate,
		PeakThreshold: pipeline.DefaultPeakThreshold,
	}, nil, service, service, nil)
	require.NoError(t, err)

	// the center frequency is 0, so the frequency of the channel is the offset of the signal
	text, found := service.textNear(testOffset, 100)
	require.Truef(t, found, "the replay must give a channel at %+.0f Hz", testOffset)
	assert.Contains(t, text, "a1bc", "the text of the recorded signal")
	// the pipeline spots a callsign again when that callsign becomes valid
	require.NotEmpty(t, service.spots, "the running station of the recording")
	for i, spot := range service.spots {
		assert.Equalf(t, "A1BC", spot, "the spot %d must name the running station", i)
	}
}

// TestReplayUsesTheCenterFrequency checks that a channel gets an absolute frequency when the caller
// knows the center frequency of the recording.
func TestReplayUsesTheCenterFrequency(t *testing.T) {
	const center = 14_020_000.0

	filename := filepath.Join(t.TempDir(), "test.iq")
	writeRecording(t, filename)

	service := newTestChannelService()
	err := Run(context.Background(), Options{
		Filename:        filename,
		SampleRate:      testSampleRate,
		CenterFrequency: center,
		PeakThreshold:   pipeline.DefaultPeakThreshold,
	}, nil, service, service, nil)
	require.NoError(t, err)

	_, found := service.textNear(center+testOffset, 100)
	assert.Truef(t, found, "the channel must be at %.0f Hz", center+testOffset)
}

func TestReplayGivesAnErrorForAMissingFile(t *testing.T) {
	err := Run(context.Background(), Options{
		Filename:   filepath.Join(t.TempDir(), "no-such-file.iq"),
		SampleRate: testSampleRate,
	}, nil, newTestChannelService(), nil, nil)

	assert.Error(t, err)
}

// TestReplayStopsWithTheContext checks that a replay that runs in real time comes back when the
// user stops it.
func TestReplayStopsWithTheContext(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "test.iq")
	writeRecording(t, filename)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := Run(ctx, Options{
		Filename:      filename,
		SampleRate:    testSampleRate,
		PeakThreshold: pipeline.DefaultPeakThreshold,
		Realtime:      true,
	}, nil, newTestChannelService(), nil, nil)

	require.NoError(t, err)
	assert.Less(t, time.Since(start), 5*time.Second, "the replay must not run the whole recording")
}

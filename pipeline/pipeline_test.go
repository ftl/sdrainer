package pipeline

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/scope/client"
)

const (
	testSampleRate = 48000
	testBlockSize  = 1024 // 46.875 Hz for each bin
	testCenter     = 7020000.0
	testOffset     = 3000.0 // the tone sits 64 bins above the center
)

// recordingServer collects the frames and the channel events of the pipeline, so a test can look at
// them.
type recordingServer struct {
	mutex         sync.Mutex
	frames        []core.SpectralFrame
	channelEvents []any
}

func (s *recordingServer) Active() bool { return true }

func (s *recordingServer) SendSpectralFrame(frame *core.SpectralFrame) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// the pipeline uses its own slices again, so the test must copy them
	recorded := *frame
	recorded.Values = append([]float64(nil), frame.Values...)
	recorded.FrequencyMarkers = make(map[core.MarkerID]float64, len(frame.FrequencyMarkers))
	for marker, value := range frame.FrequencyMarkers {
		recorded.FrequencyMarkers[marker] = value
	}
	s.frames = append(s.frames, recorded)
}

func (s *recordingServer) SendTimeFrame(frame *core.TimeFrame) {}

func (s *recordingServer) streamFrames(stream core.StreamID) []core.SpectralFrame {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	var result []core.SpectralFrame
	for _, frame := range s.frames {
		if frame.Stream == stream {
			result = append(result, frame)
		}
	}
	return result
}

func (s *recordingServer) ChannelCreated(channel core.Channel[float64]) {
	s.putChannelEvent(client.ChannelCreated{Channel: channel})
}

func (s *recordingServer) ChannelDestroyed(channel core.Channel[float64]) {
	s.putChannelEvent(client.ChannelDestroyed{Channel: channel})
}

func (s *recordingServer) ChannelStateChanged(channel core.Channel[float64]) {
	s.putChannelEvent(client.ChannelStateChanged{Channel: channel})
}

func (s *recordingServer) ChannelCharacterReceived(channel core.Channel[float64], character rune, offset int64) {
	s.putChannelEvent(client.ChannelCharacterReceived{Channel: channel, Character: character, Offset: offset})
}

func (s *recordingServer) putChannelEvent(event any) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.channelEvents = append(s.channelEvents, event)
}

func (s *recordingServer) recordedChannelEvents() []any {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return append([]any(nil), s.channelEvents...)
}

// testConfig gives a bin width that leads to a block size of testBlockSize, thus 46.875 Hz at a
// sample rate of 48 kHz. MaxWPM 0 switches the decode tier off, because these tests need no text.
func testConfig() Config[float64] {
	return Config[float64]{
		SampleRate:      testSampleRate,
		CenterFrequency: testCenter,
		BinWidth:        testSampleRate / float64(testBlockSize), // 46.875 Hz, thus a block size of testBlockSize

		MinWPM: 8,
		MaxWPM: 0,

		PeakThreshold:  8,
		NoiseFloorTime: 3 * time.Second,
		DeadTimeout:    20 * time.Second,

		ScopeFrameRate: 100000, // no decimation in the test
	}
}

// noisyTone gives a chunk of interleaved IQ samples with one continuous tone and white noise.
func noisyTone(samples int, offset float64, amplitude float64, noise float64, seed uint64) []float32 {
	random := rand.New(rand.NewPCG(seed, seed+0x9e3779b9))

	result := make([]float32, 2*samples)
	for i := range samples {
		phase := 2 * math.Pi * offset * float64(i) / testSampleRate
		result[2*i] = float32(amplitude*math.Cos(phase) + random.NormFloat64()*noise)
		result[2*i+1] = float32(amplitude*math.Sin(phase) + random.NormFloat64()*noise)
	}
	return result
}

func runPipeline(t *testing.T, config Config[float64], chunks int) *recordingServer {
	t.Helper()

	server := &recordingServer{}
	p := New[float32, float64](config, server)
	p.Notify(server) // for channel events
	p.Start()
	derived := Derive(config)
	for i := range chunks {
		p.IQData(testSampleRate, noisyTone(derived.Hop, testOffset, 0.5, 0.001, uint64(i)))
	}
	p.Stop()

	return server
}

func TestPipelineShowsBothSpectralStreams(t *testing.T) {
	const chunks = 4

	server := runPipeline(t, testConfig(), chunks)

	spectrum := server.streamFrames(ScopeSpectrum)
	assert.NotEmpty(t, spectrum, "spectrum")
	assert.Len(t, server.streamFrames(ScopeNoiseFloor), len(spectrum), "the two streams must give the same count")
}

func TestPipelineShowsTheTrueFrequencyLimits(t *testing.T) {
	// the first frame needs a full window, thus overlapFactor chunks of one hop
	server := runPipeline(t, testConfig(), overlapFactor)

	frames := server.streamFrames(ScopeSpectrum)
	require.NotEmpty(t, frames)

	binWidth := float64(testSampleRate) / float64(testBlockSize)
	assert.InDelta(t, testCenter-testSampleRate/2-binWidth/2, frames[0].FromFrequency, 1, "from")
	assert.InDelta(t, testCenter+testSampleRate/2-binWidth/2, frames[0].ToFrequency, 1, "to")
}

func TestPipelineFindsTheTone(t *testing.T) {
	config := testConfig()
	config.PeakThreshold = 12

	server := runPipeline(t, config, 4)

	frames := server.streamFrames(ScopeSpectrum)
	require.NotEmpty(t, frames)

	last := frames[len(frames)-1]
	require.Len(t, last.FrequencyMarkers, 1, "one tone must give one peak")
	assert.InDelta(t, testCenter+testOffset, last.FrequencyMarkers["peak_0"], 1, "the peak frequency")
}

// TestPipelineThresholdControlsTheFalsePeaks shows why a single frame needs a higher threshold than
// the 6 dB to 10 dB of section 4.2. The power of a bin with noise has an exponential distribution,
// so the number of bins above the threshold falls with exp(-ratio). Section 4.2 works with the
// signal tracker of section 5, which needs a detection in several frames before it opens a channel.
// Without that tracker, each false bin of each frame becomes a peak.
func TestPipelineThresholdControlsTheFalsePeaks(t *testing.T) {
	tt := []struct {
		threshold float64
		maxPeaks  int
	}{
		{threshold: 6, maxPeaks: 40},
		{threshold: 8, maxPeaks: 20},
		{threshold: 12, maxPeaks: 1},
		{threshold: 20, maxPeaks: 1},
	}

	var previous int
	for i, tc := range tt {
		t.Run(fmt.Sprintf("%.0fdB", tc.threshold), func(t *testing.T) {
			config := testConfig()
			config.PeakThreshold = tc.threshold

			server := runPipeline(t, config, 8)

			frames := server.streamFrames(ScopeSpectrum)
			require.NotEmpty(t, frames)
			peaks := len(frames[len(frames)-1].FrequencyMarkers)

			assert.GreaterOrEqual(t, peaks, 1, "the tone must always be there")
			assert.LessOrEqual(t, peaks, tc.maxPeaks)
			if i > 0 {
				assert.LessOrEqual(t, peaks, previous, "a higher threshold must not give more peaks")
			}
			previous = peaks
		})
	}
}

func TestPipelineShowsTheSpectrumInDecibel(t *testing.T) {
	server := runPipeline(t, testConfig(), 4)

	frames := server.streamFrames(ScopeSpectrum)
	require.NotEmpty(t, frames)
	last := frames[len(frames)-1]

	// a tone with the amplitude 0.5 gives the power 0.25 with a normalized window, thus -6 dB
	signalBin := testBlockSize/2 + int(testOffset/(float64(testSampleRate)/float64(testBlockSize)))
	assert.InDelta(t, -6.02, last.Values[signalBin], 0.5, "the level of the tone")

	for i, value := range last.Values {
		assert.Falsef(t, math.IsInf(value, 0), "bin %d must not be infinite", i)
		assert.Falsef(t, math.IsNaN(value), "bin %d must not be NaN", i)
	}
}

func TestPipelineDecimatesTheFramesForTheScope(t *testing.T) {
	config := testConfig()
	config.ScopeFrameRate = 10

	derived := Derive(config)
	decimation := scopeDecimation(derived.FrameInterval, config.ScopeFrameRate)
	require.Greater(t, decimation, 1, "the test needs a decimation")

	const chunks = 200
	server := runPipeline(t, config, chunks)

	// each chunk is one hop, and the first frame needs a full window
	frames := chunks - overlapFactor + 1
	// the pipeline shows the first frame and then each frame of the decimation
	expected := 1 + (frames-1)/decimation
	assert.Len(t, server.streamFrames(ScopeSpectrum), expected, "the frames of the scope")
}

func TestPipelineIgnoresAWrongSampleRate(t *testing.T) {
	server := &recordingServer{}
	p := New[float32, float64](testConfig(), server)
	p.Notify(server) // for channel events
	p.Start()

	p.IQData(24000, noisyTone(testBlockSize, testOffset, 0.5, 0.001, 1))

	p.Stop()

	assert.Empty(t, server.frames)
}

func TestPipelineWithoutStart(t *testing.T) {
	server := &recordingServer{}
	p := New[float32, float64](testConfig(), server)

	assert.NotPanics(t, func() {
		p.IQData(testSampleRate, noisyTone(testBlockSize, testOffset, 0.5, 0.001, 1))
		p.Stop()
	})
	assert.Empty(t, server.frames)
}

func TestPipelineWithoutScope(t *testing.T) {
	p := New[float32, float64](testConfig(), nil)
	p.Start()

	assert.NotPanics(t, func() {
		p.IQData(testSampleRate, noisyTone(testBlockSize, testOffset, 0.5, 0.001, 1))
	})

	p.Stop()
}

func TestPipelineSendsTheChannelEventsToTheScope(t *testing.T) {
	server := &recordingServer{}
	p := New[float32, float64](testConfig(), server)
	p.Notify(server) // for channel events

	channel := core.Channel[float64]{ID: "3", Frequency: 7020000, WPM: 25, SNR: 17.5, State: core.ConfirmedChannel}
	scopeChannel := core.Channel[float64]{ID: "3", Frequency: 7020000, WPM: 25, SNR: 17.5, State: core.ConfirmedChannel}

	idleChannel := scopeChannel
	idleChannel.State = core.IdleChannel

	p.emitChannelCreated(channel)
	p.emitChannelStateChanged(idleChannel)
	p.emitChannelCharacterReceived(channel, 'k', 4711)
	p.emitChannelDestroyed(channel)

	assert.Equal(t, []any{
		client.ChannelCreated{Channel: scopeChannel},
		client.ChannelStateChanged{Channel: idleChannel},
		client.ChannelCharacterReceived{Channel: scopeChannel, Character: 'k', Offset: 4711},
		client.ChannelDestroyed{Channel: scopeChannel},
	}, server.recordedChannelEvents())
}

func TestPipelineFeedsTheTwoTiers(t *testing.T) {
	const samples = 4096
	config := testConfig()
	config.MaxWPM = 150 // gives a decode block size of 256

	derived := Derive(config)
	require.Equal(t, 1024, derived.BlockSize)
	require.Equal(t, 256, derived.Hop)
	require.Equal(t, 256, derived.DecodeBlockSize)
	require.Equal(t, 64, derived.DecodeHop)
	detectionSize, detectionHop := derived.BlockSize, derived.Hop
	decodeSize, decodeHop := derived.DecodeBlockSize, derived.DecodeHop

	p := New[float32, float64](config, nil)

	p.Start()
	p.IQData(testSampleRate, make([]float32, 2*samples))
	p.Stop()

	// the first frame needs a full window, and each further frame needs one hop
	assert.Equal(t, uint64((samples-detectionSize)/detectionHop+1), p.stft.sequence, "frames of the detection tier")
	assert.Equal(t, uint64((samples-decodeSize)/decodeHop+1), p.decodeStft.sequence, "frames of the decode tier")
	assert.Equal(t, int64(p.decodeStft.sequence)*int64(decodeHop), p.decodeStft.offset, "the offset advances by one hop for each frame")
}

func TestPipelineWithoutADecodeTier(t *testing.T) {
	// MaxWPM 0 switches the decode tier off, and the pipeline must still run
	config := testConfig()
	config.MaxWPM = 0
	require.Zero(t, Derive(config).DecodeBlockSize)

	p := New[float32, float64](config, nil)

	p.Start()
	p.IQData(testSampleRate, make([]float32, 2*4*testBlockSize))
	p.Stop()

	assert.Zero(t, p.decodeStft.sequence, "the decode tier must make no frame")
	assert.NotZero(t, p.stft.sequence, "and the detection tier must still work")
}

// TestPipelineCompletesTheChannelOfAStateEvent checks that the event of a state holds each value of
// the channel. The tracker sends the event, and it knows neither the speed nor the callsign: the
// decode stage owns the speed and the callsign stage owns the callsign.
func TestPipelineCompletesTheChannelOfAStateEvent(t *testing.T) {
	server := &recordingServer{}
	p := New[float32, float64](testConfig(), nil)
	p.Notify(server)

	p.emitChannelCreated(core.Channel[float64]{
		ID: "3", Frequency: 7020000, WPM: 25, SNR: 17.5, State: core.ConfirmedChannel,
	})
	for _, character := range "cq a1bc test " {
		p.emitChannelCharacterReceived(core.Channel[float64]{ID: "3"}, character, 0)
	}

	// the tracker knows only the frequency, the SNR and the state
	p.emitChannelStateChanged(core.Channel[float64]{
		ID: "3", Frequency: 7020000, SNR: 17.5, State: core.ActiveChannel,
	})

	events := server.recordedChannelEvents()
	changed, ok := events[len(events)-1].(client.ChannelStateChanged)
	require.Truef(t, ok, "the last event must be the change of the state, got %T", events[len(events)-1])
	assert.Equal(t, core.ActiveChannel, changed.Channel.State)
	assert.Equal(t, 25, changed.Channel.WPM, "the speed comes from the decode stage")
	assert.Equal(t, "A1BC", changed.Channel.Callsign.String(), "the callsign comes from the callsign stage")
}

// TestDefaultConfigHoldsEachValue pins the configuration that each source of SDRainer uses. A
// change of one of these values changes the behavior of the demo, of the kiwi command and of the
// replay command at the same time.
func TestDefaultConfigHoldsEachValue(t *testing.T) {
	config := DefaultConfig(12000, 7028000.0)

	assert.Equal(t, Config[float64]{
		SampleRate:      12000,
		CenterFrequency: 7028000,
		BinWidth:        12,
		MinWPM:          8,
		MaxWPM:          56,
		PeakThreshold:   10,

		MinDutyCycle:       0.15,
		MaxDutyCycle:       0.9,
		MaxAutocorrelation: 0.4,

		NoiseFloorTime: 3 * time.Second,
		DeadTimeout:    20 * time.Second,
		MaxDrift:       2,

		ScopeFrameRate: 10,
	}, config)
}

// TestDefaultConfigGivesAWorkingPipeline checks that no value of DefaultConfig is missing: a value
// of 0 would give a size of 0 in Derive, and the pipeline would do nothing.
func TestDefaultConfigGivesAWorkingPipeline(t *testing.T) {
	derived := Derive(DefaultConfig(12000, 7028000.0))

	assert.Equal(t, 1024, derived.BlockSize)
	assert.Equal(t, 256, derived.Hop)
	assert.Equal(t, 256, derived.DecodeBlockSize)
	assert.Equal(t, 64, derived.DecodeHop)
	assert.Positive(t, derived.NoiseFloorSpan)
	assert.Positive(t, derived.PeakMergeWidth)
	assert.Positive(t, derived.MatchWidth)
	assert.Positive(t, derived.ConfirmCount)
	assert.Positive(t, derived.IdleTimeout)
	assert.Positive(t, derived.CWWindow)
	assert.Positive(t, derived.MinKeyingRate)
}

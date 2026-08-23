package pipeline

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/cw"
	"github.com/ftl/sdrainer/dsp"
	"github.com/ftl/sdrainer/pipeline/generator"
)

const (
	decodeSampleRate = 48000
	decodeBlockSize  = 1024 // 46.875 Hz for each bin
	decodeHop        = 256  // 5.33 ms for each frame, thus a highest speed of 56 WPM
	decodeCenter     = 7020000.0
	decodeSignal     = 7020000.0
	decodeFloorLevel = 1.0
	decodeMarkLevel  = 1000.0
)

// the decode stage must be usable as a listener of the lifecycle of a channel
var _ core.ChannelLifecycleListener[float64] = (*DecodeStage[float32, float64])(nil)

// recordingCharacterReporter collects the character events of the decode stage.
type recordingCharacterReporter struct {
	channels   []core.Channel[float64]
	characters []rune
	offsets    []int64
}

func (r *recordingCharacterReporter) emitChannelCharacterReceived(channel core.Channel[float64], character rune, offset int64) {
	r.channels = append(r.channels, channel)
	r.characters = append(r.characters, character)
	r.offsets = append(r.offsets, offset)
}

func (r *recordingCharacterReporter) text() string {
	return string(r.characters)
}

func newTestDecodeStage(t *testing.T) (*DecodeStage[float32, float64], *dsp.FrequencyMapping[float64]) {
	t.Helper()

	stage, mapping, _ := newTestDecodeStageWithReporter(t)
	return stage, mapping
}

func newTestDecodeStageWithReporter(t *testing.T) (*DecodeStage[float32, float64], *dsp.FrequencyMapping[float64], *recordingCharacterReporter) {
	t.Helper()

	mapping := dsp.NewFrequencyMapping[float64](decodeSampleRate, decodeBlockSize, decodeCenter)
	reporter := &recordingCharacterReporter{}
	stage := NewDecodeStage[float32, float64](mapping, decodeBlockSize, decodeSampleRate, decodeHop, reporter)

	return stage, mapping, reporter
}

// newDecodeFrame gives a frame with a flat floor. A tone at the given frequency puts its energy
// into the two bins around that frequency, so a frequency between two bins gives one half to each
// of them. This is the behavior that a signal shows when it drifts over a bin border.
func newDecodeFrame(mapping *dsp.FrequencyMapping[float64], frequency float64, level float32) *SpectralFrame[float32, float64] {
	frame := NewFramePool[float32, float64](decodeBlockSize).NewFrame()
	for i := range frame.Spectrum {
		frame.Spectrum[i] = decodeFloorLevel
	}
	if level == 0 {
		return frame
	}

	// The position is relative to the center of bin 0, because a bin index gives the center of the
	// bin. The lower bin comes from a floor and not from FrequencyToBin, because FrequencyToBin
	// rounds to the nearest bin.
	binWidth := float64(decodeSampleRate) / float64(decodeBlockSize)
	exact := (frequency - float64(mapping.BinToFrequency(0, dsp.BinCenter))) / binWidth
	lower := int(math.Floor(exact))
	distance := exact - float64(lower)

	if lower >= 0 && lower < len(frame.Spectrum) {
		frame.Spectrum[lower] += level * float32(1-distance)
	}
	if lower+1 >= 0 && lower+1 < len(frame.Spectrum) {
		frame.Spectrum[lower+1] += level * float32(distance)
	}
	return frame
}

// testChannel gives an active channel, because the decode stage decodes only an active channel: a
// channel that is idle has no signal, and its bins hold only noise.
func testChannel(id core.ChannelID, frequency float64) core.Channel[float64] {
	return core.Channel[float64]{ID: id, Frequency: frequency, State: core.ActiveChannel}
}

func testConfirmedChannel(id core.ChannelID, frequency float64) core.Channel[float64] {
	channel := testChannel(id, frequency)
	channel.State = core.ConfirmedChannel
	return channel
}

func TestDecodeStageFollowsTheKeying(t *testing.T) {
	stage, mapping := newTestDecodeStage(t)
	stage.ChannelCreated(testChannel("1", decodeSignal))

	stage.Process(newDecodeFrame(mapping, decodeSignal, decodeMarkLevel))
	mark := stage.channels["1"].envelope

	stage.Process(newDecodeFrame(mapping, decodeSignal, 0))
	space := stage.channels["1"].envelope

	assert.Greater(t, mark, 10*space, "a mark must be far above a gap")
	assert.InDelta(t, decodeMarkLevel, mark, envelopeBins*decodeFloorLevel, "a mark holds the energy of the tone and the floor")
	assert.InDelta(t, envelopeBins*decodeFloorLevel, space, 0.001, "a gap holds only the floor")
}

func TestDecodeStageKeepsTheEnvelopeOverABinBorder(t *testing.T) {
	// A tone that drifts over a bin border puts its energy into two bins. One bin alone would show
	// a gap in the keying that does not exist, so the envelope must use more than one bin.
	stage, mapping := newTestDecodeStage(t)
	stage.ChannelCreated(testChannel("1", decodeSignal))

	binWidth := float64(decodeSampleRate) / float64(decodeBlockSize)
	for _, offset := range []float64{0, 0.25 * binWidth, 0.5 * binWidth, 0.75 * binWidth, binWidth} {
		frame := newDecodeFrame(mapping, decodeSignal+offset, decodeMarkLevel)
		stage.Process(frame)

		assert.InDeltaf(t, decodeMarkLevel, stage.channels["1"].envelope, envelopeBins*decodeFloorLevel,
			"the envelope must not fall at an offset of %.1f Hz", offset)

		if offset == binWidth {
			// the teeth of this test: one bin alone loses the whole signal at a full bin of drift
			assert.Lessf(t, frame.Spectrum[stage.channels["1"].bin], float32(0.5*decodeMarkLevel),
				"one bin alone must lose the signal at an offset of %.1f Hz", offset)
		}
	}
}

func TestDecodeStageFollowsTheLifecycleOfAChannel(t *testing.T) {
	stage, _ := newTestDecodeStage(t)

	stage.ChannelCreated(testChannel("1", decodeSignal))
	stage.ChannelCreated(testChannel("2", decodeSignal+1000))
	require.Len(t, stage.channels, 2)
	assert.NotEqual(t, stage.channels["1"].bin, stage.channels["2"].bin, "two frequencies give two bins")

	stage.ChannelDestroyed(testChannel("1", decodeSignal))
	require.Len(t, stage.channels, 1)
	assert.Contains(t, stage.channels, core.ChannelID("2"))
}

func TestDecodeStageStopsAtTheEdgeOfTheSpectrum(t *testing.T) {
	// A channel needs one bin on each side for its envelope of 3 bins, so the first bin and the last
	// bin are outside the range that the stage decodes. FrequencyToBin limits its result to the bins
	// that exist, so a channel outside the spectrum would get the first or the last bin and it would
	// decode the noise of that bin.
	stage, mapping := newTestDecodeStage(t)

	outsideLow := float64(mapping.BinToFrequency(0, dsp.BinCenter))
	outsideHigh := float64(mapping.BinToFrequency(decodeBlockSize-1, dsp.BinCenter))
	insideLow := float64(mapping.BinToFrequency(1, dsp.BinCenter))
	insideHigh := float64(mapping.BinToFrequency(decodeBlockSize-2, dsp.BinCenter))
	farAway := outsideHigh + 100000

	stage.ChannelCreated(testChannel("outside-low", outsideLow))
	stage.ChannelCreated(testChannel("outside-high", outsideHigh))
	stage.ChannelCreated(testChannel("inside-low", insideLow))
	stage.ChannelCreated(testChannel("inside-high", insideHigh))
	stage.ChannelCreated(testChannel("far-away", farAway))

	// a frame with energy in each bin, so that only the test of the range can give a zero envelope
	frame := NewFramePool[float32, float64](decodeBlockSize).NewFrame()
	for i := range frame.Spectrum {
		frame.Spectrum[i] = decodeMarkLevel
	}
	stage.Process(frame)

	assert.Positive(t, stage.channels["inside-low"].envelope, "one bin inside the edge must decode")
	assert.Positive(t, stage.channels["inside-high"].envelope, "one bin inside the edge must decode")
	assert.Zero(t, stage.channels["outside-low"].envelope, "the first bin has no bin below it")
	assert.Zero(t, stage.channels["outside-high"].envelope, "the last bin has no bin above it")
	assert.Zero(t, stage.channels["far-away"].envelope, "a channel far outside must not decode a bin at the edge")
}

// decodeSignalText runs a generated CW signal through a real STFT and through the decode stage. It
// gives the reporter with the collected characters.
//
// The work of this function is the cost of the whole package under the race detector, so it must
// stay small: loops of PARIS at a high speed, and a short silence. PARIS with one word break is
// 50 dits.
func decodeSignalText(t *testing.T, wpm int, text string, loops int, silentSamples int) *recordingCharacterReporter {
	t.Helper()

	stage, _, reporter := newTestDecodeStageWithReporter(t)
	stage.ChannelCreated(testChannel("1", decodeSignal))

	source := generator.New[float32, float64](generator.GeneratorConfig[float64]{
		SampleRate:      decodeSampleRate,
		CenterFrequency: decodeCenter,
		NoiseLevel:      0.001,
		Seed:            1,
		Signals: []generator.CWSignal[float64]{
			{Frequency: decodeSignal, WPM: wpm, Text: text, RiseTime: 5 * time.Millisecond},
		},
	})

	framePool := NewFramePool[float32, float64](decodeBlockSize)
	stft := NewSTFTStage[float32, float64](framePool, decodeSampleRate, decodeHop)
	frames := make(chan *SpectralFrame[float32, float64], frameBufferSize)

	// the STFT needs its own goroutine, because it blocks when the channel of the frames is full
	go func() {
		defer close(frames)

		// a silence before the signal proves that the offset of a character is not the start of
		// the stream
		stft.Process(frames, make([]float32, 2*silentSamples))

		// one character needs approximately 10 dits, and the generator repeats the text
		ditSamples := int(1.2 * float64(decodeSampleRate) / float64(wpm))
		iq := make([]float32, 2*loops*len(text)*10*ditSamples)
		source.Read(iq)
		stft.Process(frames, iq)
	}()

	for frame := range frames {
		stage.Process(frame)
		framePool.ReturnFrame(frame)
	}

	return reporter
}

// TestDecodeStageGivesTheTextAndThePositionOfASignal is the only test of this package that decodes
// a generated signal, so it asserts all properties of one run. Two runs would double the cost of
// the package under the race detector.
func TestDecodeStageGivesTheTextAndThePositionOfASignal(t *testing.T) {
	const (
		wpm           = 30
		loops         = 4 // the first loop pays the cold start, so two clean loops stay
		silentSamples = 12000
	)
	reporter := decodeSignalText(t, wpm, "paris", loops, silentSamples)

	require.NotEmpty(t, reporter.characters)
	found := strings.Index(reporter.text(), "paris paris")
	require.GreaterOrEqualf(t, found, 0, "the decode must give the text of the signal, got %q", reporter.text())

	// The first characters are not reliable, and the test must not use them for the position. The
	// demodulator has no level of a mark and no level of a gap before it saw the signal, so its
	// limit is 0 and each value above 0 is a mark. It reports marks in the silence, and the decoder
	// makes characters from them.
	assert.GreaterOrEqual(t, reporter.offsets[found], int64(silentSamples-decodeBlockSize),
		"a character of the text must not begin before the signal begins")

	for i := 1; i < len(reporter.offsets); i++ {
		assert.GreaterOrEqualf(t, reporter.offsets[i], reporter.offsets[i-1],
			"the offset of the character %d must not go backwards", i)
	}

	assert.InDelta(t, wpm, reporter.channels[len(reporter.channels)-1].WPM, 4,
		"the channel of the event must carry the speed of the signal")
}

func TestDecodeStagePutsTheSpeedAndThePositionIntoTheEvent(t *testing.T) {
	// This test needs no signal: it gives a character directly to the decoder of the channel, so it
	// shows the mapping alone and it costs no FFT.
	stage, _, reporter := newTestDecodeStageWithReporter(t)
	stage.ChannelCreated(testChannel("1", decodeSignal))
	decoder := stage.channels["1"]
	decoder.base = 10000

	decoder.CharacterDecoded(cw.Character{Rune: 'p', WPM: 24.6, Start: 3, End: 9})

	require.Len(t, reporter.characters, 1)
	assert.Equal(t, 'p', reporter.characters[0])
	assert.Equal(t, core.ChannelID("1"), reporter.channels[0].ID, "the event carries the channel of the decoder")
	assert.Equal(t, decodeSignal, reporter.channels[0].Frequency)
	// 25 and not the default speed of the decoder, which is 20
	assert.Equal(t, 25, reporter.channels[0].WPM, "the speed rounds to the nearest whole number")
	assert.Equal(t, int64(10000+3*decodeHop), reporter.offsets[0], "the base plus the ticks of the start")
}

func TestDecodeStageKeepsTheSpeedOfEachChannelApart(t *testing.T) {
	stage, _, reporter := newTestDecodeStageWithReporter(t)
	stage.ChannelCreated(testChannel("1", decodeSignal))
	stage.ChannelCreated(testChannel("2", decodeSignal+1000))

	stage.channels["1"].CharacterDecoded(cw.Character{Rune: 'a', WPM: 18.2})
	stage.channels["2"].CharacterDecoded(cw.Character{Rune: 'b', WPM: 34.7})

	require.Len(t, reporter.channels, 2)
	assert.Equal(t, 18, reporter.channels[0].WPM)
	assert.Equal(t, 35, reporter.channels[1].WPM)
}

// fixedChannelSource gives the data of one channel, so a test can move a signal and change its
// state.
type fixedChannelSource struct {
	channel core.Channel[float64]
}

func (s *fixedChannelSource) channelOf(id core.ChannelID) (core.Channel[float64], bool) {
	if id != s.channel.ID {
		return core.Channel[float64]{}, false
	}
	return s.channel, true
}

func TestDecodeStageFollowsTheDriftOfASignal(t *testing.T) {
	// The tracker follows the drift of a signal, but it reports no event for it. Without
	// FollowChannels the bins of the envelope stay at the frequency of the creation, and a signal
	// that drifts more than one and a half bins leaves them.
	stage, mapping := newTestDecodeStage(t)
	stage.ChannelCreated(testChannel("1", decodeSignal))
	source := &fixedChannelSource{channel: testChannel("1", decodeSignal)}

	binWidth := float64(decodeSampleRate) / float64(decodeBlockSize)
	for _, drift := range []float64{0, binWidth, 3 * binWidth, 10 * binWidth} {
		frequency := decodeSignal + drift
		source.channel.Frequency = frequency
		stage.FollowChannels(source)
		stage.Process(newDecodeFrame(mapping, frequency, decodeMarkLevel))

		assert.InDeltaf(t, decodeMarkLevel, stage.channels["1"].envelope, envelopeBins*decodeFloorLevel,
			"the envelope must keep the signal at a drift of %.0f Hz", drift)
		assert.Equalf(t, mapping.FrequencyToBin(frequency), stage.channels["1"].bin,
			"the bin must follow the drift of %.0f Hz", drift)
		assert.Equalf(t, frequency, stage.channels["1"].channel.Frequency,
			"the channel of the event must carry the current frequency")
	}
}

func TestDecodeStageWithoutTheDriftLosesTheSignal(t *testing.T) {
	// the teeth of the test above: the same drift without FollowChannels loses the signal
	stage, mapping := newTestDecodeStage(t)
	stage.ChannelCreated(testChannel("1", decodeSignal))

	binWidth := float64(decodeSampleRate) / float64(decodeBlockSize)
	stage.Process(newDecodeFrame(mapping, decodeSignal+3*binWidth, decodeMarkLevel))

	assert.Less(t, stage.channels["1"].envelope, float32(0.1*decodeMarkLevel),
		"a bin that does not follow must lose the signal")
}

func TestDecodeStageGivesTheCurrentStateToTheEvent(t *testing.T) {
	// The tracker owns the state, and the copy of the decode stage would stay at the state of the
	// creation: each event of a character would give CONFIRMED and never ACTIVE.
	stage, _, reporter := newTestDecodeStageWithReporter(t)
	stage.ChannelCreated(testConfirmedChannel("1", decodeSignal))
	decoder := stage.channels["1"]

	decoder.CharacterDecoded(cw.Character{Rune: 'a', WPM: 20})
	require.Len(t, reporter.channels, 1)
	assert.Equal(t, core.ConfirmedChannel, reporter.channels[0].State, "the state at the creation")

	current := testChannel("1", decodeSignal)
	current.SNR = 23.5
	stage.FollowChannels(&fixedChannelSource{channel: current})

	decoder.CharacterDecoded(cw.Character{Rune: 'b', WPM: 20})
	require.Len(t, reporter.channels, 2)
	assert.Equal(t, core.ActiveChannel, reporter.channels[1].State, "the event must give the current state")
	assert.Equal(t, 23.5, reporter.channels[1].SNR, "and the current SNR")
	assert.Equal(t, 20, reporter.channels[1].WPM, "the speed belongs to the decode stage and must stay")
}

func TestDecodeStageDecodesOnlyAnActiveChannel(t *testing.T) {
	// A channel that is not active has no signal, so its bins hold only noise. The demodulator would
	// take that noise as its two levels and the decoder would make characters from it.
	stage, mapping, reporter := newTestDecodeStageWithReporter(t)
	stage.ChannelCreated(testChannel("1", decodeSignal))
	decoder := stage.channels["1"]

	// a keyed signal, but the channel is idle
	decoder.channel.State = core.IdleChannel
	for i := range 400 {
		level := float32(0)
		if i%20 < 10 {
			level = decodeMarkLevel
		}
		stage.Process(newDecodeFrame(mapping, decodeSignal, level))
	}
	assert.Empty(t, reporter.characters, "an idle channel must give no character")
	assert.Zero(t, decoder.envelope, "and no envelope")

	// the same signal on an active channel
	decoder.channel.State = core.ActiveChannel
	for i := range 400 {
		level := float32(0)
		if i%20 < 10 {
			level = decodeMarkLevel
		}
		stage.Process(newDecodeFrame(mapping, decodeSignal, level))
	}
	assert.NotEmpty(t, reporter.characters, "an active channel must give characters")
}

func TestDecodeStageKeepsTheTimeBaseOverAnIdlePeriod(t *testing.T) {
	// The position of a character comes from the count of the ticks of the decoder, so a tick must
	// happen for each frame, also for a channel that is not active. A tick that does not happen
	// would move each later character forwards in the stream.
	stage, mapping, reporter := newTestDecodeStageWithReporter(t)
	stage.ChannelCreated(testChannel("1", decodeSignal))
	decoder := stage.channels["1"]

	const idleFrames = 500
	decoder.channel.State = core.IdleChannel
	for range idleFrames {
		stage.Process(newDecodeFrame(mapping, decodeSignal, 0))
	}

	decoder.channel.State = core.ActiveChannel
	for i := range 600 {
		level := float32(0)
		if i%20 < 10 {
			level = decodeMarkLevel
		}
		stage.Process(newDecodeFrame(mapping, decodeSignal, level))
	}

	require.NotEmpty(t, reporter.offsets)
	assert.GreaterOrEqual(t, reporter.offsets[0], int64(idleFrames*decodeHop),
		"a character after the idle period must not have a position inside it")
}

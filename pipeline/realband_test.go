package pipeline

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/dsp"
)

/*
The tests of this file measure the detection and the tracker against a recording of a real band,
and they hold what a human heard in that recording.

A recording of a real band holds what the generator cannot make: a noise floor that is not flat,
stations that come and go, and modes that are not CW. A human listened to the WAV file of each
channel of test_14024_12k.iq and wrote down each signal that is readable, so the transcriptions of
that recording are the list of the signals that exist. The tests use that list.

	go test -run TestRealBand -v ./pipeline/
	go test -run TestMinDutyCycleSweep -v -timeout 900s ./pipeline/
*/

// realBandRecording is the recording of the 20 m band that a human listened to completely.
const realBandRecording = "testdata/test_14024_12k.iq"

// realBandCWSignals gives the offsets at which that human heard a CW signal, from the
// transcriptions of the recording: `prepare` makes one empty file for each channel, and a file with
// text says that the human heard a signal there and wrote it down. The knowledge therefore stands
// one time, in the files, and not a second time in this test.
func realBandCWSignals(t *testing.T) []float64 {
	t.Helper()

	transcriptions, err := filepath.Glob(realBandRecording + "_*.txt")
	require.NoError(t, err)

	var result []float64
	for _, transcription := range transcriptions {
		expected, err := readTranscription(transcription)
		require.NoError(t, err)
		if len(expected.overs) == 0 {
			continue
		}

		offset, err := offsetOf(transcription)
		require.NoError(t, err)
		result = append(result, offset)
	}
	sort.Float64s(result)
	require.NotEmpty(t, result, "%s has no transcription with text", realBandRecording)

	return result
}

// realBandDigitalSignal is the offset of a signal that is not CW. The two measures of the CW test
// cannot separate it from a fast CW signal, see TestRealBandKeepsADigitalSignal.
const realBandDigitalSignal = 5539

// realBandFalseChannels are the offsets at which the pipeline still gives a channel although the
// human heard nothing. They are the rest of the problem, see doc/architecture.md, section 6.3.
var realBandFalseChannels = []float64{-3012, 4719}

// realBandMaxChannels is the count of the channels that the recording may give. The 6 CW signals,
// the digital signal and the 2 false channels are 9, and the limit holds a small margin.
const realBandMaxChannels = 12

// analysedChannel holds what the tracker knew about one channel at the moment of its confirmation,
// and what happened to it afterwards.
type analysedChannel struct {
	id        core.ChannelID
	frequency float64
	frame     uint64

	dutyCycle       float64
	autocorrelation float64

	snrAtConfirm float64
	maxSNR       float64
	sumSNR       float64
	countSNR     int
	detections   int
	lifeFrames   int

	sumWidth   float64
	maxWidth   float64
	countWidth int
}

func (c analysedChannel) meanWidth() float64 {
	if c.countWidth == 0 {
		return 0
	}
	return c.sumWidth / float64(c.countWidth)
}

func (c analysedChannel) meanSNR() float64 {
	if c.countSNR == 0 {
		return 0
	}
	return c.sumSNR / float64(c.countSNR)
}

// analysisReporter takes the events of the tracker and reads the state of the signal behind each
// channel. It is in the package of the tracker, so it sees the values of the CW test.
type analysisReporter struct {
	tracker  *TrackerStage[float32, float64]
	sequence uint64

	order    []core.ChannelID
	channels map[core.ChannelID]*analysedChannel
}

func newAnalysisReporter() *analysisReporter {
	return &analysisReporter{channels: make(map[core.ChannelID]*analysedChannel)}
}

// signalOf gives the signal behind a channel, so that the test can read the values of the CW test.
func (r *analysisReporter) signalOf(id core.ChannelID) *trackedSignal[float64] {
	for _, signal := range r.tracker.signals {
		if signal.channel.ID == id {
			return signal
		}
	}
	return nil
}

func (r *analysisReporter) emitChannelCreated(channel core.Channel[float64]) {
	result := &analysedChannel{
		id:           channel.ID,
		frequency:    channel.Frequency,
		frame:        r.sequence,
		snrAtConfirm: channel.SNR,
	}
	if signal := r.signalOf(channel.ID); signal != nil {
		result.dutyCycle = signal.dutyCycle()
		result.autocorrelation = signal.minAutocorrelation(r.tracker.maxLag)
	}

	r.order = append(r.order, channel.ID)
	r.channels[channel.ID] = result
}

func (r *analysisReporter) emitChannelStateChanged(core.Channel[float64]) {}
func (r *analysisReporter) emitChannelDestroyed(core.Channel[float64])    {}

// updateWidth takes the width of each peak of the frame for the channel that stands nearest to it.
func (r *analysisReporter) updateWidth(frame *SpectralFrame[float32, float64]) {
	for _, peak := range frame.Peaks {
		signal := r.tracker.nearestSignal(float64(peak.SignalFrequency))
		if signal == nil {
			continue
		}
		current, ok := r.channels[signal.channel.ID]
		if !ok {
			continue
		}
		width := float64(peak.ToFrequency - peak.FromFrequency)
		current.sumWidth += width
		current.maxWidth = math.Max(current.maxWidth, width)
		current.countWidth++
	}
}

// update takes the values of each living channel after one frame.
func (r *analysisReporter) update() {
	for _, signal := range r.tracker.signals {
		current, ok := r.channels[signal.channel.ID]
		if !ok {
			continue
		}
		current.lifeFrames++
		current.frequency = signal.frequency
		current.detections = signal.detections
		if signal.lastFrame == r.sequence {
			current.maxSNR = math.Max(current.maxSNR, signal.channel.SNR)
			current.sumSNR += signal.channel.SNR
			current.countSNR++
		}
	}
}

func (r *analysisReporter) result() []analysedChannel {
	result := make([]analysedChannel, 0, len(r.order))
	for _, id := range r.order {
		result = append(result, *r.channels[id])
	}
	sort.Slice(result, func(i, j int) bool { return result[i].frequency < result[j].frequency })
	return result
}

// analyseRecording runs the detection tier and the tracker over a recording, in one goroutine, and
// it gives what it learned about each channel. The decode tier does not run: this measurement asks
// why a channel exists, and not what it says.
func analyseRecording(t *testing.T, filename string, sampleRate int, threshold float64) []analysedChannel {
	t.Helper()

	samples, err := readIQFile(filename)
	require.NoError(t, err)

	config := DefaultConfig(sampleRate, 0.0)
	config.PeakThreshold = threshold
	derived := Derive(config)

	mapping := dsp.NewFrequencyMapping[float64](sampleRate, derived.BlockSize, 0)
	framePool := NewFramePool[float32, float64](derived.BlockSize)
	stft := NewSTFTStage[float32, float64](framePool, sampleRate, derived.Hop)
	detection := NewDetectionStage[float32, float64](
		derived.BlockSize, derived.NoiseFloorSpan,
		dsp.SmoothingFactor(config.NoiseFloorTime, derived.FrameInterval),
		config.PeakThreshold, derived.PeakMergeWidth, mapping)

	reporter := newAnalysisReporter()
	tracker := NewTrackerStage[float32, float64](TrackerConfig[float64]{
		FrameInterval:       derived.FrameInterval,
		MatchWidth:          derived.MatchWidth,
		CandidateMatchWidth: derived.CandidateMatchWidth,
		ConfirmCount:        derived.ConfirmCount,
		IdleTimeout:         derived.IdleTimeout,
		DeadTimeout:         config.DeadTimeout,
		MaxDrift:            config.MaxDrift,
		MinChannelSNR:       derived.MinChannelSNR,
		SNRReviewTime:       derived.SNRReviewTime,
		CWWindow:            derived.CWWindow,
		MaxDutyCycle:        config.MaxDutyCycle,
		MinKeyingRate:       derived.MinKeyingRate,
		MaxAutocorrelation:  config.MaxAutocorrelation,
	}, reporter)
	reporter.tracker = tracker

	frames := make(chan *SpectralFrame[float32, float64], frameBufferSize)
	const chunkSize = 2048
	for i := 0; i+2*chunkSize <= len(samples); i += 2 * chunkSize {
		stft.Process(frames, samples[i:i+2*chunkSize])
		for len(frames) > 0 {
			frame := <-frames
			reporter.sequence = frame.Sequence
			detection.Process(frame)
			tracker.Process(frame)
			reporter.updateWidth(frame)
			reporter.update()
			framePool.ReturnFrame(frame)
		}
	}

	return reporter.result()
}

func TestRealBandAnalysis(t *testing.T) {
	for _, fixture := range analysedFixtures {
		t.Run(fixture, func(t *testing.T) { analyseAndLog(t, "testdata/"+fixture) })
	}
}

func analyseAndLog(t *testing.T, filename string) {
	sampleRate, err := sampleRateOf(filename)
	require.NoError(t, err)

	channels := analyseRecording(t, filename, sampleRate, DefaultPeakThreshold)

	t.Logf("%d channels", len(channels))
	t.Logf("%9s %6s %6s %7s %7s %7d %6s %7s %7s", "Hz", "duty", "autoc", "maxSNR", "meanSNR", 0, "frames", "meanW", "maxW")
	for _, channel := range channels {
		t.Logf("%+9.0f %6.2f %6.2f %7.1f %7.1f %7d %6d %7.0f %7.0f",
			channel.frequency, channel.dutyCycle, channel.autocorrelation,
			channel.maxSNR, channel.meanSNR(), channel.detections, channel.lifeFrames,
			channel.meanWidth(), channel.maxWidth)
	}
}

// TestAnalyseRecordingOverTheThreshold shows how the count of the channels depends on the threshold
// of the detection.
func TestRealBandThresholdSweep(t *testing.T) {
	for _, threshold := range []float64{10, 12, 14, 16, 18, 20} {
		channels := analyseRecording(t, realBandRecording, 12000, threshold)

		var frequencies []string
		for _, channel := range channels {
			frequencies = append(frequencies, fmt.Sprintf("%+.0f", channel.frequency))
		}
		t.Logf("%4.0f dB: %2d channels  %v", threshold, len(channels), frequencies)
	}
}

/* the sweep of the lower limit of the duty cycle */

// transcribedFixtures are the recordings with a transcription, so a sweep can measure what it costs
// to remove a channel. Each of them holds its sample rate in its name, see sampleRateOf, so a
// fixture that is not 12000 needs no change here.
var transcribedFixtures = []string{"test_14018_12k.iq", "test_14020_12k.iq", "test_yo-hf-dx_1_48k.iq"}

// analysedFixtures are every recording, so that TestRealBandAnalysis shows what the tracker made of
// each of them.
var analysedFixtures = []string{
	"test_14018_12k.iq",
	"test_14020_12k.iq",
	"test_14024_12k.iq",
	"test_yo-hf-dx_1_48k.iq",
	"test_yo-hf-dx_3_48k.iq",
}

// TestMinDutyCycleSweep measures what the lower limit of the duty cycle does. It gives the count of
// the channels of each recording, and the character error rate of each transcription, so that a
// value that removes the false channels but keeps the copy becomes visible.
//
//	go test -run TestMinDutyCycleSweep -v -timeout 900s ./pipeline/
func TestMinDutyCycleSweep(t *testing.T) {
	for _, minDutyCycle := range []float64{0.07, 0.15, 0.20, 0.25, 0.30, 0.40} {
		option := func(config *Config[float64]) { config.MinDutyCycle = minDutyCycle }

		decoded14024 := decodeRecording(t, realBandRecording, 12000, option)
		channels := len(decoded14024)
		var remaining []string
		for _, channel := range decoded14024 {
			remaining = append(remaining, fmt.Sprintf("%+.0f", channel.frequency))
		}

		var rates []string
		var sum float64
		var count int
		for _, fixture := range transcribedFixtures {
			sampleRate, err := sampleRateOf(fixture)
			require.NoError(t, err)
			decoded := decodeRecording(t, "testdata/"+fixture, sampleRate, option)

			transcriptions, err := filepath.Glob(filepath.Join("testdata", fixture) + "_*.txt")
			require.NoError(t, err)
			for _, transcription := range transcriptions {
				expected, err := readTranscription(transcription)
				require.NoError(t, err)
				if len(expected.overs) == 0 {
					continue // an empty file of a session that a human did not finish
				}
				offset, err := offsetOf(transcription)
				require.NoError(t, err)

				rate := bestRateFor(decoded, offset, expected)
				rates = append(rates, fmt.Sprintf("%.3f", rate))
				sum += rate
				count++
			}
		}

		t.Logf("%.2f: %2d channels in 14024, mean rate %.3f  %v", minDutyCycle, channels, sum/float64(max(1, count)), rates)
		t.Logf("      the channels of 14024: %v", remaining)
	}
}

// bestRateFor gives the error rate of the channel around the given offset that fits the
// transcription best. A transcription without a channel gives 1.
func bestRateFor(channels []transcribedChannel, offset float64, expected transcribedText) float64 {
	best := 1.0
	for _, channel := range channels {
		if math.Abs(channel.frequency-offset) > transcriptionTolerance {
			continue
		}
		best = math.Min(best, expected.errorRate(channel.text))
	}
	return best
}

/* the tests of the real band */

// TestRealBandGivesAChannelForEachCWSignal is the first half of the ground truth: no signal that a
// human heard may disappear.
func TestRealBandGivesAChannelForEachCWSignal(t *testing.T) {
	channels := decodeRecording(t, realBandRecording, 12000)

	for _, signal := range realBandCWSignals(t) {
		t.Run(fmt.Sprintf("%+.0f", signal), func(t *testing.T) {
			found, ok := channelNear(channels, signal)

			require.Truef(t, ok, "no channel within %.0f Hz of %+.0f", transcriptionTolerance, signal)
			assert.NotEmptyf(t, found.text, "the channel at %+.0f gave no text", found.frequency)
		})
	}
}

// TestRealBandGivesFewFalseChannels is the second half: a recording of 54 s gave 74 channels, of
// which 6 carried CW. A human cannot use a list where 92 % of the entries hold nothing.
//
// The reason was the lower limit of the duty cycle, see defaultMinDutyCycle and
// doc/architecture.md, section 6.3.
func TestRealBandGivesFewFalseChannels(t *testing.T) {
	channels := decodeRecording(t, realBandRecording, 12000)

	for _, channel := range channels {
		t.Logf("%+8.0f Hz  %2d wpm  %5.1f dB  %q", channel.frequency, channel.wpm, channel.snr, channel.text)
	}

	assert.LessOrEqual(t, len(channels), realBandMaxChannels)
}

// TestRealBandGivesOneChannelForOneSignal covers the case that looked like a tracker that splits a
// station: the recording gave a channel at −1056 Hz and a second one at −1194 Hz, and a human heard
// the same signal in both WAV files.
//
// **The tracker did not split anything.** The channel at −1194 Hz was a false channel of the noise,
// and its WAV file holds the signal at −1056 Hz because the filter of the listen command is 300 Hz
// wide, thus ±150 Hz, and the two stand 138 Hz apart. A false channel beside a real signal therefore
// always sounds like that signal.
func TestRealBandGivesOneChannelForOneSignal(t *testing.T) {
	channels := decodeRecording(t, realBandRecording, 12000)

	for _, signal := range realBandCWSignals(t) {
		var near []float64
		for _, channel := range channels {
			if math.Abs(channel.frequency-signal) <= transcriptionTolerance {
				near = append(near, channel.frequency)
			}
		}

		assert.Lenf(t, near, 1, "%+.0f Hz must give one channel, and it gives %v", signal, near)
	}
}

// TestRealBandKeepsADigitalSignal holds a limit that we know and do not solve. The signal at
// +5539 Hz is not CW, and the two measures of the CW test cannot see that: it has a duty cycle of
// 0.89 and a smallest autocorrelation of −0.12, and a fast CW signal of the same recording has 0.89
// and −0.14.
//
// The width of the signal does not separate the two either. A measurement gives a mean width of 28
// Hz and a largest width of 105 Hz for this signal, and 44 Hz and 129 Hz for the CW signal at
// −1056 Hz.
//
// The test asserts the current behavior, so that a change becomes visible.
func TestRealBandKeepsADigitalSignal(t *testing.T) {
	channels := decodeRecording(t, realBandRecording, 12000)

	found, ok := channelNear(channels, realBandDigitalSignal)

	require.True(t, ok, "the digital signal still gives a channel")
	t.Logf("the digital signal at %+.0f Hz gives %q", found.frequency, found.text)
}

// TestRealBandKeepsTwoFalseChannels holds the rest of the problem, so that a change becomes
// visible. Both stand approximately 2 dB above the threshold of the detection and they last long.
func TestRealBandKeepsTwoFalseChannels(t *testing.T) {
	channels := decodeRecording(t, realBandRecording, 12000)

	for _, signal := range realBandFalseChannels {
		found, ok := channelNear(channels, signal)
		if !ok {
			t.Logf("%+.0f Hz gives no channel any more, make the test stricter", signal)
			continue
		}
		t.Logf("%+8.0f Hz  %5.1f dB  %q", found.frequency, found.snr, found.text)
	}
}

// channelNear gives the channel that stands nearest to the given frequency, inside the width of the
// filter of the listen command.
func channelNear(channels []transcribedChannel, frequency float64) (transcribedChannel, bool) {
	var result transcribedChannel
	nearest := math.Inf(1)
	for _, channel := range channels {
		distance := math.Abs(channel.frequency - frequency)
		if distance <= transcriptionTolerance && distance < nearest {
			nearest, result = distance, channel
		}
	}
	return result, !math.IsInf(nearest, 1)
}

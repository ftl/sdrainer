package pipeline

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/dsp"
)

const (
	trackerFrameInterval = 20 * time.Millisecond
	trackerNoiseFloor    = 1.0
	trackerPeakValue     = 100.0
)

type trackerEvent struct {
	kind    string
	channel core.Channel[float64]
	id      core.ChannelID
	state   core.ChannelState
}

type recordingReporter struct {
	events []trackerEvent
}

func (r *recordingReporter) emitChannelCreated(channel core.Channel[float64]) {
	r.events = append(r.events, trackerEvent{kind: "created", channel: channel, id: channel.ID, state: channel.State})
}

func (r *recordingReporter) emitChannelStateChanged(channel core.Channel[float64]) {
	r.events = append(r.events, trackerEvent{kind: "state", channel: channel, id: channel.ID, state: channel.State})
}

func (r *recordingReporter) emitChannelDestroyed(channel core.Channel[float64]) {
	r.events = append(r.events, trackerEvent{kind: "destroyed", channel: channel, id: channel.ID, state: channel.State})
}

func (r *recordingReporter) kinds() []string {
	result := make([]string, 0, len(r.events))
	for _, event := range r.events {
		result = append(result, event.kind)
	}
	return result
}

func (r *recordingReporter) states() []core.ChannelState {
	result := make([]core.ChannelState, 0, len(r.events))
	for _, event := range r.events {
		result = append(result, event.state)
	}
	return result
}

func testTrackerConfig() TrackerConfig[float64] {
	return TrackerConfig[float64]{
		FrameInterval:       trackerFrameInterval,
		MatchWidth:          100,
		CandidateMatchWidth: 25,
		ConfirmCount:        5,
		IdleTimeout:         time.Second,
		DeadTimeout:         4 * time.Second,
		MaxDrift:            1,

		CWWindow:           500 * time.Millisecond, // 25 frames
		MaxDutyCycle:       0.9,
		MinKeyingRate:      2.5, // 200 ms, thus a lag range of 10 frames
		MaxAutocorrelation: 0.4,
	}
}

func newTrackerStage(t *testing.T, config TrackerConfig[float64]) (*TrackerStage[float32, float64], *recordingReporter) {
	t.Helper()

	reporter := &recordingReporter{}
	return NewTrackerStage[float32, float64](config, reporter), reporter
}

// trackerFrame gives a frame with one peak for each given frequency, and a flat noise floor.
func trackerFrame(sequence uint64, frequencies ...float64) *SpectralFrame[float32, float64] {
	frame := &SpectralFrame[float32, float64]{
		Sequence:   sequence,
		NoiseFloor: make(dsp.Block[float32], 16),
	}
	for i := range frame.NoiseFloor {
		frame.NoiseFloor[i] = trackerNoiseFloor
	}
	for _, frequency := range frequencies {
		frame.Peaks = append(frame.Peaks, dsp.Peak[float32, float64]{
			SignalFrequency: frequency,
			SignalValue:     trackerPeakValue,
			SignalBin:       0,
		})
	}
	return frame
}

// runFrames sends count frames to the tracker. present decides for each frame if the peak is there.
func runFrames(tracker *TrackerStage[float32, float64], from uint64, count int, frequency float64, present func(i int) bool) uint64 {
	sequence := from
	for i := range count {
		if present(i) {
			tracker.Process(trackerFrame(sequence, frequency))
		} else {
			tracker.Process(trackerFrame(sequence))
		}
		sequence++
	}
	return sequence
}

func always(int) bool { return true }
func never(int) bool  { return false }

// keying gives the pattern of a CW signal: on for onFrames, then off for offFrames.
func keying(onFrames int, offFrames int) func(int) bool {
	return func(i int) bool {
		return i%(onFrames+offFrames) < onFrames
	}
}

// confirmFrames is the number of frames that a candidate needs before it can become a channel.
func confirmFrames(config TrackerConfig[float64]) int {
	return framesFor(config.CWWindow, config.FrameInterval)
}

func TestTrackerIgnoresASingleDetection(t *testing.T) {
	tracker, reporter := newTrackerStage(t, testTrackerConfig())

	tracker.Process(trackerFrame(0, 7020000))
	runFrames(tracker, 1, 100, 0, never)

	assert.Empty(t, reporter.events, "one detection is noise and must give no event")
	assert.Empty(t, tracker.Channels())
}

func TestTrackerConfirmsARepeatedDetection(t *testing.T) {
	config := testTrackerConfig()
	tracker, reporter := newTrackerStage(t, config)

	runFrames(tracker, 0, confirmFrames(config), 7020000, keying(2, 3))

	require.Len(t, reporter.events, 1)
	assert.Equal(t, "created", reporter.events[0].kind)
	assert.Equal(t, core.ChannelID("1"), reporter.events[0].id)
	assert.Equal(t, core.ConfirmedChannel, reporter.events[0].state)
	assert.Equal(t, 7020000.0, reporter.events[0].channel.Frequency)
}

func TestTrackerDropsACandidateAfterTheWindow(t *testing.T) {
	config := testTrackerConfig()
	config.ConfirmCount = 5
	tracker, reporter := newTrackerStage(t, config)

	// four detections are not enough, and a candidate goes away after the window of the CW test
	runFrames(tracker, 0, 4, 7020000, always)
	runFrames(tracker, 4, framesFor(config.CWWindow, config.FrameInterval)+1, 0, never)

	assert.Empty(t, reporter.events, "a candidate that was never confirmed must give no event")
	assert.Empty(t, tracker.signals, "and it must not stay in the tracker")
}

func TestTrackerFollowsTheStatesOfASignal(t *testing.T) {
	config := testTrackerConfig()
	tracker, reporter := newTrackerStage(t, config)
	idleFrames := framesFor(config.IdleTimeout, config.FrameInterval)
	deadFrames := framesFor(config.DeadTimeout, config.FrameInterval)

	sequence := runFrames(tracker, 0, confirmFrames(config), 7020000, keying(2, 3))
	sequence = runFrames(tracker, sequence, 1, 7020000, always)
	sequence = runFrames(tracker, sequence, idleFrames+1, 0, never)
	sequence = runFrames(tracker, sequence, 1, 7020000, always)
	runFrames(tracker, sequence, deadFrames+1, 0, never)

	assert.Equal(t, []string{"created", "state", "state", "state", "state", "state", "destroyed"}, reporter.kinds())
	assert.Equal(t, []core.ChannelState{
		core.ConfirmedChannel, // the channel appears
		core.ActiveChannel,    // the peak is there
		core.IdleChannel,      // the peak is gone for more than the idle timeout
		core.ActiveChannel,    // the peak is there again
		core.IdleChannel,      // a signal always goes through idle on its way to dead
		core.DeadChannel,      // the peak is gone for more than the dead timeout
		core.DeadChannel,      // the channel goes away
	}, reporter.states())
	assert.Empty(t, tracker.signals, "a dead signal must not stay in the tracker")
}

func TestTrackerKeepsTheChannelOverAGapInTheKeying(t *testing.T) {
	config := testTrackerConfig()
	tracker, reporter := newTrackerStage(t, config)

	// a word gap at 25 WPM is approximately 340 ms, thus much shorter than the idle timeout
	sequence := runFrames(tracker, 0, confirmFrames(config), 7020000, keying(2, 3))
	sequence = runFrames(tracker, sequence, 1, 7020000, always)
	sequence = runFrames(tracker, sequence, 17, 0, never)
	runFrames(tracker, sequence, 5, 7020000, always)

	assert.Equal(t, []string{"created", "state"}, reporter.kinds(), "a gap in the keying must give no new state")
	require.Len(t, tracker.Channels(), 1)
	assert.Equal(t, core.ActiveChannel, tracker.Channels()[0].State)
}

func TestTrackerFollowsADriftingSignal(t *testing.T) {
	config := testTrackerConfig()
	config.MaxDrift = 100 // Hz for each second, so the test needs few frames
	tracker, _ := newTrackerStage(t, config)

	sequence := runFrames(tracker, 0, confirmFrames(config), 7020000, keying(2, 3))
	for range 50 {
		tracker.Process(trackerFrame(sequence, 7020020))
		sequence++
	}

	require.Len(t, tracker.Channels(), 1)
	assert.InDelta(t, 7020020, tracker.Channels()[0].Frequency, 0.1, "the channel must reach the new frequency")
}

func TestTrackerLimitsTheDrift(t *testing.T) {
	config := testTrackerConfig()
	config.MaxDrift = 1 // Hz for each second
	tracker, _ := newTrackerStage(t, config)

	sequence := runFrames(tracker, 0, confirmFrames(config), 7020000, keying(2, 3))
	// the measured peak jumps by 20 Hz, which a transmitter cannot do
	for range 10 {
		tracker.Process(trackerFrame(sequence, 7020020))
		sequence++
	}

	require.Len(t, tracker.Channels(), 1)
	// 10 frames of 20 ms with 1 Hz for each second permit far less than one Hz
	assert.InDelta(t, 7020000, tracker.Channels()[0].Frequency, 1, "the channel must not follow a jump")
}

func TestTrackerFollowsADriftThroughTheGapsOfTheKeying(t *testing.T) {
	// A CW signal has energy in only a part of the frames. The limit for the drift must be a limit
	// for each unit of time: a limit for each detection would follow the drift with the duty cycle
	// of the signal, and the channel would lose a signal that drifts fast.
	config := testTrackerConfig()
	config.MaxDrift = 10 // Hz for each second
	tracker, _ := newTrackerStage(t, config)

	sequence := runFrames(tracker, 0, confirmFrames(config), 7020000, keying(2, 3))

	frequency := 7020000.0
	for i := range 100 {
		frequency += float64(config.MaxDrift) * config.FrameInterval.Seconds()
		if keying(2, 3)(i) {
			tracker.Process(trackerFrame(sequence, frequency))
		} else {
			tracker.Process(trackerFrame(sequence))
		}
		sequence++
	}

	require.Len(t, tracker.Channels(), 1, "a signal that drifts must not give a second channel")
	assert.InDelta(t, frequency, tracker.Channels()[0].Frequency, 1, "the channel must follow the drift")
}

func TestTrackerSeparatesTwoSignals(t *testing.T) {
	config := testTrackerConfig()
	tracker, reporter := newTrackerStage(t, config)

	for i := range confirmFrames(config) {
		if keying(2, 3)(i) {
			tracker.Process(trackerFrame(uint64(i), 7020000, 7023000))
		} else {
			tracker.Process(trackerFrame(uint64(i)))
		}
	}

	require.Len(t, tracker.Channels(), 2)
	assert.Equal(t, core.ChannelID("1"), reporter.events[0].id)
	assert.Equal(t, core.ChannelID("2"), reporter.events[1].id)
	assert.Equal(t, 7020000.0, reporter.events[0].channel.Frequency)
	assert.Equal(t, 7023000.0, reporter.events[1].channel.Frequency)
}

func TestTrackerJoinsAPeakInsideTheMatchWidth(t *testing.T) {
	config := testTrackerConfig()
	tracker, _ := newTrackerStage(t, config)

	// the measured frequency of one signal moves by less than the match width of a candidate
	frequencies := []float64{7020000, 7020010, 7019990, 7020015, 7019985, 7020000}
	for i := range confirmFrames(config) {
		if keying(2, 3)(i) {
			tracker.Process(trackerFrame(uint64(i), frequencies[i%len(frequencies)]))
		} else {
			tracker.Process(trackerFrame(uint64(i)))
		}
	}

	assert.Len(t, tracker.Channels(), 1, "the jitter of the measurement must not give more channels")
}

// TestTrackerTakesAPeakBesideAChannel checks the width of a channel: a peak that is further away
// than the width of a candidate, but inside the width of a channel, must not give a second channel.
// A station that drifts or that has a wide signal gave 3 channels before this rule.
func TestTrackerTakesAPeakBesideAChannel(t *testing.T) {
	config := testTrackerConfig()
	tracker, _ := newTrackerStage(t, config)

	// the channel comes first, at one frequency
	sequence := runFrames(tracker, 0, confirmFrames(config), 7020000, keying(2, 3))
	require.Len(t, tracker.Channels(), 1)

	// the peaks now stand 60 Hz beside it: more than CandidateMatchWidth, less than MatchWidth
	runFrames(tracker, sequence, 2*confirmFrames(config), 7020060, keying(2, 3))

	assert.Len(t, tracker.Channels(), 1, "a peak inside the width of the channel gives no second channel")
}

func TestTrackerReportsTheSNR(t *testing.T) {
	config := testTrackerConfig()
	tracker, reporter := newTrackerStage(t, config)

	runFrames(tracker, 0, confirmFrames(config), 7020000, keying(2, 3))

	require.NotEmpty(t, reporter.events)
	// the peak is 100 and the floor is 1, thus 20 dB
	assert.InDelta(t, 20, reporter.events[0].channel.SNR, 0.01)
}

func TestTrackerRejectsACarrier(t *testing.T) {
	config := testTrackerConfig()
	tracker, reporter := newTrackerStage(t, config)

	// a carrier, a beacon, a birdie and a data mode have energy in every frame
	runFrames(tracker, 0, 10*confirmFrames(config), 7020000, always)

	assert.Empty(t, reporter.events, "a signal without gaps is no CW signal")
	assert.Empty(t, tracker.Channels())
}

func TestTrackerConfirmsTheDutyCycleOfRealText(t *testing.T) {
	config := testTrackerConfig()

	tt := []struct {
		desc      string
		onFrames  int
		offFrames int
		confirmed bool
	}{
		// The limit must stay generous. At a frame of 21 ms the gaps of a fast signal are hardly
		// visible, so a low limit would remove a real CW signal.
		{desc: "a carrier, duty cycle 1.00", onFrames: 1, offFrames: 0, confirmed: false},
		{desc: "almost a carrier, duty cycle 0.95", onFrames: 19, offFrames: 1, confirmed: false},
		{desc: "long marks, duty cycle 0.80", onFrames: 8, offFrames: 2, confirmed: true},
		{desc: "the duty cycle of text, 0.40", onFrames: 2, offFrames: 3, confirmed: true},
		{desc: "many gaps, duty cycle 0.20", onFrames: 1, offFrames: 4, confirmed: true},
	}
	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			tracker, reporter := newTrackerStage(t, config)

			runFrames(tracker, 0, 4*confirmFrames(config), 7020000, keying(tc.onFrames, tc.offFrames))

			assert.Equal(t, tc.confirmed, len(reporter.events) > 0)
		})
	}
}

func TestTrackerRejectsASlowlySwitchingCarrier(t *testing.T) {
	// A carrier that switches on and off slowly has a duty cycle of 0.5, thus the duty cycle alone
	// accepts it. The autocorrelation of the envelope measures the rate of the switching and removes
	// it. The window must be long enough to hold one cycle of the switching.
	config := testTrackerConfig()
	config.CWWindow = 2 * time.Second // 100 frames, section 4.3 asks for 1 s to 2 s

	tt := []struct {
		desc      string
		onFrames  int
		offFrames int
		confirmed bool
	}{
		{desc: "a carrier that switches with 0.25 Hz", onFrames: 100, offFrames: 100, confirmed: false},
		{desc: "a carrier that switches with 0.5 Hz", onFrames: 50, offFrames: 50, confirmed: false},
		// The limit of this test: 1 Hz is a rate that an operator can also key by hand, so the
		// tracker accepts it. MinKeyingRate holds the border.
		{desc: "a signal that switches with 1 Hz", onFrames: 25, offFrames: 25, confirmed: true},
		{desc: "the keying of CW", onFrames: 2, offFrames: 3, confirmed: true},
	}
	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			tracker, reporter := newTrackerStage(t, config)

			runFrames(tracker, 0, 6*confirmFrames(config), 7020000, keying(tc.onFrames, tc.offFrames))

			assert.Equal(t, tc.confirmed, len(reporter.events) > 0)
		})
	}
}

func TestTrackerUsesTheDutyCycleAndTheAutocorrelation(t *testing.T) {
	// This test gives the two measures of section 4.3 their teeth: each one alone accepts a signal
	// that the other one removes.
	config := testTrackerConfig()
	config.CWWindow = 2 * time.Second

	// a carrier that switches with 0.5 Hz has the duty cycle of CW, and only the autocorrelation
	// removes it
	config.MaxAutocorrelation = 1.1
	tracker, reporter := newTrackerStage(t, config)
	runFrames(tracker, 0, 6*confirmFrames(config), 7020000, keying(50, 50))
	assert.NotEmpty(t, reporter.events, "without the autocorrelation the duty cycle accepts a slow carrier")

	// a carrier without gaps has no change in its envelope, and only the duty cycle removes it
	config = testTrackerConfig()
	config.MaxDutyCycle = 1.1
	tracker, reporter = newTrackerStage(t, config)
	runFrames(tracker, 0, 6*confirmFrames(config), 7020000, always)
	assert.Empty(t, reporter.events, "an envelope without a change has the autocorrelation 1 at every lag")
}

func TestTrackerNeedsTheWholeCWWindow(t *testing.T) {
	config := testTrackerConfig()
	tracker, reporter := newTrackerStage(t, config)
	window := confirmFrames(config)

	// the count is reached long before the window is full
	runFrames(tracker, 0, window-1, 7020000, keying(2, 3))
	assert.Empty(t, reporter.events, "a candidate must not become a channel before the window is full")

	runFrames(tracker, uint64(window-1), 2, 7020000, keying(2, 3))
	require.NotEmpty(t, reporter.events, "and it must become one when the window is full")
	assert.Equal(t, "created", reporter.events[0].kind)
}

// TestTrackerRejectsSparseDetections is the guard against the false channels of a real band. A few
// peaks of the noise inside one CW window must not make a channel.
//
// ConfirmCount is the count of the detections that a candidate needs inside one CW window, and it
// comes from MinDutyCycle. It is therefore the lower limit of the duty cycle, and the only one:
// isCW tests no other lower limit. A value that is too small makes that test useless, see
// defaultMinDutyCycle.
func TestTrackerRejectsSparseDetections(t *testing.T) {
	config := testTrackerConfig()
	window := confirmFrames(config)

	// The window holds 25 frames, from the first detection to the frame at which isCW decides, and
	// ConfirmCount is 5. The limit of the duty cycle is therefore 0.20.
	tt := []struct {
		name      string
		period    int
		confirmed bool
	}{
		{name: "4 peaks of 25 frames, thus a duty cycle of 0.16", period: 8, confirmed: false},
		{name: "5 peaks of 25 frames, thus the limit of 0.20", period: 6, confirmed: true},
		{name: "9 peaks of 25 frames, thus a duty cycle of 0.36", period: 3, confirmed: true},
	}
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			tracker, reporter := newTrackerStage(t, config)

			runFrames(tracker, 0, 2*window, 700, func(i int) bool { return i%tc.period == 0 })

			assert.Equal(t, tc.confirmed, slices.Contains(reporter.kinds(), "created"))
		})
	}
}

// TestTrackerConfirmsARealKeyingPattern is the other side of that limit: the duty cycle of a CW
// signal is far above it. A measurement of a recording of the 20 m band gives 0.40 to 0.89 for a
// signal that a human heard, and 0.06 to 0.13 for a channel that holds nothing.
func TestTrackerConfirmsARealKeyingPattern(t *testing.T) {
	config := testTrackerConfig()
	tracker, reporter := newTrackerStage(t, config)

	// 3 frames on and 2 frames off, thus a duty cycle of 0.6
	runFrames(tracker, 0, 2*confirmFrames(config), 700, keying(3, 2))

	assert.Contains(t, reporter.kinds(), "created")
}

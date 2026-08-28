package pipeline

import (
	"math"
	"strconv"
	"time"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/dsp"
)

type TrackerConfig[F dsp.Number] struct {
	FrameInterval time.Duration
	MatchWidth    F // Hz, a peak within this distance belongs to a channel that we track
	// Hz, the same for a signal that is not a channel yet. See candidateMatchWidthBins.
	CandidateMatchWidth F
	ConfirmCount        int           // detections that a candidate needs before it becomes a channel
	IdleTimeout         time.Duration // section 5 asks for more than 1 s
	DeadTimeout         time.Duration // section 5 asks for 10 s to 30 s
	MaxDrift            F             // Hz for each second, section 5 asks for approximately 1

	CWWindow           time.Duration // section 4.3 asks for 1 s to 2 s
	MaxDutyCycle       float64       // a carrier, a beacon and a data mode are on all the time
	MinKeyingRate      float64       // Hz, the slowest keying that is still CW, section 4.3 names 5 to 25
	MaxAutocorrelation float64       // above this the envelope repeats too slowly for CW
}

// channelReporter has unexported methods, so only this package can implement it. The pipeline
// implements it and sends the events to the listeners, and the test implements it to collect them.
type channelReporter[F dsp.Number] interface {
	emitChannelCreated(core.Channel[F])
	emitChannelStateChanged(core.Channel[F])
	emitChannelDestroyed(core.Channel[F])
}

type trackedSignal[F dsp.Number] struct {
	channel core.Channel[F]

	// frequency stays a float64, so a drift of much less than one Hz for each frame does not
	// disappear when F is an integer type
	frequency  float64
	detections int
	firstFrame uint64
	lastFrame  uint64

	// envelope holds for each frame of the CW window if the signal had energy in it. The duty
	// cycle and the rate of the keying come from it.
	envelope []bool
	next     int
	samples  int
}

func (s *trackedSignal[F]) putEnvelope(on bool) {
	s.envelope[s.next] = on
	s.next = (s.next + 1) % len(s.envelope)
	if s.samples < len(s.envelope) {
		s.samples++
	}
}

func (s *trackedSignal[F]) envelopeAt(i int) bool {
	return s.envelope[(s.next-s.samples+i+len(s.envelope))%len(s.envelope)]
}

func (s *trackedSignal[F]) dutyCycle() float64 {
	if s.samples == 0 {
		return 0
	}
	on := 0
	for i := range s.samples {
		if s.envelopeAt(i) {
			on++
		}
	}
	return float64(on) / float64(s.samples)
}

// autocorrelation of the envelope at the given lag, from -1 (the envelope is the opposite of
// itself) over 0 (no relation) to 1 (the envelope repeats itself).
func (s *trackedSignal[F]) autocorrelation(lag int) float64 {
	if lag <= 0 || lag >= s.samples {
		return 0
	}
	mean := s.dutyCycle()

	var covariance, variance float64
	for i := range s.samples {
		value := boolToFloat(s.envelopeAt(i)) - mean
		variance += value * value
		if i+lag < s.samples {
			covariance += value * (boolToFloat(s.envelopeAt(i+lag)) - mean)
		}
	}
	if variance == 0 {
		return 1 // an envelope without a change repeats itself at every lag
	}
	return covariance / variance
}

// minAutocorrelation is the smallest autocorrelation over the lags from 1 to maxLag. An envelope
// that switches faster than maxLag comes out of phase with itself somewhere in this range, and the
// value goes to 0 or below. An envelope that switches slower stays high at every lag of the range.
func (s *trackedSignal[F]) minAutocorrelation(maxLag int) float64 {
	result := 1.0
	for lag := 1; lag <= min(maxLag, s.samples-1); lag++ {
		result = min(result, s.autocorrelation(lag))
	}
	return result
}

func boolToFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

type TrackerStage[S, F dsp.Number] struct {
	config   TrackerConfig[F]
	reporter channelReporter[F]

	signals []*trackedSignal[F]
	nextID  int

	cwWindow         int
	maxLag           int
	candidateWindow  int
	idleFrames       int
	deadFrames       int
	maxDriftPerFrame float64
}

func NewTrackerStage[S, F dsp.Number](config TrackerConfig[F], reporter channelReporter[F]) *TrackerStage[S, F] {
	return &TrackerStage[S, F]{
		config:   config,
		reporter: reporter,

		cwWindow:         framesFor(config.CWWindow, config.FrameInterval),
		maxLag:           framesFor(halfPeriod(config.MinKeyingRate), config.FrameInterval),
		candidateWindow:  framesFor(config.CWWindow, config.FrameInterval),
		idleFrames:       framesFor(config.IdleTimeout, config.FrameInterval),
		deadFrames:       framesFor(config.DeadTimeout, config.FrameInterval),
		maxDriftPerFrame: float64(config.MaxDrift) * config.FrameInterval.Seconds(),
	}
}

// halfPeriod is the time of one element of a signal that switches on and off with the given rate.
// It is the lag at which such a signal is the opposite of itself.
func halfPeriod(rate float64) time.Duration {
	if rate <= 0 {
		return 0
	}
	return time.Duration(0.5 / rate * float64(time.Second))
}

func framesFor(duration time.Duration, interval time.Duration) int {
	if duration <= 0 || interval <= 0 {
		return 1
	}
	return max(1, int(math.Round(duration.Seconds()/interval.Seconds())))
}

func (t *TrackerStage[S, F]) Channels() []core.Channel[F] {
	result := make([]core.Channel[F], 0, len(t.signals))
	for _, signal := range t.signals {
		if signal.channel.State != core.NewChannel {
			result = append(result, signal.channel)
		}
	}
	return result
}

// channelOf gives the current data of a channel. The frequency, the SNR and the state all change
// over the life of a channel.
func (t *TrackerStage[S, F]) channelOf(id core.ChannelID) (core.Channel[F], bool) {
	for _, signal := range t.signals {
		if signal.channel.ID == id {
			return signal.channel, true
		}
	}
	return core.Channel[F]{}, false
}

func (t *TrackerStage[S, F]) Process(frame *SpectralFrame[S, F]) {
	for i := range frame.Peaks {
		t.matchPeak(&frame.Peaks[i], frame)
	}
	for _, signal := range t.signals {
		signal.putEnvelope(signal.lastFrame == frame.Sequence)
	}
	t.updateSignals(frame.Sequence)
}

func (t *TrackerStage[S, F]) matchPeak(peak *dsp.Peak[S, F], frame *SpectralFrame[S, F]) {
	snr := float64(dsp.RatioIndB(peak.SignalValue, frame.NoiseFloor[peak.SignalBin]))

	signal := t.nearestSignal(float64(peak.SignalFrequency))
	if signal == nil {
		t.signals = append(t.signals, &trackedSignal[F]{
			channel:    core.Channel[F]{Frequency: peak.SignalFrequency, SNR: snr, State: core.NewChannel},
			frequency:  float64(peak.SignalFrequency),
			detections: 1,
			firstFrame: frame.Sequence,
			lastFrame:  frame.Sequence,
			envelope:   make([]bool, t.cwWindow),
		})
		return
	}
	if signal.lastFrame == frame.Sequence && signal.detections > 0 {
		// a second peak of the same frame belongs to the same signal, and it must not count twice
		return
	}

	// the step is a limit for each unit of time, not for each detection: a CW signal has energy in
	// only a part of the frames, so a limit for each detection would follow a drift with the duty
	// cycle of the signal, and it would lose a signal that drifts fast
	frames := int(frame.Sequence-signal.lastFrame) + 1

	signal.detections++
	signal.lastFrame = frame.Sequence
	signal.frequency = t.follow(signal.frequency, float64(peak.SignalFrequency), frames)
	signal.channel.Frequency = F(signal.frequency)
	signal.channel.SNR = snr
}

// follow moves the frequency of a signal towards the measured peak, with a limited step. This
// follows the drift of a transmitter, and it does not follow the noise.
func (t *TrackerStage[S, F]) follow(current float64, measured float64, frames int) float64 {
	limit := t.maxDriftPerFrame * float64(frames)

	delta := measured - current
	if delta > limit {
		delta = limit
	}
	if delta < -limit {
		delta = -limit
	}
	return current + delta
}

// nearestSignal gives the signal that the given peak belongs to. A channel takes a peak from a
// wider distance than a candidate, see matchWidthHz and candidateMatchWidthBins.
func (t *TrackerStage[S, F]) nearestSignal(frequency float64) *trackedSignal[F] {
	var result *trackedSignal[F]
	nearest := math.Inf(1)

	for _, signal := range t.signals {
		width := float64(t.config.CandidateMatchWidth)
		if signal.channel.State != core.NewChannel {
			width = float64(t.config.MatchWidth)
		}

		distance := math.Abs(signal.frequency - frequency)
		if distance <= width && distance <= nearest {
			nearest = distance
			result = signal
		}
	}
	return result
}

func (t *TrackerStage[S, F]) updateSignals(sequence uint64) {
	kept := t.signals[:0]
	for _, signal := range t.signals {
		if t.transition(signal, sequence) {
			kept = append(kept, signal)
		}
	}
	t.signals = kept

	t.mergeCloseChannels()
}

func (t *TrackerStage[S, F]) transition(signal *trackedSignal[F], sequence uint64) bool {
	silence := int(sequence - signal.lastFrame)

	if signal.channel.State == core.NewChannel {
		if t.isCW(signal, sequence) {
			// One station gives more than one candidate, see mergeCloseChannels. The channel that
			// exists keeps it, and the candidate goes away without an event: no listener knows it
			// yet, so a pair of ChannelCreated and ChannelDestroyed would say nothing.
			if t.hasChannelNear(signal) {
				return false
			}

			t.confirm(signal)
			return true
		}
		// a candidate that never became a channel goes away without an event, because no listener
		// knows it
		return int(sequence-signal.firstFrame) < t.candidateWindow
	}

	switch {
	case silence >= t.deadFrames:
		t.setState(signal, core.DeadChannel)
		t.reporter.emitChannelDestroyed(signal.channel)
		return false
	case silence >= t.idleFrames:
		t.setState(signal, core.IdleChannel)
	case silence == 0:
		t.setState(signal, core.ActiveChannel)
	}
	return true
}

// isCW tests the keying modulation of a candidate, as section 4.3 asks for. It uses the two
// measures of that section, and a candidate must pass both.
//
// The duty cycle is the part of the frames in which the candidate has energy. A carrier, a beacon,
// a birdie and a data mode have energy all the time, thus a duty cycle near 1. A CW signal has
// gaps between the symbols, between the characters and between the words. The window must be at
// least as long as one character, otherwise a single long da gives a duty cycle of 1 and the test
// would remove a real signal. Section 4.3 asks for 1 s to 2 s.
//
// The duty cycle alone does not measure the rate of the keying, so a carrier that switches on and
// off slowly passes it with a duty cycle of 0.5. The autocorrelation of the envelope measures that
// rate: an envelope that switches faster than the lag range comes out of phase with itself inside
// the range, and the smallest value goes to 0 or below. A slow envelope stays high at every lag.
//
// The measurement with the scene of the test command gives these values for the smallest
// autocorrelation over the lags of a keying rate of 2 Hz:
//
//   - CW from 6 WPM to 35 WPM: 0.02 and below,
//   - a carrier: 1.00,
//   - a carrier that switches with 0.5 Hz: 0.57, with 0.25 Hz: 0.79.
//
// The duty cycle of real CW is 0.70 to 0.87 and not the 0.4 of an ideal square, because the window
// of the STFT is longer than one dit and it smears the gaps. See
// doc/architecture.md, section 3.1.
func (t *TrackerStage[S, F]) isCW(signal *trackedSignal[F], sequence uint64) bool {
	frames := int(sequence-signal.firstFrame) + 1
	if frames < t.cwWindow || signal.detections < t.config.ConfirmCount {
		return false
	}

	dutyCycle := float64(signal.detections) / float64(frames)
	if dutyCycle > t.config.MaxDutyCycle {
		return false
	}

	return signal.minAutocorrelation(t.maxLag) <= t.config.MaxAutocorrelation
}

// mergeCloseChannels holds the invariant that no two channels stand closer than MatchWidth. That
// width is the distance inside which two channels decode the same signal, see matchWidthHz, so two
// channels inside it are one station and the receiver must give one spot of it and not two.
//
// **matchPeak alone does not hold that invariant.** It keeps a new signal away from a signal that
// exists, and nothing ever removes a duplicate that exists already. Two ways make one:
//
//   - The width of a candidate, CandidateMatchWidth, is 2 bins and thus 23.4 Hz, while the width of
//     a channel is 100 Hz. peakMergeWidthHz is 40 Hz, so two peak groups of one frame always stand
//     at least 40 Hz apart, and a station whose spectrum gives a second group therefore gives a
//     second candidate. channelNear in transition catches that case, before the candidate becomes a
//     channel.
//   - Two channels that are born farther apart than MatchWidth drift towards each other, because
//     follow pulls both of them onto the peak of the same station. This method catches that case.
//
// Section 6.6 of doc/architecture.md holds the measurement behind both.
//
// It merges one pair for each frame. A further pair comes 21 ms later in the next frame, and a
// recording of a full band holds a few such pairs at most, so the invariant holds after a few
// frames.
//
// ponytail: the scan is O(n²) over the channels, and n is the count of the channels of one receiver,
// thus tens. A sort by frequency would make it O(n log n), and it would need a buffer that lives as
// long as the stage.
func (t *TrackerStage[S, F]) mergeCloseChannels() {
	for i := range t.signals {
		if t.signals[i].channel.State == core.NewChannel {
			continue
		}

		for j := i + 1; j < len(t.signals); j++ {
			if t.signals[j].channel.State == core.NewChannel {
				continue
			}
			if math.Abs(t.signals[i].frequency-t.signals[j].frequency) >= float64(t.config.CandidateMatchWidth) {
				continue
			}

			// the younger of the two goes, so the older one keeps its ID and everything that hangs
			// on it, see remove
			loser := j
			if t.signals[j].firstFrame < t.signals[i].firstFrame {
				loser = i
			}
			t.remove(loser)
			return
		}
	}
}

// hasChannelNear tells if a channel stands closer to the given signal than CandidateMatchWidth. A
// signal that is not a channel yet is no answer: two candidates that stand close together go their
// own way until one of them becomes a channel.
func (t *TrackerStage[S, F]) hasChannelNear(signal *trackedSignal[F]) bool {
	for _, other := range t.signals {
		if other == signal || other.channel.State == core.NewChannel {
			continue
		}
		if math.Abs(other.frequency-signal.frequency) < float64(t.config.CandidateMatchWidth) {
			return true
		}
	}
	return false
}

// remove takes the signal at the given index out and tells the listeners that its channel is gone.
// The signal is always a channel here and never a candidate: a candidate goes away without an event,
// because no listener knows it.
//
// **The older of the two survives**, so everything that hangs on the ID of a channel stays: the hits
// of its callsign, the quality of its spot, the spot itself in the DX cluster, and the state of its
// decoder. The survivor also keeps its own frequency. Moving it onto the frequency of the stronger
// of the two looks better and it is worse: a measurement over the 71 transcriptions gave a worse
// error rate at each of the three places where it happened, and it made the copy of −3667 Hz of
// test_14018_12k.iq and of −1958 Hz of the contest recording worse.
func (t *TrackerStage[S, F]) remove(index int) {
	signal := t.signals[index]

	t.setState(signal, core.DeadChannel)
	t.reporter.emitChannelDestroyed(signal.channel)

	t.signals = append(t.signals[:index], t.signals[index+1:]...)
}

func (t *TrackerStage[S, F]) confirm(signal *trackedSignal[F]) {
	t.nextID++
	signal.channel.ID = core.ChannelID(strconv.Itoa(t.nextID))
	signal.channel.State = core.ConfirmedChannel

	t.reporter.emitChannelCreated(signal.channel)
}

func (t *TrackerStage[S, F]) setState(signal *trackedSignal[F], state core.ChannelState) {
	if signal.channel.State == state {
		return
	}

	signal.channel.State = state
	t.reporter.emitChannelStateChanged(signal.channel)
}

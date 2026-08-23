package cw

import (
	"math"
	"time"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/dsp"
)

const (
	scopeDemod = "demod"

	// The keying detector. See the research document, section 7.
	//
	// The mark level and the gap level are the maximum and the minimum over a sliding window. The
	// window is a multiple of one dit, not an absolute time, because every element of a CW signal
	// scales with the speed. It must always hold at least one mark and one gap, and the longest
	// pair is a word gap of 7 dits and a da of 3 dits.
	levelWindowDits = 12
	slowestWPM      = 5 // the window must also work at the slowest speed that we support

	// decisionFraction is the position of the decision level between the level of the gaps and the
	// level of the marks, in the amplitude. The value comes from a measurement, and it is not 0.5
	// for two reasons.
	//
	// **A signal that fades.** The level of the marks is the maximum over the window, so it holds
	// the value from before a fade. A mark must reach decisionFraction+defaultHysteresis of the
	// span to switch the state on, thus 0.5 of the amplitude, and a mark that is more than 6 dB
	// below the strongest mark inside the window is therefore lost. With 0.5 the limit was 0.65 of
	// the span, thus 3.7 dB, and the signal of the scene with a fade of 6 dB gave a character error
	// rate of 0.353. It now gives 0.000. The limit is the excursion inside the window and not the
	// depth of the fade: a fade of 20 dB with a period of 20 s gives no error, because the window
	// of 2.88 s sees only 4.3 dB of it.
	//
	// **Noise.** The value is also near the optimum for a low SNR. The error rate of a
	// transmission of 231 characters at 35 WPM:
	//
	//	fraction   20 dB   15 dB   12 dB   10 dB
	//	0.50       0.004   0.065   0.307   0.645
	//	0.40       0.004   0.004   0.069   0.208
	//	0.35       0.004   0.004   0.065   0.160
	//	0.30       0.004   0.030   0.160   0.338
	//	0.25       0.022   0.286   0.502   0.632
	//
	// A value that is too high loses a mark that the noise makes weaker, and a value that is too
	// low makes a mark from a peak of the noise in a gap.
	decisionFraction  = 0.35
	defaultHysteresis = 0.15

	// levelFillDivisor sets how much of the level window must hold values before the demodulator
	// makes a decision, as a part of that window. The window is 12 dits of the slowest speed, thus
	// approximately 2.9 s, and one sixteenth of it is approximately 0.18 s.
	//
	// A demodulator that starts knows neither level: the maximum and the minimum are the same
	// value, so the span, the decision level and the margin are all 0, and each value above 0 is a
	// mark. Without this limit the demodulator reports marks before it saw the signal, and the
	// decoder makes wrong characters from them, with a position before the signal. A measurement
	// with the scene gives up to 8 wrong characters at the start of a channel, and 0 or 1 with this
	// limit.
	//
	// The cost is the first element of a stream that begins with a mark: the demodulator reports a
	// gap until it holds values. That is the correct answer, because it knows nothing then. In the
	// pipeline it costs nothing, because the tracker confirms a channel more than one second after
	// the signal appeared, so the decoder of that channel always begins inside a transmission.
	//
	// A measurement of the scene over the divisors 2, 4, 8 and 16 gives the best result at 16: a
	// larger part of the window removes no more wrong characters, and it loses text.
	levelFillDivisor = 16
	minElementTime   = 8 * time.Millisecond // a mark or a gap below this time is noise

	// debounceDits is the time that the keying must stay stable, as a part of one dit. A run that
	// is shorter is not an element of the signal.
	//
	// The value must come from the speed and not from a time: at 18.75 WPM a dit is 64 ms and a run
	// of 27 ms is a fragment, and at 40 WPM a dit is 30 ms and the same 27 ms is almost a dit.
	// minElementTime stays as the smallest value, for the case that the estimate of the speed is
	// still wrong.
	debounceDits = 0.25

	// markDecayTime is the time constant with which the level of the marks goes down. The level
	// follows each mark upwards at once, and without the decay it holds the loudest mark of the
	// whole window: a signal that fades then has marks far below that level, and the detector loses
	// them. See section 7.1 of doc/architecture.md.
	//
	// 0 switches the decay off, and the level is then the maximum over the window.
	markDecayTime = 750 * time.Millisecond
)

// SpectralDemodulator demodulates a CW signal detected in a spectral representation of the
// frequency domain. It tracks the level of the marks and the level of the gaps, and it uses a
// Schmitt trigger between the two levels to find the keying.
// M is used to represent magnitude values.
type SpectralDemodulator[M dsp.Number] struct {
	signalDebouncer *dsp.BoolDebouncer
	decoder         *Decoder
	scope           core.ScopeService

	hysteresis float64

	envelope      *dsp.RollingHistory[float64]
	maxWindow     int
	minLevelCount int
	minDebounce   int
	markDecay     float64
	count         int
	markLevel     float64
	spaceLevel    float64
	state         bool
}

// NewSpectralDemodulator makes a demodulator that takes one envelope value for each frame of a
// spectral analysis. hop is the distance between two frames in samples, and not the size of the
// FFT: it gives the time of one tick, thus the time resolution of the decode.
func NewSpectralDemodulator[M dsp.Number](sink CharacterSink, sampleRate int, hop int) *SpectralDemodulator[M] {
	tickInterval := tickInterval(sampleRate, hop)
	maxWindow := levelWindow(tickInterval, slowestWPM)

	return &SpectralDemodulator[M]{
		minDebounce:     debounceThreshold(tickInterval),
		signalDebouncer: dsp.NewBoolDebouncer(debounceThreshold(tickInterval)),
		decoder:         NewDecoder(sink, sampleRate, hop),
		scope:           &core.NullScopeService{},

		hysteresis:    defaultHysteresis,
		envelope:      dsp.NewRollingHistory[float64](maxWindow),
		maxWindow:     maxWindow,
		minLevelCount: max(1, maxWindow/levelFillDivisor),
		markDecay:     markDecay(tickInterval, markDecayTime),
	}
}

// markDecay gives the factor by which the level of the marks goes down with each tick, for the
// given time constant. A time constant of 0 gives 1, and the level then holds its maximum.
func markDecay(tickInterval time.Duration, decayTime time.Duration) float64 {
	if tickInterval <= 0 || decayTime <= 0 {
		return 1
	}
	return math.Exp(-tickInterval.Seconds() / decayTime.Seconds())
}

// levelWindow returns the length of the level window in ticks, for the given speed.
func levelWindow(tickInterval time.Duration, wpm float64) int {
	if tickInterval <= 0 || wpm <= 0 {
		return 1
	}
	ditSeconds := 60.0 / (50.0 * wpm)

	return max(1, int(math.Ceil(levelWindowDits*ditSeconds/tickInterval.Seconds())))
}

func tickInterval(sampleRate int, hop int) time.Duration {
	if sampleRate <= 0 {
		return 0
	}
	return time.Duration(float64(hop) / float64(sampleRate) * float64(time.Second))
}

// debounceThreshold returns the number of ticks that cover minElementTime. If one tick is longer
// than minElementTime, the result is 1 and the debouncer does nothing, because the frame rate
// already rejects every element that is too short.
func debounceThreshold(tickInterval time.Duration) int {
	if tickInterval <= 0 {
		return 1
	}
	return max(1, int(math.Round(float64(minElementTime)/float64(tickInterval))))
}

func (d *SpectralDemodulator[M]) SetSignalDebounce(debounce int) {
	d.signalDebouncer.SetThreshold(debounce)
}

func (d *SpectralDemodulator[M]) SetScope(scope core.ScopeService) {
	d.scope = scope
	d.decoder.SetScope(scope)
}

func (d *SpectralDemodulator[M]) Reset() {
	d.envelope.Reset()
	d.count = 0
	d.markLevel = 0
	d.spaceLevel = 0
	d.state = false
	d.decoder.Reset()
}

// MarkLevel is the tracked level of the marks, in the domain of the input values.
func (d *SpectralDemodulator[M]) MarkLevel() float64 {
	return d.markLevel
}

// SpaceLevel is the tracked level of the gaps, in the domain of the input values.
func (d *SpectralDemodulator[M]) SpaceLevel() float64 {
	return d.spaceLevel
}

// Tick puts the next value of the signal envelope into the demodulator and returns the state of
// the keying. The demodulator finds its own decision level, so the caller needs no threshold.
func (d *SpectralDemodulator[M]) Tick(value M) bool {
	level := float64(value)
	d.updateLevels(level)

	// The decision uses the amplitude and not the power, because the two edges of a mark must move
	// by the same time. The window of the spectral analysis slides over the edge of a mark, so the
	// power follows the square of the part of the window that the mark covers. A limit in the
	// middle of the power is at a coverage of 71 %: the mark begins 0.71 windows late and it ends
	// only 0.29 windows late, thus each mark is approximately 0.4 windows too short and each gap is
	// as much too long. The error is a constant time, so it is small at a low speed and large at a
	// high speed. A limit in the middle of the amplitude is at a coverage of 50 %, and the two
	// edges then move by the same time.
	markAmplitude := math.Sqrt(max(d.markLevel, 0))
	spaceAmplitude := math.Sqrt(max(d.spaceLevel, 0))
	amplitude := math.Sqrt(max(level, 0))

	span := markAmplitude - spaceAmplitude
	middle := spaceAmplitude + decisionFraction*span
	margin := d.hysteresis * span

	switch {
	case d.count < d.minLevelCount || span <= 0:
		// The window holds only one level, so it saw no mark and no gap yet, or only one of the
		// two. The limit and the margin are then 0, and each value above 0 would be a mark: the
		// demodulator would report a mark in the silence. It knows nothing here, so it reports a
		// gap.
		d.state = false
	case d.state:
		// a Schmitt trigger: the limit to switch on is above the limit to switch off
		d.state = amplitude >= middle-margin
	default:
		d.state = amplitude >= middle+margin
	}

	d.signalDebouncer.SetThreshold(max(d.minDebounce, d.decoder.debounceTicks(debounceDits)))
	debounced := d.signalDebouncer.Debounce(d.state)
	d.decoder.Tick(debounced)
	// the scope shows the power, so the limit goes back into that domain
	d.scopeDemod(middle*middle, level, d.state, debounced)

	return debounced
}

// updateLevels takes the maximum and the minimum over the last levelWindowDits dits. A window over
// several elements is necessary: a filter that follows the minimum would descend into the noise of
// a long mark, and the decision limit would then be inside the mark.
func (d *SpectralDemodulator[M]) updateLevels(level float64) {
	d.envelope.Put(level)
	if d.count < d.maxWindow {
		d.count++
	}

	// The window has the length for the slowest speed. It must not follow the speed of the signal,
	// because the decoder gets its speed from this demodulator: a window that is too short gives a
	// wrong state, the wrong state gives a wrong speed, and the two never recover.
	//
	// The level of the marks follows each mark upwards at once and it goes down with markDecayTime,
	// so a loud mark loses its weight while the window still holds it.
	if d.markDecay < 1 {
		d.markLevel = math.Max(level, d.markLevel*d.markDecay)
	} else {
		d.markLevel = d.envelope.Max(d.count)
	}
	d.spaceLevel = d.envelope.Min(d.count)
}

func (d *SpectralDemodulator[M]) scopeDemod(threshold float64, value float64, state bool, debounced bool) {
	if !d.scope.Active() {
		return
	}

	stateInt := -1
	if state {
		stateInt = 100
	}
	debouncedInt := -1
	if debounced {
		debouncedInt = 80
	}
	d.scope.SendTimeFrame(&core.TimeFrame{
		Frame: core.Frame{
			Stream:    scopeDemod,
			Timestamp: time.Now(),
		},
		Values: map[core.ValueID]float64{
			"threshold":   threshold,
			"value":       value,
			"mark_level":  d.markLevel,
			"space_level": d.spaceLevel,
			"state":       float64(stateInt),
			"debounced":   float64(debouncedInt),
		},
	})
}

package dsp

import (
	"math"
	"slices"
	"time"
)

// MovingMedian calculates the median over a sliding span of a block. The span is measured in
// elements of the block. A centered window must have an odd length, so an even span becomes the
// next odd value. At both ends of the block the window moves inwards, it does not become shorter.
// Every element thus uses the same number of values.
type MovingMedian[T Number] struct {
	span   int
	window []T
}

func NewMovingMedian[T Number](span int) *MovingMedian[T] {
	if span < 1 {
		span = 1
	}
	if span%2 == 0 {
		span++
	}
	return &MovingMedian[T]{
		span:   span,
		window: make([]T, 0, span),
	}
}

// Span is the effective length of the window.
func (m *MovingMedian[T]) Span() int {
	return m.span
}

// Apply writes the median of the span around each element of src to dst. dst and src must not
// share memory. The window is sorted one time and then moves with the output element, so the cost
// for each element does not grow with the span.
func (m *MovingMedian[T]) Apply(dst Block[T], src Block[T]) {
	if len(src) == 0 || len(dst) < len(src) {
		return
	}

	span := min(m.span, len(src))
	half := span / 2
	lastStart := len(src) - span

	m.window = append(m.window[:0], src[:span]...)
	slices.Sort(m.window)

	start := 0
	for i := range src {
		for wanted := max(0, min(i-half, lastStart)); start < wanted; start++ {
			m.remove(src[start])
			m.insert(src[start+span])
		}
		dst[i] = medianOfSorted(m.window)
	}
}

func (m *MovingMedian[T]) remove(value T) {
	i, _ := slices.BinarySearch(m.window, value)
	m.window = append(m.window[:i], m.window[i+1:]...)
}

func (m *MovingMedian[T]) insert(value T) {
	i, _ := slices.BinarySearch(m.window, value)
	m.window = append(m.window, value)
	copy(m.window[i+1:], m.window[i:])
	m.window[i] = value
}

func medianOfSorted[T Number](sorted []T) T {
	count := len(sorted)
	if count == 0 {
		return 0
	}
	if count%2 == 1 {
		return sorted[count/2]
	}
	return (sorted[count/2-1] + sorted[count/2]) / 2
}

// warmUpTimeConstants is how long the estimate stays in its warm-up, in time constants of its own
// time filter, and levelSamples is how many bins the level of one frame comes from.
//
// **The first frame must not decide the whole estimate.** The time filter has nothing to smooth
// against at the start, so the first value is the estimate, and from then on the filter needs many
// time constants to leave a value that was wrong. A stream that begins with a burst therefore blinds
// the detection long after the burst is over: the detection compares each bin against this floor.
//
// A measurement of pipeline/testdata/test_hpsdr_1_48k.iq, a recording of a Hermes-Lite 2, shows what
// that costs. Its stream begins with approximately 1 s that stands 31.3 dB above the level of its
// band, and the floor of that recording then stood more than 3 dB too high for 26.2 s of its 74 s.
// The recording test_14020_12k.iq of a KiwiSDR shows the same effect with 8.6 dB and 5.8 s, and the
// other four recordings show none.
//
// **A burst lifts every bin at once, so the shape of the estimate is right and only its level is
// wrong.** The level is one number, and one number can be held over the warm-up without touching the
// filter: the estimate collects the level of each frame of the warm-up, and at the end it moves the
// whole estimate onto the **median** of those levels, one time. The filter itself never changes its
// speed, so nothing about the way it follows the band changes.
//
// The median answers while less than one half of the warm-up holds the burst, and it answers with
// the level of the band and not with a value below it. The smallest level of the warm-up is the
// other choice and it is worse: the level of one frame is itself a median over levelSamples bins and
// it moves by approximately 16 %, so the smallest of 141 frames stands approximately 2.5 dB under
// the band. That cost the copy of one transcribed signal of test_yo-hf-dx_1_48k.iq.
//
// **Making the filter faster instead is measured and it is wrong.** The keying of CW makes the power
// of a channel fall in each gap, so an estimate that follows a level down quickly loses the noise
// between two marks and makes channels out of the edges:
//
//   - A filter that follows down 10 times faster at every moment gave 6 channels for the 2 stations
//     of one QSO of the scene of the demo.
//   - A warm-up that is only faster, by any factor from 2 upwards, gives a channel beside a station
//     that keys softly, where TestQSOKeyClicksMakeChannelsBesideTheStations asks for none.
//   - A warm-up that takes the smallest value of each single bin answers with the lowest sample of
//     the noise and not with its level: the floor then stood more than 3 dB **below** its own level
//     for 1 to 2 s of four of the six recordings.
//
// The level of one frame is the median over levelSamples bins, spread over the whole block. A median
// over that many samples is stable enough that its smallest value over the warm-up is the level of
// the band and not an outlier of it.
//
// Section 6.2 of doc/architecture.md holds each measurement.
const (
	warmUpTimeConstants = 1
	levelSamples        = 64

	// minWarmUpCorrection is the smallest error of the level that the warm-up corrects, as a factor,
	// thus 3 dB.
	//
	// **A correction that is not necessary costs more than it gives.** The level of the warm-up comes
	// from a median over levelSamples bins of each frame, and the estimate of the filter comes from
	// every bin of every frame, so inside a few dB the filter knows the band better than the warm-up
	// does. A measurement over the 6 recordings shows the cost of correcting anyway: the copy of the
	// signal at −13875 Hz of test_yo-hf-dx_1_48k.iq went from 0.133–0.333 over 10 runs to 0.300–0.400,
	// and that recording begins with no burst at all.
	//
	// With this limit the warm-up holds only the two recordings that need it, and it leaves the four
	// that do not.
	minWarmUpCorrection = 2.0
)

// NoiseFloor estimates the noise power for each bin of a power spectrum. It uses a moving median
// across the frequency axis, and an exponential filter along the time axis with a warm-up, see
// warmUpTimeConstants.
type NoiseFloor[T Number] struct {
	median       *MovingMedian[T]
	medians      Block[T]
	floor        []float64
	out          Block[T]
	medianToMean float64
	smoothing    float64

	// warmUp is the count of the frames of the warm-up, warmUpLevels holds the level of each of
	// them, and levels is the scratch buffer of levelOf.
	warmUp       int
	frames       int
	warmUpLevels []float64
	levels       []float64

	started bool
}

// NewNoiseFloor for a spectrum with the given block size. The span is the width of the median
// window in bins. The smoothing factor is the weight of the newest value in the time filter, from
// 0.0 (no change) to 1.0 (no smoothing). Use SmoothingFactor to calculate it from a time constant.
func NewNoiseFloor[T Number](blockSize int, span int, smoothing float64) *NoiseFloor[T] {
	if blockSize < 1 {
		blockSize = 1
	}
	median := NewMovingMedian[T](span)

	limited := max(0, min(1, smoothing))

	// one time constant holds approximately 1/smoothing frames, so the warm-up needs no interval
	warmUp := 0
	if limited > 0 {
		warmUp = warmUpTimeConstants * int(1/limited)
	}

	return &NoiseFloor[T]{
		median:       median,
		medians:      make(Block[T], blockSize),
		floor:        make([]float64, blockSize),
		out:          make(Block[T], blockSize),
		medianToMean: 1 / exponentialMedianFactor(min(median.Span(), blockSize)),
		smoothing:    limited,
		warmUp:       warmUp,
		warmUpLevels: make([]float64, 0, warmUp),
		levels:       make([]float64, 0, levelSamples),
	}
}

// exponentialMedianFactor returns the expected value of the median of n values with an exponential
// distribution, in units of the mean of that distribution. The value goes to ln(2) for a large n,
// but it is significantly larger for a small window.
func exponentialMedianFactor(n int) float64 {
	var factor float64
	for i := (n + 1) / 2; i <= n; i++ {
		factor += 1 / float64(i)
	}
	return factor
}

// Update the noise floor with a new power spectrum and return the noise power for each bin. The
// values are in the same domain as the input, thus linear power, and they are the estimated
// arithmetic mean of the noise, not the median.
//
// The first frame is the whole estimate, because there is nothing to smooth against yet. The warm-up
// that follows it uses a shorter time constant, so a stream that begins with a burst finds the level
// of its band inside the warm-up instead of after many time constants. See warmUpTimeConstants.
func (n *NoiseFloor[T]) Update(psd Block[T]) Block[T] {
	if len(psd) != len(n.medians) {
		return n.out
	}

	n.median.Apply(n.medians, psd)
	for i := range n.medians {
		// the power of complex Gaussian noise has an exponential distribution, so the median of
		// the window is a known fraction of the mean noise power
		mean := float64(n.medians[i]) * n.medianToMean

		if n.started {
			n.floor[i] += n.smoothing * (mean - n.floor[i])
		} else {
			n.floor[i] = mean
		}
		n.out[i] = T(n.floor[i])
	}
	n.started = true
	n.frames++
	n.holdTheLevelOfTheWarmUp()

	return n.out
}

// holdTheLevelOfTheWarmUp collects the level of each frame of the warm-up, and it moves the whole
// estimate onto the median of those levels when the warm-up ends. See warmUpTimeConstants.
func (n *NoiseFloor[T]) holdTheLevelOfTheWarmUp() {
	if n.frames > n.warmUp {
		return
	}

	if level := n.levelOf(n.medians, n.medianToMean); level > 0 {
		n.warmUpLevels = append(n.warmUpLevels, level)
	}
	if n.frames < n.warmUp || len(n.warmUpLevels) == 0 {
		return
	}

	slices.Sort(n.warmUpLevels)
	wanted := n.warmUpLevels[len(n.warmUpLevels)/2]
	n.warmUpLevels = n.warmUpLevels[:0]

	current := n.levelOf(n.out, 1)
	if current <= 0 || wanted <= 0 || current < wanted*minWarmUpCorrection {
		return
	}

	scale := wanted / current
	for i := range n.floor {
		n.floor[i] *= scale
		n.out[i] = T(n.floor[i])
	}
}

// levelOf gives the level of a block as the median over levelSamples of its bins, spread over the
// whole block, times the given factor.
func (n *NoiseFloor[T]) levelOf(block Block[T], factor float64) float64 {
	if len(block) == 0 {
		return 0
	}

	step := max(1, len(block)/levelSamples)
	n.levels = n.levels[:0]
	for i := 0; i < len(block); i += step {
		n.levels = append(n.levels, float64(block[i])*factor)
	}
	slices.Sort(n.levels)

	return n.levels[len(n.levels)/2]
}

// Get the noise power of the last Update call.
func (n *NoiseFloor[T]) Get() Block[T] {
	return n.out
}

// Reset makes the estimate start again, with a new warm-up. A change of the center frequency gives
// each bin another content, so the old floor says nothing about the new one.
func (n *NoiseFloor[T]) Reset() {
	clear(n.floor)
	clear(n.out)
	n.started = false
	n.frames = 0
	n.warmUpLevels = n.warmUpLevels[:0]
}

// SmoothingFactor for an exponential filter that reaches 63 % of a step within the given time
// constant, if it is updated once for each interval.
func SmoothingFactor(timeConstant time.Duration, interval time.Duration) float64 {
	if timeConstant <= 0 || interval <= 0 {
		return 1
	}
	return 1 - math.Exp(-interval.Seconds()/timeConstant.Seconds())
}

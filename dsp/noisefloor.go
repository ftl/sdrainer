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

// NoiseFloor estimates the noise power for each bin of a power spectrum. It uses a moving median
// across the frequency axis, and an exponential filter along the time axis.
type NoiseFloor[T Number] struct {
	median       *MovingMedian[T]
	medians      Block[T]
	floor        []float64
	out          Block[T]
	medianToMean float64
	smoothing    float64
	started      bool
}

// NewNoiseFloor for a spectrum with the given block size. The span is the width of the median
// window in bins. The smoothing factor is the weight of the newest value in the time filter, from
// 0.0 (no change) to 1.0 (no smoothing). Use SmoothingFactor to calculate it from a time constant.
func NewNoiseFloor[T Number](blockSize int, span int, smoothing float64) *NoiseFloor[T] {
	if blockSize < 1 {
		blockSize = 1
	}
	median := NewMovingMedian[T](span)

	return &NoiseFloor[T]{
		median:       median,
		medians:      make(Block[T], blockSize),
		floor:        make([]float64, blockSize),
		out:          make(Block[T], blockSize),
		medianToMean: 1 / exponentialMedianFactor(min(median.Span(), blockSize)),
		smoothing:    max(0, min(1, smoothing)),
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

	return n.out
}

// Get the noise power of the last Update call.
func (n *NoiseFloor[T]) Get() Block[T] {
	return n.out
}

func (n *NoiseFloor[T]) Reset() {
	clear(n.floor)
	clear(n.out)
	n.started = false
}

// SmoothingFactor for an exponential filter that reaches 63 % of a step within the given time
// constant, if it is updated once for each interval.
func SmoothingFactor(timeConstant time.Duration, interval time.Duration) float64 {
	if timeConstant <= 0 || interval <= 0 {
		return 1
	}
	return 1 - math.Exp(-interval.Seconds()/timeConstant.Seconds())
}

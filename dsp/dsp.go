// Package dsp provides generic implementations of some DSP functionalities.
package dsp

import (
	"fmt"
	"math"
	"time"
)

const (
	DefaultBlocksizeRatio     = 0.005
	DefaultMagnitudeThreshold = 0.75
)

// FilterBlock represents
type FilterBlock []float32

// Max returns the maximum absolute sample value in this filter block
func (b FilterBlock) Max() float32 {
	var max float32
	for _, s := range b {
		abs := float32(math.Abs(float64(s)))
		if abs > max {
			max = abs
		}
	}
	return max
}

// Goertzel filter to detect a specific pitch frequency.
// See also:
// * https://www.embedded.com/the-goertzel-algorithm/
// * https://www.embedded.com/single-tone-detection-with-the-goertzel-algorithm/
type Goertzel struct {
	pitch      float64
	sampleRate int

	blocksize int
	coeff     float64

	magnitudeLimitLow  float64
	magnitudeLimit     float64
	magnitudeThreshold float64
}

// NewDefaultGoertzel returns a new Goertzel filter that uses the DefaultBlocksizeRatio.
func NewDefaultGoertzel(pitch float64, sampleRate int) *Goertzel {
	return NewGoertzel(pitch, sampleRate, DefaultBlocksizeRatio)
}

// NewGoertzel returns a new Goertzel filter to detect the given pitch frequency.
// blocksizeRatio = blocksize / sampleRate
// This is also the duration in seconds that should be covered by one filter block.
// The blocksizeRatio is used to calculate the best fitting block size for the given pitch and sample rate.
func NewGoertzel(pitch float64, sampleRate int, blocksizeRatio float64) *Goertzel {
	blocksize := calculateBlocksize(pitch, sampleRate, blocksizeRatio)
	binIndex := int(0.5 + (float64(blocksize) * pitch / float64(sampleRate)))
	var omega float64 = 2 * math.Pi * float64(binIndex) / float64(blocksize)

	return &Goertzel{
		pitch:      pitch,
		sampleRate: sampleRate,

		blocksize: blocksize,
		coeff:     2 * math.Cos(omega),

		magnitudeLimitLow:  float64(blocksize) / 2, // this is a guesstimation, I just saw that the magnitude values depend on the blocksize
		magnitudeThreshold: DefaultMagnitudeThreshold,
	}
}

func calculateBlocksize(pitch float64, sampleRate int, blocksizeRatio float64) int {
	minBlocksize := math.Round(float64(sampleRate) / pitch)
	return int(math.Round((blocksizeRatio*float64(sampleRate))/minBlocksize)) * int(minBlocksize)
}

// SetMagnitudeThreshold sets the magnitude threshold.
func (f *Goertzel) SetMagnitudeThreshold(threshold float64) {
	f.magnitudeThreshold = threshold
}

// MagnitudeThreshold defines the threshold for the normalized magnitude to be detected as signal.
func (f *Goertzel) MagnitudeThreshold() float64 {
	return f.magnitudeThreshold
}

// Blocksize used for the given pitch and sample rate.
func (f *Goertzel) Blocksize() int {
	return f.blocksize
}

// Tick returns the duration of one filter block.
func (f *Goertzel) Tick() time.Duration {
	return time.Duration((float64(f.blocksize) / float64(f.sampleRate)) * float64(time.Second))
}

// Magnitude returns the relative magnitude of the pitch frequency in the given filter block.
func (f *Goertzel) Magnitude(block FilterBlock) float64 {
	var q0, q1, q2 float64
	for _, sample := range block {
		q0 = f.coeff*q1 - q2 + float64(sample)
		q2 = q1
		q1 = q0
	}
	return math.Sqrt((q1 * q1) + (q2 * q2) - q1*q2*f.coeff)
}

// NormalizedMagnitude returns the magnitude of the pitch frequency in the given feature block
// in relation to the current magnitude limit.
// The normalized magnitude must exceed the magnitude threshold to detect the signal.
func (f *Goertzel) NormalizedMagnitude(block FilterBlock) float64 {
	magnitude := f.Magnitude(block)

	// moving average filter
	if magnitude > f.magnitudeLimitLow {
		f.magnitudeLimit = (f.magnitudeLimit + ((magnitude - f.magnitudeLimit) / 6))
	}
	if f.magnitudeLimit < f.magnitudeLimitLow {
		f.magnitudeLimit = f.magnitudeLimitLow
	}

	return magnitude / f.magnitudeLimit
}

// Detect the pitch in the given buffer. Only the first blocksize samples of the given buffer are used.
// Returns the normalized magnitude, the detected signal state, and the number of samples taken from the buffer.
func (f *Goertzel) Detect(buf []float32) (float64, bool, int, error) {
	if len(buf) < f.blocksize {
		return 0, false, 0, fmt.Errorf("buffer must contain at least %d samples", f.blocksize)
	}

	magnitude := f.NormalizedMagnitude(buf[:f.blocksize])
	state := magnitude > f.magnitudeThreshold

	return magnitude, state, f.blocksize, nil
}

// BoolDebouncer is a debouncer for boolean signals.
type BoolDebouncer struct {
	threshold int

	effectiveState bool
	lastRawState   bool
	stateCount     int
}

// NewBoolDeboncer returns a new debouncer for boolean signals with the given threshold.
func NewBoolDebouncer(threshold int) *BoolDebouncer {
	return &BoolDebouncer{
		threshold: threshold,
	}
}

func (d *BoolDebouncer) SetThreshold(threshold int) {
	d.threshold = threshold
}

func (d *BoolDebouncer) Threshold() int {
	return d.threshold
}

// Debounce is periodically called with the raw signal state. It returns the debounced signal state.
// The signal must be stable for threshold calls of Debounce until a state change is propagated by Debounce.
func (d *BoolDebouncer) Debounce(rawState bool) bool {
	if d.threshold < 2 {
		return rawState
	}

	if rawState != d.lastRawState {
		d.stateCount = 1
	} else {
		d.stateCount++
	}
	d.lastRawState = rawState

	if d.stateCount >= d.threshold {
		if rawState != d.effectiveState {
			d.effectiveState = rawState
		}
	}
	return d.effectiveState
}

// RollingVariance calculates the variance over n values.
type RollingVariance[T Number] struct {
	values []T
	n      T
	next   int

	sumForMean     T
	mean           T
	sumForVariance T
	variance       T
}

// NewRollingVariance with size n.
func NewRollingVariance[T Number](n int) *RollingVariance[T] {
	return &RollingVariance[T]{
		values: make([]T, n),
		n:      T(n),
	}
}

// Put a new value into the rolling window and get the new variance back.
func (v *RollingVariance[T]) Put(value T) T {
	v.sumForMean -= v.values[v.next]
	oldSummand := (v.values[v.next] - v.mean)
	v.sumForVariance -= oldSummand * oldSummand

	v.values[v.next] = value

	v.sumForMean += v.values[v.next]
	v.mean = v.sumForMean / v.n
	newSummand := (v.values[v.next] - v.mean)
	v.sumForVariance += newSummand * newSummand
	v.variance = v.sumForVariance / v.n

	v.next = (v.next + 1) % len(v.values)

	return v.variance
}

// Get the current variance value.
func (v *RollingVariance[T]) Get() T {
	return v.variance
}

// Reset the rolling window.
func (v *RollingVariance[T]) Reset() {
	clear(v.values)
	v.next = 0
	v.sumForMean = 0
	v.mean = 0
	v.sumForVariance = 0
	v.variance = 0
}

// RollingMean calculates the mean over n values.
type RollingMean[T Number] struct {
	values []T
	n      T
	next   int

	sumForMean T
	mean       T
}

// NewRollingMean with size n.
func NewRollingMean[T Number](n int) *RollingMean[T] {
	return &RollingMean[T]{
		values: make([]T, n),
		n:      T(n),
	}
}

// Put a new value into the rolling window and get the new mean back.
func (v *RollingMean[T]) Put(value T) T {
	v.sumForMean -= v.values[v.next]

	v.values[v.next] = value

	v.sumForMean += v.values[v.next]
	v.mean = v.sumForMean / v.n

	v.next = (v.next + 1) % len(v.values)

	return v.mean
}

// Get the current mean value.
func (v *RollingMean[T]) Get() T {
	return v.mean
}

// Reset the rolling window.
func (v *RollingMean[T]) Reset() {
	clear(v.values)
	v.next = 0
	v.sumForMean = 0
	v.mean = 0
}

// RollingHistory provides access to the last <length> values and a set of functions based on those historical values.
type RollingHistory[T Number] struct {
	ring   []T
	length int
	next   int
}

// NewRollingHistory returns a new RollingHistory of the given length.
func NewRollingHistory[T Number](length int) *RollingHistory[T] {
	return &RollingHistory[T]{
		ring:   make([]T, length),
		length: length,
		next:   0,
	}
}

func (h *RollingHistory[T]) ringIndex(index int) int {
	if index > h.length {
		panic(fmt.Sprintf("index %d is greater then the available history length of %d", index, h.length))
	}
	return (h.next - index + h.length) % h.length
}

func (h *RollingHistory[T]) forEach(sliceLength int, f func(value T)) {
	if sliceLength > h.length {
		panic(fmt.Sprintf("history length of %d exceeded: %d", h.length, sliceLength))
	}
	for i := 1; i <= sliceLength; i++ {
		value := h.ring[h.ringIndex(i)]
		f(value)
	}
}

// Reset the rolling history.
func (h *RollingHistory[T]) Reset() {
	clear(h.ring)
	h.next = 0
}

// Get provides the value that was inserted <index> Put calls in the past.
func (h *RollingHistory[T]) Get(index int) T {
	return h.ring[h.ringIndex(index)]
}

// Put a new value into the history.
func (h *RollingHistory[T]) Put(value T) {
	h.ring[h.next] = value
	h.next = (h.next + 1) % h.length
}

// Sum up the last n values.
func (h *RollingHistory[T]) Sum(n int) T {
	var sum T
	h.forEach(n, func(value T) {
		sum += value
	})
	return sum
}

// Max of the last n values.
func (h *RollingHistory[T]) Max(n int) T {
	max := h.Get(1)
	h.forEach(n, func(value T) {
		if max < value {
			max = value
		}
	})
	return max
}

// Min of the last n values.
func (h *RollingHistory[T]) Min(n int) T {
	min := h.Get(1)
	h.forEach(n, func(value T) {
		if min > value {
			min = value
		}
	})
	return min
}

// SumAt sums up the values at the given indexes.
func (h *RollingHistory[T]) SumAt(indexes ...int) T {
	var sum T
	for _, index := range indexes {
		sum += h.Get(index)
	}
	return sum
}

// MaxAt returns the maximum of the values at the given indexes.
func (h *RollingHistory[T]) MaxAt(indexes ...int) T {
	max := h.Get(indexes[0])
	for _, index := range indexes {
		value := h.Get(index)
		if max < value {
			max = value
		}
	}
	return max
}

// MinAt returns the minimum of the values at the given indexes.
func (h *RollingHistory[T]) MinAt(indexes ...int) T {
	min := h.Get(indexes[0])
	for _, index := range indexes {
		value := h.Get(index)
		if min > value {
			min = value
		}
	}
	return min
}

// Mean of the last n values.
func (h *RollingHistory[T]) Mean(n int) T {
	sum := h.Sum(n)
	return T(float64(sum) / float64(n))
}

// Variance of the last n values.
func (h *RollingHistory[T]) Variance(n int) float64 {
	mean := float64(h.Sum(n)) / float64(n)
	var sum float64
	h.forEach(n, func(value T) {
		sum += math.Pow(float64(value)-mean, 2)
	})
	return sum / float64(n)
}

// SDev returns the standard deviation of the last n values.
func (h *RollingHistory[T]) SDev(n int) float64 {
	return math.Sqrt(h.Variance(n))
}

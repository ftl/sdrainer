package dsp

import "math"

// LowPass is a FIR filter with a windowed sinc. It passes each frequency below its cutoff and it
// removes each frequency above the cutoff plus the transition width.
type LowPass struct {
	taps    []float64
	history []float64
	next    int
}

// NewLowPass makes a filter with the given cutoff and transition width, both in Hz.
//
// The count of the taps comes from the transition width: a Blackman window needs approximately
// 4·sampleRate/transition taps for it. A smaller transition width therefore costs time.
func NewLowPass(cutoff float64, transition float64, sampleRate int) *LowPass {
	if sampleRate <= 0 || cutoff <= 0 || transition <= 0 {
		return &LowPass{taps: []float64{1}, history: make([]float64, 1)}
	}

	count := int(math.Ceil(4 * float64(sampleRate) / transition))
	if count%2 == 0 {
		// an odd count gives a symmetric filter with the maximum in the middle
		count++
	}

	taps := make([]float64, count)
	middle := float64(count-1) / 2
	normalized := cutoff / float64(sampleRate)

	var sum float64
	for i := range taps {
		position := float64(i) - middle
		taps[i] = sinc(2*normalized*position) * blackman(i, count)
		sum += taps[i]
	}
	// the sum of the taps must be 1, so that the filter changes no level in its passband
	for i := range taps {
		taps[i] /= sum
	}

	return &LowPass{
		taps:    taps,
		history: make([]float64, count),
	}
}

// Filter takes one value and gives the value of the filter for it. The filter delays the signal by
// one half of the count of its taps.
func (f *LowPass) Filter(value float64) float64 {
	f.history[f.next] = value
	f.next = (f.next + 1) % len(f.history)

	var result float64
	index := f.next
	for i := len(f.taps) - 1; i >= 0; i-- {
		result += f.taps[i] * f.history[index]
		index = (index + 1) % len(f.history)
	}

	return result
}

// Taps gives the count of the taps of this filter.
func (f *LowPass) Taps() int {
	return len(f.taps)
}

// Reset forgets the values of the stream so far.
func (f *LowPass) Reset() {
	clear(f.history)
	f.next = 0
}

func sinc(x float64) float64 {
	if x == 0 {
		return 1
	}
	return math.Sin(math.Pi*x) / (math.Pi * x)
}

func blackman(i int, count int) float64 {
	n := float64(i) / float64(count-1)
	return 0.42 - 0.5*math.Cos(2*math.Pi*n) + 0.08*math.Cos(4*math.Pi*n)
}

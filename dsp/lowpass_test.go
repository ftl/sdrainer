package dsp

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// levelOf gives the level of the filter at the given frequency, from 1 (the filter passes it) to 0
// (the filter removes it). It uses a tone that is long enough for the filter to settle.
func levelOf(filter *LowPass, frequency float64, sampleRate int) float64 {
	filter.Reset()

	settle := 4 * filter.Taps()
	var peak float64
	for i := range settle + 2*sampleRate/int(math.Max(frequency, 1)) {
		value := math.Sin(2 * math.Pi * frequency * float64(i) / float64(sampleRate))
		result := filter.Filter(value)
		if i >= settle {
			peak = math.Max(peak, math.Abs(result))
		}
	}
	return peak
}

// TestLowPassPassesAndRemoves checks the two ends of the filter that the listen command uses: the
// passband holds the signal, and a station beside it is gone.
func TestLowPassPassesAndRemoves(t *testing.T) {
	const (
		sampleRate = 12000
		cutoff     = 150.0
		transition = 100.0
	)

	filter := NewLowPass(cutoff, transition, sampleRate)

	tt := []struct {
		frequency float64
		minLevel  float64
		maxLevel  float64
		reason    string
	}{
		{frequency: 10, minLevel: 0.95, maxLevel: 1.05, reason: "the middle of the passband"},
		{frequency: 100, minLevel: 0.9, maxLevel: 1.05, reason: "still inside the passband"},
		{frequency: 400, maxLevel: 0.01, reason: "above the cutoff and the transition"},
		{frequency: 1000, maxLevel: 0.001, reason: "the next station"},
		{frequency: 3000, maxLevel: 0.001, reason: "far away"},
	}

	for _, tc := range tt {
		level := levelOf(filter, tc.frequency, sampleRate)

		assert.LessOrEqualf(t, level, tc.maxLevel, "%.0f Hz, %s: %f", tc.frequency, tc.reason, level)
		if tc.minLevel > 0 {
			assert.GreaterOrEqualf(t, level, tc.minLevel, "%.0f Hz, %s: %f", tc.frequency, tc.reason, level)
		}
	}
}

// TestLowPassTapsComeFromTheTransition checks the rule of the count of the taps: a smaller
// transition width needs more of them.
func TestLowPassTapsComeFromTheTransition(t *testing.T) {
	wide := NewLowPass(150, 200, 12000)
	narrow := NewLowPass(150, 100, 12000)

	assert.Equal(t, 241, wide.Taps())
	assert.Equal(t, 481, narrow.Taps())
	assert.Equal(t, 1, wide.Taps()%2, "an odd count gives a symmetric filter")
}

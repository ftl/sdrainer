package dsp

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMovingMedianIgnoresNarrowPeaks(t *testing.T) {
	src := make(Block[float64], 100)
	for i := range src {
		src[i] = 1
	}
	for i := 48; i <= 52; i++ {
		src[i] = 100
	}

	dst := make(Block[float64], len(src))
	NewMovingMedian[float64](21).Apply(dst, src)

	for i := range dst {
		assert.InDeltaf(t, 1, dst[i], 1e-9, "bin %d must not see the peak", i)
	}
}

func TestMovingMedianFollowsAStep(t *testing.T) {
	src := make(Block[float64], 100)
	for i := range src {
		if i < 50 {
			src[i] = 1
		} else {
			src[i] = 5
		}
	}

	dst := make(Block[float64], len(src))
	NewMovingMedian[float64](11).Apply(dst, src)

	assert.InDelta(t, 1, dst[0], 1e-9, "far below the step")
	assert.InDelta(t, 1, dst[40], 1e-9, "below the step")
	assert.InDelta(t, 5, dst[60], 1e-9, "above the step")
	assert.InDelta(t, 5, dst[99], 1e-9, "far above the step")
}

func TestMovingMedianAtTheEdgesOfTheBlock(t *testing.T) {
	src := Block[float64]{9, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	dst := make(Block[float64], len(src))

	NewMovingMedian[float64](5).Apply(dst, src)

	// the window of bin 0 moves inwards to {9, 1, 2, 3, 4}
	assert.InDelta(t, 3, dst[0], 1e-9, "first bin")
	// the window of bin 9 moves inwards to {5, 6, 7, 8, 9}
	assert.InDelta(t, 7, dst[9], 1e-9, "last bin")
}

func TestMovingMedianUsesAnOddSpan(t *testing.T) {
	m := NewMovingMedian[float64](4)
	assert.Equal(t, 5, m.Span(), "an even span becomes the next odd value")

	src := Block[float64]{1, 2, 3, 4, 5, 6, 7, 8}
	dst := make(Block[float64], len(src))

	m.Apply(dst, src)

	// the window of bin 4 is {3, 4, 5, 6, 7}
	assert.InDelta(t, 5, dst[4], 1e-9)
}

func TestMovingMedianWithASpanLargerThanTheBlock(t *testing.T) {
	src := Block[float64]{5, 1, 3}
	dst := make(Block[float64], len(src))

	NewMovingMedian[float64](21).Apply(dst, src)

	for i := range dst {
		assert.InDeltaf(t, 3, dst[i], 1e-9, "bin %d uses the whole block", i)
	}
}

func TestMovingMedianMatchesTheNaiveImplementation(t *testing.T) {
	random := rand.New(rand.NewPCG(7, 11))
	for _, span := range []int{1, 3, 5, 21, 43} {
		for _, length := range []int{1, 2, 44, 200} {
			t.Run(fmt.Sprintf("span_%d_length_%d", span, length), func(t *testing.T) {
				src := make(Block[float64], length)
				for i := range src {
					// a small set of values gives many duplicates
					src[i] = float64(random.IntN(5))
				}

				actual := make(Block[float64], length)
				NewMovingMedian[float64](span).Apply(actual, src)

				expected := naiveMovingMedian(src, min(span, length))
				assert.Equal(t, expected, actual)
			})
		}
	}
}

func naiveMovingMedian(src Block[float64], span int) Block[float64] {
	half := span / 2
	lastStart := len(src) - span

	result := make(Block[float64], len(src))
	for i := range src {
		from := max(0, min(i-half, lastStart))

		window := make([]float64, span)
		copy(window, src[from:from+span])
		slices.Sort(window)
		result[i] = medianOfSorted(window)
	}
	return result
}

func TestExponentialMedianFactor(t *testing.T) {
	assert.InDelta(t, 1, exponentialMedianFactor(1), 1e-9, "one value is its own median")
	assert.InDelta(t, 1.0/2+1.0/3, exponentialMedianFactor(3), 1e-9, "three values")
	assert.InDelta(t, math.Ln2, exponentialMedianFactor(100001), 1e-4, "many values go to ln(2)")
	assert.Greater(t, exponentialMedianFactor(43), math.Ln2, "a small window has a positive bias")
}

func TestNoiseFloorEstimatesTheMeanOfExponentialNoise(t *testing.T) {
	const (
		blockSize = 4096
		meanPower = 3.0
	)
	random := rand.New(rand.NewPCG(23, 42))

	psd := make(Block[float64], blockSize)
	for i := range psd {
		// the power of complex Gaussian noise has an exponential distribution
		psd[i] = -meanPower * math.Log(random.Float64())
	}

	floor := NewNoiseFloor[float64](blockSize, 43, 1).Update(psd)

	var sum float64
	for _, value := range floor {
		sum += value
	}
	estimated := sum / float64(len(floor))

	// the median of one window has a standard deviation of the mean divided by the square root of
	// the span, and the windows overlap, so 5 % is close to three standard errors
	assert.InDelta(t, meanPower, estimated, 0.05*meanPower, "the estimated floor must be the mean noise power")
}

func TestNoiseFloorIgnoresSignals(t *testing.T) {
	const blockSize = 1024
	level := exponentialMedianFactor(43) // one, after the correction

	psd := make(Block[float64], blockSize)
	for i := range psd {
		psd[i] = level
	}
	for _, bin := range []int{100, 101, 102, 500, 501, 800} {
		psd[bin] = 1000
	}

	floor := NewNoiseFloor[float64](blockSize, 43, 1).Update(psd)

	for i := range floor {
		assert.InDeltaf(t, 1, floor[i], 1e-9, "bin %d must not see the signals", i)
	}
}

func TestNoiseFloorSmoothsOverTime(t *testing.T) {
	const blockSize = 64
	level := exponentialMedianFactor(5)

	low := make(Block[float64], blockSize)
	high := make(Block[float64], blockSize)
	for i := range low {
		low[i] = level
		high[i] = 11 * level
	}

	n := NewNoiseFloor[float64](blockSize, 5, 0.5)

	first := n.Update(low)
	require.InDelta(t, 1, first[0], 1e-9, "the first frame must not be smoothed")

	// the warm-up holds the level of its quietest frame, see warmUpTimeConstants, so this test
	// gives it the same quiet frame until it is over
	for range warmUpFramesFor(0.5) {
		require.InDelta(t, 1, n.Update(low)[0], 1e-9, "a quiet warm-up must not move the estimate")
	}

	assert.InDelta(t, 6, n.Update(high)[0], 1e-9, "half of the step")
	assert.InDelta(t, 8.5, n.Update(high)[0], 1e-9, "half of the rest")
}

func warmUpFramesFor(smoothing float64) int {
	return warmUpTimeConstants * int(1/smoothing)
}

// TestNoiseFloorLeavesTheLevelOfABurstAtTheStart covers the stream that begins with a burst. The
// first frame is the whole estimate, so without the warm-up that burst stays in it for many time
// constants: a recording of a Hermes-Lite 2 kept a floor that was more than 3 dB too high for 26.2 s
// of its 74 s. See warmUpTimeConstants.
func TestNoiseFloorLeavesTheLevelOfABurstAtTheStart(t *testing.T) {
	const blockSize = 64
	const smoothing = 0.01
	level := exponentialMedianFactor(5)

	quiet := make(Block[float64], blockSize)
	burst := make(Block[float64], blockSize)
	for i := range quiet {
		quiet[i] = level
		burst[i] = 1000 * level // 30 dB above the band
	}

	n := NewNoiseFloor[float64](blockSize, 5, smoothing)

	// the stream begins with the burst, over a part of the warm-up
	warmUp := warmUpFramesFor(smoothing)
	for range warmUp / 3 {
		n.Update(burst)
	}
	require.Greater(t, n.Get()[0], 100.0, "the estimate stands far above the band while the burst holds")

	// and the band is quiet for the rest of the warm-up
	var out Block[float64]
	for range warmUp {
		out = n.Update(quiet)
	}

	assert.InDelta(t, 1, out[0], 0.05, "the estimate stands at the level of the band when the warm-up ends")
}

// TestNoiseFloorKeepsTheSlowFilterAfterTheWarmUp holds that the warm-up changes the level and never
// the speed: an estimate that follows a level down quickly loses the noise between two marks of the
// keying, see warmUpTimeConstants.
func TestNoiseFloorKeepsTheSlowFilterAfterTheWarmUp(t *testing.T) {
	const blockSize = 64
	const smoothing = 0.01
	level := exponentialMedianFactor(5)

	quiet := make(Block[float64], blockSize)
	loud := make(Block[float64], blockSize)
	for i := range quiet {
		quiet[i] = level
		loud[i] = 100 * level
	}

	n := NewNoiseFloor[float64](blockSize, 5, smoothing)
	for range warmUpFramesFor(smoothing) + 1 {
		n.Update(quiet)
	}
	require.InDelta(t, 1, n.Get()[0], 1e-9)

	// one loud frame after the warm-up moves the estimate by the smoothing factor and no more
	out := n.Update(loud)

	assert.InDelta(t, 1+smoothing*(100-1), out[0], 1e-6, "one frame moves the estimate by its factor")
}

func TestNoiseFloorWithAWrongBlockSize(t *testing.T) {
	n := NewNoiseFloor[float64](64, 5, 1)

	assert.NotPanics(t, func() {
		n.Update(make(Block[float64], 32))
	})
}

func TestSmoothingFactor(t *testing.T) {
	tt := []struct {
		desc         string
		timeConstant time.Duration
		interval     time.Duration
		expected     float64
	}{
		{desc: "one time constant for each interval", timeConstant: time.Second, interval: time.Second, expected: 1 - 1/math.E},
		{desc: "a slow filter", timeConstant: 5 * time.Second, interval: 21300 * time.Microsecond, expected: 0.00426},
		{desc: "no time constant", timeConstant: 0, interval: time.Second, expected: 1},
		{desc: "no interval", timeConstant: time.Second, interval: 0, expected: 1},
	}
	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			assert.InDelta(t, tc.expected, SmoothingFactor(tc.timeConstant, tc.interval), 1e-5)
		})
	}
}

func BenchmarkNoiseFloorUpdate(b *testing.B) {
	const blockSize = 4096
	random := rand.New(rand.NewPCG(23, 42))
	psd := make(Block[float32], blockSize)
	for i := range psd {
		psd[i] = float32(-3 * math.Log(random.Float64()))
	}
	n := NewNoiseFloor[float32](blockSize, 43, 0.004)

	b.ResetTimer()
	for range b.N {
		n.Update(psd)
	}
}

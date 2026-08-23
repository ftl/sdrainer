package dsp

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/mjibson/go-dsp/fft"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBinToSpectrumIndex(t *testing.T) {
	tt := []struct {
		blockSize int
		bin       int
		expected  int
	}{
		{blockSize: 512, bin: 0, expected: 256},
		{blockSize: 512, bin: 1, expected: 257},
		{blockSize: 512, bin: 255, expected: 511},
		{blockSize: 512, bin: 256, expected: 0},
		{blockSize: 512, bin: 257, expected: 1},
		{blockSize: 512, bin: 511, expected: 255},
	}
	for _, tc := range tt {
		t.Run(fmt.Sprintf("%d_%d", tc.blockSize, tc.bin), func(t *testing.T) {
			actual := binToSpectrumIndex(tc.bin, tc.blockSize)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestPeakContainsBin(t *testing.T) {
	tt := []struct {
		peak     Peak[float64, float64]
		bin      int
		expected bool
	}{
		{peak: Peak[float64, float64]{From: 10, To: 20}, bin: 9, expected: false},
		{peak: Peak[float64, float64]{From: 10, To: 20}, bin: 10, expected: true},
		{peak: Peak[float64, float64]{From: 10, To: 20}, bin: 15, expected: true},
		{peak: Peak[float64, float64]{From: 10, To: 20}, bin: 20, expected: true},
		{peak: Peak[float64, float64]{From: 10, To: 20}, bin: 21, expected: false},
		{peak: Peak[float64, float64]{From: 10, To: 10}, bin: 10, expected: true},
		{peak: Peak[float64, float64]{From: 10, To: 10}, bin: 11, expected: false},
	}
	for _, tc := range tt {
		t.Run(fmt.Sprintf("%d_%d_%d", tc.peak.From, tc.peak.To, tc.bin), func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.peak.ContainsBin(tc.bin))
		})
	}
}

func TestBlockMax(t *testing.T) {
	block := Block[float64]{100, 1, 2, 3, 4, 2}
	tt := []struct {
		from, to      int
		expectedValue float64
		expectedIndex int
	}{
		{from: 0, to: 5, expectedValue: 100, expectedIndex: 0},
		{from: 1, to: 5, expectedValue: 4, expectedIndex: 4},
		{from: 1, to: 3, expectedValue: 3, expectedIndex: 3},
		{from: 2, to: 2, expectedValue: 2, expectedIndex: 2},
	}
	for _, tc := range tt {
		t.Run(fmt.Sprintf("%d_%d", tc.from, tc.to), func(t *testing.T) {
			value, index := block.Max(tc.from, tc.to)

			assert.Equal(t, tc.expectedValue, value, "value")
			assert.Equal(t, tc.expectedIndex, index, "index")
		})
	}
}

func TestPeakCenterCorrection(t *testing.T) {
	tt := []struct {
		desc     string
		spectrum Block[float64]
		bin      int
		expected float64
	}{
		{desc: "symmetric peak", spectrum: Block[float64]{1, 2, 4, 2, 1}, bin: 2, expected: 0},
		{desc: "peak shifted up", spectrum: Block[float64]{1, 2, 4, 3, 1}, bin: 2, expected: 0.16666},
		{desc: "peak shifted down", spectrum: Block[float64]{1, 3, 4, 2, 1}, bin: 2, expected: -0.16666},
		{desc: "flat top", spectrum: Block[float64]{1, 5, 5, 5, 1}, bin: 2, expected: 0},
		{desc: "constant", spectrum: Block[float64]{3, 3, 3, 3, 3}, bin: 2, expected: 0},
		{desc: "at the lower edge", spectrum: Block[float64]{4, 2, 1, 1, 1}, bin: 0, expected: 0},
		{desc: "at the upper edge", spectrum: Block[float64]{1, 1, 1, 2, 4}, bin: 4, expected: 0},
		{desc: "almost flat, clamped to the bin limit", spectrum: Block[float64]{1, 4, 5, 4.9999999, 1}, bin: 2, expected: 0.5},
		{desc: "step, clamped to the bin limit", spectrum: Block[float64]{1, 1, 5, 5, 1}, bin: 2, expected: 0.5},
	}
	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			actual := PeakCenterCorrection[float64, float64](tc.bin, tc.spectrum)

			assert.False(t, math.IsNaN(float64(actual)), "the correction must not be NaN")
			assert.GreaterOrEqual(t, actual, BinFrom, "the correction must stay inside the bin")
			assert.LessOrEqual(t, actual, BinTo, "the correction must stay inside the bin")
			assert.InDelta(t, tc.expected, float64(actual), 1e-4)
		})
	}
}

func TestFindNoiseFloor(t *testing.T) {
	psd := make(Block[float64], 1000)
	for i := range psd {
		if i < 500 {
			psd[i] = 5
		} else {
			psd[i] = 2
		}
	}

	floor := FindNoiseFloor(psd, 100)

	assert.InDelta(t, 2, floor, 1e-9, "the floor is the mean of the quietest window")
}

func TestFindNoiseFloorWithUnusableParameters(t *testing.T) {
	tt := []struct {
		desc      string
		length    int
		edgeWidth int
	}{
		{desc: "empty block", length: 0, edgeWidth: 0},
		{desc: "block smaller than one window", length: 32, edgeWidth: 14},
		{desc: "edge width covers the whole block", length: 100, edgeWidth: 50},
		{desc: "edge width larger than the block", length: 100, edgeWidth: 200},
		{desc: "negative edge width", length: 100, edgeWidth: -10},
	}
	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			psd := make(Block[float64], tc.length)
			for i := range psd {
				psd[i] = 1
			}

			floor := FindNoiseFloor(psd, tc.edgeWidth)

			assert.False(t, math.IsNaN(float64(floor)), "the floor must not be NaN")
			assert.False(t, math.IsInf(float64(floor), 0), "the floor must not be infinite")
		})
	}
}

func TestFrequencyToBinAgreesWithBinToFrequency(t *testing.T) {
	const (
		sampleRate      = 48000
		blockSize       = 4096
		centerFrequency = 7020000.0
	)
	binSize := float64(sampleRate) / float64(blockSize) // 11.71875 Hz

	m := NewFrequencyMapping[float64](sampleRate, blockSize, centerFrequency)
	centerBin := m.FrequencyToBin(centerFrequency)
	require.Equal(t, blockSize/2, centerBin)

	tt := []struct {
		desc     string
		offset   float64
		expected int
	}{
		{desc: "on the center of the bin", offset: 0, expected: centerBin},
		{desc: "inside the upper half of the bin", offset: 0.4 * binSize, expected: centerBin},
		{desc: "inside the lower half of the bin", offset: -0.4 * binSize, expected: centerBin},
		{desc: "above the upper limit of the bin", offset: 0.6 * binSize, expected: centerBin + 1},
		{desc: "below the lower limit of the bin", offset: -0.6 * binSize, expected: centerBin - 1},
		{desc: "one bin higher", offset: binSize, expected: centerBin + 1},
		{desc: "one bin lower", offset: -binSize, expected: centerBin - 1},
	}
	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			frequency := centerFrequency + tc.offset

			actual := m.FrequencyToBin(frequency)

			assert.Equal(t, tc.expected, actual)
			assert.LessOrEqual(t, m.BinToFrequency(actual, BinFrom), frequency, "the bin must start at or below the frequency")
			assert.GreaterOrEqual(t, m.BinToFrequency(actual, BinTo), frequency, "the bin must end at or above the frequency")
		})
	}
}

func TestBinToFrequencyKeepsTheFractionOfAHertz(t *testing.T) {
	const (
		sampleRate = 48000
		blockSize  = 4096
	)
	binSize := float64(sampleRate) / float64(blockSize) // 11.71875 Hz

	m := NewFrequencyMapping[float64](sampleRate, blockSize, 7020000)
	fromFrequency := 7020000.0 - float64(sampleRate)/2

	assert.InDelta(t, fromFrequency+3*binSize, m.BinToFrequency(3, BinCenter), 1e-9, "center")
	assert.InDelta(t, fromFrequency+2.5*binSize, m.BinToFrequency(3, BinFrom), 1e-9, "lower limit")
	assert.InDelta(t, fromFrequency+3.5*binSize, m.BinToFrequency(3, BinTo), 1e-9, "upper limit")
}

func TestBinToFrequencyRoundsForAnIntegerFrequencyType(t *testing.T) {
	const (
		sampleRate = 48000
		blockSize  = 4096
	)

	m := NewFrequencyMapping[int](sampleRate, blockSize, 7020000)
	// bin 3 is 6996035.15625 Hz, bin 4 is 6996046.875 Hz
	assert.Equal(t, 6996035, m.BinToFrequency(3, BinCenter))
	assert.Equal(t, 6996047, m.BinToFrequency(4, BinCenter))
}

func TestFrequencyToBinStaysInsideTheBlock(t *testing.T) {
	m := NewFrequencyMapping[float64](48000, 4096, 7020000)

	// the block covers 6996000 Hz to 7044000 Hz
	assert.Equal(t, 0, m.FrequencyToBin(6996000), "the first bin")
	assert.Equal(t, 4095, m.FrequencyToBin(7044000-1), "the last bin")
	assert.Equal(t, 0, m.FrequencyToBin(6990000), "far below the block")
	assert.Equal(t, 4095, m.FrequencyToBin(7050000), "far above the block")
}

func TestRatioIndB(t *testing.T) {
	tt := []struct {
		desc      string
		power     float64
		reference float64
		expected  float64
	}{
		{desc: "equal", power: 1, reference: 1, expected: 0},
		{desc: "two times the power", power: 2, reference: 1, expected: 3.0103},
		{desc: "ten times the power", power: 10, reference: 1, expected: 10},
		{desc: "one hundred times the power", power: 100, reference: 1, expected: 20},
		{desc: "half the power", power: 0.5, reference: 1, expected: -3.0103},
		{desc: "the reference is not one", power: 40, reference: 4, expected: 10},
		{desc: "the 6 dB threshold of the research document", power: 3.9811, reference: 1, expected: 6},
		{desc: "the 10 dB threshold of the research document", power: 10, reference: 1, expected: 10},
	}
	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			assert.InDelta(t, tc.expected, RatioIndB(tc.power, tc.reference), 1e-4)
		})
	}
}

func TestPowerIndB(t *testing.T) {
	assert.InDelta(t, 0, PowerIndB(1.0), 1e-9)
	assert.InDelta(t, 20, PowerIndB(100.0), 1e-9)
	assert.InDelta(t, -20, PowerIndB(0.01), 1e-9)

	// a tone with the amplitude 0.5 gives the power 0.25 with a normalized window
	assert.InDelta(t, -6.0206, PowerIndB(0.25), 1e-4)
	assert.InDelta(t, float32(-6.0206), PowerIndB(float32(0.25)), 1e-4)
}

func TestPSDValueIndBHasNoArbitraryFactor(t *testing.T) {
	// the value N² is the power of a tone with the amplitude 1.0, for an FFT of the size N with a
	// rectangular window, so it must give 0 dB
	assert.InDelta(t, 0, PSDValueIndB(4096.0*4096.0, 4096), 1e-9, "full scale")
	assert.InDelta(t, 0, PSDValueIndB(1.0, 1), 1e-9, "a block size of one")
	assert.InDelta(t, -20, PSDValueIndB(4096.0*4096.0/100, 4096), 1e-9, "one hundredth of the power")

	assert.InDelta(t, PowerIndB(0.25), PSDValueIndB(0.25, 1), 1e-9, "it agrees with PowerIndB")
}

func TestDecibelValuesStayFinite(t *testing.T) {
	assert.Equal(t, minimumDB, RatioIndB(0.0, 1.0), "power zero")
	assert.Equal(t, minimumDB, RatioIndB(1.0, 0.0), "reference zero")
	assert.Equal(t, minimumDB, RatioIndB(-1.0, 1.0), "negative power")
	assert.Equal(t, minimumDB, PowerIndB(0.0), "power zero")
	assert.Equal(t, minimumDB, PSDValueIndB(0.0, 4096), "the old function with power zero")
	assert.Equal(t, minimumDB, PSDValueIndB(1.0, 0), "the old function without a block size")

	for _, value := range []float64{RatioIndB(0.0, 1.0), PowerIndB(0.0), PSDValueIndB(0.0, 4096)} {
		assert.False(t, math.IsInf(value, 0), "no infinite value")
		assert.False(t, math.IsNaN(value), "no NaN")
	}
}

func TestRadix2MatchesGoDSP(t *testing.T) {
	for _, blockSize := range []int{1, 2, 4, 8, 16, 64, 256, 1024, 4096} {
		t.Run(fmt.Sprintf("%d", blockSize), func(t *testing.T) {
			samples := randomSamples(blockSize, 1)
			expected := fft.FFT(samples)

			actual := make([]complex128, blockSize)
			copy(actual, samples)
			var r radix2
			r.transform(actual)

			require.Len(t, actual, len(expected))
			for i := range expected {
				assert.InDelta(t, real(expected[i]), real(actual[i]), 1e-9, "real part of bin %d", i)
				assert.InDelta(t, imag(expected[i]), imag(actual[i]), 1e-9, "imaginary part of bin %d", i)
			}
		})
	}
}

func TestRadix2ReusesTheTwiddleFactors(t *testing.T) {
	samples := randomSamples(1024, 2)
	var r radix2

	allocations := testing.AllocsPerRun(10, func() {
		r.transform(samples)
	})

	assert.Zero(t, allocations, "the transform must work in place")
}

func TestIQToSpectrumMatchesGoDSP(t *testing.T) {
	for _, blockSize := range []int{100, 512, 1000, 1024} {
		t.Run(fmt.Sprintf("%d", blockSize), func(t *testing.T) {
			iqSamples := randomIQSamples(blockSize, 3)

			expected := goDSPSpectrum(iqSamples)

			actual := make([]float64, blockSize)
			NewFFT[float64]().IQToSpectrum(actual, iqSamples, Magnitude[float64])

			for i := range expected {
				assert.InDelta(t, expected[i], actual[i], 1e-9, "bin %d", i)
			}
		})
	}
}

func goDSPSpectrum(iqSamples []float64) []float64 {
	blockSize := len(iqSamples) / 2
	samples := make([]complex128, blockSize)
	for i := range samples {
		samples[i] = complex(iqSamples[2*i], iqSamples[2*i+1])
	}

	result := make([]float64, blockSize)
	for i, value := range fft.FFT(samples) {
		result[binToSpectrumIndex(i, blockSize)] = Magnitude[float64](value, blockSize)
	}
	return result
}

func randomSamples(blockSize int, seed uint64) []complex128 {
	random := rand.New(rand.NewPCG(seed, seed+0x9e3779b9))
	result := make([]complex128, blockSize)
	for i := range result {
		result[i] = complex(2*random.Float64()-1, 2*random.Float64()-1)
	}
	return result
}

func randomIQSamples(blockSize int, seed uint64) []float64 {
	random := rand.New(rand.NewPCG(seed, seed+0x9e3779b9))
	result := make([]float64, 2*blockSize)
	for i := range result {
		result[i] = 2*random.Float64() - 1
	}
	return result
}

func BenchmarkRadix2Transform(b *testing.B) {
	const blockSize = 4096
	source := randomSamples(blockSize, 4)
	samples := make([]complex128, blockSize)
	var r radix2
	r.transform(samples)

	b.ResetTimer()
	for range b.N {
		copy(samples, source)
		r.transform(samples)
	}
}

func BenchmarkGoDSPFFT(b *testing.B) {
	const blockSize = 4096
	source := randomSamples(blockSize, 4)
	samples := make([]complex128, blockSize)
	fft.EnsureRadix2Factors(blockSize)

	b.ResetTimer()
	for range b.N {
		copy(samples, source)
		fft.FFT(samples)
	}
}

func BenchmarkIQToSpectrum(b *testing.B) {
	const blockSize = 4096
	iqSamples := randomIQSamples(blockSize, 5)
	spectrum := make([]float64, blockSize)
	f := NewFFT[float64]()

	b.ResetTimer()
	for range b.N {
		f.IQToSpectrum(spectrum, iqSamples, PSD[float64])
	}
}

func TestFrequencyMapping(t *testing.T) {
	sampleRate := 48000
	blockSize := 512
	centerFrequency := 7020000
	tt := []struct {
		bin    int
		center int
	}{
		{0, centerFrequency - sampleRate/2},
		{256, centerFrequency},
	}
	for _, tc := range tt {
		t.Run(fmt.Sprintf("%d", tc.bin), func(t *testing.T) {
			m := NewFrequencyMapping[int](sampleRate, blockSize, centerFrequency)

			assert.Equal(t, tc.bin, m.FrequencyToBin(tc.center), "center to bin")
			assert.Equal(t, tc.center, m.BinToFrequency(tc.bin, BinCenter), "bin to center")
		})
	}
}

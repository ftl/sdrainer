package dsp

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBoolDebouncer(t *testing.T) {
	d := NewBoolDebouncer(3)

	assert.False(t, d.Debounce(true), "1")
	assert.False(t, d.Debounce(true), "2")
	assert.True(t, d.Debounce(true), "3")
	assert.True(t, d.Debounce(true), "4")
	assert.True(t, d.Debounce(false), "5")
	assert.True(t, d.Debounce(false), "6")
	assert.False(t, d.Debounce(false), "7")
}

func TestFilter_SignalState(t *testing.T) {
	sampleRate := 48000
	pitch := 700.0
	tt := []struct {
		desc      string
		filter    *Goertzel
		signalGen func([]float32)
		blocks    int
		expected  bool
	}{
		{
			desc:   "1 block sinewave on pitch",
			filter: NewDefaultGoertzel(pitch, sampleRate),
			signalGen: func(out []float32) {
				generateSinewave(out, 1, pitch, 0, sampleRate)
			},
			blocks:   1,
			expected: true,
		},
		{
			desc:   "10 blocks sinewave on pitch",
			filter: NewDefaultGoertzel(pitch, sampleRate),
			signalGen: func(out []float32) {
				generateSinewave(out, 1, pitch, 0, sampleRate)
			},
			blocks:   10,
			expected: true,
		},
		{
			desc:   "1 block sinewave half pitch",
			filter: NewDefaultGoertzel(pitch/2, sampleRate),
			signalGen: func(out []float32) {
				generateSinewave(out, 1, pitch, 0, sampleRate)
			},
			blocks:   1,
			expected: false,
		},
		{
			desc:   "10 blocks sinewave half pitch",
			filter: NewDefaultGoertzel(pitch/2, sampleRate),
			signalGen: func(out []float32) {
				generateSinewave(out, 1, pitch, 0, sampleRate)
			},
			blocks:   10,
			expected: false,
		},
		{
			desc:   "1 block silence",
			filter: NewDefaultGoertzel(pitch, sampleRate),
			signalGen: func(out []float32) {
				for i := range out {
					out[i] = 0
				}
			},
			blocks:   1,
			expected: false,
		},
		{
			desc:   "10 blocks silence",
			filter: NewDefaultGoertzel(pitch, sampleRate),
			signalGen: func(out []float32) {
				for i := range out {
					out[i] = 0
				}
			},
			blocks:   10,
			expected: false,
		},
		{
			desc:   "1 block dc",
			filter: NewDefaultGoertzel(pitch, sampleRate),
			signalGen: func(out []float32) {
				for i := range out {
					out[i] = 0.8
				}
			},
			blocks:   1,
			expected: false,
		},
		{
			desc:   "10 blocks dc",
			filter: NewDefaultGoertzel(pitch, sampleRate),
			signalGen: func(out []float32) {
				for i := range out {
					out[i] = 0.8
				}
			},
			blocks:   10,
			expected: false,
		},
		{
			desc:   "1 block noise",
			filter: NewDefaultGoertzel(pitch, sampleRate),
			signalGen: func(out []float32) {
				generateNoise(out, 0.1)
			},
			blocks:   1,
			expected: false,
		},
		{
			desc:   "10 blocks noise",
			filter: NewDefaultGoertzel(pitch, sampleRate),
			signalGen: func(out []float32) {
				generateNoise(out, 0.1)
			},
			blocks:   10,
			expected: false,
		},
	}
	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			signal := make([]float32, tc.blocks*tc.filter.blocksize)
			tc.signalGen(signal)
			var actual bool
			for i := 0; i < tc.blocks; i++ {
				start := i * tc.filter.blocksize
				end := start + tc.filter.blocksize
				block := FilterBlock(signal[start:end])
				_, state, _, _ := tc.filter.Detect(block)
				actual = state || actual
			}
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestFilter_Blocksize(t *testing.T) {
	sampleRate := 48000

	for i := 301; i < sampleRate/2; i++ {
		blocksize := calculateBlocksize(float64(i), sampleRate, DefaultBlocksizeRatio)
		ratio := float64(blocksize) / float64(sampleRate)
		delta := math.Abs(ratio - DefaultBlocksizeRatio)

		assert.Truef(t, delta <= 0.0017, "f=%d blocksize is %d, ratio is %f, delta is %f", i, blocksize, ratio, delta)
	}
}

func TestFilter_Bandwidth(t *testing.T) {
	sampleRate := 48000
	pitch := 700.0
	filter := NewDefaultGoertzel(pitch, sampleRate)

	lowestFrequency := 0
	highestFrequency := 0
	pitchDetected := false
	for i := 1; i < 3000; i++ {
		const blockCount = 10
		signal := make([]float32, blockCount*filter.blocksize)
		generateSinewave(signal[:], 1, float64(i), 0, sampleRate)
		var detected bool
		for j := 0; j < blockCount; j++ {
			start := j * filter.blocksize
			end := start + filter.blocksize
			block := FilterBlock(signal[start:end])
			_, state, _, _ := filter.Detect(block)
			detected = state || detected
		}
		if detected {
			if float64(i) == pitch {
				pitchDetected = true
			}
			if lowestFrequency == 0 {
				lowestFrequency = i
			}
			highestFrequency = i
		}
	}
	bandwidth := highestFrequency - lowestFrequency

	assert.True(t, pitchDetected, "pitch not detected")
	assert.Truef(t, bandwidth < 300, "bandwidth is %d", bandwidth)
}

func TestFilter_Sensitivity(t *testing.T) {
	sampleRate := 48000
	pitch := 700.0
	filter := NewDefaultGoertzel(pitch, sampleRate)

	var lowestAmplitude float64
	for i := 0; i <= 100; i++ {
		amplitude := float64(i) / 100
		const blockCount = 10
		signal := make([]float32, blockCount*filter.blocksize)
		generateSinewave(signal[:], amplitude, pitch, 0, sampleRate)

		var detected bool
		for j := 0; j < blockCount; j++ {
			start := j * filter.blocksize
			end := start + filter.blocksize
			block := FilterBlock(signal[start:end])
			_, state, _, _ := filter.Detect(block)
			detected = state || detected
		}

		if detected {
			lowestAmplitude = amplitude
			break
		}
	}

	assert.Truef(t, lowestAmplitude <= DefaultMagnitudeThreshold, "lowest amplitude is %f", lowestAmplitude)
}

func TestFilter_SNR(t *testing.T) {
	sampleRate := 48000
	pitch := 700.0
	filter := NewDefaultGoertzel(pitch, sampleRate)

	var highestAmplitude float64
	for i := 0; i <= 100; i++ {
		amplitude := float64(i) / 100
		const blockCount = 1
		signal := make([]float32, blockCount*filter.blocksize)
		generateSinewave(signal[:], 1, pitch, 0, sampleRate)
		mixWithNoise(signal[:], amplitude)

		var detected bool
		for j := 0; j < blockCount; j++ {
			start := j * filter.blocksize
			end := start + filter.blocksize
			block := FilterBlock(signal[start:end])
			_, state, _, _ := filter.Detect(block)
			detected = state || detected
		}

		if i == 0 {
			require.True(t, detected, "not detected without noise")
		}

		if detected {
			highestAmplitude = amplitude
		} else {
			break
		}
	}

	assert.Truef(t, highestAmplitude > 0.8, "highest noise amplitude is %f", highestAmplitude)
}

func TestFilter_NoiseTolerance(t *testing.T) {
	sampleRate := 48000
	pitch := 700.0
	filter := NewDefaultGoertzel(pitch, sampleRate)

	var highestAmplitude float64
	for i := 0; i <= 100; i++ {
		amplitude := float64(i) / 100
		const blockCount = 1
		signal := make([]float32, blockCount*filter.blocksize)
		generateNoise(signal[:], amplitude)

		var detected bool
		for j := 0; j < blockCount; j++ {
			start := j * filter.blocksize
			end := start + filter.blocksize
			block := FilterBlock(signal[start:end])
			_, state, _, _ := filter.Detect(block)
			detected = state || detected
		}

		if !detected {
			highestAmplitude = amplitude
		} else {
			break
		}
	}

	assert.Truef(t, highestAmplitude == 1, "highest noise amplitude is %f", highestAmplitude)
}

func generateSinewave(out []float32, amplitude, frequency, phase float64, sampleRate int) {
	var tick float64 = 1.0 / float64(sampleRate)
	var t float64
	for i := range out {
		out[i] = float32(amplitude * math.Cos(2*math.Pi*frequency*t+phase))
		t += tick
	}
}

func generateNoise(out []float32, amplitude float64) {
	for i := range out {
		noise := rand.Float32() * float32(amplitude)
		negative := (rand.Float32() > 0.5)
		if negative {
			noise *= -1
		}
		out[i] = noise
	}
}

func mixWithNoise(out []float32, amplitude float64) {
	for i := range out {
		noise := rand.Float32() * float32(amplitude)
		negative := (rand.Float32() > 0.5)
		if negative {
			noise *= -1
		}
		out[i] = float32(math.Max(math.Min(1.0, float64(out[i]+noise)), -1.0))
	}
}

func TestValueHistory_RingIndex(t *testing.T) {
	tt := []struct {
		length   int
		next     int
		index    int
		expected int
	}{
		{
			length:   10,
			next:     0,
			index:    1,
			expected: 9,
		},
		{
			length:   10,
			next:     1,
			index:    1,
			expected: 0,
		},
		{
			length:   10,
			next:     9,
			index:    10,
			expected: 9,
		},
		{
			length:   10,
			next:     5,
			index:    5,
			expected: 0,
		},
	}
	for i, tc := range tt {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			h := &RollingHistory[int]{
				length: tc.length,
				next:   tc.next,
			}

			actual := h.ringIndex(tc.index)

			assert.Equal(t, tc.expected, actual)
		})
	}
}

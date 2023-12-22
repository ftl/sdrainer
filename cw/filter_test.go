package cw

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilter_SignalState(t *testing.T) {
	sampleRate := 48000
	pitch := 700.0
	tt := []struct {
		desc      string
		filter    *filter
		signalGen func([]float32)
		blocks    int
		expected  bool
	}{
		{
			desc:   "1 block sinewave on pitch",
			filter: newFilter(pitch, sampleRate),
			signalGen: func(out []float32) {
				generateSinewave(out, 1, pitch, 0, sampleRate)
			},
			blocks:   1,
			expected: true,
		},
		{
			desc:   "10 blocks sinewave on pitch",
			filter: newFilter(pitch, sampleRate),
			signalGen: func(out []float32) {
				generateSinewave(out, 1, pitch, 0, sampleRate)
			},
			blocks:   10,
			expected: true,
		},
		{
			desc:   "1 block sinewave half pitch",
			filter: newFilter(pitch/2, sampleRate),
			signalGen: func(out []float32) {
				generateSinewave(out, 1, pitch, 0, sampleRate)
			},
			blocks:   1,
			expected: false,
		},
		{
			desc:   "10 blocks sinewave half pitch",
			filter: newFilter(pitch/2, sampleRate),
			signalGen: func(out []float32) {
				generateSinewave(out, 1, pitch, 0, sampleRate)
			},
			blocks:   10,
			expected: false,
		},
		{
			desc:   "1 block silence",
			filter: newFilter(pitch, sampleRate),
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
			filter: newFilter(pitch, sampleRate),
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
			filter: newFilter(pitch, sampleRate),
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
			filter: newFilter(pitch, sampleRate),
			signalGen: func(out []float32) {
				for i := range out {
					out[i] = 0.8
				}
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
				block := filterBlock(signal[start:end])
				actual = tc.filter.signalState(block) || actual
			}
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestFilter_Blocksize(t *testing.T) {
	sampleRate := 48000
	for i := 1; i < sampleRate/2; i++ {
		filter := newFilter(float64(i), sampleRate)

		assert.Truef(t, math.Abs(float64(filter.blocksize)/float64(sampleRate)-blocksizeRatio) < 0.1, "f=%d blocksize is %d, ratio is %f", i, filter.blocksize, float64(filter.blocksize)/float64(sampleRate))
	}
}

func TestFilter_Bandwidth(t *testing.T) {
	sampleRate := 48000
	pitch := 700.0
	filter := newFilter(pitch, sampleRate)

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
			block := filterBlock(signal[start:end])
			detected = filter.signalState(block) || detected
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
	assert.Truef(t, bandwidth < 110, "bandwidth is %d", bandwidth)
}

func TestFilter_Sensitivity(t *testing.T) {
	sampleRate := 48000
	pitch := 700.0
	filter := newFilter(pitch, sampleRate)

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
			block := filterBlock(signal[start:end])
			detected = filter.signalState(block) || detected
		}

		if detected {
			lowestAmplitude = amplitude
			break
		}
	}

	assert.Truef(t, lowestAmplitude < 0.65, "lowest amplitude is %f", lowestAmplitude)
}

func generateSinewave(out []float32, amplitude, frequency, phase float64, sampleRate int) {
	var tick float64 = 1.0 / float64(sampleRate)
	var t float64
	for i := range out {
		out[i] = float32(amplitude * math.Cos(2*math.Pi*frequency*t+phase))
		t += tick
	}
}

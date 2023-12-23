package cw

import (
	"bufio"
	"bytes"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ftl/digimodes/cw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		{
			desc:   "1 block noise",
			filter: newFilter(pitch, sampleRate),
			signalGen: func(out []float32) {
				generateNoise(out, 0.1)
			},
			blocks:   1,
			expected: false,
		},
		{
			desc:   "10 blocks noise",
			filter: newFilter(pitch, sampleRate),
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
				block := filterBlock(signal[start:end])
				actual = tc.filter.signalState(block) || actual
			}
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestFilter_Blocksize(t *testing.T) {
	sampleRate := 48000

	for i := 301; i < sampleRate/2; i++ {
		blocksize := calculateBlocksize(float64(i), sampleRate)
		ratio := float64(blocksize) / float64(sampleRate)
		delta := math.Abs(ratio - blocksizeRatio)

		assert.Truef(t, delta <= 0.0017, "f=%d blocksize is %d, ratio is %f, delta is %f", i, blocksize, ratio, delta)
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
	assert.Truef(t, bandwidth < 300, "bandwidth is %d", bandwidth)
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

func TestFilter_SNR(t *testing.T) {
	sampleRate := 48000
	pitch := 700.0
	filter := newFilter(pitch, sampleRate)

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
			block := filterBlock(signal[start:end])
			detected = filter.signalState(block) || detected
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

	assert.Truef(t, highestAmplitude == 1, "highest noise amplitude is %f", highestAmplitude)
}

func TestFilter_NoiseTolerance(t *testing.T) {
	sampleRate := 48000
	pitch := 700.0
	filter := newFilter(pitch, sampleRate)

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
			block := filterBlock(signal[start:end])
			detected = filter.signalState(block) || detected
		}

		if !detected {
			highestAmplitude = amplitude
		} else {
			break
		}
	}

	assert.Truef(t, highestAmplitude == 1, "highest noise amplitude is %f", highestAmplitude)
}

func TestToCWChar(t *testing.T) {
	a := cwChar{cw.Dit, cw.Da}
	assert.Equal(t, a, toCWChar(cw.Dit, cw.Da))
}

func TestDecodeTable(t *testing.T) {
	table := generateDecodeTable()

	assert.Equal(t, 'a', table[toCWChar(cw.Dit, cw.Da)])
	assert.Equal(t, '§', table[toCWChar(cw.Dit, cw.Dit, cw.Dit, cw.Dit, cw.Dit, cw.Dit, cw.Dit, cw.Dit)])
}

func TestDemodulator_RecordedStreams(t *testing.T) {
	blockTick := 5 * time.Millisecond
	clock := new(manualClock)
	buffer := bytes.NewBuffer([]byte{})
	demodulator := newDemodulator(buffer, clock)

	stream, err := readLines("pse.txt")
	require.NoError(t, err)
	for _, state := range stream {
		clock.Add(blockTick)
		demodulator.tick(state == "1")
	}
	demodulator.stop()

	assert.Equal(t, "pse", buffer.String())
}

func readLines(filename string) ([]string, error) {
	file, err := os.Open(filepath.Join("testdata", filename))
	if err != nil {
		return nil, err
	}
	defer file.Close()

	result := make([]string, 0, 10000)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		result = append(result, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return result, nil
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

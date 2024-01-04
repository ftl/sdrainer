package dsp

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBinToSpectrumIndex(t *testing.T) {
	tt := []struct {
		blockSize int
		bin       int
		expected  int
	}{
		{blockSize: 512, bin: 0, expected: 256},
		{blockSize: 512, bin: 255, expected: 511},
		{blockSize: 512, bin: 256, expected: 0},
		{blockSize: 512, bin: 511, expected: 255},
	}
	for _, tc := range tt {
		t.Run(fmt.Sprintf("%d_%d", tc.blockSize, tc.bin), func(t *testing.T) {
			actual := binToSpectrumIndex(tc.bin, tc.blockSize)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestFrequencyMapping(t *testing.T) {
	sampleRate := 48000
	blockSize := 512
	centerFrequency := 7020000
	binSize := sampleRate / blockSize
	tt := []struct {
		bin  int
		from int
		to   int
	}{
		{0, centerFrequency - sampleRate/2, centerFrequency - sampleRate/2 + binSize},
		{256, centerFrequency, centerFrequency + binSize},
		{511, centerFrequency + sampleRate/2 - binSize, centerFrequency + sampleRate/2},
	}
	for _, tc := range tt {
		t.Run(fmt.Sprintf("%d", tc.bin), func(t *testing.T) {
			m := NewFrequencyMapping[int](sampleRate, blockSize, centerFrequency)

			assert.Equal(t, tc.bin, m.FrequencyToBin(tc.from), "from to bin")
			assert.Equal(t, tc.bin, m.FrequencyToBin(tc.to), "to to bin")
			assert.Equal(t, tc.from-(tc.bin%2), m.BinToFrequency(tc.bin, BinFrom), "bin to from")
			assert.Equal(t, tc.to, m.BinToFrequency(tc.bin, BinTo), "bin to to")
		})
	}
}

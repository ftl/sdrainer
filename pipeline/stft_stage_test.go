package pipeline

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSTFTStageScalesTheSpectrumToTheSignalPower(t *testing.T) {
	const (
		sampleRate = 48000
		size       = 1024
		frequency  = 3000.0 // exactly on bin 64, so there is no leakage
		amplitude  = 0.5
	)

	pool := NewFramePool[float32, float64](size)
	stage := NewSTFTStage[float32, float64](pool, sampleRate, size)

	out := make(chan *SpectralFrame[float32, float64], 4)
	stage.Process(out, tone(size, sampleRate, frequency, amplitude))
	close(out)

	frames := collectFrames(out)
	require.Len(t, frames, 1)

	binWidth := float64(sampleRate) / float64(size)
	signalBin := size/2 + int(frequency/binWidth)

	assert.InDelta(t, amplitude*amplitude, frames[0].Spectrum[signalBin], 1e-5, "the peak bin holds the power of the tone")
	assert.InDelta(t, 0, frames[0].Spectrum[signalBin+10], 1e-9, "a bin far from the tone is empty")
}

func TestSTFTStageEmitsOneFrameForEachHop(t *testing.T) {
	const (
		sampleRate = 48000
		size       = 1024
		hop        = 512
	)

	pool := NewFramePool[float32, float64](size)
	stage := NewSTFTStage[float32, float64](pool, sampleRate, hop)

	out := make(chan *SpectralFrame[float32, float64], 16)
	stage.Process(out, tone(2*size, sampleRate, 3000, 0.5))
	stage.Process(out, tone(hop, sampleRate, 3000, 0.5))
	close(out)

	frames := collectFrames(out)

	require.Len(t, frames, 4)
	for i, frame := range frames {
		assert.Equal(t, uint64(i), frame.Sequence)
		assert.Equal(t, int64(i*hop), frame.Offset)
		assert.Equal(t, sampleRate, frame.SampleRate)
	}
}

func tone(samples int, sampleRate int, frequency float64, amplitude float64) []float32 {
	result := make([]float32, 2*samples)
	for i := range samples {
		phase := 2 * math.Pi * frequency * float64(i) / float64(sampleRate)
		result[2*i] = float32(amplitude * math.Cos(phase))
		result[2*i+1] = float32(amplitude * math.Sin(phase))
	}
	return result
}

func collectFrames(out <-chan *SpectralFrame[float32, float64]) []*SpectralFrame[float32, float64] {
	result := make([]*SpectralFrame[float32, float64], 0, 16)
	for frame := range out {
		result = append(result, frame)
	}
	return result
}

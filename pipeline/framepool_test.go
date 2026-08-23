package pipeline

import (
	"testing"

	"github.com/ftl/sdrainer/dsp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFramePoolGivesAnEmptyFrame(t *testing.T) {
	const blockSize = 64

	frame := NewFramePool[float32, float64](blockSize).NewFrame()

	assert.Len(t, frame.Spectrum, blockSize, "spectrum")
	assert.Len(t, frame.NoiseFloor, blockSize, "noise floor")
	assert.Empty(t, frame.Peaks, "peaks")
}

func TestFramePoolResetsAReturnedFrame(t *testing.T) {
	const blockSize = 64
	pool := NewFramePool[float32, float64](blockSize)

	used := pool.NewFrame()
	used.Sequence = 42
	used.Offset = 4711
	used.SampleRate = 48000
	used.Spectrum[0] = 1
	used.NoiseFloor[0] = 2
	used.Peaks = append(used.Peaks, dsp.Peak[float32, float64]{From: 1, To: 3})
	require.NotEmpty(t, used.Peaks, "the test needs a frame that is in use")

	pool.ReturnFrame(used)
	frame := pool.NewFrame()

	assert.Zero(t, frame.Sequence, "sequence")
	assert.Zero(t, frame.Offset, "offset")
	assert.Zero(t, frame.SampleRate, "sample rate")
	assert.Empty(t, frame.Peaks, "peaks")
	assert.Len(t, frame.Spectrum, blockSize, "spectrum")
	assert.Len(t, frame.NoiseFloor, blockSize, "noise floor")
}

// TestFramePoolResetKeepsTheMemory gives the proof that reset makes the peaks shorter and does not
// throw the memory away. A frame goes through the pool many times each second, so a new block for
// each frame would make much work for the garbage collector.
func TestFramePoolResetKeepsTheMemory(t *testing.T) {
	const blockSize = 64
	frame := NewFramePool[float32, float64](blockSize).NewFrame()

	for range 4 {
		frame.Peaks = append(frame.Peaks, dsp.Peak[float32, float64]{From: 1, To: 3})
	}
	peakCapacity := cap(frame.Peaks)
	spectrum := &frame.Spectrum[0]
	noiseFloor := &frame.NoiseFloor[0]
	require.NotZero(t, peakCapacity, "the test needs a frame that is in use")

	frame.reset()

	assert.Empty(t, frame.Peaks, "the peaks must be gone")
	assert.Equal(t, peakCapacity, cap(frame.Peaks), "but their memory must stay")
	assert.Same(t, spectrum, &frame.Spectrum[0], "the spectrum must keep its block")
	assert.Same(t, noiseFloor, &frame.NoiseFloor[0], "the noise floor must keep its block")
}

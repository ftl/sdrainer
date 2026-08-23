package pipeline

import (
	"sync"

	"github.com/ftl/sdrainer/dsp"
)

type SpectralFrame[S, F dsp.Number] struct {
	Sequence   uint64
	Offset     int64
	SampleRate int
	Spectrum   dsp.Block[S]

	NoiseFloor dsp.Block[S]
	Peaks      []dsp.Peak[S, F]
}

// reset does not clear Spectrum and NoiseFloor, because each stage writes all values of its own
// block again. Only Peaks has a variable length.
func (f *SpectralFrame[S, F]) reset() {
	f.Sequence = 0
	f.Offset = 0
	f.SampleRate = 0
	f.Peaks = f.Peaks[:0]
}

type FramePool[S, F dsp.Number] struct {
	availableFrames sync.Pool

	blockSize int
}

func NewFramePool[S, F dsp.Number](blockSize int) *FramePool[S, F] {
	result := &FramePool[S, F]{
		blockSize: blockSize,
	}
	result.availableFrames.New = result.newFrame

	return result
}

func (p *FramePool[S, F]) BlockSize() int {
	return p.blockSize
}

func (p *FramePool[S, F]) newFrame() any {
	return &SpectralFrame[S, F]{
		Spectrum:   make(dsp.Block[S], p.blockSize),
		NoiseFloor: make(dsp.Block[S], p.blockSize),
	}
}

func (p *FramePool[S, F]) NewFrame() *SpectralFrame[S, F] {
	return p.availableFrames.Get().(*SpectralFrame[S, F])
}

func (p *FramePool[S, F]) ReturnFrame(frame *SpectralFrame[S, F]) {
	frame.reset()
	p.availableFrames.Put(frame)
}

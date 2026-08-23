package pipeline

import (
	"math"

	"github.com/ftl/sdrainer/dsp"
)

type STFTStage[S, F dsp.Number] struct {
	framePool *FramePool[S, F]

	sampleRate int
	size       int
	hop        int

	window  []float64
	fft     *dsp.FFT[S]
	buffer  []S
	scratch []S

	offset   int64
	sequence uint64
}

func NewSTFTStage[S, F dsp.Number](framePool *FramePool[S, F], sampleRate int, hop int) *STFTStage[S, F] {
	size := framePool.BlockSize()
	if hop <= 0 || hop > size {
		hop = size
	}

	return &STFTStage[S, F]{
		framePool:  framePool,
		sampleRate: sampleRate,
		size:       size,
		hop:        hop,
		window:     hannWindow(size),
		fft:        dsp.NewFFT[S](),
		buffer:     make([]S, 0, 4*size),
		scratch:    make([]S, 2*size),
	}
}

func hannWindow(size int) []float64 {
	result := make([]float64, size)
	var sum float64
	for i := range result {
		result[i] = 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(size)))
		sum += result[i]
	}
	if sum == 0 {
		return result
	}
	// the coherent gain of 1 makes the spectrum independent of the FFT size and of the window shape
	for i := range result {
		result[i] /= sum
	}
	return result
}

// Process appends the given chunk of interleaved IQ samples to the internal sliding buffer and
// emits one SpectralFrame for each complete hop into out. One call thus produces zero, one, or
// many frames, because the chunk size of the source is independent of the FFT size and of the hop.
// The call blocks while out is full, which lets the backpressure reach the source.
//
// Domain: the values in SpectralFrame.Spectrum are magnitude-squared, thus linear power. They are
// not magnitudes, and they are not dB. The window is normalized to a coherent gain of 1, so a
// complex tone with the amplitude A gives the value A² in its peak bin, independent of the FFT
// size and of the window shape. Two STFT stages with different FFT sizes are thus comparable.
// The scale is exact for a tone. A noise floor also depends on the noise bandwidth of the window,
// which is 1.5 for a Hann window. Apply that factor in the stage that gives the noise a physical
// meaning.
//
// Consumers must respect three properties of the linear power domain:
//   - A mean, a sum, or an exponential filter is correct in this domain, and only in this domain.
//     A mean of magnitudes and a mean of dB values are both too small.
//   - A threshold in dB is a ratio. 6 dB above a floor is 3.98 times the floor, and 10 dB is 10
//     times the floor. No logarithm is necessary for the detection.
//   - A parabolic interpolation of a peak position is more exact on the logarithm of the values.
//     Convert only the three bins around a candidate, not the full frame.
//
// Bin layout: index 0 holds the lowest frequency and the index increases with the frequency. The
// bin width is SampleRate/len(Spectrum). The stage does not know the center frequency of the
// stream, so the consumer makes the frequency mapping, for example with dsp.NewFrequencyMapping.
//
// Frame metadata: Sequence counts the frames from the start of the stream. Offset is the index of
// the first sample of the window in the stream, so consecutive frames are hop samples apart. Both
// values are exact and repeatable, which makes a test independent of a clock.
//
// Ownership: the consumer owns the frame and must give it back with FramePool.ReturnFrame. The
// content of a frame is undefined after that call. Process is not safe for concurrent use.
func (s *STFTStage[S, F]) Process(out chan<- *SpectralFrame[S, F], samples []S) {
	if s.size <= 0 {
		return
	}

	s.buffer = append(s.buffer, samples...)

	windowLength := 2 * s.size
	hopLength := 2 * s.hop

	for len(s.buffer) >= windowLength {
		s.applyWindow(s.buffer[:windowLength])

		frame := s.framePool.NewFrame()
		frame.Sequence = s.sequence
		frame.Offset = s.offset
		frame.SampleRate = s.sampleRate
		s.fft.IQToSpectrum(frame.Spectrum, s.scratch, dsp.PSD[S])

		out <- frame

		s.sequence++
		s.offset += int64(s.hop)
		s.buffer = append(s.buffer[:0], s.buffer[hopLength:]...)
	}
}

func (s *STFTStage[S, F]) applyWindow(segment []S) {
	for i := 0; i < s.size; i++ {
		coefficient := s.window[i]
		s.scratch[2*i] = S(float64(segment[2*i]) * coefficient)
		s.scratch[2*i+1] = S(float64(segment[2*i+1]) * coefficient)
	}
}

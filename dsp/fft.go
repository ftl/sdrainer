package dsp

import (
	"fmt"
	"math"

	"github.com/mjibson/go-dsp/fft"
	"golang.org/x/exp/constraints"
)

type Number interface {
	constraints.Integer | constraints.Float
}

type FFT[T Number] struct {
	samples []complex128
	radix2  radix2
}

func NewFFT[T Number]() *FFT[T] {
	return &FFT[T]{}
}

func (f *FFT[T]) IQToSpectrumAndPSD(spectrum []T, psd []T, iqSamples []T, projection func(complex128, int) T) {
	f.setSamplesFromIQ(iqSamples)

	fftResult := f.transform()
	blockSize := len(fftResult)
	if len(spectrum) != blockSize {
		panic(fmt.Sprintf("the spectrum slice must have the same length as the FFT's result: %d", blockSize))
	}

	for i, value := range fftResult {
		k := binToSpectrumIndex(i, blockSize)
		spectrum[k] = projection(value, blockSize)
		psd[k] = PSD[T](value, blockSize)
	}
}

func (f *FFT[T]) IQToSpectrum(spectrum []T, iqSamples []T, projection func(complex128, int) T) {
	f.setSamplesFromIQ(iqSamples)

	fftResult := f.transform()
	blockSize := len(fftResult)
	if len(spectrum) != blockSize {
		panic(fmt.Sprintf("the spectrum slice must have the same length as the FFT's result: %d", blockSize))
	}

	for i, value := range fftResult {
		k := binToSpectrumIndex(i, blockSize)
		spectrum[k] = projection(value, blockSize)
	}
}

func binToSpectrumIndex(bin int, blockSize int) int {
	centerBin := blockSize / 2
	return (bin + centerBin) % blockSize
}

func (f *FFT[T]) setSamplesFromIQ(iqSamples []T) {
	blockSize := len(iqSamples) / 2
	if len(f.samples) != blockSize {
		f.samples = make([]complex128, blockSize)
	}
	for i := range f.samples {
		iSample := float64(iqSamples[i*2])
		qSample := float64(iqSamples[i*2+1])
		f.samples[i] = complex(iSample, qSample)
	}
}

// transform replaces the sample buffer with its forward FFT and returns it. The radix-2
// implementation works in place and allocates nothing. It needs a block size that is a power of
// two. For all other block sizes the go-dsp library does the work and returns a new slice.
func (f *FFT[T]) transform() []complex128 {
	if !isPowerOfTwo(len(f.samples)) {
		return fft.FFT(f.samples)
	}

	f.radix2.transform(f.samples)
	return f.samples
}

func isPowerOfTwo(n int) bool {
	return n > 0 && n&(n-1) == 0
}

// radix2 is an in-place radix-2 decimation-in-time FFT. It keeps the twiddle factors for the last
// block size, so a repeated transform with the same block size allocates nothing. The result uses
// the same sign convention as github.com/mjibson/go-dsp/fft: exp(-2πi·k·n/N).
type radix2 struct {
	twiddles []complex128
}

func (r *radix2) transform(samples []complex128) {
	n := len(samples)
	r.prepareTwiddles(n)
	reverseBitOrder(samples)

	for stage := 2; stage <= n; stage <<= 1 {
		step := n / stage
		half := stage / 2
		for start := 0; start < n; start += stage {
			for j := 0; j < half; j++ {
				even := start + j
				odd := even + half
				a := samples[even]
				b := samples[odd] * r.twiddles[step*j]
				samples[even] = a + b
				samples[odd] = a - b
			}
		}
	}
}

func (r *radix2) prepareTwiddles(n int) {
	if len(r.twiddles) == n/2 {
		return
	}

	r.twiddles = make([]complex128, n/2)
	for i := range r.twiddles {
		sin, cos := math.Sincos(-2 * math.Pi * float64(i) / float64(n))
		r.twiddles[i] = complex(cos, sin)
	}
}

func reverseBitOrder(samples []complex128) {
	n := len(samples)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j |= bit

		if i < j {
			samples[i], samples[j] = samples[j], samples[i]
		}
	}
}

func PSD[T Number](fftValue complex128, blockSize int) T {
	re := real(fftValue)
	im := imag(fftValue)
	return T(re*re + im*im)
}

func Magnitude[T Number](fftValue complex128, blockSize int) T {
	return T(math.Sqrt(float64(PSD[T](fftValue, blockSize))))
}

func MagnitudeIndB[T Number](fftValue complex128, blockSize int) T {
	return PSDValueIndB(PSD[T](fftValue, blockSize), blockSize)
}

func PSDValueIndB[T Number](psdValue T, blockSize int) T {
	if psdValue <= 0 || blockSize <= 0 {
		return T(minimumDB)
	}
	size := float64(blockSize)
	return T(10.0 * math.Log10(float64(psdValue)/(size*size)))
}

// minimumDB is the result for a power of zero. It is a variable, because a negative constant is
// not representable in an unsigned integer type.
var minimumDB = -200.0

// RatioIndB returns the ratio of two power values in dB. Both values must be linear power, not
// magnitude and not dB. Use this for an SNR: no calibration constant is necessary, because the
// reference is explicit.
func RatioIndB[T Number](power T, reference T) T {
	if power <= 0 || reference <= 0 {
		return T(minimumDB)
	}
	return T(10.0 * math.Log10(float64(power)/float64(reference)))
}

// PowerIndB returns a linear power value in dB, relative to the power 1.0. With a window that has
// a coherent gain of 1, a tone with the amplitude 1.0 thus gives 0 dB.
func PowerIndB[T Number](power T) T {
	return RatioIndB(power, T(1))
}

type BinLocation float64

const (
	BinFrom   BinLocation = -0.5
	BinCenter BinLocation = 0
	BinTo     BinLocation = 0.5
)

type FrequencyMapping[F Number] struct {
	sampleRate int
	blockSize  int
	binSize    float64

	centerFrequency float64
	fromFrequency   float64
}

func NewFrequencyMapping[F Number](sampleRate int, blockSize int, centerFrequency F) *FrequencyMapping[F] {
	result := &FrequencyMapping[F]{
		sampleRate: sampleRate,
		blockSize:  blockSize,
		binSize:    float64(sampleRate) / float64(blockSize),
	}
	result.SetCenterFrequency(centerFrequency)

	return result
}

func (m *FrequencyMapping[F]) String() string {
	return fmt.Sprintf("[%.0f - %.0f - %v]", m.fromFrequency, m.centerFrequency, m.BinToFrequency(m.blockSize-1, BinTo))
}

func (m *FrequencyMapping[F]) SetCenterFrequency(frequency F) {
	m.centerFrequency = float64(frequency)
	m.fromFrequency = m.centerFrequency - float64(m.sampleRate)/2
}

func (m *FrequencyMapping[F]) BinToFrequency(bin int, location BinLocation) F {
	return toFrequency[F](m.fromFrequency + (float64(bin)+float64(location))*m.binSize)
}

func (m *FrequencyMapping[F]) FrequencyToBin(frequency F) int {
	bin := int(math.Round((float64(frequency) - m.fromFrequency) / m.binSize))
	return max(0, min(bin, m.blockSize-1))
}

func toFrequency[F Number](value float64) F {
	// 0.5 must be a variable here, because it is not representable in an integer type
	half := 0.5
	if F(half) == 0 {
		return F(math.Round(value))
	}
	return F(value)
}

// Block represents a block of samples that are processed as one unit.
type Block[T Number] []T

// Size returns the blocksize.
func (b Block[T]) Size() int {
	return len(b)
}

// Subblock returns the given section of this block.
func (b Block[T]) Subblock(from, to int) Block[T] {
	return b[from : to+1]
}

// Sum of the values in the given section of this block.
func (b Block[T]) Sum(from, to int) T {
	var sum T
	for i := from; i <= to; i++ {
		sum += b[i]
	}
	return sum
}

// Mean of the values in the given section of this block.
func (b Block[T]) Mean(from, to int) T {
	return b.Sum(from, to) / T(to-from+1)
}

// Max imum value in the given section of this block.
func (b Block[T]) Max(from, to int) (T, int) {
	maxValue := b[from]
	maxI := from
	for i := from; i <= to; i++ {
		if maxValue < b[i] {
			maxValue = b[i]
			maxI = i
		}
	}
	return maxValue, maxI
}

// Peak represents a section in a block that contains a peak.
// M is used to represent magnitude values in the spectrum, F is the type used to represent frequencies
type Peak[M, F Number] struct {
	From          int
	To            int
	FromFrequency F
	ToFrequency   F

	SignalFrequency F
	SignalValue     M
	SignalBin       int
}

// Center index
func (p Peak[T, F]) Center() int {
	return p.From + ((p.To - p.From) / 2)
}

// CenterFrequency, based on the FromFrequency and ToFrequency fields. Those fields must be filled with meaningful values.
func (p Peak[T, F]) CenterFrequency() F {
	return p.FromFrequency + (p.WidthHz() / 2)
}

// Width in bins.
func (p Peak[T, F]) Width() int {
	return (p.To - p.From) + 1
}

// WidthHz in Hz, based on the FromFrequency and ToFrequency fields. Those fiels must be filled with meaningful values.
func (p Peak[T, F]) WidthHz() F {
	return p.ToFrequency - p.FromFrequency
}

// ContainsBin indicates if the given bin is within this peak.
func (p Peak[T, F]) ContainsBin(bin int) bool {
	return p.From <= bin && bin <= p.To
}

func FindNoiseFloor[T Number](psd Block[T], edgeWidth int) T {
	if edgeWidth < 0 {
		return 0
	}
	windowSize := (len(psd) - 2*edgeWidth) / 10
	if windowSize < 1 {
		return 0
	}

	to := len(psd) - edgeWidth
	minMean := math.Inf(1)
	for from := edgeWidth; from+windowSize <= to; from += windowSize {
		var sum float64
		for i := from; i < from+windowSize; i++ {
			sum += float64(psd[i])
		}

		mean := sum / float64(windowSize)
		if mean < minMean {
			minMean = mean
		}
	}

	return T(minMean)
}

func FindPeaks[T, F Number](peaks []Peak[T, F], spectrum Block[T], cumulationSize int, threshold T, frequencyMapping *FrequencyMapping[F]) []Peak[T, F] {
	peaks = peaks[:0]

	var currentPeak *Peak[T, F]
	for i, v := range spectrum {
		value := v / T(cumulationSize)
		if currentPeak == nil && value > threshold {
			currentPeak = &Peak[T, F]{From: i, SignalValue: value, SignalBin: i}
		} else if currentPeak != nil && value <= threshold {
			currentPeak.To = i - 1
			currentPeak.FromFrequency = frequencyMapping.BinToFrequency(currentPeak.From, BinFrom)
			currentPeak.ToFrequency = frequencyMapping.BinToFrequency(currentPeak.To, BinTo)
			centerCorrection := PeakCenterCorrection[T, F](currentPeak.SignalBin, spectrum)
			currentPeak.SignalFrequency = frequencyMapping.BinToFrequency(currentPeak.SignalBin, centerCorrection)
			peaks = append(peaks, *currentPeak)
			currentPeak = nil
		} else if currentPeak != nil && currentPeak.SignalValue < value {
			currentPeak.SignalValue = value
			currentPeak.SignalBin = i
		}
	}

	if currentPeak != nil {
		currentPeak.To = len(spectrum) - 1
		currentPeak.FromFrequency = frequencyMapping.BinToFrequency(currentPeak.From, BinFrom)
		currentPeak.ToFrequency = frequencyMapping.BinToFrequency(currentPeak.To, BinTo)
		currentPeak.SignalFrequency = SignalFrequency(currentPeak.SignalBin, spectrum, frequencyMapping)
		peaks = append(peaks, *currentPeak)
	}

	return peaks
}

func SignalFrequency[T, F Number](bin int, spectrum Block[T], frequencyMapping *FrequencyMapping[F]) F {
	centerCorrection := PeakCenterCorrection[T, F](bin, spectrum)
	return frequencyMapping.BinToFrequency(bin, centerCorrection)
}

func PeakCenterCorrection[T, F Number](bin int, spectrum Block[T]) BinLocation {
	// see https://dspguru.com/dsp/howtos/how-to-interpolate-fft-peak/
	if bin <= 0 || bin >= spectrum.Size()-1 {
		return 0
	}

	value := func(i int) float64 {
		return math.Abs(float64(spectrum[i]))
	}

	// quadratic interpolation
	y1 := value(bin - 1)
	y2 := value(bin)
	y3 := value(bin + 1)
	denominator := 2 * (2*y2 - y1 - y3)
	if denominator == 0 {
		return 0
	}
	result := (y3 - y1) / denominator

	return BinLocation(max(float64(BinFrom), min(float64(BinTo), result)))
}

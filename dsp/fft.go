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
}

func NewFFT[T Number]() *FFT[T] {
	return &FFT[T]{}
}

func (f *FFT[T]) IQToSpectrumAndPSD(spectrum []T, psd []T, iqSamples []T, projection func(complex128, int) T) {
	f.setSamplesFromIQ(iqSamples)

	fftResult := fft.FFT(f.samples)
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

	fftResult := fft.FFT(f.samples)
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

func PSD[T Number](fftValue complex128, blockSize int) T {
	return T(math.Pow(real(fftValue), 2) + math.Pow(imag(fftValue), 2))
}

func Magnitude[T Number](fftValue complex128, blockSize int) T {
	return T(math.Sqrt(float64(PSD[T](fftValue, blockSize))))
}

func MagnitudeIndB[T Number](fftValue complex128, blockSize int) T {
	return T(10.0 * math.Log10(20.0*float64(PSD[T](fftValue, blockSize))/math.Pow(float64(blockSize), 2)))
}

func PSDValueIndB[T Number](psdValue T, blockSize int) T {
	return T(10.0 * math.Log10(20.0*float64(psdValue)/math.Pow(float64(blockSize), 2)))
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
	centerBin  int

	centerFrequency int
	fromFrequency   int
}

func NewFrequencyMapping[F Number](sampleRate int, blockSize int, centerFrequency F) *FrequencyMapping[F] {
	result := &FrequencyMapping[F]{
		sampleRate: sampleRate,
		blockSize:  blockSize,
		binSize:    float64(sampleRate) / float64(blockSize),
		centerBin:  blockSize / 2,
	}
	result.SetCenterFrequency(centerFrequency)

	return result
}

func (m *FrequencyMapping[F]) String() string {
	return fmt.Sprintf("[%v - %v - %v]", m.fromFrequency, m.centerFrequency, m.BinToFrequency(m.blockSize-1, BinTo))
}

func (m *FrequencyMapping[F]) SetCenterFrequency(frequency F) {
	m.centerFrequency = int(frequency)
	m.fromFrequency = m.centerFrequency - m.sampleRate/2
}

func (m *FrequencyMapping[F]) BinToFrequency(bin int, location BinLocation) F {
	locationDelta := float64(m.binSize) * float64(location)

	return F(m.fromFrequency + int(float64(bin)*m.binSize+locationDelta))
}

func (m *FrequencyMapping[F]) FrequencyToBin(frequency F) int {
	bin := int((float64(frequency) - float64(m.fromFrequency)) / m.binSize)
	return max(0, min(bin, m.blockSize-1))
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
	maxValue := b[0]
	maxI := 0
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
	return p.From >= bin && p.To <= bin
}

func FindNoiseFloor[T Number](psd Block[T], edgeWidth int) T {
	windowSize := len(psd) / 10
	minValue := psd[0]
	var sum T
	count := 0
	first := true
	for i := edgeWidth; i < len(psd)-edgeWidth; i++ {
		if count == windowSize {
			count = 0
			mean := sum / T(windowSize)
			if mean < minValue || first {
				minValue = mean
				first = false
			}
			sum = 0
		}
		sum += psd[i]
		count++
	}

	return minValue
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
		centerCorrection := PeakCenterCorrection[T, F](currentPeak.SignalBin, spectrum)
		currentPeak.SignalFrequency = frequencyMapping.BinToFrequency(currentPeak.SignalBin, centerCorrection)
		peaks = append(peaks, *currentPeak)
	}

	return peaks
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
	result := (y3 - y1) / (2 * (2*y2 - y1 - y3))

	return BinLocation(result)
}

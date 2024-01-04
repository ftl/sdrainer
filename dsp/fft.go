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

	for i := range fftResult {
		k := binToSpectrumIndex(i, blockSize)
		spectrum[k] = projection(fftResult[i], blockSize)
		psd[k] = T(math.Pow(real(fftResult[i]), 2) + math.Pow(imag(fftResult[i]), 2))
	}
}

func (f *FFT[T]) IQToSpectrum(spectrum []T, iqSamples []T, projection func(complex128, int) T) {
	f.setSamplesFromIQ(iqSamples)

	fftResult := fft.FFT(f.samples)
	blockSize := len(fftResult)
	if len(spectrum) != blockSize {
		panic(fmt.Sprintf("the spectrum slice must have the same length as the FFT's result: %d", blockSize))
	}

	for i := range fftResult {
		k := binToSpectrumIndex(i, blockSize)
		spectrum[k] = projection(fftResult[i], blockSize)
	}
}

func binToSpectrumIndex(bin int, blockSize int) int {
	centerBin := blockSize / 2

	if bin >= centerBin {
		return (bin - centerBin)
	} else {
		return (bin + centerBin)
	}
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

func Magnitude[T Number](fftValue complex128, blockSize int) T {
	return T(math.Sqrt(math.Pow(real(fftValue), 2) + math.Pow(imag(fftValue), 2)))
}

func Magnitude2dBm[T Number](fftValue complex128, blockSize int) T {
	return T(10.0 * math.Log10(20.0*(math.Pow(real(fftValue), 2)+math.Pow(imag(fftValue), 2))/math.Pow(float64(blockSize), 2)))
}

func PSDValue2dBm[T Number](psdValue T, blockSize int) T {
	return T(10.0 * math.Log10(20.0*float64(psdValue)/math.Pow(float64(blockSize), 2)))
}

type BinLocation int

const (
	BinFrom   BinLocation = 0
	BinCenter BinLocation = 2
	BinTo     BinLocation = 1
)

type FrequencyMapping[F Number] struct {
	sampleRate int
	blockSize  int
	binSize    int
	centerBin  int

	centerFrequency int
	fromFrequency   int
}

func NewFrequencyMapping[F Number](sampleRate int, blockSize int, centerFrequency F) *FrequencyMapping[F] {
	result := &FrequencyMapping[F]{
		sampleRate: sampleRate,
		blockSize:  blockSize,
		binSize:    sampleRate / blockSize,
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
	m.fromFrequency = m.centerFrequency - m.centerBin*m.binSize
}

func (m *FrequencyMapping[F]) BinToFrequency(bin int, location BinLocation) F {
	var locationDelta int
	if location != 0 {
		locationDelta = int(float64(m.binSize)*(1.0/float64(location))) - 1
	} else {
		locationDelta = 0
	}

	return F(m.fromFrequency + bin*m.binSize + locationDelta)
}

func (m *FrequencyMapping[F]) FrequencyToBin(frequency F) int {
	bin := (int(frequency) - m.fromFrequency) / m.binSize
	return max(0, min(bin, m.blockSize-1))
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

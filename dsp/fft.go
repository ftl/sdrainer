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
	samples     []complex128
	realSamples []float64
}

func NewFFT[T Number]() *FFT[T] {
	return &FFT[T]{}
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
	samplesSize := len(iqSamples) / 2
	if len(f.samples) != samplesSize {
		f.samples = make([]complex128, samplesSize)
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

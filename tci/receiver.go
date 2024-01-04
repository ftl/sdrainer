package tci

import (
	"log"
	"math/rand"
	"time"

	"github.com/ftl/sdrainer/cw"
	"github.com/ftl/sdrainer/dsp"
	"github.com/ftl/sdrainer/trace"
)

const (
	traceSpectrum = "spectrum"

	iqBufferSize   = 100
	cumulationSize = 30
	dBmShift       = 120

	defaultPeakThreshold = 15
	defaultEdgeWidth     = 70
)

type ReceiverMode string

const (
	VFOMode        ReceiverMode = "vfo"
	RandomPeakMode ReceiverMode = "random"
)

type ReceiverIndicator[T, F dsp.Number] interface {
	ShowPeaks(receiver string, peaks []dsp.Peak[T, F])
	ShowDecode(receiver string, peak dsp.Peak[T, F])
	HideDecode(receiver string)
}

type Receiver[T, F dsp.Number] struct {
	id              string
	indicator       ReceiverIndicator[T, F]
	mode            ReceiverMode
	peakThreshold   T
	edgeWidth       int
	sampleRate      int
	blockSize       int
	centerFrequency F
	vfoOffset       F

	in               chan []T
	op               chan func()
	fft              *dsp.FFT[T]
	frequencyMapping *dsp.FrequencyMapping[F]
	demodulator      *cw.SpectralDemodulator[T, F]

	tracer trace.Tracer
}

func NewReceiver[T, F dsp.Number](id string, indicator ReceiverIndicator[T, F], mode ReceiverMode) *Receiver[T, F] {
	result := &Receiver[T, F]{
		id:            id,
		indicator:     indicator,
		mode:          mode,
		peakThreshold: defaultPeakThreshold,
		edgeWidth:     defaultEdgeWidth,

		fft: dsp.NewFFT[T](),

		tracer: new(trace.NoTracer),
	}
	return result
}

func (r *Receiver[T, F]) Start(sampleRate int, blockSize int) {
	if r.in != nil {
		return
	}

	r.in = make(chan []T, iqBufferSize)
	r.op = make(chan func())

	r.sampleRate = sampleRate
	r.blockSize = blockSize
	r.demodulator = cw.NewSpectralDemodulator[T, F](int(sampleRate), r.blockSize)
	r.demodulator.SetSignalThreshold(r.peakThreshold)
	r.demodulator.SetTracer(r.tracer)
	r.frequencyMapping = dsp.NewFrequencyMapping(r.sampleRate, r.blockSize, r.centerFrequency)

	go r.run()
}

func (r *Receiver[T, F]) Stop() {
	if r.in == nil {
		return
	}

	r.tracer.Stop()

	close(r.in)
	close(r.op)
	r.in = nil
	r.op = nil
}

func (r *Receiver[T, F]) do(f func()) {
	if r.op == nil {
		f()
	} else {
		r.op <- f
	}
}

func (r *Receiver[T, F]) SetTracer(tracer trace.Tracer) {
	r.do(func() {
		r.tracer = tracer
		r.demodulator.SetTracer(tracer)
	})
}

func (r *Receiver[T, F]) SetPeakThreshold(threshold T) {
	r.do(func() {
		r.peakThreshold = threshold
		r.demodulator.SetSignalThreshold(threshold)
	})
}

func (r *Receiver[T, F]) SetEdgeWidth(edgeWidth int) {
	r.do(func() {
		r.edgeWidth = edgeWidth
	})
}

func (r *Receiver[T, F]) SetSignalDebounce(debounce int) {
	r.do(func() {
		r.demodulator.SetSignalDebounce(debounce)
	})
}

func (r *Receiver[T, F]) SetCenterFrequency(frequency F) {
	r.do(func() {
		r.centerFrequency = frequency
		if r.demodulator != nil {
			r.demodulator.Reset()
		}
		if r.frequencyMapping != nil {
			r.frequencyMapping.SetCenterFrequency(frequency)
		}
	})
}

func (r *Receiver[T, F]) CenterFrequency() F {
	result := make(chan F)
	r.do(func() {
		result <- r.centerFrequency
	})
	return <-result
}

func (r *Receiver[T, F]) SetVFOOffset(offset F) {
	r.do(func() {
		r.vfoOffset = offset
		if r.blockSize == 0 {
			return
		}
		if r.mode == VFOMode {
			freq := r.vfoOffset + r.centerFrequency
			bin := r.frequencyMapping.FrequencyToBin(freq)
			r.demodulator.Attach(&dsp.Peak[T, F]{
				From:          max(0, bin-1),
				To:            min(bin+1, r.blockSize-1),
				FromFrequency: freq,
				ToFrequency:   freq,
				MaxValue:      1,
			})
		}
	})
	r.tracer.Start()
}

func (r *Receiver[T, F]) IQData(sampleRate int, data []T) {
	if r.in == nil {
		return
	}
	if r.sampleRate != int(sampleRate) {
		log.Printf("wrong incoming sample rate on receiver %s: %d!", r.id, sampleRate)
		return
	}
	if r.blockSize != len(data)/2 {
		log.Printf("wrong incoming block size on receiver %s: %d", r.id, len(data))
		return
	}

	select {
	case r.in <- data:
		return
	default:
		log.Printf("IQ data skipped on receiver %s", r.id)
	}
}

func (r *Receiver[T, F]) run() {
	var spectrum dsp.Block[T]
	var psd dsp.Block[T]
	var cumulation dsp.Block[T]
	var peaks []dsp.Peak[T, F]

	cumulationCount := 0

	peakTicker := time.NewTicker(5 * time.Second)
	defer peakTicker.Stop()

	for {
		select {
		case op := <-r.op:
			op()
		case <-peakTicker.C:
			// peaksToShow := make([]dsp.peak[T, int], len(peaks))
			// copy(peaksToShow, peaks)
			// r.indicator.ShowPeaks(r.id, peaksToShow)
		case frame := <-r.in:
			if len(frame) == 0 {
				continue
			}

			if spectrum.Size() != r.blockSize {
				spectrum = make([]T, r.blockSize)
				psd = make([]T, r.blockSize)
				cumulation = make([]T, r.blockSize)
				peaks = make([]dsp.Peak[T, F], 0, r.blockSize)
			}

			shiftedMagnitude := func(fftValue complex128, blockSize int) T {
				return dsp.Magnitude2dBm[T](fftValue, blockSize) + dBmShift
			}
			r.fft.IQToSpectrumAndPSD(spectrum, psd, frame, shiftedMagnitude)

			psdNoiseFloor := dsp.FindNoiseFloor(psd, r.edgeWidth)
			noiseFloor := dsp.PSDValue2dBm(psdNoiseFloor, r.blockSize) + dBmShift

			if r.demodulator.Attached() {
				maxValue, _ := spectrum.Max(r.demodulator.PeakRange())

				r.demodulator.Tick(maxValue, noiseFloor)

				if r.mode == RandomPeakMode && r.demodulator.TimeoutExceeded() {
					r.demodulator.Detach()
					r.indicator.HideDecode(r.id)
					r.tracer.Stop()
				}
			}

			for i := range spectrum {
				cumulation[i] += spectrum[i]
			}
			cumulationCount++

			if cumulationCount == cumulationSize {
				threshold := r.peakThreshold + noiseFloor
				peaks = dsp.FindPeaks(peaks, cumulation, cumulationSize, threshold, r.frequencyMapping)

				if r.tracer.Context() == traceSpectrum {
					peakFrame := peaksToPeakFrame(peaks, r.blockSize)
					for i, v := range cumulation {
						r.tracer.Trace(traceSpectrum, "%f;%f;%f;%f\n", v/T(cumulationSize), threshold, noiseFloor, peakFrame[i])
					}
					r.tracer.Stop()
				}

				if r.mode == RandomPeakMode && len(peaks) > 0 && !r.demodulator.Attached() {
					peakIndex := rand.Intn(len(peaks))
					peak := peaks[peakIndex]

					peak.MaxValue = peak.MaxValue / T(cumulationSize)
					peak.From = max(0, peak.MaxBin-1)
					peak.To = min(peak.MaxBin+1, r.blockSize-1)
					peak.FromFrequency = r.frequencyMapping.BinToFrequency(peak.From, dsp.BinFrom)
					peak.ToFrequency = r.frequencyMapping.BinToFrequency(peak.To, dsp.BinTo)

					r.demodulator.Attach(&peak)
					r.indicator.ShowDecode(r.id, peak)

					r.tracer.Start()
				}

				clear(cumulation)
				cumulationCount = 0
			}
		}
	}
}

func peaksToPeakFrame[T, F dsp.Number](peaks []dsp.Peak[T, F], blockSize int) []T {
	result := make([]T, blockSize)

	for _, p := range peaks {
		for i := p.From; i <= p.To; i++ {
			result[i] = p.MaxValue
		}
	}

	return result
}

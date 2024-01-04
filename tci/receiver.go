package tci

import (
	"log"
	"math"
	"math/rand"
	"time"

	tci "github.com/ftl/tci/client"

	"github.com/ftl/sdrainer/dsp"
	"github.com/ftl/sdrainer/trace"
)

const (
	traceSpectrum = "spectrum"

	iqBufferSize   = 100
	cumulationSize = 30

	defaultPeakThreshold float32 = 15
)

type Receiver struct {
	process         *Process
	trx             int
	mode            Mode
	peakThreshold   float32
	sampleRate      int
	centerFrequency int
	vfoOffset       int
	blockSize       int

	in               chan []float32
	op               chan func()
	fft              *dsp.FFT[float32]
	frequencyMapping *dsp.FrequencyMapping[int]
	decoder          *decoder[float32, int]

	tracer trace.Tracer
}

func NewReceiver(process *Process, trx int, mode Mode) *Receiver {
	result := &Receiver{
		process:       process,
		trx:           trx,
		mode:          mode,
		peakThreshold: defaultPeakThreshold,

		fft: dsp.NewFFT[float32](),

		tracer: new(trace.NoTracer),
	}
	return result
}

func (h *Receiver) Start() {
	if h.in != nil {
		return
	}

	h.in = make(chan []float32, iqBufferSize)
	h.op = make(chan func())
	h.sampleRate = 0
	h.blockSize = 0
	h.decoder = nil
	h.frequencyMapping = nil

	go h.run()
}

func (h *Receiver) Stop() {
	if h.in == nil {
		return
	}

	h.tracer.Stop()

	close(h.in)
	close(h.op)
	h.in = nil
	h.op = nil
}

func (h *Receiver) do(f func()) {
	if h.op == nil {
		f()
	} else {
		h.op <- f
	}
}

func (h *Receiver) SetTracer(tracer trace.Tracer) {
	h.do(func() {
		h.tracer = tracer
		if h.decoder != nil {
			h.decoder.SetTracer(tracer)
		}
	})
}

func (h *Receiver) SetPeakThreshold(threshold float32) {
	h.do(func() {
		h.peakThreshold = threshold
		if h.decoder != nil {
			h.decoder.SetSignalThreshold(threshold)
		}
	})
}

func (h *Receiver) SetSignalDebounce(debounce int) {
	h.do(func() {
		if h.decoder != nil {
			h.decoder.SetSignalDebounce(debounce)
		}
	})
}

func (h *Receiver) SetCenterFrequency(frequency int) {
	h.do(func() {
		h.centerFrequency = frequency
		if h.decoder != nil {
			h.decoder.reset()
		}
		if h.frequencyMapping != nil {
			h.frequencyMapping.SetCenterFrequency(frequency)
			log.Printf("frequency mapping: %s", h.frequencyMapping)
		}
	})
}

func (h *Receiver) CenterFrequency() int {
	result := make(chan int)
	h.do(func() {
		result <- h.centerFrequency
	})
	return <-result
}

func (h *Receiver) SetVFOOffset(vfo tci.VFO, offset int) {
	if vfo == tci.VFOB {
		return
	}
	h.do(func() {
		h.vfoOffset = offset
		if h.blockSize == 0 {
			return
		}
		if h.mode == VFOMode {
			freq := h.vfoOffset + h.centerFrequency
			bin := h.frequencyToBin(freq)
			h.decoder.Attach(&dsp.Peak[float32, int]{
				From:          max(0, bin-1),
				To:            min(bin+1, h.blockSize-1),
				FromFrequency: freq,
				ToFrequency:   freq,
				MaxValue:      0.1,
			})
		}
	})
	h.tracer.Start()
}

func (h *Receiver) IQData(sampleRate tci.IQSampleRate, data []float32) {
	if h.in == nil {
		return
	}
	if h.sampleRate == 0 {
		h.sampleRate = int(sampleRate)
		h.blockSize = len(data) / 2
		h.decoder = newDecoder[float32, int](int(sampleRate), len(data))
		h.decoder.SetSignalThreshold(h.peakThreshold)
		h.decoder.SetTracer(h.tracer)
		h.frequencyMapping = dsp.NewFrequencyMapping(h.sampleRate, h.blockSize, h.centerFrequency)
		log.Printf("frequency mapping: %s", h.frequencyMapping)
	} else if h.sampleRate != int(sampleRate) {
		log.Printf("wrong incoming sample rate on trx %d: %d!", h.trx, sampleRate)
		return
	} else if h.blockSize != len(data)/2 {
		log.Printf("wrong incoming block size on trx %d: %d", h.trx, len(data))
	}

	select {
	case h.in <- data:
		return
	default:
		log.Printf("IQ data skipped on trx %d", h.trx)
	}
}

func (h *Receiver) run() {
	var spectrum dsp.Block[float32]
	var psd dsp.Block[float32]
	var cumulation dsp.Block[float32]
	var peaks []dsp.Peak[float32, int]

	cumulationCount := 0

	peakTicker := time.NewTicker(5 * time.Second)
	defer peakTicker.Stop()

	for {
		select {
		case op := <-h.op:
			op()
		case <-peakTicker.C:
			// peaksToShow := make([]dsp.peak[float32, int], len(peaks))
			// copy(peaksToShow, peaks)
			// h.process.ShowPeaks(peaksToShow)
		case frame := <-h.in:
			if len(frame) == 0 {
				continue
			}

			if spectrum.Size() != h.blockSize {
				spectrum = make([]float32, h.blockSize)
				psd = make([]float32, h.blockSize)
				cumulation = make([]float32, h.blockSize)
				peaks = make([]dsp.Peak[float32, int], 0, h.blockSize)
			}

			shiftedMagnitude := func(fftValue complex128, blockSize int) float32 {
				return dsp.Magnitude2dBm[float32](fftValue, blockSize) + 120
			}
			h.fft.IQToSpectrumAndPSD(spectrum, psd, frame, shiftedMagnitude)
			noiseFloor := h.findNoiseFloor(psd)

			if h.decoder != nil && h.decoder.Attached() {
				maxValue, _ := spectrum.Max(h.decoder.PeakRange())

				h.decoder.Tick(maxValue, noiseFloor)

				if h.mode == RandomPeakMode && h.decoder.TimeoutExceeded() {
					h.decoder.Detach()
					h.process.HideDecode()
					h.tracer.Stop()
				}
			}

			for i := range spectrum {
				cumulation[i] += spectrum[i]
			}
			cumulationCount++

			if cumulationCount == cumulationSize {
				var threshold float32
				peaks, threshold = h.detectPeaks(peaks, cumulation, cumulationSize, noiseFloor)
				_ = threshold

				if h.tracer.Context() == traceSpectrum {
					peakFrame := peaksToPeakFrame(peaks, h.blockSize)
					for i, v := range cumulation {
						h.tracer.Trace(traceSpectrum, "%f;%f;%f;%f\n", v/float32(cumulationSize), threshold, noiseFloor, peakFrame[i])
					}
					h.tracer.Stop()
				}

				if h.mode == RandomPeakMode && h.decoder != nil && len(peaks) > 0 && !h.decoder.Attached() {
					peakIndex := rand.Intn(len(peaks))
					peak := peaks[peakIndex]

					peak.MaxValue = peak.MaxValue / float32(cumulationSize)
					peak.From = max(0, peak.MaxBin-1)
					peak.To = min(peak.MaxBin+1, h.blockSize-1)
					peak.FromFrequency = h.binToFrequency(peak.From, dsp.BinFrom)
					peak.ToFrequency = h.binToFrequency(peak.To, dsp.BinTo)

					h.decoder.Attach(&peak)
					h.process.ShowDecode(peak)

					h.tracer.Start()
				}

				clear(cumulation)
				cumulationCount = 0
			}
		}
	}
}

func (h *Receiver) findNoiseFloor(psd dsp.Block[float32]) float32 {
	const edgeWidth = 70

	windowSize := len(psd) / 10
	var minValue float32 = math.MaxFloat32
	var sum float32
	count := 0
	for i := edgeWidth; i < len(psd)-edgeWidth; i++ {
		if count == windowSize {
			count = 0
			mean := sum / float32(windowSize)
			if mean < minValue {
				minValue = mean
			}
			sum = 0
		}
		sum += psd[i]
		count++
	}

	return dsp.PSDValue2dBm(minValue, h.blockSize) + 120
}

func (h *Receiver) detectPeaks(peaks []dsp.Peak[float32, int], spectrum dsp.Block[float32], cumulationSize int, noiseFloor float32) ([]dsp.Peak[float32, int], float32) {
	peaks = peaks[:0]

	threshold := h.peakThreshold + noiseFloor
	var currentPeak *dsp.Peak[float32, int]
	for i, v := range spectrum {
		value := v / float32(cumulationSize)
		if currentPeak == nil && value > threshold {
			currentPeak = &dsp.Peak[float32, int]{From: i, MaxValue: value, MaxBin: i}
		} else if currentPeak != nil && value <= threshold {
			currentPeak.To = i - 1
			currentPeak.FromFrequency = h.binToFrequency(currentPeak.From, dsp.BinFrom)
			currentPeak.ToFrequency = h.binToFrequency(currentPeak.To, dsp.BinTo)
			peaks = append(peaks, *currentPeak)
			currentPeak = nil
		} else if currentPeak != nil && currentPeak.MaxValue < value {
			currentPeak.MaxValue = value
			currentPeak.MaxBin = i
		}
	}

	if currentPeak != nil {
		currentPeak.To = len(spectrum) - 1
		peaks = append(peaks, *currentPeak)
	}

	return peaks, threshold
}

func (h *Receiver) binToFrequency(bin int, location dsp.BinLocation) int {
	if h.frequencyMapping == nil {
		return h.centerFrequency
	}
	return h.frequencyMapping.BinToFrequency(bin, location)
}

func (h *Receiver) frequencyToBin(frequency int) int {
	if h.frequencyMapping == nil {
		return 0
	}
	return h.frequencyMapping.FrequencyToBin(frequency)
}

func peaksToPeakFrame(peaks []dsp.Peak[float32, int], blockSize int) []float32 {
	result := make([]float32, blockSize)

	for _, p := range peaks {
		for i := p.From; i <= p.To; i++ {
			result[i] = p.MaxValue
		}
	}

	return result
}

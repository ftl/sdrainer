package tci

import (
	"log"
	"math"
	"math/rand"
	"time"

	"github.com/ftl/sdrainer/dsp"
	tci "github.com/ftl/tci/client"
)

const (
	iqBufferSize = 100
)

type tracer interface {
	Start()
	Trace(string, ...any)
	Stop()
}

type Receiver struct {
	process         *Process
	trx             int
	sampleRate      int
	centerFrequency int
	vfoOffset       int
	blockSize       int

	in               chan []float32
	op               chan func()
	fft              *dsp.FFT[float32]
	frequencyMapping *dsp.FrequencyMapping[int]
	decoder          *decoder

	tracer tracer
}

func NewReceiver(process *Process, trx int) *Receiver {
	result := &Receiver{
		process: process,
		trx:     trx,
		fft:     dsp.NewFFT[float32](),

		// tracer: NewFileTracer("trace.csv"),
		tracer: NewUDPTracer("localhost:3536"),
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
		if true || h.blockSize == 0 { // TODO REMOVE INACTIVATION
			return
		}
		freq := h.vfoOffset + h.centerFrequency
		bin := h.frequencyToBin(freq)
		h.decoder.Attach(&peak{
			from:          max(0, bin-1),
			to:            min(bin+1, h.blockSize-1),
			fromFrequency: freq,
			toFrequency:   freq,
			max:           0.1,
		})

	})
	// h.tracer.Start() // TODO remove tracing
}

func (h *Receiver) IQData(sampleRate tci.IQSampleRate, data []float32) {
	if h.in == nil {
		return
	}
	if h.sampleRate == 0 {
		h.sampleRate = int(sampleRate)
		h.blockSize = len(data) / 2
		h.decoder = newDecoder(int(sampleRate), len(data))
		h.frequencyMapping = dsp.NewFrequencyMapping(h.sampleRate, h.blockSize, h.centerFrequency)
		log.Printf("frequency mapping: %s", h.frequencyMapping)

		// TRACING
		h.decoder.tracer = h.tracer
		h.decoder.decoder.SetTracer(h.tracer)
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
	var spectrum block
	var psd block
	var cumulation block
	var peaks []peak

	cumulationSize := 30
	cumulationCount := 0

	peakTicker := time.NewTicker(5 * time.Second)
	defer peakTicker.Stop()

	traced := false
	cycle := 0
	for {
		cycle++
		select {
		case op := <-h.op:
			op()
		case <-peakTicker.C:
			peaksToShow := make([]peak, len(peaks))
			copy(peaksToShow, peaks)
			// h.process.doAsync(func() {
			// 	h.process.showPeaks(peaksToShow)
			// })
		case frame := <-h.in:
			if len(frame) == 0 {
				continue
			}
			if spectrum.size() != h.blockSize {
				spectrum = make([]float32, h.blockSize)
				psd = make([]float32, h.blockSize)
				cumulation = make([]float32, h.blockSize)
				peaks = make([]peak, 0, h.blockSize)
			}

			shiftedMagnitude := func(fftValue complex128, blockSize int) float32 {
				return dsp.Magnitude2dBm[float32](fftValue, blockSize) + 120
			}
			h.fft.IQToSpectrumAndPSD(spectrum, psd, frame, shiftedMagnitude)
			noiseFloor := h.findNoiseFloor(psd)

			if h.decoder != nil && h.decoder.Attached() {
				maxValue, _ := spectrum.rangeMax(h.decoder.PeakRange())

				h.decoder.Tick(maxValue, noiseFloor)

				if true && h.decoder.TimeoutExceeded() { // TODO REMOVE INACTIVATION
					h.decoder.Detach()
					h.process.doAsync(func() {
						h.process.hideDecode()
					})
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

				// if h.tracer != nil && cycle > 100 {
				// 	peakFrame := peaksToPeakFrame(peaks, h.blockSize)
				// 	for i, v := range cumulation {
				// 		h.tracer.Trace("%f;%f;%f;%f\n", v/float32(cumulationSize), threshold, noiseFloor, peakFrame[i])
				// 	}
				// 	h.tracer.Stop()
				// }

				if true && h.decoder != nil && len(peaks) > 0 && !h.decoder.Attached() { // TODO REMOVE INACTIVATION
					peakIndex := rand.Intn(len(peaks))
					peak := peaks[peakIndex]

					peak.max = peak.max / float32(cumulationSize)
					peak.from = max(0, peak.maxBin-1)
					peak.to = min(peak.maxBin+1, h.blockSize-1)
					peak.fromFrequency = h.binToFrequency(peak.from, dsp.BinFrom)
					peak.toFrequency = h.binToFrequency(peak.to, dsp.BinTo)

					h.decoder.Attach(&peak)
					h.process.doAsync(func() {
						h.process.showDecode(peak)
					})

					if !traced {
						// traced = true
						// h.tracer.Start() // TODO remove tracing
					}
				}

				clear(cumulation)
				cumulationCount = 0
			}
		}
	}
}

func (h *Receiver) findNoiseFloor(psd block) float32 {
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

func (h *Receiver) detectPeaks(peaks []peak, spectrum block, cumulationSize int, noiseFloor float32) ([]peak, float32) {
	const peakThreshold float32 = 1.3
	peaks = peaks[:0]

	threshold := peakThreshold * noiseFloor
	var currentPeak *peak
	for i, v := range spectrum {
		value := v / float32(cumulationSize)
		if currentPeak == nil && value > threshold {
			currentPeak = &peak{from: i, max: value, maxBin: i}
		} else if currentPeak != nil && value <= threshold {
			currentPeak.to = i - 1
			currentPeak.fromFrequency = h.binToFrequency(currentPeak.from, dsp.BinFrom)
			currentPeak.toFrequency = h.binToFrequency(currentPeak.to, dsp.BinTo)
			peaks = append(peaks, *currentPeak)
			currentPeak = nil
		} else if currentPeak != nil && currentPeak.max < value {
			currentPeak.max = value
			currentPeak.maxBin = i
		}
	}

	if currentPeak != nil {
		currentPeak.to = len(spectrum) - 1
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

type block []float32

func (b block) size() int {
	return len(b)
}

func (b block) rangeSum(from, to int) float32 {
	var sum float32
	for i := from; i <= to; i++ {
		sum += b[i]
	}
	return sum
}

func (b block) rangeMean(from, to int) float32 {
	return b.rangeSum(from, to) / float32(to-from+1)
}

func (b block) rangeMax(from, to int) (float32, int) {
	var maxValue float32 = -1 * math.MaxFloat32
	var maxI int
	for i := from; i <= to; i++ {
		if maxValue < b[i] {
			maxValue = b[i]
			maxI = i
		}
	}
	return maxValue, maxI
}

type peak struct {
	from          int
	to            int
	fromFrequency int
	toFrequency   int
	max           float32
	maxBin        int
}

func (p peak) Center() int {
	return p.from + ((p.to - p.from) / 2)
}

func (p peak) CenterFrequency() int {
	return p.fromFrequency + ((p.toFrequency - p.fromFrequency) / 2)
}

func (p peak) Width() int {
	return (p.to - p.from) + 1
}

func (p peak) WidthHz() int {
	return p.toFrequency - p.fromFrequency
}

func peaksToPeakFrame(peaks []peak, blockSize int) []float32 {
	result := make([]float32, blockSize)

	for _, p := range peaks {
		for i := p.from; i <= p.to; i++ {
			result[i] = p.max
		}
	}

	return result
}

package tci

import (
	"log"
	"math"
	"math/rand"
	"time"

	"github.com/ftl/sdrainer/dsp"
	tci "github.com/ftl/tci/client"
)

type tracer interface {
	Start()
	Trace(string, ...any)
	Stop()
}

type trxHandler struct {
	process         *Process
	trx             int
	sampleRate      int
	centerFrequency int
	vfoOffset       int
	blockSize       int

	in      chan []float32
	op      chan func()
	fft     *dsp.FFT[float32]
	decoder *decoder

	tracer tracer
}

func newTRXHandler(process *Process, trx int) *trxHandler {
	result := &trxHandler{
		process: process,
		trx:     trx,

		in:  make(chan []float32, 100),
		op:  make(chan func()),
		fft: dsp.NewFFT[float32](),

		tracer: NewFileTracer("trace.csv"),
		// tracer: NewUDPTracer("localhost:3536"),
	}
	go result.run()
	return result
}

func (h *trxHandler) Close() {
	close(h.in)
	h.tracer.Stop()
}

func (h *trxHandler) do(f func()) {
	h.op <- f
}

func (h *trxHandler) SetCenterFrequency(frequency int) {
	h.do(func() {
		h.centerFrequency = frequency
		if h.decoder != nil {
			h.decoder.reset()
		}
	})
}

func (h *trxHandler) SetVFOOffset(vfo tci.VFO, offset int) {
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
			from:          bin - 2,
			to:            bin + 2,
			fromFrequency: freq,
			toFrequency:   freq,
			max:           0.1,
		})

	})
	// h.tracer.Start() // TODO remove tracing
}

func (h *trxHandler) IQData(sampleRate tci.IQSampleRate, data []float32) {
	if h.sampleRate == 0 {
		h.sampleRate = int(sampleRate)
		h.blockSize = len(data) / 2
		h.decoder = newDecoder(int(sampleRate), len(data))
		h.decoder.tracer = h.tracer
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

func (h *trxHandler) run() {
	var spectrum block
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
				cumulation = make([]float32, h.blockSize)
				peaks = make([]peak, 0, h.blockSize)
			}

			shiftedMagnitude := func(fftValue complex128, blockSize int) float32 {
				return dsp.Magnitude2dBm[float32](fftValue, blockSize) + 120
			}
			h.fft.IQToSpectrum(spectrum, frame, shiftedMagnitude)

			if h.decoder != nil && h.decoder.Attached() {
				maxValue, _ := spectrum.rangeMax(h.decoder.PeakRange())

				h.decoder.Tick(maxValue) // TODO this should not happen here

				if true && h.decoder.TimeoutExceeded() { // TODO REMOVE INACTIVATION
					h.decoder.Detach()
					h.process.doAsync(func() {
						h.process.hideDecode()
					})
					// h.tracer.Stop()
				}
			}

			for i := range spectrum {
				cumulation[i] += spectrum[i]
			}
			cumulationCount++

			if cumulationCount == cumulationSize {
				var threshold float32
				peaks, threshold = h.detectPeaks(peaks, cumulation)
				_ = threshold

				// if h.tracer != nil && cycle > 100 {
				// 	peakFrame := peaksToPeakFrame(peaks, h.blockSize)
				// 	for i, v := range cumulation {
				// 		h.tracer.Trace("%f;%f;%f\n", v, threshold, peakFrame[i])
				// 	}
				// 	h.tracer.Stop()
				// }

				if true && h.decoder != nil && len(peaks) > 0 && !h.decoder.Attached() { // TODO REMOVE INACTIVATION
					peakIndex := rand.Intn(len(peaks))
					peak := peaks[peakIndex]

					peak.max = peak.max / float32(cumulationSize)
					peak.from = max(0, peak.maxBin-3)
					peak.to = min(peak.maxBin+3, h.blockSize-1)
					peak.fromFrequency = h.binToFrequency(peak.from, h.blockSize, binFrom)
					peak.toFrequency = h.binToFrequency(peak.to, h.blockSize, binTo)

					h.decoder.Attach(&peak)
					h.decoder.Tick(peak.max)
					h.process.doAsync(func() {
						h.process.showDecode(peak)
					})

					if !traced {
						traced = true
						// h.tracer.Start() // TODO remove tracing
					}
				}

				clear(cumulation)
				cumulationCount = 0
			}
		}
	}
}

func (h *trxHandler) detectPeaks(peaks []peak, spectrum block) ([]peak, float32) {
	const peakThreshold float32 = 0.3
	const silenceThreshold float32 = 400
	const edgeWidth = 70
	peaks = peaks[:0]

	var max float32 = 0
	var min float32 = math.MaxFloat32
	for i, v := range spectrum {
		if (i <= edgeWidth) || (i > spectrum.size()-edgeWidth) {
			continue
		}
		if max < v {
			max = v
		}
		if min > v {
			min = v
		}
	}
	delta := max - min
	if delta < silenceThreshold {
		return peaks, 0
	}

	threshold := peakThreshold*delta + min
	var currentPeak *peak
	for i, v := range spectrum {
		if currentPeak == nil && v > threshold {
			currentPeak = &peak{from: i, max: v, maxBin: i}
		} else if currentPeak != nil && v <= threshold {
			currentPeak.to = i - 1
			currentPeak.fromFrequency = h.binToFrequency(currentPeak.from, spectrum.size(), binFrom)
			currentPeak.toFrequency = h.binToFrequency(currentPeak.to, spectrum.size(), binTo)
			peaks = append(peaks, *currentPeak)
			currentPeak = nil
		} else if currentPeak != nil && currentPeak.max < v {
			currentPeak.max = v
			currentPeak.maxBin = i
		}
	}

	if currentPeak != nil {
		currentPeak.to = len(spectrum) - 1
		peaks = append(peaks, *currentPeak)
	}

	return peaks, threshold
}

func (h *trxHandler) binToFrequency(bin int, blockSize int, edge binEdge) int {
	binSize := h.sampleRate / blockSize
	centerBin := blockSize / 2

	return (binSize * (bin - centerBin)) + int(float32(binSize)*float32(edge)) + h.centerFrequency
}

func (h *trxHandler) frequencyToBin(frequency int) int {
	binSize := h.sampleRate / h.blockSize

	return (frequency-h.centerFrequency)/binSize + (h.blockSize / 2)
}

type binEdge float32

const (
	binFrom   binEdge = 0.0
	binCenter binEdge = 0.5
	binTo     binEdge = 1.0
)

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

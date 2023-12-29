package tci

import (
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"os"
	"time"

	"github.com/ftl/sdrainer/dsp"
	tci "github.com/ftl/tci/client"
)

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

	traceFile io.WriteCloser
}

func newTRXHandler(process *Process, trx int) *trxHandler {
	result := &trxHandler{
		process: process,
		trx:     trx,

		in:  make(chan []float32, 100),
		op:  make(chan func()),
		fft: dsp.NewFFT[float32](),
	}
	go result.run()
	return result
}

func (h *trxHandler) Close() {
	close(h.in)
	h.stopTrace()
}

func (h *trxHandler) do(f func()) {
	h.op <- f
}

func (h *trxHandler) SetCenterFrequency(frequency int) {
	h.do(func() {
		h.centerFrequency = frequency
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

		// h.startTrace() // TODO remove tracing
	})
}

func (h *trxHandler) IQData(sampleRate tci.IQSampleRate, data []float32) {
	if h.sampleRate == 0 {
		h.sampleRate = int(sampleRate)
		h.blockSize = len(data) / 2
		h.decoder = newDecoder(int(sampleRate), len(data))
		h.decoder.tracer = h
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
	var threshold float32

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

			h.fft.IQToSpectrum(spectrum, frame, dsp.Magnitude[float32])

			if h.decoder != nil && h.decoder.Attached() {
				begin, end, peakMax := h.decoder.PeakRange()
				maxValue, _ := spectrum.rangeMax(begin, end)
				maxRatio := maxValue / peakMax

				h.decoder.Tick(maxRatio)

				if true && h.decoder.TimeoutExceeded() { // TODO REMOVE INACTIVATION
					h.decoder.Detach()
					h.process.doAsync(func() {
						h.process.hideDecode()
					})
					h.stopTrace()
				}
			}

			for i := range spectrum {
				cumulation[i] += spectrum[i]
			}
			cumulationCount++
			if cumulationCount == cumulationSize {
				peaks, threshold = h.detectPeaks(peaks, cumulation)

				if false && ((len(peaks) > 0 && len(peaks) < 100) || cycle > 100) { // TODO remove tracing code
					frameToCSV(spectrum, peaksToPeakFrame(peaks, h.blockSize), threshold)
					if true {
						panic("FRAME SAVED TO FILE")
					}
				}

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
						// h.startTrace() // TODO remove tracing
					}
				}

				clear(cumulation)
				cumulationCount = 0
			}
		}
	}
}

func (h *trxHandler) detectPeaks(peaks []peak, spectrum block) ([]peak, float32) {
	const peakThreshold float32 = 0.15    // 0.3  // 0.075
	const silenceThreshold float32 = 0.25 // 1 // 0.25
	const lowerBound float32 = 0.025      // 0.1     // 0.025
	peaks = peaks[:0]

	var max float32 = lowerBound
	var min float32 = math.MaxFloat32
	for _, v := range spectrum {
		if max < v {
			max = v
		}
		if min > v && v > lowerBound {
			min = v
		}
	}
	if (max - min) < silenceThreshold {
		return peaks, 0
	}

	threshold := peakThreshold
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

func (h *trxHandler) startTrace() {
	if h.traceFile != nil {
		return
	}

	var err error
	h.traceFile, err = os.Create("trace.csv")
	if err != nil {
		h.traceFile = nil
		log.Printf("cannot start trace: %v", err)
	}
}

func (h *trxHandler) trace(format string, args ...any) {
	if h.traceFile == nil {
		return
	}

	fmt.Fprintf(h.traceFile, format, args...)
}

func (h *trxHandler) stopTrace() {
	if h.traceFile == nil {
		return
	}

	h.traceFile.Close()
	h.traceFile = nil
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

func (b block) checkRange(from, to int, threshold float32) bool {
	var value float32
	for i := from; i <= to; i++ {
		value += b[i]
	}
	return value > threshold
}

func (b block) rangeSum(from, to int) float32 {
	var sum float32
	for i := from; i <= to; i++ {
		sum += b[i]
	}
	return sum
}

func (b block) rangeMax(from, to int) (float32, int) {
	var maxValue float32
	var maxI int
	for i := from; i <= to; i++ {
		if maxValue < b[i] {
			maxValue = b[i]
			maxI = i
		}
	}
	return maxValue, maxI
}

func (b block) rangeValues(from, to int) (sum float32, max float32, mean float32) {
	for i := from; i <= to; i++ {
		sum += b[i]
		if max < b[i] {
			max = b[i]
		}
	}
	mean = sum / float32(to-from+1)
	return sum, max, mean
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

func frameToCSV(frame []float32, peakFrame []float32, threshold float32) {
	f, err := os.Create("frame.csv")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	for i, v := range frame {
		var peak float32
		if peakFrame != nil {
			peak = peakFrame[i]
		}
		fmt.Fprintf(f, "%f;%f;%f\n", v, peak, threshold)
	}
}

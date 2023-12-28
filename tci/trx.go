package tci

import (
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"github.com/ftl/sdrainer/dsp"
	tci "github.com/ftl/tci/client"
)

type trxHandler struct {
	process         *Process
	trx             int
	sampleRate      float32
	centerFrequency float32
	vfoOffset       float32

	in  chan []float32
	op  chan func()
	fft *dsp.FFT[float32]
}

func newTRXHandler(process *Process, trx int) *trxHandler {
	result := &trxHandler{
		process: process,
		trx:     trx,

		in:  make(chan []float32, 10),
		op:  make(chan func()),
		fft: dsp.NewFFT[float32](),
	}
	go result.run()
	return result
}

func (h *trxHandler) Close() {
	close(h.in)
}

func (h *trxHandler) do(f func()) {
	h.op <- f
}

func (h *trxHandler) SetCenterFrequency(frequency int) {
	h.do(func() {
		h.centerFrequency = float32(frequency)
	})
}

func (h *trxHandler) SetVFOOffset(vfo tci.VFO, offset int) {
	if vfo == tci.VFOB {
		return
	}
	h.do(func() {
		h.vfoOffset = float32(offset)
	})
}

func (h *trxHandler) IQData(sampleRate tci.IQSampleRate, data []float32) {
	if h.sampleRate == 0 {
		h.sampleRate = float32(sampleRate)
	} else if h.sampleRate != float32(sampleRate) {
		log.Printf("wrong incoming sample rate on trx %d: %d!", h.trx, sampleRate)
		return
	}

	select {
	case h.in <- data:
		return
	default:
		log.Printf("IQ data skipped on trx %d", h.trx)
	}
}

func (h *trxHandler) run() {
	var spectrum []float32
	var cumulation []float32
	var peaks []peak
	cumulationSize := 10
	cumulationCount := 0

	peakTicker := time.NewTicker(5 * time.Second)
	defer peakTicker.Stop()

	cycle := 0
	for {
		cycle++
		select {
		case op := <-h.op:
			op()
		case <-peakTicker.C:
			peaksToShow := make([]peak, len(peaks))
			copy(peaksToShow, peaks)
			h.process.doAsync(func() {
				h.process.showPeaks(peaksToShow)
			})
		case frame := <-h.in:
			blockSize := len(frame) / 2
			if len(spectrum) != blockSize {
				spectrum = make([]float32, blockSize)
				cumulation = make([]float32, blockSize)
				peaks = make([]peak, 0, blockSize)
			}

			h.fft.IQToSpectrum(spectrum, frame, dsp.Magnitude[float32])

			for i := range spectrum {
				cumulation[i] += spectrum[i]
			}
			cumulationCount++
			if cumulationCount == cumulationSize {
				var threshold float32
				peaks, threshold = h.detectPeaks(peaks, cumulation, blockSize)

				if ((len(peaks) > 0 && len(peaks) < 100) || cycle > 10) && false {
					frameToCSV(cumulation, peaksToPeakFrame(peaks, blockSize), threshold)
					if true {
						panic("FRAME SAVED TO FILE")
					}
				}

				clear(cumulation)
				cumulationCount = 0
			}
		}
	}
}

func (h *trxHandler) detectPeaks(peaks []peak, spectrum []float32, blockSize int) ([]peak, float32) {
	const peakThreshold float32 = 0.75    // 0.3  // 0.075
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
			currentPeak = &peak{from: i, max: v}
		} else if currentPeak != nil && v <= threshold {
			currentPeak.to = i - 1
			currentPeak.fromFrequency = h.binToFrequency(currentPeak.from, blockSize, binFrom)
			currentPeak.toFrequency = h.binToFrequency(currentPeak.to, blockSize, binTo)
			peaks = append(peaks, *currentPeak)
			// r := currentPeak.max / max
			// log.Printf("peak at %d: %d Hz %d width %f", currentPeak.Center(), currentPeak.CenterFrequency(), currentPeak.Width(), r)
			currentPeak = nil
		} else if currentPeak != nil && currentPeak.max < v {
			currentPeak.max = v
		}
	}

	if currentPeak != nil {
		currentPeak.to = len(spectrum) - 1
		peaks = append(peaks, *currentPeak)
	}

	if len(peaks) > 0 && false {
		log.Printf("%d peaks, %f %f %f", len(peaks), min, threshold, max)
		log.Printf("%d to %d", h.binToFrequency(0, blockSize, binFrom), h.binToFrequency(blockSize-1, blockSize, binTo))
	}

	return peaks, threshold
}

type binEdge float32

const (
	binFrom   binEdge = 0.0
	binCenter binEdge = 0.5
	binTo     binEdge = 1.0
)

func (h *trxHandler) binToFrequency(bin int, blockSize int, edge binEdge) int {
	binSize := int(h.sampleRate) / blockSize
	centerBin := blockSize / 2

	return (binSize * (bin - centerBin)) + int(float32(binSize)*float32(edge)) + int(h.centerFrequency)
}

type peak struct {
	from          int
	to            int
	fromFrequency int
	toFrequency   int
	max           float32
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

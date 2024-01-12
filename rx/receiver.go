package rx

import (
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/ftl/sdrainer/cw"
	"github.com/ftl/sdrainer/dsp"
	"github.com/ftl/sdrainer/trace"
)

const (
	traceSpectrum = "spectrum"

	iqBufferSize   = 100
	cumulationSize = 50
	dBmShift       = 120
	peakPadding    = 0
	noiseWindow    = 30

	defaultPeakThreshold = 15
	defaultEdgeWidth     = 70

	defaultSilenceTimeout    = 20 * time.Second
	defaultAttachmentTimeout = 2 * time.Minute
)

type Clock interface {
	Now() time.Time
}

type ClockFunc func() time.Time

func (f ClockFunc) Now() time.Time {
	return f()
}

var WallClock = ClockFunc(time.Now)

type manualClock struct {
	now time.Time
}

func (c *manualClock) Now() time.Time {
	return c.now
}

func (c *manualClock) Set(now time.Time) {
	c.now = now
}

type ReceiverMode string

const (
	VFOMode        ReceiverMode = "vfo"
	RandomPeakMode ReceiverMode = "random"
)

type ReceiverIndicator[T, F dsp.Number] interface {
	ShowPeaks(receiver string, peaks []dsp.Peak[T, F])
	ShowDecode(receiver string, peak dsp.Peak[T, F])
	HideDecode(receiver string)
	ShowSpot(receiver string, callsign string, frequency F)
}

type Receiver[T, F dsp.Number] struct {
	clock Clock

	id                string
	indicator         ReceiverIndicator[T, F]
	mode              ReceiverMode
	peakThreshold     T
	edgeWidth         int
	silenceTimeout    time.Duration
	attachmentTimeout time.Duration
	sampleRate        int
	blockSize         int
	centerFrequency   F
	vfoOffset         F

	in               chan []T
	op               chan func()
	fft              *dsp.FFT[T]
	frequencyMapping *dsp.FrequencyMapping[F]

	demodulator   *cw.SpectralDemodulator[T, F]
	textProcessor *TextProcessor
	lastAttach    time.Time

	tracer trace.Tracer
}

func NewReceiver[T, F dsp.Number](id string, mode ReceiverMode, clock Clock, indicator ReceiverIndicator[T, F]) *Receiver[T, F] {
	if clock == nil {
		clock = WallClock
	}
	return &Receiver[T, F]{
		clock:     clock,
		id:        id,
		indicator: indicator,
		mode:      mode,

		peakThreshold:     defaultPeakThreshold,
		edgeWidth:         defaultEdgeWidth,
		silenceTimeout:    defaultSilenceTimeout,
		attachmentTimeout: defaultAttachmentTimeout,

		fft: dsp.NewFFT[T](),

		tracer: new(trace.NoTracer),
	}
}

func (r *Receiver[T, F]) Start(sampleRate int, blockSize int) {
	if r.in != nil {
		return
	}

	r.in = make(chan []T, iqBufferSize)
	r.op = make(chan func())

	r.sampleRate = sampleRate
	r.blockSize = blockSize
	r.frequencyMapping = dsp.NewFrequencyMapping(r.sampleRate, r.blockSize, r.centerFrequency)

	r.textProcessor = NewTextProcessor(os.Stdout, r.clock, r)
	r.demodulator = cw.NewSpectralDemodulator[T, F](r.textProcessor, int(sampleRate), r.blockSize)
	r.demodulator.SetSignalThreshold(r.peakThreshold)
	r.demodulator.SetTracer(r.tracer)

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

func (r *Receiver[T, F]) SetSilenceTimeout(timeout time.Duration) {
	r.do(func() {
		r.silenceTimeout = timeout
	})
}

func (r *Receiver[T, F]) SetAttachmentTimeout(timeout time.Duration) {
	r.do(func() {
		r.attachmentTimeout = timeout
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
			peak := r.newPeakCenteredOnFrequency(freq)
			peak.SignalValue = 80
			r.attachDemodulator(&peak)

			r.tracer.Start()
		}
	})
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

func (r *Receiver[T, F]) ShowSpot(callsign string) {
	callsign = strings.ToUpper(callsign)
	r.indicator.ShowSpot(r.id, callsign, r.demodulator.Peak().SignalFrequency)
}

func (r *Receiver[T, F]) run() {
	var spectrum dsp.Block[T]
	var psd dsp.Block[T]
	var cumulation dsp.Block[T]
	var peaks []dsp.Peak[T, F]
	noiseFloorMean := dsp.NewRollingMean[T](noiseWindow)

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
				return dsp.MagnitudeIndB[T](fftValue, blockSize) + dBmShift
			}
			r.fft.IQToSpectrumAndPSD(spectrum, psd, frame, shiftedMagnitude)

			psdNoiseFloor := dsp.FindNoiseFloor(psd, r.edgeWidth)
			noiseFloor := noiseFloorMean.Put(dsp.PSDValueIndB(psdNoiseFloor, r.blockSize) + dBmShift)
			threshold := r.peakThreshold + noiseFloor

			if r.demodulator.Attached() {
				maxValue, _ := spectrum.Max(r.demodulator.PeakRange())

				r.demodulator.Tick(maxValue, noiseFloor)

				if r.mode == RandomPeakMode && r.demodulatorTimeoutExceeded() {
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
				if r.mode == RandomPeakMode && !r.demodulator.Attached() {
					peaks = dsp.FindPeaks(peaks, cumulation, cumulationSize, threshold, r.frequencyMapping)

					if len(peaks) > 0 {
						peakIndex := rand.Intn(len(peaks))
						peak := peaks[peakIndex]
						centeredPeak := r.newPeakCenteredOnSignal(peak)
						log.Printf("selected peak %#v -> %#v", peak, centeredPeak)

						r.attachDemodulator(&centeredPeak)
						r.tracer.Start()
					}
				}

				if r.tracer.Context() == traceSpectrum {
					r.tracer.TraceBlock(traceSpectrum, scaledValuesForTracing(cumulation, 1.0/float64(cumulationSize)))
					r.tracer.Trace(traceSpectrum, "meta;yThreshold;%v", threshold)

					if r.demodulator.Attached() {
						peak := r.demodulator.Peak()
						r.tracer.Trace(traceSpectrum, "meta;xSignalBin;%v", peak.SignalBin)
					} else {
						r.tracer.Trace(traceSpectrum, "meta;xSignalBin;%v", -1)
					}
				}

				clear(cumulation)
				cumulationCount = 0
			}
		}
	}
}

func scaledValuesForTracing[T dsp.Number](block dsp.Block[T], scale float64) []any {
	result := make([]any, len(block))
	for i := range result {
		result[i] = float64(block[i]) * scale
	}
	return result
}

func (r *Receiver[T, F]) newPeakCenteredOnSignal(peak dsp.Peak[T, F]) dsp.Peak[T, F] {
	result := r.newPeakCenteredOnBin(peak.SignalBin)
	result.SignalFrequency = peak.SignalFrequency
	result.SignalValue = peak.SignalValue
	result.SignalBin = peak.SignalBin
	return result
}

func (r *Receiver[T, F]) newPeakCenteredOnFrequency(frequency F) dsp.Peak[T, F] {
	bin := r.frequencyMapping.FrequencyToBin(frequency)
	result := r.newPeakCenteredOnBin(bin)
	result.SignalBin = bin
	result.SignalFrequency = frequency
	return result
}

func (r *Receiver[T, F]) newPeakCenteredOnBin(centerBin int) dsp.Peak[T, F] {
	peak := dsp.Peak[T, F]{
		From: max(0, centerBin-peakPadding),
		To:   min(centerBin+peakPadding, r.blockSize-1),
	}
	peak.FromFrequency = r.frequencyMapping.BinToFrequency(peak.From, dsp.BinFrom)
	peak.ToFrequency = r.frequencyMapping.BinToFrequency(peak.To, dsp.BinTo)
	peak.SignalFrequency = peak.CenterFrequency()

	return peak
}

func (r *Receiver[T, F]) attachDemodulator(peak *dsp.Peak[T, F]) {
	r.demodulator.Attach(peak)
	r.lastAttach = r.clock.Now()
	r.textProcessor.Reset()
	r.indicator.ShowDecode(r.id, *peak)
}

func (r *Receiver[T, F]) demodulatorTimeoutExceeded() bool {
	now := r.clock.Now()
	attachmentExceeded := now.Sub(r.lastAttach) > r.attachmentTimeout
	silenceExceeded := now.Sub(r.textProcessor.LastWrite()) > r.silenceTimeout
	if attachmentExceeded || silenceExceeded {
		log.Printf("timeout a: %v %t s: %v %t", r.attachmentTimeout, attachmentExceeded, r.silenceTimeout, silenceExceeded)
	}
	return attachmentExceeded || silenceExceeded
}

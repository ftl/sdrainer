package rx

import (
	"log"
	"os"
	"time"

	"github.com/ftl/sdrainer/dsp"
	"github.com/ftl/sdrainer/trace"
)

const (
	traceSpectrum = "spectrum"

	iqBufferSize   = 100
	cumulationSize = 100
	dBmShift       = 120
	peakPadding    = 0
	noiseWindow    = 30

	defaultPeakThreshold    = 15
	defaultEdgeWidth        = 70
	defaultListenerPoolSize = 30
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

func (c *manualClock) Add(d time.Duration) {
	c.now = c.now.Add(d)
}

type ReceiverMode string

const (
	VFOMode        ReceiverMode = "vfo"
	RandomPeakMode ReceiverMode = "random"
)

type ReceiverIndicator[T, F dsp.Number] interface {
	ListenerIndicator[T, F]

	ShowPeaks(receiver string, peaks []dsp.Peak[T, F])
}

type Receiver[T, F dsp.Number] struct {
	id        string
	mode      ReceiverMode
	clock     Clock
	indicator ReceiverIndicator[T, F]

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
	peaks            *PeaksTable[T, F]

	listeners         *ListenerPool[T, F]
	silenceTimeout    time.Duration
	attachmentTimeout time.Duration

	tracer trace.Tracer
}

func NewReceiver[T, F dsp.Number](id string, mode ReceiverMode, clock Clock, indicator ReceiverIndicator[T, F]) *Receiver[T, F] {
	if clock == nil {
		clock = WallClock
	}
	result := &Receiver[T, F]{
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
	result.listeners = NewListenerPool[T, F](defaultListenerPoolSize, result.id, result.newListener)
	return result
}

func (r *Receiver[T, F]) newListener(id string) *Listener[T, F] {
	// TODO handle the output properly instead of hardcoding os.Stdout
	result := NewListener[T, F](id, os.Stdout, r.clock, r.indicator, r.sampleRate, r.blockSize)
	result.SetAttachmentTimeout(r.attachmentTimeout)
	result.SetSilenceTimeout(r.silenceTimeout)
	result.SetSignalThreshold(r.peakThreshold)
	result.SetTracer(r.tracer)
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
	r.frequencyMapping = dsp.NewFrequencyMapping(r.sampleRate, r.blockSize, r.centerFrequency)
	r.peaks = NewPeaksTable[T, F](r.blockSize, r.clock)

	go r.run()
}

func (r *Receiver[T, F]) Stop() {
	if r.in == nil {
		return
	}

	r.listeners.Reset()

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
		r.listeners.ForEach(func(l *Listener[T, F]) {
			l.SetTracer(tracer)
		})
	})
}

func (r *Receiver[T, F]) SetPeakThreshold(threshold T) {
	r.do(func() {
		r.peakThreshold = threshold
		r.listeners.ForEach(func(l *Listener[T, F]) {
			l.SetSignalThreshold(threshold)
		})
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
		r.listeners.ForEach(func(l *Listener[T, F]) {
			l.SetSilenceTimeout(timeout)
		})
	})
}

func (r *Receiver[T, F]) SetAttachmentTimeout(timeout time.Duration) {
	r.do(func() {
		r.attachmentTimeout = timeout
		r.listeners.ForEach(func(l *Listener[T, F]) {
			l.SetAttachmentTimeout(timeout)
		})
	})
}

func (r *Receiver[T, F]) SetSignalDebounce(debounce int) {
	r.do(func() {
		r.listeners.ForEach(func(l *Listener[T, F]) {
			l.SetSignalDebounce(debounce)
		})
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
		// if r.mode == VFOMode {
		// TODO implement the VFO mode using the ListenerPool
		// if r.listener.Attached() {
		// 	r.peaks.Deactivate(r.listener.Peak()) // beware of temporal coupling!
		// 	r.listener.Detach()
		// }

		// freq := r.vfoOffset + r.centerFrequency
		// peak := r.newPeakCenteredOnFrequency(freq)
		// peak.SignalValue = 80
		// r.peaks.ForcePut(&peak)
		// r.peaks.Activate(&peak)
		// r.listener.Attach(&peak)

		// r.tracer.Start()
		// }
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

func (r *Receiver[T, F]) run() {
	var spectrum dsp.Block[T]
	var psd dsp.Block[T]
	var cumulation dsp.Block[T]
	var peaks []dsp.Peak[T, F]
	noiseFloorMean := dsp.NewRollingMean[T](noiseWindow)

	cumulationCount := 0

	peakTicker := time.NewTicker(5 * time.Second)
	defer peakTicker.Stop()

	cleanupTicker := time.NewTicker(1 * time.Second)
	defer cleanupTicker.Stop()

	detachedListeners := make([]*Listener[T, F], r.listeners.Size())

	for {
		select {
		case op := <-r.op:
			op()
		case <-peakTicker.C:
			// peaksToShow := make([]dsp.peak[T, int], len(peaks))
			// copy(peaksToShow, peaks)
			// r.indicator.ShowPeaks(r.id, peaksToShow)
		case <-cleanupTicker.C:
			r.listeners.ForEach(func(l *Listener[T, F]) {
				l.CheckWriteTimeout()
			})
			r.peaks.Cleanup()
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

			detachedListeners = detachedListeners[:0]
			r.listeners.ForEach(func(l *Listener[T, F]) {
				if !l.Attached() {
					return
				}
				maxValue, _ := spectrum.Max(l.PeakRange())

				l.Listen(maxValue, noiseFloor)

				if r.mode == RandomPeakMode && l.TimeoutExceeded() {
					r.peaks.Deactivate(l.Peak()) // beware of temporal coupling!
					l.Detach()
					detachedListeners = append(detachedListeners, l)
					// r.tracer.Stop() // TODO handle tracing
				}
			})
			r.listeners.Release(detachedListeners...)

			for i := range spectrum {
				cumulation[i] += spectrum[i]
			}
			cumulationCount++

			if cumulationCount == cumulationSize {
				if r.mode == RandomPeakMode && r.listeners.Available() {
					peaks = dsp.FindPeaks(peaks, cumulation, cumulationSize, threshold, r.frequencyMapping)

					for _, p := range peaks {
						centeredPeak := r.newPeakCenteredOnSignal(p)
						r.peaks.Put(&centeredPeak)
					}

					selectedPeak := r.peaks.FindNext()
					if selectedPeak != nil {
						log.Printf("selected peak %#v", selectedPeak)

						listener, ok := r.listeners.BindNext()
						if ok {
							r.peaks.Activate(selectedPeak)
							listener.Attach(selectedPeak)
						}
						// r.tracer.Start() // TODO handle tracing
					}
				}

				// TODO handle tracing
				// if r.tracer.Context() == traceSpectrum {
				// 	r.tracer.TraceBlock(traceSpectrum, scaledValuesForTracing(cumulation, 1.0/float64(cumulationSize)))
				// 	r.tracer.Trace(traceSpectrum, "meta;yThreshold;%v", threshold)

				// 	if r.listener.Attached() {
				// 		r.tracer.Trace(traceSpectrum, "meta;xSignalBin;%v", r.listener.SignalBin())
				// 	} else {
				// 		r.tracer.Trace(traceSpectrum, "meta;xSignalBin;%v", -1)
				// 	}
				// }

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

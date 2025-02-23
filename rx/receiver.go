package rx

import (
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"time"

	"github.com/ftl/sdrainer/dsp"
	"github.com/ftl/sdrainer/scope"
)

const (
	scopeSpectrum = "spectrum"

	iqBufferSize   = 100
	cumulationSize = 100
	dBmShift       = 120
	peakPadding    = 0
	noiseWindow    = 60

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
	DecodeMode ReceiverMode = "decode"
	StrainMode ReceiverMode = "strain"
)

type Receiver[T, F dsp.Number] struct {
	id        string
	mode      ReceiverMode
	clock     Clock
	reporters []Reporter[F]

	peakThreshold   T
	edgeWidth       int
	sampleRate      int
	blockSize       int
	centerFrequency F
	vfoOffset       F

	in               chan []T
	op               chan func()
	stop             chan struct{}
	stopped          chan struct{}
	fft              *dsp.FFT[T]
	frequencyMapping *dsp.FrequencyMapping[F]
	peaks            *PeaksTable[T, F]
	out              *ChannelWriter

	listeners         *ListenerPool[T, F]
	silenceTimeout    time.Duration
	attachmentTimeout time.Duration

	scope scope.Scope
}

func NewReceiver[T, F dsp.Number](id string, mode ReceiverMode, clock Clock) *Receiver[T, F] {
	if clock == nil {
		clock = WallClock
	}
	result := &Receiver[T, F]{
		id:    id,
		mode:  mode,
		clock: clock,

		peakThreshold:     defaultPeakThreshold,
		edgeWidth:         defaultEdgeWidth,
		silenceTimeout:    defaultSilenceTimeout,
		attachmentTimeout: defaultAttachmentTimeout,

		fft: dsp.NewFFT[T](),
		out: NewChannelWriter(os.Stdout),

		scope: scope.NewNullScope(),
	}

	listenerPoolSize := defaultListenerPoolSize
	if mode == DecodeMode {
		listenerPoolSize = 1
	}
	result.listeners = NewListenerPool[T, F](listenerPoolSize, result.id, result.newListener)

	return result
}

func (r *Receiver[T, F]) newListener(id string) *Listener[T, F] {
	result := NewListener[T, F](id, r.out.Channel(id), r.clock, r, r.sampleRate, r.blockSize)
	result.SetAttachmentTimeout(r.attachmentTimeout)
	result.SetSilenceTimeout(r.silenceTimeout)
	result.SetScope(r.scope)
	return result
}

func (r *Receiver[T, F]) Start(sampleRate int, blockSize int) {
	if r.in != nil {
		return
	}

	r.stop = make(chan struct{})
	r.stopped = make(chan struct{})
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

	close(r.stop)
	<-r.stopped
	close(r.in)
	close(r.op)

	r.stop = nil
	r.stopped = nil
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

func (r *Receiver[T, F]) AddReporter(reporter Reporter[F]) {
	r.reporters = append(r.reporters, reporter)
}

func (r *Receiver[T, F]) ListenerActivated(listener string, frequency F) {
	for _, reporter := range r.reporters {
		reporter.ListenerActivated(listener, frequency)
	}
}

func (r *Receiver[T, F]) ListenerDeactivated(listener string, frequency F) {
	for _, reporter := range r.reporters {
		reporter.ListenerDeactivated(listener, frequency)
	}
}

func (r *Receiver[T, F]) CallsignDecoded(listener string, callsign string, frequency F, count int, weight int) {
	for _, reporter := range r.reporters {
		reporter.CallsignDecoded(listener, callsign, frequency, count, weight)
	}
}

func (r *Receiver[T, F]) CallsignSpotted(listener string, callsign string, frequency F) {
	for _, reporter := range r.reporters {
		reporter.CallsignSpotted(listener, callsign, frequency)
	}
}

func (r *Receiver[T, F]) SpotTimeout(listener string, callsign string, frequency F) {
	for _, reporter := range r.reporters {
		reporter.SpotTimeout(listener, callsign, frequency)
	}
}

func (r *Receiver[T, F]) SetScope(scope scope.Scope) {
	r.do(func() {
		r.scope = scope
		r.listeners.ForEach(func(l *Listener[T, F]) {
			l.SetScope(scope)
		})
	})
}

func (r *Receiver[T, F]) SetPeakThreshold(threshold T) {
	r.do(func() {
		r.peakThreshold = threshold
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
		if r.in == nil {
			return
		}

		switch r.mode {
		case DecodeMode:
			if !r.listeners.Available() {
				r.listeners.Reset()
			}
			listener, ok := r.listeners.BindNext()
			if !ok {
				log.Printf("cannot bind listener to VFO")
				return
			}

			freq := r.vfoOffset + r.centerFrequency
			peak := r.newPeakCenteredOnFrequency(freq)
			peak.SignalValue = 80
			r.peaks.ForcePut(&peak)
			r.peaks.Activate(&peak)
			listener.Attach(&peak)
			r.out.SetActive(listener.ID())
		case StrainMode:
			freq := r.vfoOffset + r.centerFrequency
			bin := r.frequencyMapping.FrequencyToBin(freq)
			found := false
			r.out.SetActive("")
			r.listeners.ForEach(func(l *Listener[T, F]) {
				if l.Peak().ContainsBin(bin) {
					r.out.SetActive(l.ID())
					found = true
				}
			})
			if found {
				fmt.Fprintln(r.out)
			}
		}
	})
}

func (r *Receiver[T, F]) IQData(sampleRate int, data []T) {
	if r.in == nil {
		return
	}
	if r.sampleRate != sampleRate {
		log.Printf("wrong incoming sample rate on receiver %s: %d instead of %d!", r.id, sampleRate, r.sampleRate)
		return
	}
	if r.blockSize != len(data)/2 {
		log.Printf("wrong incoming block size on receiver %s: %d instead of %d", r.id, len(data), r.blockSize)
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
	defer close(r.stopped)

	var spectrum dsp.Block[T]
	var psd dsp.Block[T]
	var cumulation dsp.Block[T]
	var peaks []dsp.Peak[T, F]
	noiseFloorMean := dsp.NewRollingMean[T](noiseWindow)
	noiseDeviationMean := dsp.NewRollingMean[T](noiseWindow)

	cumulationCount := 0

	cleanupTicker := time.NewTicker(1 * time.Second)
	defer cleanupTicker.Stop()

	detachedListeners := make([]*Listener[T, F], r.listeners.Size())

	for {
		select {
		case <-r.stop:
			return
		case op := <-r.op:
			op()
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

			psdNoiseFloor, noiseVariance := dsp.FindNoiseFloor(psd, r.edgeWidth)
			// log.Printf("noise variance %f %f", dsp.PSDValueIndB(T(noiseVariance), r.blockSize), dsp.PSDValueIndB(T(math.Sqrt(noiseVariance)), r.blockSize)+dBmShift)
			noiseDeviation := noiseDeviationMean.Put(T(float64(dsp.PSDValueIndB(T(math.Sqrt(noiseVariance)), r.blockSize)+dBmShift) * 0.25))
			noiseFloor := noiseFloorMean.Put(dsp.PSDValueIndB(psdNoiseFloor, r.blockSize) + dBmShift)
			peakThreshold := r.peakThreshold + noiseFloor

			detachedListeners = detachedListeners[:0]
			r.listeners.ForEach(func(l *Listener[T, F]) {
				if !l.Attached() {
					return
				}

				signalValue := spectrum[l.SignalBin()]
				l.Listen(signalValue, noiseFloor+noiseDeviation)

				if r.mode == StrainMode && l.TimeoutExceeded() {
					r.peaks.Deactivate(l.Peak()) // beware of temporal coupling!
					l.Detach()
					detachedListeners = append(detachedListeners, l)
				}
			})
			r.listeners.Release(detachedListeners...)

			for i := range spectrum {
				cumulation[i] += spectrum[i]
			}
			cumulationCount++

			if cumulationCount == cumulationSize {
				if r.mode == StrainMode && r.listeners.Available() {
					peaks = dsp.FindPeaks(peaks, cumulation, cumulationSize, peakThreshold, r.frequencyMapping)

					for _, p := range peaks {
						centeredPeak := r.newPeakCenteredOnSignal(p)
						r.peaks.Put(&centeredPeak)
					}

					selectedPeak := r.peaks.FindNext()
					if selectedPeak != nil {
						listener, ok := r.listeners.BindNext()
						if ok {
							r.peaks.Activate(selectedPeak)
							listener.Attach(selectedPeak)
						}
					}
				}

				if r.scope.Active() {
					signalBin := -1
					switch r.mode {
					case DecodeMode:
						r.listeners.ForEach(func(l *Listener[T, F]) {
							signalBin = l.Peak().SignalBin
						})
					case StrainMode:
						listener, ok := r.listeners.First()
						if ok {
							signalBin = listener.Peak().SignalBin
						}
					}

					r.scope.ShowSpectralFrame(&scope.SpectralFrame{
						Frame: scope.Frame{
							Stream:    scopeSpectrum,
							Timestamp: time.Now(),
						},
						FromFrequency: 0,
						ToFrequency:   1,
						Values:        scaledValuesForScope(cumulation, 1.0/float64(cumulationSize)),
						FrequencyMarkers: map[scope.MarkerID]float64{
							"signal_bin": float64(signalBin),
						},
						MagnitudeMarkers: map[scope.MarkerID]float64{
							"threshold": float64(peakThreshold),
						},
					})
				}

				clear(cumulation)
				cumulationCount = 0
			}
		}
	}
}

func scaledValuesForScope[T dsp.Number](block dsp.Block[T], scale float64) []float64 {
	result := make([]float64, len(block))
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

type WriterFunc func([]byte) (int, error)

func (f WriterFunc) Write(bytes []byte) (int, error) {
	return f(bytes)
}

type ChannelWriter struct {
	out           io.Writer
	activeChannel string
}

func NewChannelWriter(out io.Writer) *ChannelWriter {
	return &ChannelWriter{
		out: out,
	}
}

func (w *ChannelWriter) Write(bytes []byte) (int, error) {
	return w.out.Write(bytes)
}

func (w *ChannelWriter) write(channel string, bytes []byte) (int, error) {
	if channel != w.activeChannel {
		// ignore everything, except data for the active channel
		return len(bytes), nil
	}
	return w.Write(bytes)
}

func (w *ChannelWriter) Channel(channel string) WriterFunc {
	return func(bytes []byte) (int, error) {
		return w.write(channel, bytes)
	}
}

func (w *ChannelWriter) SetActive(channel string) {
	w.activeChannel = channel
}

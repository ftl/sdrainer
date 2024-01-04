package tci

import (
	"log"
	"os"

	"github.com/ftl/sdrainer/cw"
	"github.com/ftl/sdrainer/dsp"
	"github.com/ftl/sdrainer/trace"
)

const (
	traceSignal = "signal"

	silenceTimeout = 400

	defaultSignalThreshold = 15
	defaultSignalDebounce  = 1
)

type decoder[T, F dsp.Number] struct {
	signalThreshold T

	signalDebouncer *dsp.BoolDebouncer
	decoder         *cw.Decoder
	tracer          trace.Tracer

	peak     *dsp.Peak[T, F]
	lowTicks int
}

func newDecoder[T, F dsp.Number](sampleRate int, blockSize int) *decoder[T, F] {
	result := &decoder[T, F]{
		signalThreshold: T(defaultSignalThreshold),
		signalDebouncer: dsp.NewBoolDebouncer(defaultSignalDebounce),
		decoder:         cw.NewDecoder(os.Stdout, sampleRate, blockSize),
		tracer:          new(trace.NoTracer),
	}
	result.reset()

	return result
}

func (d *decoder[T, F]) reset() {
	d.lowTicks = 0
}

func (d *decoder[T, F]) SetSignalThreshold(threshold T) {
	d.signalThreshold = threshold
}

func (d *decoder[T, F]) SetSignalDebounce(debounce int) {
	d.signalDebouncer.SetThreshold(debounce)
}

func (d *decoder[T, F]) SetTracer(tracer trace.Tracer) {
	d.tracer = tracer
	d.decoder.SetTracer(tracer)
}

func (d *decoder[T, F]) Attach(peak *dsp.Peak[T, F]) {
	d.peak = peak
	d.reset()
	log.Printf("\ndecoding at %v (%d - %d)\n", peak.CenterFrequency(), peak.From, peak.To)
}

func (d *decoder[T, F]) Attached() bool {
	return d.peak != nil
}

func (d *decoder[T, F]) Detach() {
	d.peak = nil
	d.decoder.Reset()
	log.Printf("\ndecoding stopped\n")
}

func (d *decoder[T, F]) PeakRange() (int, int) {
	if !d.Attached() {
		return 0, 0
	}
	return d.peak.From, d.peak.To
}

func (d *decoder[T, F]) TimeoutExceeded() bool {
	return d.lowTicks > silenceTimeout
}

func (d *decoder[T, F]) Tick(value T, noiseFloor T) {
	if !d.Attached() {
		return
	}

	threshold := d.signalThreshold + noiseFloor
	state := value > threshold
	debounced := d.signalDebouncer.Debounce(state)

	d.decoder.Tick(debounced)

	stateInt := -1
	_ = stateInt
	if debounced {
		stateInt = 80
	}
	d.tracer.Trace(traceSignal, "%f;%f;%f;%d\n", noiseFloor, threshold, value, stateInt)

	if debounced {
		d.lowTicks = 0
	} else {
		d.lowTicks++
	}
}

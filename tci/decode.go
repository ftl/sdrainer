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

	defaultSignalThreshold float32 = 15

	defaultSignalDebounce = 1
)

type decoder struct {
	signalThreshold float32

	signalDebouncer *dsp.BoolDebouncer
	decoder         *cw.Decoder
	tracer          trace.Tracer

	peak     *peak
	lowTicks int
}

func newDecoder(sampleRate int, blockSize int) *decoder {
	result := &decoder{
		signalThreshold: defaultSignalThreshold,
		signalDebouncer: dsp.NewBoolDebouncer(defaultSignalDebounce),
		decoder:         cw.NewDecoder(os.Stdout, sampleRate, blockSize),
		tracer:          new(trace.NoTracer),
	}
	result.reset()

	return result
}

func (d *decoder) reset() {
	d.lowTicks = 0
}

func (d *decoder) SetSignalThreshold(threshold float32) {
	d.signalThreshold = threshold
}

func (d *decoder) SetSignalDebounce(debounce int) {
	d.signalDebouncer.SetThreshold(debounce)
}

func (d *decoder) SetTracer(tracer trace.Tracer) {
	d.tracer = tracer
	d.decoder.SetTracer(tracer)
}

func (d *decoder) Attach(peak *peak) {
	d.peak = peak
	d.reset()
	log.Printf("\ndecoding at %d (%d - %d)\n", peak.CenterFrequency(), peak.from, peak.to)
}

func (d *decoder) Attached() bool {
	return d.peak != nil
}

func (d *decoder) Detach() {
	d.peak = nil
	d.decoder.Reset()
	log.Printf("\ndecoding stopped\n")
}

func (d *decoder) PeakRange() (int, int) {
	if !d.Attached() {
		return 0, 0
	}
	return d.peak.from, d.peak.to
}

func (d *decoder) TimeoutExceeded() bool {
	return d.lowTicks > silenceTimeout
}

func (d *decoder) Tick(value float32, noiseFloor float32) {
	if !d.Attached() {
		return
	}

	threshold := d.signalThreshold + noiseFloor
	state := value > threshold
	debounced := d.signalDebouncer.Debounce(state)

	d.decoder.Tick(debounced)

	stateInt := -1.0
	_ = stateInt
	if debounced {
		stateInt = 80
	}

	d.tracer.Trace(traceSignal, "%f;%f;%f;%f\n", noiseFloor, threshold, value, stateInt) // TODO remove tracing

	if debounced {
		d.lowTicks = 0
	} else {
		d.lowTicks++
	}
}

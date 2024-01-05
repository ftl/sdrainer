package cw

import (
	"io"
	"log"

	"github.com/ftl/sdrainer/dsp"
	"github.com/ftl/sdrainer/trace"
)

const (
	traceDemod = "demod"

	silenceTimeout = 400

	defaultSignalThreshold = 15
	defaultSignalDebounce  = 1
)

// SpectralDemodulator demodulates a CW signal detected in a spectral representation of the frequency domain.
// M is used to represent magnitude values, F is used to represent frequency values.
type SpectralDemodulator[M, F dsp.Number] struct {
	signalThreshold M

	signalDebouncer *dsp.BoolDebouncer
	decoder         *Decoder
	tracer          trace.Tracer

	peak     *dsp.Peak[M, F]
	lowTicks int
}

func NewSpectralDemodulator[M, F dsp.Number](out io.Writer, sampleRate int, blockSize int) *SpectralDemodulator[M, F] {
	result := &SpectralDemodulator[M, F]{
		signalThreshold: defaultSignalThreshold,
		signalDebouncer: dsp.NewBoolDebouncer(defaultSignalDebounce),
		decoder:         NewDecoder(out, sampleRate, blockSize),
		tracer:          new(trace.NoTracer),
	}
	result.Reset()

	return result
}

func (d *SpectralDemodulator[M, F]) Reset() {
	d.lowTicks = 0
}

func (d *SpectralDemodulator[M, F]) SetSignalThreshold(threshold M) {
	d.signalThreshold = threshold
}

func (d *SpectralDemodulator[M, F]) SetSignalDebounce(debounce int) {
	d.signalDebouncer.SetThreshold(debounce)
}

func (d *SpectralDemodulator[M, F]) SetTracer(tracer trace.Tracer) {
	d.tracer = tracer
	d.decoder.SetTracer(tracer)
}

func (d *SpectralDemodulator[M, F]) Attach(peak *dsp.Peak[M, F]) {
	d.peak = peak
	d.Reset()
	log.Printf("\ndemodulating at %v (%d - %d)\n", peak.CenterFrequency(), peak.From, peak.To)
}

func (d *SpectralDemodulator[M, F]) Attached() bool {
	return d.peak != nil
}

func (d *SpectralDemodulator[M, F]) Detach() {
	d.peak = nil
	d.decoder.Reset()
	log.Printf("\ndemodulation stopped\n")
}

func (d *SpectralDemodulator[M, F]) PeakRange() (int, int) {
	if !d.Attached() {
		return 0, 0
	}
	return d.peak.From, d.peak.To
}

func (d *SpectralDemodulator[M, F]) TimeoutExceeded() bool {
	return d.lowTicks > silenceTimeout
}

func (d *SpectralDemodulator[M, F]) Tick(value M, noiseFloor M) {
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
	d.tracer.Trace(traceDemod, "%f;%f;%f;%d\n", noiseFloor, threshold, value, stateInt)

	if debounced {
		d.lowTicks = 0
	} else {
		d.lowTicks++
	}
}

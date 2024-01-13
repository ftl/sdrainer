package cw

import (
	"io"

	"github.com/ftl/sdrainer/dsp"
	"github.com/ftl/sdrainer/trace"
)

const (
	traceDemod = "demod"

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
}

func NewSpectralDemodulator[M, F dsp.Number](out io.Writer, sampleRate int, blockSize int) *SpectralDemodulator[M, F] {
	result := &SpectralDemodulator[M, F]{
		signalThreshold: defaultSignalThreshold,
		signalDebouncer: dsp.NewBoolDebouncer(defaultSignalDebounce),
		decoder:         NewDecoder(out, sampleRate, blockSize),
		tracer:          new(trace.NoTracer),
	}

	return result
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

func (d *SpectralDemodulator[M, F]) Reset() {
	d.decoder.Reset()
}

func (d *SpectralDemodulator[M, F]) Tick(value M, noiseFloor M) {
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
}

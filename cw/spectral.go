package cw

import (
	"io"
	"time"

	"github.com/ftl/sdrainer/dsp"
	"github.com/ftl/sdrainer/scope"
)

const (
	scopeDemod = "demod"

	defaultSignalDebounce = 1
)

// SpectralDemodulator demodulates a CW signal detected in a spectral representation of the frequency domain.
// M is used to represent magnitude values, F is used to represent frequency values.
type SpectralDemodulator[M, F dsp.Number] struct {
	signalDebouncer *dsp.BoolDebouncer
	decoder         *Decoder
	scope           scope.Scope
}

func NewSpectralDemodulator[M, F dsp.Number](out io.Writer, sampleRate int, blockSize int) *SpectralDemodulator[M, F] {
	result := &SpectralDemodulator[M, F]{
		signalDebouncer: dsp.NewBoolDebouncer(defaultSignalDebounce),
		decoder:         NewDecoder(out, sampleRate, blockSize),
		scope:           scope.NewNullScope(),
	}

	return result
}

func (d *SpectralDemodulator[M, F]) SetSignalDebounce(debounce int) {
	d.signalDebouncer.SetThreshold(debounce)
}

func (d *SpectralDemodulator[M, F]) SetScope(scope scope.Scope) {
	d.scope = scope
	d.decoder.SetScope(scope)
}

func (d *SpectralDemodulator[M, F]) Reset() {
	d.decoder.Reset()
}

func (d *SpectralDemodulator[M, F]) Tick(value M, threshold M) {
	state := value > threshold
	debounced := d.signalDebouncer.Debounce(state)

	d.decoder.Tick(debounced)
	d.scopeDemod(threshold, value, state, debounced)
}

func (d *SpectralDemodulator[M, F]) scopeDemod(threshold M, value M, state bool, debounced bool) {
	if !d.scope.Active() {
		return
	}

	stateInt := -1
	if state {
		stateInt = 100
	}
	debouncedInt := -1
	if debounced {
		debouncedInt = 80
	}
	d.scope.ShowTimeFrame(&scope.TimeFrame{
		Frame: scope.Frame{
			Stream:    scopeDemod,
			Timestamp: time.Now(),
		},
		Values: map[scope.ChannelID]float64{
			"threshold": float64(threshold),
			"value":     float64(value),
			"state":     float64(stateInt),
			"debounced": float64(debouncedInt),
		},
	})
}

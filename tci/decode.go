package tci

import (
	"log"
	"os"

	"github.com/ftl/sdrainer/cw"
	"github.com/ftl/sdrainer/dsp"
)

const (
	signalThreshold float32 = 15

	silenceTimeout           = 400
	silenceThreshold float32 = 15
	minThreshold     float32 = 0
	readjustAfter            = 20
	meanWindow               = 3

	defaultSignalDebounceThreshold = 1
)

type decoder struct {
	signalDebouncer *dsp.BoolDebouncer
	decoder         *cw.Decoder
	tracer          tracer

	peak     *peak
	lowTicks int
}

func newDecoder(sampleRate int, blockSize int) *decoder {
	result := &decoder{
		signalDebouncer: dsp.NewBoolDebouncer(defaultSignalDebounceThreshold),
		decoder:         cw.NewDecoder(os.Stdout, sampleRate, blockSize),
	}
	result.reset()

	return result
}

func (d *decoder) reset() {
	d.lowTicks = 0
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

	threshold := signalThreshold + noiseFloor
	state := value > threshold
	debounced := d.signalDebouncer.Debounce(state)

	d.decoder.Tick(debounced)

	stateInt := -1.0
	_ = stateInt
	if debounced {
		stateInt = 80
	}

	if d.tracer != nil {
		d.tracer.Trace("%f;%f;%f;%f\n", noiseFloor, threshold, value, stateInt) // TODO remove tracing
	}

	if debounced {
		d.lowTicks = 0
	} else {
		d.lowTicks++
	}
}

package tci

import (
	"log"
	"math"
	"os"

	"github.com/ftl/sdrainer/cw"
	"github.com/ftl/sdrainer/dsp"
)

const (
	silenceTimeout            = 300
	silenceThreshold  float32 = 1
	signalThreshold   float32 = 0.6
	readjustAfter             = 100
	readjustingPeriod         = 25

	defaultDebounceThreshold = 2
)

type tracer interface {
	trace(string, ...any)
}

type decoder struct {
	debouncer *dsp.BoolDebouncer
	decoder   *cw.Decoder
	tracer    tracer

	peak           *peak
	lowTicks       int
	maxValue       float32
	minValue       float32
	delta          float32
	threshold      float32
	maxAdjustTicks int
}

func newDecoder(sampleRate int, blockSize int) *decoder {
	result := &decoder{
		debouncer: dsp.NewBoolDebouncer(defaultDebounceThreshold),
		decoder:   cw.NewDecoder(os.Stdout, sampleRate, blockSize),
	}
	result.reset()

	return result
}

func (d *decoder) reset() {
	d.maxValue = 0
	d.minValue = math.MaxFloat32
	d.lowTicks = 0
}

func (d *decoder) adjust(value float32) {
	d.maxAdjustTicks++
	if d.maxAdjustTicks > readjustAfter {
		d.maxValue = value
		d.maxAdjustTicks = 0
	} else if d.maxValue < value {
		d.maxValue = value
	}
	if d.minValue > value {
		d.minValue = value
	}
	d.delta = d.maxValue - d.minValue

	if d.delta > silenceThreshold {
		d.threshold = signalThreshold*d.delta + d.minValue
	} else {
		d.threshold = signalThreshold*d.delta + d.maxValue
	}

	if d.tracer != nil {
		d.tracer.trace("%f;%f;%f;%f;%f\n", d.maxValue, d.minValue, d.delta, d.threshold, value) // TODO remove tracing
	}
}

func (d *decoder) readjusting() bool {
	return d.maxAdjustTicks < readjustingPeriod
}

func (d *decoder) isHigh(value float32) bool {
	return (d.delta > silenceThreshold) && (value > d.threshold) && (d.maxValue > d.threshold)
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

func (d *decoder) PeakRange() (int, int, float32) {
	if !d.Attached() {
		return 0, 0, 0
	}
	return d.peak.from, d.peak.to, d.peak.max
}

func (d *decoder) TimeoutExceeded() bool {
	return d.lowTicks > silenceTimeout
}

func (d *decoder) Tick(value float32) {
	if !d.Attached() {
		return
	}

	d.adjust(value)

	state := d.isHigh(value)
	debounced := d.debouncer.Debounce(state)

	d.decoder.Tick(debounced)

	// log.Printf("debounced: %t value: %f threshold: %f delta: %f lowTicks: %d", debounced, value, d.threshold, d.delta, d.lowTicks)

	if debounced {
		d.lowTicks = 0
	} else if !d.readjusting() {
		d.lowTicks++
	}
}

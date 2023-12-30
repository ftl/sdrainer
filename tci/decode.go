package tci

import (
	"log"
	"math"
	"os"

	"github.com/ftl/sdrainer/cw"
	"github.com/ftl/sdrainer/dsp"
)

const (
	silenceTimeout            = 400
	silenceThreshold  float32 = 20
	minThreshold      float32 = 0
	signalThreshold   float32 = 0.6
	readjustAfter             = 100
	readjustingPeriod         = 25
	meanWindow                = 3

	defaultSignalDebounceThreshold = 1
)

type decoder struct {
	signalDebouncer *dsp.BoolDebouncer
	decoder         *cw.Decoder
	tracer          tracer

	peak           *peak
	lowTicks       int
	maxValue       [2]float32
	minValue       [2]float32
	boundsIndex    int
	delta          float32
	threshold      float32
	maxAdjustTicks int
	mean           *dsp.RollingMean[float32]
}

func newDecoder(sampleRate int, blockSize int) *decoder {
	result := &decoder{
		signalDebouncer: dsp.NewBoolDebouncer(defaultSignalDebounceThreshold),
		decoder:         cw.NewDecoder(os.Stdout, sampleRate, blockSize),
		mean:            dsp.NewRollingMean[float32](meanWindow),
	}
	result.reset()

	return result
}

func (d *decoder) reset() {
	d.maxValue[0] = 0
	d.maxValue[1] = 0
	d.minValue[0] = math.MaxFloat32
	d.minValue[1] = math.MaxFloat32
	d.boundsIndex = 0
	d.lowTicks = 0
	d.mean.Reset()
}

func (d *decoder) adjust(value float32) {
	mean := d.mean.Put(value)

	d.maxAdjustTicks++
	if d.maxAdjustTicks > readjustAfter {
		d.maxValue[d.boundsIndex] = 0
		d.minValue[d.boundsIndex] = math.MaxFloat32
		d.boundsIndex = (d.boundsIndex + 1) % 2
		d.maxAdjustTicks = 0
	}

	otherBoundsIndex := (d.boundsIndex + 1) % 2
	if d.maxValue[otherBoundsIndex] < mean {
		d.maxValue[otherBoundsIndex] = mean
	}
	if d.minValue[otherBoundsIndex] > value {
		d.minValue[otherBoundsIndex] = value
	}
	if d.maxValue[d.boundsIndex] < mean {
		d.maxValue[d.boundsIndex] = mean
	}
	if d.minValue[d.boundsIndex] > value {
		d.minValue[d.boundsIndex] = value
	}

	d.delta = d.maxValue[d.boundsIndex] - d.minValue[d.boundsIndex]
	if d.delta > silenceThreshold {
		d.threshold = max(minThreshold, signalThreshold*d.delta+d.minValue[d.boundsIndex])
	} else {
		d.threshold = max(minThreshold, signalThreshold*d.delta+d.maxValue[d.boundsIndex])
	}
}

func (d *decoder) readjusting() bool {
	return d.maxAdjustTicks < readjustingPeriod
}

func (d *decoder) isHigh(value float32) bool {
	return (value > d.threshold)
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

func (d *decoder) Tick(value float32) {
	if !d.Attached() {
		return
	}

	d.adjust(value)

	state := d.isHigh(value)
	debounced := d.signalDebouncer.Debounce(state)

	d.decoder.Tick(debounced)

	stateInt := -1.0
	_ = stateInt
	if debounced {
		stateInt = 0.8
	}

	if d.tracer != nil {
		d.tracer.Trace("%f;%f;%f;%f;%f\n", d.delta, d.threshold, d.maxValue[d.boundsIndex], value, stateInt) // TODO remove tracing
	}

	if debounced {
		d.lowTicks = 0
	} else if !d.readjusting() {
		d.lowTicks++
	}
}

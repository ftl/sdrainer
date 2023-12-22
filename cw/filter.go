package cw

import (
	"fmt"
	"math"
	"time"
)

/*

The following is an implementation of a CW decoder based on the Goertzel algorithm. It is based
on OZ1JHM's implementation for the Arduino.

See also:
* https://www.embedded.com/the-goertzel-algorithm/
* https://www.embedded.com/single-tone-detection-with-the-goertzel-algorithm/
* http://www.oz1jhm.dk/sites/default/files/decoder11.ino
* https://github.com/G6EJD/ESP32-Morse-Decoder/blob/master/ESP32_Morse_Code_Decoder_02.ino

*/

// blocksizeRatio = blocksize / sampleRate
// this is also the duration in seconds that is covered by one filter block
const blocksizeRatio = 0.01

type filterBlock []float32

type filter struct {
	pitch      float64
	sampleRate int

	blocksize int
	coeff     float64

	magnitudeLimitLow  float64
	magnitudeLimit     float64
	magnitudeThreshold float64
}

func newFilter(pitch float64, sampleRate int) *filter {
	minBlocksize := math.Round(float64(sampleRate) / pitch)
	blocksize := int(math.Round((blocksizeRatio*float64(sampleRate))/minBlocksize)) * int(minBlocksize)

	binIndex := int(0.5 + (float64(blocksize) * pitch / float64(sampleRate)))
	var omega float64 = 2 * math.Pi * float64(binIndex) / float64(blocksize)

	return &filter{
		pitch:      pitch,
		sampleRate: sampleRate,

		blocksize: blocksize,
		coeff:     2 * math.Cos(omega),

		magnitudeLimitLow:  float64(blocksize) / 2, // this is a guesstimation, I just saw that the magnitude values depend on the blocksize
		magnitudeThreshold: 0.6,
	}
}

func (f *filter) magnitude(block filterBlock) float64 {
	var q0, q1, q2 float64
	for _, sample := range block {
		q0 = f.coeff*q1 - q2 + float64(sample)
		q2 = q1
		q1 = q0
	}
	return math.Sqrt((q1 * q1) + (q2 * q2) - q1*q2*f.coeff)
}

func (f *filter) normalizedMagnitude(block filterBlock) float64 {
	magnitude := f.magnitude(block)

	// moving average filter
	if magnitude > f.magnitudeLimitLow {
		f.magnitudeLimit = (f.magnitudeLimit + ((magnitude - f.magnitudeLimit) / 6))
	}
	if f.magnitudeLimit < f.magnitudeLimitLow {
		f.magnitudeLimit = f.magnitudeLimitLow
	}

	return magnitude / f.magnitudeLimit
}

func (f *filter) signalState(block filterBlock) bool {
	return f.normalizedMagnitude(block) > f.magnitudeThreshold
}

func (f *filter) Detect(buf []float32) (bool, int, error) {
	if len(buf) < f.blocksize {
		return false, 0, fmt.Errorf("buffer must contain at least %d samples", f.blocksize)
	}

	result := f.signalState(buf[:f.blocksize])
	return result, f.blocksize, nil
}

type clock interface {
	Now() time.Time
}

type debouncer struct {
	clock     clock
	threshold time.Duration

	effectiveState bool
	lastRawState   bool
	lastTimestamp  time.Time
}

func newDebouncer(clock clock, threshold time.Duration) *debouncer {
	return &debouncer{
		clock:     clock,
		threshold: threshold,
	}
}

func (d *debouncer) debounce(rawState bool) bool {
	now := d.clock.Now()
	if rawState != d.lastRawState {
		d.lastTimestamp = now
	}
	if now.Sub(d.lastTimestamp) > d.threshold {
		if rawState != d.effectiveState {
			d.effectiveState = rawState
		}
	}
	return d.effectiveState
}

package cw

import (
	"fmt"
	"io"
	"log"
	"math"
	"time"

	"github.com/ftl/digimodes/cw"
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
// this is also the duration in seconds that should be covered by one filter block
const blocksizeRatio = 0.01

type filterBlock []float32

func (b filterBlock) max() float32 {
	var max float32
	for _, s := range b {
		if s > max {
			max = s
		}
	}
	return max
}

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

type debouncer struct {
	clock     Clock
	threshold time.Duration

	effectiveState bool
	lastRawState   bool
	lastTimestamp  time.Time
}

func newDebouncer(clock Clock, threshold time.Duration) *debouncer {
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
	d.lastRawState = rawState

	if now.Sub(d.lastTimestamp) > d.threshold {
		if rawState != d.effectiveState {
			d.effectiveState = rawState
		}
	}
	return d.effectiveState
}

const maxSymbolCount = 8

var noSymbol = cw.Symbol{}

type cwChar [maxSymbolCount]cw.Symbol

func toCWChar(symbols ...cw.Symbol) cwChar {
	var result cwChar
	result.set(symbols)
	return result
}

func (c *cwChar) String() string {
	result := ""
loop:
	for _, s := range c {
		switch s {
		case noSymbol:
			break loop
		case cw.Dit:
			result += "."
		case cw.Da:
			result += "-"
		}
	}
	return result
}

func (c *cwChar) clear() {
	for i := range c {
		c[i] = noSymbol
	}
}

func (c *cwChar) append(symbol cw.Symbol) bool {
	for i, s := range c {
		if s == noSymbol {
			c[i] = symbol
			return true
		}
	}
	return false
}

func (c *cwChar) set(symbols []cw.Symbol) {
	for i := range c {
		if i < len(symbols) {
			c[i] = symbols[i]
		} else {
			c[i] = noSymbol
		}
	}
}

func (c *cwChar) len() int {
	for i, s := range c {
		if s == noSymbol {
			return i
		}
	}
	return maxSymbolCount
}

func (c *cwChar) empty() bool {
	return c[0] == noSymbol
}

type demodulator struct {
	out   io.Writer
	clock Clock

	lastState bool
	onStart   time.Time
	offStart  time.Time
	ditTime   time.Duration
	wpm       float64
	decoding  bool

	currentChar cwChar
	decodeTable map[cwChar]rune
}

func newDemodulator(out io.Writer, clock Clock) *demodulator {
	result := &demodulator{
		out:         out,
		clock:       clock,
		offStart:    clock.Now(),
		ditTime:     60 * time.Millisecond, // 20 WpM
		decodeTable: generateDecodeTable(),
	}
	result.currentChar.clear()

	return result
}

func generateDecodeTable() map[cwChar]rune {
	result := make(map[cwChar]rune, len(cw.Code))
	for text, symbols := range cw.Code {
		var c cwChar
		c.set(symbols)
		result[c] = text
	}
	return result
}

func (d *demodulator) tick(state bool) {
	now := d.clock.Now()

	if state != d.lastState {
		if state {
			d.onStart = now
			offDuration := now.Sub(d.offStart)
			d.onRisingEdge(offDuration)
		} else {
			d.offStart = now
			onDuration := now.Sub(d.onStart)
			d.onFallingEdge(onDuration)
		}
		d.decoding = true
	}
	d.lastState = state

	var currentDuration time.Duration
	if state {
		currentDuration = now.Sub(d.onStart)
	} else {
		currentDuration = now.Sub(d.offStart)
	}
	upperBound := time.Duration(float64(d.ditTime) * 10)

	if d.decoding && currentDuration > upperBound {
		d.decoding = false
		d.decodeCurrentChar()
		fmt.Println() // TODO REMOVE THIS
		d.writeToOutput('\n')
	}
}

func (d *demodulator) onRisingEdge(offDuration time.Duration) {
	// fmt.Printf("\noff for %v => ", offDuration)

	lack := 1.0
	if d.wpm > 30 {
		lack = 1.2
	}
	if d.wpm > 35 {
		lack = 1.5
	}
	lowerBound := time.Duration(float64(d.ditTime) * 2 * lack)
	upperBound := time.Duration(float64(d.ditTime) * 5 * lack)

	if offDuration > lowerBound && offDuration < upperBound {
		// we have a new char
		d.decodeCurrentChar()
		fmt.Print("|") // TODO REMOVE THIS
	} else if offDuration >= upperBound {
		// we have a word break
		d.decodeCurrentChar()
		d.writeToOutput(' ')
		fmt.Print("| |") // TODO REMOVE THIS
		// } else {
		// 	fmt.Printf("%v %v %v |%v %v|\n", d.ditTime, (d.ditTime * 2), lack, lowerBound, upperBound)
	}
}

func (d *demodulator) onFallingEdge(onDuration time.Duration) {
	// fmt.Printf("\non for %v => ", onDuration)

	if onDuration < (2*d.ditTime) || d.ditTime == 0 {
		d.ditTime = (onDuration + d.ditTime + d.ditTime) / 3
	}
	if onDuration > (5 * d.ditTime) {
		d.ditTime = onDuration + d.ditTime
	}

	if onDuration < d.ditTime*2 && onDuration > time.Duration(float64(d.ditTime)*0.6) {
		d.appendSymbol(cw.Dit)
		fmt.Print(".") // TODO REMOVE THIS
	}
	if onDuration > d.ditTime*2 && onDuration < (d.ditTime*6) {
		d.appendSymbol(cw.Da)
		fmt.Print("_") // TODO REMOVE THIS
		d.wpm = (d.wpm + (1200 / (float64(d.ditTime.Milliseconds()) / 3))) / 2
	}
}

func (d *demodulator) appendSymbol(s cw.Symbol) {
	if !d.currentChar.append(s) {
		// TODO make this transparent to the user
		d.decodeCurrentChar()
		d.currentChar.append(s)
	}
}

func (d *demodulator) decodeCurrentChar() {
	if d.currentChar.empty() {
		fmt.Print("X") // TODO REMOVE THIS
		return
	}

	r, ok := d.decodeTable[d.currentChar]
	if ok {
		err := d.writeToOutput(r)
		if err != nil {
			log.Printf("cannot write decoded char %q to output: %v", string(r), err)
		} else {
			fmt.Print(string(r)) // TODO REMOVE THIS
		}
	} else {
		// TODO make this transparent to the user
		log.Printf("unknown char %s", d.currentChar.String())
		// fmt.Print("?") // TODO REMOVE THIS
	}
	d.currentChar.clear()
}

func (d *demodulator) writeToOutput(r rune) error {
	_, err := fmt.Fprint(d.out, string(r))
	return err
}

func (d *demodulator) stop() {
	d.decodeCurrentChar()
}

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

const (
	traceCW = "cw"

	defaultWPM     = 20
	maxSymbolCount = 8

	minDitTime ticks = 2.5
	maxDitTime ticks = 6.0
)

type Tracer interface {
	Trace(string, string, ...any)
}

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

type ticks float64
type Decoder struct {
	out         io.Writer
	tickSeconds float64
	ticks       ticks

	lastState bool
	onStart   ticks
	offStart  ticks
	ditTime   ticks
	wpm       float64
	decoding  bool

	abortDecodeAfterDits int

	currentChar cwChar
	decodeTable map[cwChar]rune

	tracer Tracer
}

func NewDecoder(out io.Writer, sampleRate int, blockSize int) *Decoder {

	result := &Decoder{
		out:                  out,
		tickSeconds:          float64(blockSize) / float64(sampleRate),
		wpm:                  defaultWPM,
		abortDecodeAfterDits: 10,
		decodeTable:          generateDecodeTable(),
	}
	result.ditTime = result.wpmToDit(result.wpm)
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

func (d *Decoder) SetTracer(tracer Tracer) {
	d.tracer = tracer
}

func (d *Decoder) Reset() {
	d.wpm = defaultWPM
	d.ditTime = d.wpmToDit(d.wpm)
}

func (d *Decoder) presetWPM(wpm int) {
	d.wpm = float64(wpm)
	d.ditTime = d.wpmToDit(d.wpm)
}

func (d *Decoder) wpmToDit(wpm float64) ticks {
	ditSeconds := 60.0 / (50.0 * wpm)

	return ticks(math.Ceil(ditSeconds / d.tickSeconds))
}

func (d *Decoder) ditToWPM(ditTicks ticks) float64 {
	ditSeconds := float64(ditTicks) * d.tickSeconds
	return 60.0 / (50.0 * ditSeconds)
}

func (d *Decoder) Tick(state bool) {
	d.ticks++
	now := d.ticks

	if state != d.lastState {
		if state {
			d.onStart = now
			offDuration := now - d.offStart
			d.onRisingEdge(offDuration)
		} else {
			d.offStart = now
			onDuration := now - d.onStart
			d.onFallingEdge(onDuration)
		}
		d.decoding = true
	}
	d.lastState = state

	var currentDuration ticks
	if state {
		currentDuration = now - d.onStart
	} else {
		currentDuration = now - d.offStart
	}
	upperBound := d.ditTime * ticks(d.abortDecodeAfterDits)

	if d.tracer != nil {
		stateInt := -1
		if state {
			stateInt = 1
		}
		d.tracer.Trace(traceCW, "%f;%f;%d\n", d.ditTime, 3.0, stateInt)
	}

	if d.decoding && currentDuration > upperBound {
		d.decoding = false
		d.decodeCurrentChar()
		// fmt.Println() // TODO REMOVE THIS
	}
}

func (d *Decoder) onRisingEdge(offDuration ticks) {
	offRatio := float64(offDuration) / float64(d.ditTime)
	// fmt.Printf("\noff for %v (%.3f) => ", offDuration, offRatio)

	lack := 1.0
	if d.wpm > 30 {
		lack = 0.75
	}
	if d.wpm > 35 {
		lack = 0.60
	}
	lowerBound := 2 * lack
	upperBound := 5 * lack

	if offRatio > lowerBound && offRatio < upperBound {
		// we have a new char
		d.decodeCurrentChar()
		// fmt.Print("|") // TODO REMOVE THIS
	} else if offRatio >= upperBound {
		// we have a word break
		d.decodeCurrentChar()
		d.writeToOutput(' ')
		// fmt.Print("| |") // TODO REMOVE THIS
		// } else {
		// 	fmt.Printf("%v %v %v |%v %v|\n", d.ditTime, (d.ditTime * 2), lack, lowerBound, upperBound)
		// }
	}
}

func (d *Decoder) onFallingEdge(onDuration ticks) {
	onRatio := float64(onDuration) / float64(d.ditTime)
	// fmt.Printf("\non for %v (%.3f) => ", onDuration, onRatio)

	switch {
	case onDuration < minDitTime:
		// ignore
	case (onRatio < 2), d.ditTime == 0:
		d.setDitTime((onDuration + d.ditTime + d.ditTime) / 3)
	case onRatio > 5:
		d.setDitTime(d.ditTime * 1.5)
	case onRatio > 7:
		d.setDitTime(d.ditTime * 2)
	}

	if onRatio < 2 && onRatio > 0.6 {
		d.appendSymbol(cw.Dit)
		// fmt.Print(".") // TODO REMOVE THIS
	}
	if onRatio >= 2 && onRatio < 6 {
		d.appendSymbol(cw.Da)
		// fmt.Print("-") // TODO REMOVE THIS
		d.wpm = (d.wpm + d.ditToWPM(d.ditTime)) / 2.0
	}
}

func (d *Decoder) setDitTime(value ticks) {
	d.ditTime = max(minDitTime, min(value, maxDitTime))
}

func ditToWPM(dit time.Duration) float64 {
	return 60.0 / (50.0 * float64(dit.Seconds()))
}

func (d *Decoder) appendSymbol(s cw.Symbol) {
	if !d.currentChar.append(s) {
		// TODO make this transparent to the user
		d.decodeCurrentChar()
		d.currentChar.append(s)
	}
}

func (d *Decoder) decodeCurrentChar() {
	if d.currentChar.empty() {
		// fmt.Print("X") // TODO REMOVE THIS
		return
	}

	r, ok := d.decodeTable[d.currentChar]
	if ok {
		err := d.writeToOutput(r)
		if err != nil {
			log.Printf("cannot write decoded char %q to output: %v", string(r), err)
		} else {
			// fmt.Print(string(r)) // TODO REMOVE THIS
		}
	} else {
		// TODO make this transparent to the user
		// log.Printf("unknown char %s", d.currentChar.String())
		err := d.writeToOutput('X')
		if err != nil {
			log.Printf("cannot write unknown marker to output: %v", err)
		}
		// fmt.Print("?") // TODO REMOVE THIS
	}
	d.currentChar.clear()
}

func (d *Decoder) writeToOutput(r rune) error {
	_, err := fmt.Fprint(d.out, string(r))
	return err
}

func (d *Decoder) stop() {
	d.decodeCurrentChar()
}

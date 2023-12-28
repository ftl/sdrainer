package cw

import (
	"fmt"
	"io"
	"log"
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
	defaultWPM     = 20
	maxSymbolCount = 8
)

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

type Decoder struct {
	out   io.Writer
	clock Clock

	lastState bool
	onStart   time.Time
	offStart  time.Time
	ditTime   time.Duration
	wpm       float64
	decoding  bool

	abortDecodeAfterDits int

	currentChar cwChar
	decodeTable map[cwChar]rune
}

func NewDecoder(out io.Writer, clock Clock) *Decoder {
	result := &Decoder{
		out:                  out,
		clock:                clock,
		offStart:             clock.Now(),
		ditTime:              cw.WPMToDit(defaultWPM),
		wpm:                  defaultWPM,
		abortDecodeAfterDits: 10,
		decodeTable:          generateDecodeTable(),
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

func (d *Decoder) reset() {
	d.ditTime = cw.WPMToDit(defaultWPM)
	d.wpm = defaultWPM
}

func (d *Decoder) presetWPM(wpm int) {
	d.wpm = float64(wpm)
	d.ditTime = cw.WPMToDit(wpm)
}

func (d *Decoder) Tick(state bool) {
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
	upperBound := d.ditTime * time.Duration(d.abortDecodeAfterDits)

	if d.decoding && currentDuration > upperBound {
		d.decoding = false
		d.decodeCurrentChar()
		// fmt.Println() // TODO REMOVE THIS
	}
}

func (d *Decoder) onRisingEdge(offDuration time.Duration) {
	offRatio := float64(offDuration) / float64(d.ditTime)
	// fmt.Printf("\noff for %v (%.3f) => ", offDuration, offRatio)

	lack := 1.0
	if d.wpm > 30 {
		lack = 1.2
	}
	if d.wpm > 35 {
		lack = 1.5
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

func (d *Decoder) onFallingEdge(onDuration time.Duration) {
	onRatio := float64(onDuration) / float64(d.ditTime)
	// fmt.Printf("\non for %v (%.3f) => ", onDuration, onRatio)

	if onRatio < 2 || d.ditTime == 0 {
		d.ditTime = (onDuration + d.ditTime + d.ditTime) / 3
	}
	if onRatio > 5 {
		d.ditTime = onDuration + d.ditTime
	}

	if onRatio < 2 && onRatio > 0.6 {
		d.appendSymbol(cw.Dit)
		// fmt.Print(".") // TODO REMOVE THIS
	}
	if onRatio >= 2 && onRatio < 6 {
		d.appendSymbol(cw.Da)
		// fmt.Print("-") // TODO REMOVE THIS
		d.wpm = (d.wpm + ditToWPM(d.ditTime)) / 2.0
	}
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

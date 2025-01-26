package cw

import (
	"fmt"
	"io"
	"log"
	"math"
	"time"

	"github.com/ftl/digimodes/cw"
	"github.com/ftl/sdrainer/scope"
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
	scopeDecode       scope.StreamID = "decode"
	scopeSignalTiming scope.StreamID = "signal_timing"
	scopeGapTiming    scope.StreamID = "gap_timing"
	scopeSignal       scope.StreamID = "signal"

	unknownCharacter rune = 0xA6

	defaultWPM     = 20
	maxSymbolCount = 8

	minDitTime ticks = 2.0
	maxDitTime ticks = 7.0
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

type ticks float64
type Decoder struct {
	out         io.Writer
	tickSeconds float64
	ticks       ticks

	lastState bool
	onStart   ticks
	offStart  ticks
	wpm       float64
	decoding  bool

	abortDecodeAfterDits int

	currentChar        cwChar
	currentCharInvalid bool
	decodeTable        map[cwChar]rune
	onThreshold        *AdaptiveThreshold
	offThreshold       *AdaptiveThreshold

	scope      scope.Scope
	traceEdges bool
}

func NewDecoder(out io.Writer, sampleRate int, blockSize int) *Decoder {
	result := &Decoder{
		out:                  out,
		tickSeconds:          float64(blockSize) / float64(sampleRate),
		wpm:                  defaultWPM,
		abortDecodeAfterDits: 10,
		decodeTable:          generateDecodeTable(),
		scope:                scope.NewNullScope(),
	}
	result.currentChar.clear()

	ditTime := result.wpmToDit(result.wpm)
	result.onThreshold = NewAdaptiveThreshold(ditTime)
	result.offThreshold = NewAdaptiveThreshold(ditTime)

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

func (d *Decoder) SetScope(scope scope.Scope) {
	if scope == nil {
		panic("scope must not be nil")
	}
	d.scope = scope
}

func (d *Decoder) Reset() {
	d.presetWPM(defaultWPM)
	d.Clear()
	d.onThreshold.Reset()
}

func (d *Decoder) Clear() {
	d.decoding = false
	d.currentChar.clear()
	d.ticks = 0
	d.onStart = 0
	d.offStart = 0
}

func (d *Decoder) presetWPM(wpm int) {
	d.wpm = float64(wpm)
	ditTime := d.wpmToDit(d.wpm)
	d.onThreshold.Preset(ditTime)
	d.offThreshold.Preset(ditTime)
}

func ditToWPM(dit time.Duration) float64 {
	return 60.0 / (50.0 * float64(dit.Seconds()))
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
	upperBound := d.offThreshold.Get() * ticks(d.abortDecodeAfterDits)

	if d.scope.Active() {
		onDuration := currentDuration
		offDuration := currentDuration
		stateInt := 0
		if state {
			stateInt = 1
			offDuration = 0
		} else {
			onDuration = 0
		}
		frameTime := time.Now()
		d.scopeDecode(frameTime, currentDuration, d.onThreshold, stateInt)
		d.scopeSignalTiming(frameTime, onDuration, d.onThreshold, stateInt)
		d.scopeGapTiming(frameTime, offDuration, d.offThreshold, stateInt)
		d.scopeSignal(frameTime, stateInt)
	}

	if d.decoding && currentDuration > upperBound {
		d.decoding = false
		d.decodeCurrentChar()
		// fmt.Println() // TODO REMOVE THIS
	}
}

func (d *Decoder) onRisingEdge(offDuration ticks) {
	d.traceEdgef("\noff for %v (%.3f) => ", offDuration, d.offThreshold.Get())
	if offDuration < minDitTime {
		return
	}

	d.offThreshold.Put(offDuration, true)

	threshold := d.offThreshold.Get()
	upperThreshold := 4.5 * d.offThreshold.Low()
	// upperThreshold := 2*d.offThreshold.High() - d.offThreshold.Get()
	if offDuration >= upperThreshold {
		// we have a word break
		d.decodeCurrentChar()
		d.writeToOutput(' ')
		d.traceEdgef("| |")
	} else if offDuration >= threshold {
		// we have a new char
		d.decodeCurrentChar()
		d.traceEdgef("|")
	} else {
		d.traceEdgef("X")
	}
}

func (d *Decoder) onFallingEdge(onDuration ticks) {
	d.traceEdgef("\non for %v (%.3f) => ", onDuration, d.onThreshold.Get())
	if onDuration < minDitTime {
		return
	}

	d.onThreshold.Put(onDuration, true)

	threshold := d.onThreshold.Get()
	upperThreshold := 2 * d.onThreshold.High()
	if onDuration >= upperThreshold {
		d.currentCharInvalid = true
		d.traceEdgef("Y")
	} else if onDuration >= threshold {
		d.appendSymbol(cw.Da)
		d.traceEdgef("—")
		d.wpm = (d.wpm + d.ditToWPM(d.onThreshold.Low())) / 2.0
	} else {
		d.appendSymbol(cw.Dit)
		d.traceEdgef("•")
	}
}

func (d *Decoder) traceEdgef(format string, args ...any) {
	if !d.traceEdges {
		return
	}
	fmt.Printf(format, args...)
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
	if d.currentCharInvalid {
		// fmt.Print("X") // TODO REMOVE THIS
		d.currentCharInvalid = false
		d.currentChar.clear()
		err := d.writeToOutput(unknownCharacter)
		if err != nil {
			log.Printf("cannot write unknown marker to output: %v", err)
		}
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
		err := d.writeToOutput(unknownCharacter)
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

type AdaptiveThreshold struct {
	preset     ticks
	upperBound ticks

	low  ticks
	high ticks

	last      ticks
	threshold ticks
}

func NewAdaptiveThreshold(preset ticks) *AdaptiveThreshold {
	result := &AdaptiveThreshold{
		preset:     preset,
		upperBound: 10,
	}
	result.Reset()
	return result
}

func (t *AdaptiveThreshold) Reset() {
	t.low = t.preset
	t.high = 3 * t.low // default 1:3 timing
	t.last = t.low
	t.updateThreshold()
}

func (t *AdaptiveThreshold) Preset(preset ticks) {
	t.preset = preset
	t.Reset()
}

func (t *AdaptiveThreshold) Put(duration ticks, state bool) {
	const highFactor = 2
	const avgWeight = 0.75
	const currentWeight = 1.0 - avgWeight

	if duration >= t.low*t.upperBound {
		return
	}

	if t.last >= duration*highFactor { // last high, now low {
		t.low = avgWeight*t.low + currentWeight*duration
		t.high = avgWeight*t.high + currentWeight*t.last
	} else if duration >= t.last*highFactor { // last low, now high
		t.low = avgWeight*t.low + currentWeight*t.last
		t.high = avgWeight*t.high + currentWeight*duration
	}
	t.last = duration
	t.updateThreshold()
}

func (t *AdaptiveThreshold) updateThreshold() {
	// geometric mean
	t.threshold = ticks(math.Sqrt(float64(t.low) * float64(t.high)))
}

func (t *AdaptiveThreshold) Get() ticks {
	return t.threshold
}

func (t *AdaptiveThreshold) Ratio() ticks {
	return t.high / t.low
}

func (t *AdaptiveThreshold) Low() ticks {
	return t.low
}

func (t *AdaptiveThreshold) High() ticks {
	return t.high
}

func (d *Decoder) scopeDecode(frameTime time.Time, currentDuration ticks, onThreshold *AdaptiveThreshold, state int) {
	d.scope.ShowTimeFrame(&scope.TimeFrame{
		Frame: scope.Frame{
			Stream:    scopeDecode,
			Timestamp: frameTime,
		},
		Values: map[scope.ChannelID]float64{
			"duration":     float64(currentDuration),
			"on_threshold": float64(onThreshold.Get()),
			"state":        float64(state),
		},
	})
}

func (d *Decoder) scopeSignalTiming(frameTime time.Time, onDuration ticks, onThreshold *AdaptiveThreshold, state int) {
	d.scope.ShowTimeFrame(&scope.TimeFrame{
		Frame: scope.Frame{
			Stream:    scopeSignalTiming,
			Timestamp: frameTime,
		},
		Values: map[scope.ChannelID]float64{
			"on_duration":         float64(onDuration),
			"on_threshold":        float64(onThreshold.Get()),
			"on_threshold_low":    float64(onThreshold.Low()),
			"on_threshold_high":   float64(onThreshold.High()),
			"on_threshold_high_2": float64(2 * onThreshold.High()),
			"state":               float64(state),
		},
	})
}

func (d *Decoder) scopeGapTiming(frameTime time.Time, offDuration ticks, offThreshold *AdaptiveThreshold, state int) {
	d.scope.ShowTimeFrame(&scope.TimeFrame{
		Frame: scope.Frame{
			Stream:    scopeGapTiming,
			Timestamp: frameTime,
		},
		Values: map[scope.ChannelID]float64{
			"off_duration":         float64(offDuration),
			"off_threshold":        float64(offThreshold.Get()),
			"off_threshold_low":    float64(offThreshold.Low()),
			"off_threshold_high":   float64(offThreshold.High()),
			"off_threshold_high_2": float64(2*offThreshold.High() - offThreshold.Get()),
			"state":                float64(state),
		},
	})
}

func (d *Decoder) scopeSignal(frameTime time.Time, state int) {
	d.scope.ShowTimeFrame(&scope.TimeFrame{
		Frame: scope.Frame{
			Stream:    scopeSignal,
			Timestamp: frameTime,
		},
		Values: map[scope.ChannelID]float64{
			"state": float64(state),
		},
	})
}

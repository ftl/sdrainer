package cw

import (
	"math"
	"slices"
	"time"

	"github.com/ftl/digimodes/cw"

	"github.com/ftl/sdrainer/core"
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

// OverMarker is the character that the decoder gives at the end of an over, thus at the end of one
// transmission of one station. A line break is the natural form of it: a consumer that writes the
// text gets one line for each over, a consumer that splits words at each whitespace needs no change,
// and a transcription that a human writes holds one line for each over already.
const OverMarker rune = '\n'

const (
	scopeDecode       core.StreamID = "decode"
	scopeSignalTiming core.StreamID = "signal_timing"
	scopeGapTiming    core.StreamID = "gap_timing"
	scopeSignal       core.StreamID = "signal"

	// UnknownCharacter stands for a character that the decoder could not read. It must be a rune
	// that the code table does not hold, otherwise a consumer cannot tell the two apart: "?" is
	// ..--.. and a real character, and a "qrl" with one unknown character behind it gave the
	// keyword "qrl?" of the callsign stage. U+FFFD is the replacement character of Unicode, and it
	// stands for exactly this case.
	UnknownCharacter rune = '\uFFFD'

	defaultWPM     = 20
	maxSymbolCount = 8

	minDitTime ticks = 2.0

	// The timing model of a CW signal, in dits. A dit is 1, a da is 3, the gap inside a character
	// is 1, the gap between two characters is 3, and the gap between two words is 7. All decisions
	// use the middle between two of these values as their limit.
	ditDaRatio  ticks = 2 // a mark of 2 dits or more is a da
	charGapDits ticks = 2 // a gap of 2 dits or more ends the character
	wordGapDits ticks = 5 // a gap of 5 dits or more ends the word
	maxMarkDits ticks = 6 // a mark of 6 dits or more is not an element of a character

	// overGapDits is the length of a gap that ends an over, thus one transmission of one station,
	// in dits. The decoder gives OverMarker for such a gap.
	//
	// The value must stand far above wordGapDits, otherwise a pause of an operator inside a
	// transmission ends the over. A measurement of the recorded streams in testdata, with the dit
	// of the decoder itself, gives the two groups: a pause inside one transmission reaches 16.1
	// dits, and the break between two transmissions is 41.1 dits. 30 stands between the two, with
	// margin on each side. doc/architecture.md, section 7.2, holds the measurement.
	overGapDits ticks = 30

	// The unit estimate. The marks and the gaps get their own estimator, because the keying
	// detector is not symmetric: it makes the marks longer and the gaps shorter. The recorded
	// streams in testdata show a difference of up to 2:1 between the two units.
	unitWindowSize  = 32  // durations, thus approximately 10 characters
	minUnitSamples  = 2   // less durations do not give a useful percentile
	unitPercentile  = 0.2 // see the research document, section 8
	minUnitContrast = 2   // the window must hold more than one class of duration
	unitSmoothing   = 0.2 // 63 % of a change within 5 durations

	// maxMarkClassRatio is the largest ratio between the longest class and the shortest class of the
	// marks at which the shortest class is the dit. A da is 3 dits, so the ratio of a signal is 3.
	//
	// The detector cuts a mark into pieces, and those pieces are shorter than a dit. They make their
	// own class, and lowestClass then takes that class: a measurement of OM1UM in
	// pipeline/testdata gives a unit of 9.1 ticks against a dit of 12, and the decisions of the
	// decoder move with it. Above this ratio the shortest class is therefore not the dit, and the
	// longest class gives it: the da of that measurement is 35 ticks and it gives 11.7.
	//
	// The rule holds only for the marks. The gaps have 3 classes, 1, 3 and 7 dits, so a ratio of 7
	// is normal for them.
	maxMarkClassRatio = 4.0
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
	sink        CharacterSink
	tickSeconds float64
	ticks       ticks

	lastState bool
	sawMark   bool
	onStart   ticks
	offStart  ticks
	charStart ticks
	wpm       float64
	decoding  bool

	abortDecodeAfterDits int

	// overEmitted holds that the current gap already gave its over marker, so that one gap gives
	// one marker and no space behind it.
	overEmitted bool

	currentChar        cwChar
	currentCharInvalid bool
	decodeTable        map[cwChar]rune
	markUnits          *UnitEstimator
	gapUnits           *UnitEstimator

	scope core.ScopeService
}

// NewDecoder makes a decoder that takes one state for each frame of a spectral analysis. hop is
// the distance between two frames in samples, and not the size of the FFT: it gives the time of one
// tick.
func NewDecoder(sink CharacterSink, sampleRate int, hop int) *Decoder {
	result := &Decoder{
		sink:                 sink,
		tickSeconds:          float64(hop) / float64(sampleRate),
		wpm:                  defaultWPM,
		abortDecodeAfterDits: 10,
		decodeTable:          generateDecodeTable(),
		scope:                &core.NullScopeService{},
	}
	result.currentChar.clear()
	ditTime := result.wpmToDit(result.wpm)
	result.markUnits = NewMarkUnitEstimator(ditTime)
	result.gapUnits = NewUnitEstimator(ditTime)

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

func (d *Decoder) SetScope(scope core.ScopeService) {
	if scope == nil {
		panic("scope must not be nil")
	}
	d.scope = scope
}

func (d *Decoder) Reset() {
	d.presetWPM(defaultWPM)
	d.Clear()
}

func (d *Decoder) Clear() {
	d.decoding = false
	d.currentChar.clear()
	d.currentCharInvalid = false
	d.lastState = false
	d.sawMark = false
	d.ticks = 0
	d.onStart = 0
	d.offStart = 0
	d.charStart = 0
}

func (d *Decoder) presetWPM(wpm int) {
	d.wpm = float64(wpm)
	ditTime := d.wpmToDit(d.wpm)
	d.markUnits.Preset(ditTime)
	d.gapUnits.Preset(ditTime)
}

func ditToWPM(dit time.Duration) float64 {
	return 60.0 / (50.0 * float64(dit.Seconds()))
}

func (d *Decoder) wpmToDit(wpm float64) ticks {
	ditSeconds := 60.0 / (50.0 * wpm)

	return ticks(math.Round(ditSeconds / d.tickSeconds))
}

func (d *Decoder) ditToWPM(ditTicks ticks) float64 {
	ditSeconds := float64(ditTicks) * d.tickSeconds
	return 60.0 / (50.0 * ditSeconds)
}

func (d *Decoder) Tick(state bool) {
	// the count of the ticks is zero-based, so the first tick is 0. Each decision of the decoder
	// uses a difference of two ticks and does not depend on the base, but Character.Start and
	// Character.End give the position to a consumer, and that consumer must add no correction.
	now := d.ticks
	d.ticks++

	if state != d.lastState {
		if state {
			d.onStart = now
			offDuration := now - d.offStart
			d.onRisingEdge(offDuration)
			d.overEmitted = false
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
	upperBound := d.gapUnits.Get() * ticks(d.abortDecodeAfterDits)

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
		dit := d.gapUnits.Get()
		d.scopeDecode(frameTime, currentDuration, dit, stateInt)
		d.scopeSignalTiming(frameTime, onDuration, dit, stateInt)
		d.scopeGapTiming(frameTime, offDuration, dit, stateInt)
		d.scopeSignal(frameTime, stateInt)
	}

	if d.decoding && currentDuration > upperBound {
		d.decoding = false
		d.decodeCurrentChar()
	}

	// The over ends while the signal is off, and the decoder must not wait for the next
	// transmission: a station that stops for good never gives a rising edge again. The gap of the
	// abort above is much shorter than this one, so the current character is already complete.
	if !state && !d.overEmitted && d.sawMark && currentDuration > overGapDits*d.dit() {
		d.overEmitted = true
		d.emit(OverMarker, now-currentDuration, now)
	}
}

// dit gives the length of one dit in ticks, as the mean of the two estimates. The keying detector
// makes the marks longer and the gaps shorter, so the mean of the two removes that bias and only it
// is a real dit. The speed uses the same value.
//
// The limits of the timing model use the estimate of the gaps alone, and they are calibrated
// against it. overGapDits does not: it is a physical time, so it needs the real dit.
func (d *Decoder) dit() ticks {
	return 0.5 * (d.markUnits.Get() + d.gapUnits.Get())
}

func (d *Decoder) onRisingEdge(offDuration ticks) {
	// A stream can begin in a gap, and that gap began before the stream. Its length is not the
	// length of a gap of the signal, and it must not go into the estimate of the unit. Without this
	// guard a stream that begins with a short gap gives a unit that is much too small, and the
	// decoder then makes one character from each element until the estimate recovers.
	if !d.sawMark {
		return
	}
	if offDuration < minDitTime {
		return
	}

	unit := d.gapUnits.Put(offDuration)
	switch {
	case offDuration >= wordGapDits*unit:
		d.decodeCurrentChar()
		if !d.overEmitted {
			// the gap itself is the word break, so it gives the position of the space
			d.emit(' ', d.onStart-offDuration, d.onStart)
		}
	case offDuration >= charGapDits*unit:
		d.decodeCurrentChar()
	}
}

func (d *Decoder) onFallingEdge(onDuration ticks) {
	d.sawMark = true
	if onDuration < minDitTime {
		return
	}

	unit := d.markUnits.Put(onDuration)

	// The speed comes from the mean of the two estimates, and not from the marks alone. The
	// decision level of the demodulator is not in the middle of the span between the level of the
	// gaps and the level of the marks, so each mark is longer than its true time by a constant, and
	// each gap is shorter by the same constant. The mean of the two estimates removes that error,
	// and it does so for each value of decisionFraction.
	d.wpm = d.ditToWPM(0.5 * (unit + d.gapUnits.Get()))

	switch {
	case onDuration >= maxMarkDits*unit:
		d.startChar()
		d.currentCharInvalid = true
	case onDuration >= ditDaRatio*unit:
		d.appendSymbol(cw.Da)
	default:
		d.appendSymbol(cw.Dit)
	}
}

// debounceTicks gives the count of the ticks that the keying must stay stable, as a part of the
// current estimate of one dit. The demodulator uses it to remove the runs that are shorter than an
// element of the signal.
//
// The estimate comes from the durations that the demodulator gave before, so the two need each
// other. That is no problem: the estimate begins at defaultWPM and it follows the signal.
func (d *Decoder) debounceTicks(dits float64) int {
	dit := 0.5 * (d.markUnits.Get() + d.gapUnits.Get())
	if dit <= 0 {
		return 1
	}
	return max(1, int(math.Round(dits*float64(dit))))
}

// startChar records the position of the first mark of the current character. The caller runs on a
// falling edge, so onStart still holds the start of the mark that just ended.
func (d *Decoder) startChar() {
	if d.currentChar.empty() {
		d.charStart = d.onStart
	}
}

func (d *Decoder) appendSymbol(s cw.Symbol) {
	d.startChar()
	if d.currentChar.append(s) {
		return
	}

	// more symbols than a character can hold, so the decode of this character failed
	d.currentCharInvalid = true
	d.decodeCurrentChar()
	// the mark that did not fit is the first mark of the next character
	d.charStart = d.onStart
	d.currentChar.append(s)
}

func (d *Decoder) decodeCurrentChar() {
	if d.currentChar.empty() {
		return
	}
	// the signal goes off at the end of the last mark of the character, so offStart is that end
	start, end := d.charStart, d.offStart

	if d.currentCharInvalid {
		d.currentCharInvalid = false
		d.currentChar.clear()
		d.emit(UnknownCharacter, start, end)
		return
	}

	r, ok := d.decodeTable[d.currentChar]
	if !ok {
		// TODO make this transparent to the user
		r = UnknownCharacter
	}
	d.currentChar.clear()
	d.emit(r, start, end)
}

func (d *Decoder) emit(r rune, start ticks, end ticks) {
	if d.sink == nil {
		return
	}
	d.sink.CharacterDecoded(Character{
		Rune:  r,
		WPM:   d.wpm,
		Start: int64(start),
		End:   int64(end),
	})
}

func (d *Decoder) stop() {
	d.decodeCurrentChar()
}

// UnitEstimator estimates the length of one dit from a window of durations. It takes a low
// percentile, so it finds the shortest class of duration in the window and it ignores the longer
// classes. See the research document, section 8.
type UnitEstimator struct {
	preset    ticks
	durations [unitWindowSize]ticks
	scratch   []ticks
	next      int
	count     int

	// marks tells that the durations are marks, which hold 2 classes at 1 and 3 dits. See
	// maxMarkClassRatio.
	marks bool

	unit ticks
}

func NewUnitEstimator(preset ticks) *UnitEstimator {
	result := &UnitEstimator{
		scratch: make([]ticks, 0, unitWindowSize),
	}
	result.Preset(preset)
	return result
}

// NewMarkUnitEstimator makes an estimator for the durations of the marks. It knows that a mark is
// 1 dit or 3 dits, and it uses that to find the pieces that the detector makes of a mark.
func NewMarkUnitEstimator(preset ticks) *UnitEstimator {
	result := NewUnitEstimator(preset)
	result.marks = true
	return result
}

func (e *UnitEstimator) Preset(preset ticks) {
	e.preset = preset
	e.Reset()
}

func (e *UnitEstimator) Reset() {
	e.unit = e.preset
	e.next = 0
	e.count = 0
}

// Put a new duration into the estimator and get the new unit estimate back.
func (e *UnitEstimator) Put(duration ticks) ticks {
	e.durations[e.next] = duration
	e.next = (e.next + 1) % len(e.durations)
	if e.count < len(e.durations) {
		e.count++
	}
	if e.count < minUnitSamples {
		return e.unit
	}

	unit := e.lowestClass()
	if unit == 0 {
		return e.unit
	}

	if e.count < len(e.durations) {
		// the window is not full, so there is no history that is better than the new value
		e.unit = unit
	} else {
		e.unit += ticks(unitSmoothing * float64(unit-e.unit))
	}
	return e.unit
}

// lowestClass returns the mean of the shortest class of duration in the window, or 0 if the window
// holds only one class. A low percentile gives a seed inside the shortest class, and the mean of
// all durations near that seed gives the center of the class. The percentile alone would give a
// value at the lower edge of the class.
func (e *UnitEstimator) lowestClass() ticks {
	e.scratch = append(e.scratch[:0], e.durations[:e.count]...)
	slices.Sort(e.scratch)

	seed := e.scratch[int(float64(len(e.scratch)-1)*unitPercentile)]
	limit := minUnitContrast * seed

	// The longest duration must be in another class. If it is not, all durations are of the same
	// class, and the window cannot tell which class that is.
	if e.scratch[len(e.scratch)-1] < limit {
		return 0
	}

	var sum ticks
	var count int
	var restSum ticks
	var restCount int
	for _, duration := range e.scratch {
		if duration >= limit {
			restSum += duration
			restCount++
			continue
		}
		sum += duration
		count++
	}
	if count == 0 {
		return 0
	}
	lowest := sum / ticks(count)

	// The classes of a mark are 1 dit and 3 dits. A span that is much wider says that the shortest
	// class holds the pieces that the detector made of a mark, and not the dit. The longest class
	// is then the da, and it gives the dit.
	if e.marks && restCount > 0 && lowest > 0 {
		highest := restSum / ticks(restCount)
		if highest > maxMarkClassRatio*lowest {
			return highest / 3
		}
	}

	return lowest
}

// Get the current unit estimate.
func (e *UnitEstimator) Get() ticks {
	return e.unit
}

func (d *Decoder) scopeDecode(frameTime time.Time, currentDuration ticks, dit ticks, state int) {
	d.scope.SendTimeFrame(&core.TimeFrame{
		Frame: core.Frame{
			Stream:    scopeDecode,
			Timestamp: frameTime,
		},
		Values: map[core.ValueID]float64{
			"duration": float64(currentDuration),
			"dit":      float64(dit),
			"state":    float64(state),
		},
	})
}

func (d *Decoder) scopeSignalTiming(frameTime time.Time, onDuration ticks, dit ticks, state int) {
	d.scope.SendTimeFrame(&core.TimeFrame{
		Frame: core.Frame{
			Stream:    scopeSignalTiming,
			Timestamp: frameTime,
		},
		Values: map[core.ValueID]float64{
			"on_duration": float64(onDuration),
			"dit":         float64(dit),
			"da_limit":    float64(ditDaRatio * dit),
			"mark_limit":  float64(maxMarkDits * dit),
			"state":       float64(state),
		},
	})
}

func (d *Decoder) scopeGapTiming(frameTime time.Time, offDuration ticks, dit ticks, state int) {
	d.scope.SendTimeFrame(&core.TimeFrame{
		Frame: core.Frame{
			Stream:    scopeGapTiming,
			Timestamp: frameTime,
		},
		Values: map[core.ValueID]float64{
			"off_duration": float64(offDuration),
			"dit":          float64(dit),
			"char_gap":     float64(charGapDits * dit),
			"word_gap":     float64(wordGapDits * dit),
			"state":        float64(state),
		},
	})
}

func (d *Decoder) scopeSignal(frameTime time.Time, state int) {
	d.scope.SendTimeFrame(&core.TimeFrame{
		Frame: core.Frame{
			Stream:    scopeSignal,
			Timestamp: frameTime,
		},
		Values: map[core.ValueID]float64{
			"state": float64(state),
		},
	})
}

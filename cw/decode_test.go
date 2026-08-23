package cw

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ftl/digimodes/cw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToCWChar(t *testing.T) {
	a := cwChar{cw.Dit, cw.Da}
	assert.Equal(t, a, toCWChar(cw.Dit, cw.Da))
}

// decoded gives the text of the buffer without the over marker at its end. A stream that ends with
// a long silence gives that marker, and these tests measure the characters and not the marker.
func decoded(buffer *bytes.Buffer) string {
	return strings.TrimSuffix(buffer.String(), string(OverMarker))
}

func TestDecodeTable(t *testing.T) {
	table := generateDecodeTable()

	assert.Equal(t, 'a', table[toCWChar(cw.Dit, cw.Da)])
	assert.Equal(t, '/', table[toCWChar(cw.Da, cw.Dit, cw.Dit, cw.Da, cw.Dit)])
	assert.Equal(t, '§', table[toCWChar(cw.Dit, cw.Dit, cw.Dit, cw.Dit, cw.Dit, cw.Dit, cw.Dit, cw.Dit)])
}

func TestDitToWPM(t *testing.T) {
	assert.Equal(t, 20.0, ditToWPM(60*time.Millisecond))
}

func TestDecoder_CodeTable(t *testing.T) {
	const sampleRate = 48000
	const blockSize = 512
	buffer := bytes.NewBuffer([]byte{})
	decoder := NewDecoder(NewTextSink(buffer), sampleRate, blockSize)

	for r := range cw.Code {
		t.Run(string(r), func(t *testing.T) {
			buffer.Reset()
			decoder.Reset()
			expected := string(r)

			stream := generateStream(sampleRate, blockSize, int(decoder.wpm), defaultTiming, expected)
			for _, state := range stream {
				decoder.Tick(state == "1")
			}
			decoder.stop()

			assert.Equal(t, expected, decoded(buffer))
		})
	}
}

func TestDecoder_WPMToDitRoundsToTheNearestTick(t *testing.T) {
	const sampleRate = 48000
	const blockSize = 512 // one tick is 10.667 ms
	decoder := NewDecoder(nil, sampleRate, blockSize)

	tt := []struct {
		wpm      float64
		exact    float64
		expected ticks
	}{
		{wpm: 20, exact: 5.625, expected: 6},
		{wpm: 12, exact: 9.375, expected: 9},  // the old code rounded up to 10
		{wpm: 10, exact: 11.25, expected: 11}, // the old code rounded up to 12
		{wpm: 35, exact: 3.214, expected: 3},  // the old code rounded up to 4
	}
	for _, tc := range tt {
		t.Run(fmt.Sprintf("%.0f", tc.wpm), func(t *testing.T) {
			require.InDelta(t, tc.exact, 60.0/(50.0*tc.wpm)/decoder.tickSeconds, 1e-3, "the exact number of ticks")

			assert.Equal(t, tc.expected, decoder.wpmToDit(tc.wpm))
		})
	}
}

func TestDecoder_TooManySymbolsGiveTheUnknownMarker(t *testing.T) {
	const sampleRate = 48000
	const blockSize = 512
	buffer := bytes.NewBuffer([]byte{})
	decoder := NewDecoder(NewTextSink(buffer), sampleRate, blockSize)

	// maxSymbolCount dits fit into one character, the next dit does not
	stream := make([]string, 0)
	for range maxSymbolCount + 1 {
		stream = append(stream, generateSymbolStream(sampleRate, blockSize, int(decoder.wpm), cw.Dit)...)
	}
	for _, state := range stream {
		decoder.Tick(state == "1")
	}
	decoder.stop()

	assert.Equal(t, string(UnknownCharacter)+"e", decoded(buffer))
}

func generateSymbolStream(sampleRate int, blockSize int, wpm int, symbol cw.Symbol) []string {
	tickSeconds := float64(blockSize) / float64(sampleRate)
	baseTicks := int(cw.WPMToDit(wpm) / time.Duration(tickSeconds*float64(time.Second)))

	result := make([]string, 0, 4*baseTicks)
	switch symbol {
	case cw.Dit:
		result = appendStrings(result, "1", baseTicks)
	case cw.Da:
		result = appendStrings(result, "1", 3*baseTicks)
	}
	return appendStrings(result, "0", baseTicks)
}

func TestDecoder_ClearRemovesTheStateOfTheLastSignal(t *testing.T) {
	const sampleRate = 48000
	const blockSize = 512
	buffer := bytes.NewBuffer([]byte{})
	decoder := NewDecoder(NewTextSink(buffer), sampleRate, blockSize)

	// a mark that is much too long marks the current character as invalid, and it leaves the
	// decoder in the on state
	ditTicks := int(decoder.wpmToDit(decoder.wpm))
	for range 10 * ditTicks {
		decoder.Tick(true)
	}
	decoder.Tick(false)
	for range 10 * ditTicks {
		decoder.Tick(true)
	}
	require.True(t, decoder.currentCharInvalid, "the test needs an invalid character")
	require.True(t, decoder.lastState, "the test needs the on state")

	decoder.Reset()

	assert.False(t, decoder.currentCharInvalid, "currentCharInvalid after Reset")
	assert.False(t, decoder.lastState, "lastState after Reset")

	buffer.Reset()
	stream := generateStream(sampleRate, blockSize, int(decoder.wpm), defaultTiming, "e")
	for _, state := range stream {
		decoder.Tick(state == "1")
	}
	decoder.stop()

	assert.Equal(t, "e", decoded(buffer), "the next signal must not inherit the invalid character")
}

func TestDecoder_SpeedTolerance(t *testing.T) {
	const sampleRate = 48000
	const blockSize = 512
	buffer := bytes.NewBuffer([]byte{})
	decoder := NewDecoder(NewTextSink(buffer), sampleRate, blockSize)
	expected := "paris"

	minWpm := 0
	maxWpm := 0
	for wpm := 5; wpm < 40; wpm++ {
		buffer.Reset()
		decoder.Reset()

		stream := generateStream(sampleRate, blockSize, wpm, defaultTiming, expected)
		for _, state := range stream {
			decoder.Tick(state == "1")
		}
		decoder.stop()

		if expected == decoded(buffer) && minWpm == 0 {
			minWpm = wpm
		}
		if expected != decoded(buffer) && minWpm != 0 && maxWpm == 0 {
			maxWpm = wpm - 1
		}
	}

	assert.Equal(t, 10, minWpm, "min")
	assert.Equal(t, 0, maxWpm, "max, 0 means that the decoder worked up to the end of the tested range")
}

func TestDecoder_SpeedAdaptionRate(t *testing.T) {
	const sampleRate = 48000
	const blockSize = 512
	const maxRounds = 15
	buffer := bytes.NewBuffer([]byte{})
	decoder := NewDecoder(NewTextSink(buffer), sampleRate, blockSize)
	expected := "paris"

	tt := []struct {
		wpm            int
		expectedRounds int
	}{
		{28, 1},
		{29, 1},
		{38, 1},
		{56, 1},
		{57, maxRounds},
		{12, 1},
		{11, 1},
		{10, 1},
		{7, 2},
		{6, 2},
		{5, 2},
	}
	for _, tc := range tt {
		t.Run(fmt.Sprintf("%d", tc.wpm), func(t *testing.T) {
			stream := generateStream(sampleRate, blockSize, tc.wpm, defaultTiming, expected)
			rounds := 0
			actual := ""
			decoder.Reset()
			for actual != expected && rounds < maxRounds {
				buffer.Reset()
				decoder.Clear()

				for _, state := range stream {
					decoder.Tick(state == "1")
				}
				decoder.stop()
				actual = decoded(buffer)

				rounds++
			}

			assert.Equal(t, tc.expectedRounds, rounds)
		})
	}
}

func TestDecoder_SpeedRange(t *testing.T) {
	const sampleRate = 48000
	const blockSize = 512
	buffer := bytes.NewBuffer([]byte{})
	decoder := NewDecoder(NewTextSink(buffer), sampleRate, blockSize)
	expected := "paris"
	maxRounds := 3

	minWpm := 0
	maxWpm := 0
	for wpm := 5; wpm < 100; wpm++ {
		stream := generateStream(sampleRate, blockSize, wpm, defaultTiming, expected)
		rounds := 0
		actual := ""
		decoder.Reset()
		for actual != expected && rounds < maxRounds {
			buffer.Reset()
			decoder.Clear()

			for _, state := range stream {
				decoder.Tick(state == "1")
			}
			decoder.stop()
			actual = decoded(buffer)

			rounds++
		}

		if rounds < maxRounds && minWpm == 0 {
			minWpm = wpm
		}
		if rounds < maxRounds && minWpm != 0 {
			maxWpm = wpm
		}
	}

	assert.Equal(t, 5, minWpm, "min")
	assert.Equal(t, 56, maxWpm, "max")
}

// TestDecoder_RecordedStreams decodes the streams of a real receiver. The text of the first
// characters is not always correct, because the decoder must first find the speed of the signal.
//
// The four streams of ly2px begin with one tick of gap and then with a mark. Before onRisingEdge
// got its guard for the gap of the start, that gap of the start went into the estimate of the unit,
// and the estimate became much too small: each of the first four elements became one character
// ("etet"), and the gap after "cq" became a word gap. The values below are the values with the
// guard, and they no longer depend on the base of the count of the ticks.
func TestDecoder_RecordedStreams(t *testing.T) {
	const sampleRate = 48000
	const blockSize = 512
	tt := []struct {
		filename string
		expected string
	}{
		{filename: "db100fk_1.txt", expected: "i100fk"},
		// the stream holds a break of 41 dits behind the first call, thus an over marker
		{filename: "db100fk_2.txt", expected: "i100fk\ncq db1de" + string(UnknownCharacter) + "fk"},
		{filename: "db100fk_3.txt", expected: "i100fk\ncq db1de" + string(UnknownCharacter) + "fk db100fk"},
		{filename: "gb4wwa.txt", expected: "r q gb4wwa gb4wwa up"},
		{filename: "ii3wwa.txt", expected: "k de ii3wwa ii3wwa pse k"},
		{filename: "ly2px_1.txt", expected: "ä cq"},
		{filename: "ly2px_2.txt", expected: "ä cqcqde"},
		{filename: "ly2px_3.txt", expected: "ä cqcqde ly2px ly2px"},
		{filename: "ly2px_4.txt", expected: "ä cqcqde ly2px ly2px cqcqcqde ly2px ly2px ly2gx ä"},
	}

	buffer := bytes.NewBuffer([]byte{})
	decoder := NewDecoder(NewTextSink(buffer), sampleRate, blockSize)
	for _, tc := range tt {
		t.Run(tc.filename, func(t *testing.T) {
			decoder.Reset()
			buffer.Reset()

			stream, err := readLines(tc.filename)
			require.NoError(t, err)
			for _, state := range stream {
				decoder.Tick(state == "1")
			}
			decoder.stop()

			assert.Equal(t, tc.expected, decoded(buffer))
		})
	}
}

func readLines(filename string) ([]string, error) {
	file, err := os.Open(filepath.Join("testdata", filename))
	if err != nil {
		return nil, err
	}
	defer file.Close()

	result := make([]string, 0, 10000)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		result = append(result, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

var defaultTiming = timing{1, 3, 1, 3, 7}

type timing struct {
	dit         int
	da          int
	symbolBreak int
	charBreak   int
	wordBreak   int
}

func (t timing) AddScalar(s int) timing {
	return timing{
		dit:         s * t.dit,
		da:          s * t.da,
		symbolBreak: s * t.symbolBreak,
		charBreak:   s * t.charBreak,
		wordBreak:   s * t.wordBreak,
	}
}

func generateStream(sampleRate int, blockSize int, wpm int, timing timing, text string) []string {
	tickSeconds := float64(blockSize) / float64(sampleRate)
	baseTicks := int(cw.WPMToDit(wpm) / time.Duration(tickSeconds*float64(time.Second)))
	ditTiming := timing.AddScalar(baseTicks)

	// The writer goes into the goroutine and the reader stays here. The other way around gives a
	// data race: close(symbolStream) ends the loop of the reader, but it does not wait for the last
	// append of the reader, so this function can read the slice while the goroutine still writes it.
	symbolStream := make(chan cw.Symbol)
	go func() {
		defer close(symbolStream)
		cw.WriteToSymbolStream(context.Background(), symbolStream, text)
	}()

	symbols := make([]cw.Symbol, 0)
	for s := range symbolStream {
		symbols = append(symbols, s)
	}

	result := make([]string, 0)
	for _, s := range symbols {
		switch s {
		case cw.Dit:
			result = appendStrings(result, "1", ditTiming.dit)
		case cw.Da:
			result = appendStrings(result, "1", ditTiming.da)
		case cw.SymbolBreak:
			result = appendStrings(result, "0", ditTiming.symbolBreak)
		case cw.CharBreak:
			result = appendStrings(result, "0", ditTiming.charBreak)
		case cw.WordBreak:
			result = appendStrings(result, "0", ditTiming.wordBreak)
		}
	}
	result = appendStrings(result, "0", 3*ditTiming.wordBreak)
	return result
}

func appendStrings(result []string, s string, count int) []string {
	for i := 0; i < count; i++ {
		result = append(result, s)
	}
	return result
}

type recordingSink struct {
	characters []Character
}

func (s *recordingSink) CharacterDecoded(character Character) {
	s.characters = append(s.characters, character)
}

func TestDecoderGivesThePositionAndTheSpeedOfACharacter(t *testing.T) {
	const (
		sampleRate = 48000
		blockSize  = 512 // one tick is 10.667 ms
		wpm        = 20
	)
	sink := &recordingSink{}
	decoder := NewDecoder(sink, sampleRate, blockSize)

	// generateStream truncates one dit of 5.625 ticks to 5 ticks, so the stream is really 22.5 WPM.
	// r is dit-da-dit, thus 5 + 5 + 15 + 5 + 5 = 35 ticks, and the first mark begins at tick 0.
	// The text holds two characters, because the speed comes from the mean of the estimate of the
	// marks and the estimate of the gaps, and the estimate of the gaps needs a gap first.
	stream := generateStream(sampleRate, blockSize, wpm, defaultTiming, "rr")
	for _, state := range stream {
		decoder.Tick(state == "1")
	}
	decoder.stop()

	require.Len(t, sink.characters, 2)
	assert.Equal(t, 'r', sink.characters[0].Rune)
	assert.Equal(t, int64(0), sink.characters[0].Start, "the character begins with its first mark")
	assert.Equal(t, int64(35), sink.characters[0].End, "and it ends when its last mark ends")
	assert.InDelta(t, 22.5, sink.characters[1].WPM, 0.1, "the speed of the moment of the character")
}

// TestDecoderMarksTheEndOfAnOver covers the marker of a long gap. The decoder must give it while the
// signal is off: a station that stops for good gives no rising edge again.
func TestDecoderMarksTheEndOfAnOver(t *testing.T) {
	const (
		sampleRate = 48000
		blockSize  = 512
		wpm        = 20
	)
	buffer := bytes.NewBuffer([]byte{})
	decoder := NewDecoder(NewTextSink(buffer), sampleRate, blockSize)

	stream := generateStream(sampleRate, blockSize, wpm, defaultTiming, "paris paris")
	for _, state := range stream {
		decoder.Tick(state == "1")
	}
	// the silence of one over, thus far more than overGapDits
	for range 200 {
		decoder.Tick(false)
	}

	assert.Equal(t, "paris paris"+string(OverMarker), buffer.String())
}

// TestDecoderGivesOneMarkerForOneGap checks that a silence that lasts gives one marker, and that the
// word break behind it does not give a space too: the marker is already a break.
func TestDecoderGivesOneMarkerForOneGap(t *testing.T) {
	const (
		sampleRate = 48000
		blockSize  = 512
		wpm        = 20
	)
	buffer := bytes.NewBuffer([]byte{})
	decoder := NewDecoder(NewTextSink(buffer), sampleRate, blockSize)

	first := generateStream(sampleRate, blockSize, wpm, defaultTiming, "paris")
	for _, state := range first {
		decoder.Tick(state == "1")
	}
	for range 400 {
		decoder.Tick(false)
	}
	second := generateStream(sampleRate, blockSize, wpm, defaultTiming, "paris")
	for _, state := range second {
		decoder.Tick(state == "1")
	}

	assert.Equal(t, "paris"+string(OverMarker)+"paris", buffer.String())
}

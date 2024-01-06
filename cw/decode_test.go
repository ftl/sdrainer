package cw

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ftl/digimodes/cw"
	"github.com/stretchr/testify/assert"
)

func TestToCWChar(t *testing.T) {
	a := cwChar{cw.Dit, cw.Da}
	assert.Equal(t, a, toCWChar(cw.Dit, cw.Da))
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
	decoder := NewDecoder(buffer, sampleRate, blockSize)

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

			assert.Equal(t, expected, buffer.String())
		})
	}
}

func TestDecoder_SpeedTolerance(t *testing.T) {
	const sampleRate = 48000
	const blockSize = 512
	buffer := bytes.NewBuffer([]byte{})
	decoder := NewDecoder(buffer, sampleRate, blockSize)
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

		if expected == buffer.String() && minWpm == 0 {
			minWpm = wpm
		}
		if expected != buffer.String() && minWpm != 0 && maxWpm == 0 {
			maxWpm = wpm - 1
		}
	}

	assert.Equal(t, 10, minWpm, "min")
	assert.Equal(t, 28, maxWpm, "max")
}

func TestDecoder_SpeedAdaptionRate(t *testing.T) {
	const sampleRate = 48000
	const blockSize = 512
	buffer := bytes.NewBuffer([]byte{})
	decoder := NewDecoder(buffer, sampleRate, blockSize)
	expected := "paris"

	tt := []struct {
		wpm            int
		expectedRounds int
	}{
		{29, 2},
		{30, 2},
		{35, 2},
		{37, 2},
		{38, 10},
		{11, 1},
		{10, 1},
		{9, 10},
	}
	for _, tc := range tt {
		t.Run(fmt.Sprintf("%d", tc.wpm), func(t *testing.T) {
			stream := generateStream(sampleRate, blockSize, tc.wpm, defaultTiming, expected)
			rounds := 0
			actual := ""
			decoder.Reset()
			for actual != expected && rounds < 10 {
				buffer.Reset()
				decoder.Clear()

				for _, state := range stream {
					decoder.Tick(state == "1")
				}
				decoder.stop()
				actual = buffer.String()

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
	decoder := NewDecoder(buffer, sampleRate, blockSize)
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
			actual = buffer.String()

			rounds++
		}

		if rounds < maxRounds && minWpm == 0 {
			minWpm = wpm
		}
		if rounds < maxRounds && minWpm != 0 {
			maxWpm = wpm
		}
	}

	assert.Equal(t, 10, minWpm, "min")
	assert.Equal(t, 56, maxWpm, "max")
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

	symbols := make([]cw.Symbol, 0)
	symbolStream := make(chan cw.Symbol)
	go func() {
		for s := range symbolStream {
			symbols = append(symbols, s)
		}
	}()
	cw.WriteToSymbolStream(context.Background(), symbolStream, text)
	close(symbolStream)

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

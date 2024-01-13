package cw

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
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

	assert.Equal(t, 12, minWpm, "min")
	assert.Equal(t, 37, maxWpm, "max")
}

func TestDecoder_SpeedAdaptionRate(t *testing.T) {
	const sampleRate = 48000
	const blockSize = 512
	const maxRounds = 15
	buffer := bytes.NewBuffer([]byte{})
	decoder := NewDecoder(buffer, sampleRate, blockSize)
	expected := "paris"

	tt := []struct {
		wpm            int
		expectedRounds int
	}{
		{28, 1},
		{29, 1},
		{38, 2},
		{56, 2},
		{57, maxRounds},
		{12, 1},
		{11, 2},
		{10, 2},
		{7, 2},
		{6, 2},
		{5, maxRounds},
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

	assert.Equal(t, 6, minWpm, "min")
	assert.Equal(t, 56, maxWpm, "max")
}

func TestDecoder_RecordedStreams(t *testing.T) {
	const sampleRate = 48000
	const blockSize = 512
	tt := []struct {
		filename string
		expected string
	}{
		{filename: "db100fk_1.txt", expected: "i100fk"},
		{filename: "db100fk_2.txt", expected: "i100fk cq db1drfk"},
		{filename: "db100fk_3.txt", expected: "i100fk cq db1drfk db100fk"},
		{filename: "gb4wwa.txt", expected: "rqgb4wwa gb4wwa up"},
		{filename: "ly2px_1.txt", expected: "q cq"},
		{filename: "ly2px_2.txt", expected: "q cqcqde"},
		{filename: "ly2px_3.txt", expected: "q cqcqde ly2px ly2px"},
		{filename: "ly2px_4.txt", expected: "q cqcqde ly2px ly2px cqcqcqde ly2px ly2px ly2gx ä"},
	}

	buffer := bytes.NewBuffer([]byte{})
	decoder := NewDecoder(buffer, sampleRate, blockSize)
	decoder.traceEdges = false
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

			assert.Equal(t, tc.expected, buffer.String())
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

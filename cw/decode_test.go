package cw

import (
	"bufio"
	"bytes"
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
	assert.Equal(t, '§', table[toCWChar(cw.Dit, cw.Dit, cw.Dit, cw.Dit, cw.Dit, cw.Dit, cw.Dit, cw.Dit)])
}

func TestDecoder_RecordedStreams(t *testing.T) {
	buffer := bytes.NewBuffer([]byte{})
	decoder := NewDecoder(buffer, 48000, 240)

	stream, err := readLines("pse.txt")
	require.NoError(t, err)
	for _, state := range stream {
		decoder.Tick(state == "1")
	}
	decoder.stop()

	assert.Equal(t, "pse", buffer.String())
}

func TestDitToWPM(t *testing.T) {
	assert.Equal(t, 20.0, ditToWPM(60*time.Millisecond))
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

package iq

import (
	"encoding/binary"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWriterHoldsTheSamplesOfTheStream checks the layout of the file: interleaved float32, little
// endian, without a header.
func TestWriterHoldsTheSamplesOfTheStream(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "test.iq")
	samples := []float32{1, -1, 0.5, -0.5, 0, 0.25}

	writer, err := NewWriter(filename)
	require.NoError(t, err)
	require.NoError(t, writer.RecordIQ(samples))
	require.NoError(t, writer.Close())

	content, err := os.ReadFile(filename)
	require.NoError(t, err)
	require.Len(t, content, 4*len(samples), "one float32 for each value")

	for i, expected := range samples {
		actual := math.Float32frombits(binary.LittleEndian.Uint32(content[4*i:]))
		assert.Equalf(t, expected, actual, "the value at %d", i)
	}
}

// TestWriterKeepsTheStreamOverMoreThanOneCall checks that the calls of RecordIQ give one continuous
// stream, and that the buffer of one call does not disturb the next one.
func TestWriterKeepsTheStreamOverMoreThanOneCall(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "test.iq")

	writer, err := NewWriter(filename)
	require.NoError(t, err)
	require.NoError(t, writer.RecordIQ([]float32{1, 2, 3, 4}))
	require.NoError(t, writer.RecordIQ([]float32{5, 6}))
	require.NoError(t, writer.RecordIQ([]float32{7, 8, 9, 10, 11, 12}))
	require.NoError(t, writer.Close())

	content, err := os.ReadFile(filename)
	require.NoError(t, err)
	require.Len(t, content, 4*12)

	for i := range 12 {
		actual := math.Float32frombits(binary.LittleEndian.Uint32(content[4*i:]))
		assert.Equalf(t, float32(i+1), actual, "the value at %d", i)
	}
}

// TestWriterNeedsTheCloseForTheLastSamples shows why the caller must call Close: the samples stay in
// the buffer of the writer until then.
func TestWriterNeedsTheCloseForTheLastSamples(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "test.iq")

	writer, err := NewWriter(filename)
	require.NoError(t, err)
	require.NoError(t, writer.RecordIQ([]float32{1, 2}))

	content, err := os.ReadFile(filename)
	require.NoError(t, err)
	assert.Empty(t, content, "the samples are still in the buffer")

	require.NoError(t, writer.Close())

	content, err = os.ReadFile(filename)
	require.NoError(t, err)
	assert.Len(t, content, 8, "Close writes the buffer")
}

func TestNewWriterGivesAnErrorForABadPath(t *testing.T) {
	_, err := NewWriter(filepath.Join(t.TempDir(), "no-such-directory", "test.iq"))

	assert.Error(t, err)
}

// TestReaderGivesTheSamplesOfTheWriter is the round trip: what the writer wrote, the reader gives
// back.
func TestReaderGivesTheSamplesOfTheWriter(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "test.iq")
	expected := make([]float32, 1000)
	for i := range expected {
		expected[i] = float32(i) / 1000
	}

	writer, err := NewWriter(filename)
	require.NoError(t, err)
	require.NoError(t, writer.RecordIQ(expected[:400]))
	require.NoError(t, writer.RecordIQ(expected[400:]))
	require.NoError(t, writer.Close())

	reader, err := NewReader(filename)
	require.NoError(t, err)
	defer reader.Close()

	assert.Equal(t, int64(500), reader.IQSamples(), "1000 values are 500 IQ samples")

	var actual []float32
	buffer := make([]float32, 256)
	for {
		count, err := reader.ReadIQ(buffer)
		actual = append(actual, buffer[:count]...)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}

	assert.Equal(t, expected, actual)
}

// TestReaderGivesOnlyCompleteSamples checks the end of a file that stopped in the middle of an IQ
// sample. A recording of a source that was killed can end that way.
func TestReaderGivesOnlyCompleteSamples(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "test.iq")
	// 3 values and 2 bytes: one IQ sample, and then a value of I without its value of Q
	require.NoError(t, os.WriteFile(filename, make([]byte, 4*3+2), 0o600))

	reader, err := NewReader(filename)
	require.NoError(t, err)
	defer reader.Close()

	count, err := reader.ReadIQ(make([]float32, 256))

	assert.Equal(t, io.EOF, err)
	assert.Equal(t, 2, count, "the value of I without its value of Q must not come out")
}

func TestNewReaderGivesAnErrorForAMissingFile(t *testing.T) {
	_, err := NewReader(filepath.Join(t.TempDir(), "no-such-file.iq"))

	assert.Error(t, err)
}

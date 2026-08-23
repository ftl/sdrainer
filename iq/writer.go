// Package iq reads and writes recordings of an IQ stream.
//
// The file holds the samples in the layout that the pipeline uses: interleaved I and Q values as
// float32, little endian, without a header. The sample rate and the center frequency are not in the
// file, and a command takes them as flags. doc/architecture.md, section 9, gives the reason.
package iq

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"math"
	"os"
)

// SampleSize is the size of one IQ sample in the file: one float32 for I and one for Q.
const SampleSize = 8

// writeBufferSize is the buffer between the goroutine of the source and the disk. The stream is
// 96 kB/s at 12 kHz, so this buffer covers approximately 0.7 s of it.
const writeBufferSize = 64 * 1024

// Writer records an IQ stream into a file.
type Writer struct {
	file   *os.File
	out    *bufio.Writer
	buffer []byte
}

// NewWriter makes the file for a recording. The caller must call Close, otherwise the last samples
// stay in the buffer.
func NewWriter(filename string) (*Writer, error) {
	file, err := os.Create(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot create the IQ file %s: %w", filename, err)
	}

	return &Writer{
		file: file,
		out:  bufio.NewWriterSize(file, writeBufferSize),
	}, nil
}

// RecordIQ writes the given interleaved IQ samples. The pipeline calls this method with the samples
// that it processes.
func (w *Writer) RecordIQ(samples []float32) error {
	size := 4 * len(samples)
	if cap(w.buffer) < size {
		w.buffer = make([]byte, size)
	}
	w.buffer = w.buffer[:size]

	for i, sample := range samples {
		binary.LittleEndian.PutUint32(w.buffer[4*i:], math.Float32bits(sample))
	}

	_, err := w.out.Write(w.buffer)
	return err
}

// Close writes the samples that are still in the buffer and closes the file.
func (w *Writer) Close() error {
	err := w.out.Flush()
	if closeErr := w.file.Close(); err == nil {
		err = closeErr
	}
	return err
}

// Name gives the name of the file of this recording.
func (w *Writer) Name() string {
	return w.file.Name()
}

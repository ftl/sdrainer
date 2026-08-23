package iq

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

// readBufferSize is the buffer between the disk and the caller.
const readBufferSize = 64 * 1024

// Reader reads a recording of an IQ stream.
type Reader struct {
	file   *os.File
	in     *bufio.Reader
	buffer []byte
}

// NewReader opens a recording.
func NewReader(filename string) (*Reader, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot open the IQ file %s: %w", filename, err)
	}

	return &Reader{
		file: file,
		in:   bufio.NewReaderSize(file, readBufferSize),
	}, nil
}

// ReadIQ fills the given slice with interleaved IQ samples and gives the count of the values that it
// filled in. That count is even, because a value of I without its value of Q is no sample. The
// error is io.EOF at the end of the file, and the count of that last call can still be above 0.
func (r *Reader) ReadIQ(samples []float32) (int, error) {
	size := 4 * len(samples)
	if cap(r.buffer) < size {
		r.buffer = make([]byte, size)
	}
	r.buffer = r.buffer[:size]

	read, err := io.ReadFull(r.in, r.buffer)
	if err == io.ErrUnexpectedEOF {
		// the file ended in the middle of the slice, which is the usual case for the last call
		err = io.EOF
	}

	count := read / 4
	count -= count % 2

	for i := range count {
		samples[i] = math.Float32frombits(binary.LittleEndian.Uint32(r.buffer[4*i:]))
	}

	return count, err
}

// IQSamples gives the count of the IQ samples of the whole file.
func (r *Reader) IQSamples() int64 {
	info, err := r.file.Stat()
	if err != nil {
		return 0
	}
	return info.Size() / SampleSize
}

func (r *Reader) Close() error {
	return r.file.Close()
}

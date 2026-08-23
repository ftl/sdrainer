package listen

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
)

// wavHeaderSize is the size of the header of a WAV file with 16 bit PCM.
const wavHeaderSize = 44

// wavWriter writes a WAV file with 16 bit PCM, mono. The count of the samples must be known before
// the first sample, because the two sizes of the header stand before the data.
type wavWriter struct {
	file *os.File
	out  *bufio.Writer
	raw  [2]byte
}

func newWAVWriter(filename string, sampleRate int, sampleCount int) (*wavWriter, error) {
	file, err := os.Create(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot create the WAV file %s: %w", filename, err)
	}

	result := &wavWriter{
		file: file,
		out:  bufio.NewWriterSize(file, 64*1024),
	}
	if err := result.writeHeader(sampleRate, sampleCount); err != nil {
		file.Close()
		return nil, err
	}

	return result, nil
}

func (w *wavWriter) writeHeader(sampleRate int, sampleCount int) error {
	const (
		pcm      = 1
		channels = 1
		bits     = 16
	)
	dataSize := uint32(2 * sampleCount)
	header := make([]byte, 0, wavHeaderSize)

	header = append(header, "RIFF"...)
	header = binary.LittleEndian.AppendUint32(header, 36+dataSize)
	header = append(header, "WAVE"...)

	header = append(header, "fmt "...)
	header = binary.LittleEndian.AppendUint32(header, 16) // the size of this chunk
	header = binary.LittleEndian.AppendUint16(header, pcm)
	header = binary.LittleEndian.AppendUint16(header, channels)
	header = binary.LittleEndian.AppendUint32(header, uint32(sampleRate))
	header = binary.LittleEndian.AppendUint32(header, uint32(sampleRate*channels*bits/8))
	header = binary.LittleEndian.AppendUint16(header, channels*bits/8)
	header = binary.LittleEndian.AppendUint16(header, bits)

	header = append(header, "data"...)
	header = binary.LittleEndian.AppendUint32(header, dataSize)

	_, err := w.out.Write(header)
	return err
}

func (w *wavWriter) write(value int16) error {
	binary.LittleEndian.PutUint16(w.raw[:], uint16(value))
	_, err := w.out.Write(w.raw[:])
	return err
}

func (w *wavWriter) Close() error {
	err := w.out.Flush()
	if closeErr := w.file.Close(); err == nil {
		err = closeErr
	}
	return err
}

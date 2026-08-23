package hpsdr

import (
	"github.com/jancona/hpsdr"
)

// toIQ writes the samples of one receiver into the buffer, as the interleaved I and Q values that
// Pipeline.IQData takes. It gives the part of the buffer that holds the samples.
//
// The buffer belongs to the caller and it grows when it is too small, so one receiver needs one
// allocation and no more: the device gives 25 to 126 samples with each call, and it calls many
// times for each second.
//
// **The scale of the values comes from the library.** One sample of the device is 24 bit and
// signed, and IFloat and QFloat give a value between −1 and 1. The pipeline works with the relation
// of the values and not with their absolute size, so the exact scale changes nothing.
func toIQ(buffer []float32, samples []hpsdr.ReceiveSample) []float32 {
	needed := 2 * len(samples)
	if cap(buffer) < needed {
		buffer = make([]float32, needed)
	}
	buffer = buffer[:needed]

	for i, sample := range samples {
		buffer[2*i] = sample.IFloat()
		buffer[2*i+1] = sample.QFloat()
	}

	return buffer
}

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
// **The two parts stand in the other order than their names say.** The pipeline takes the real part
// first and the imaginary part second, and the device gives the part that the protocol calls Q as
// the real one. A stream in the wrong order gives a spectrum that is mirrored about its center: a
// station above the center then stands below it, by the same distance.
//
// A measurement with a Hermes-Lite 2 on 2026-08-24 shows it. The center was 7024 kHz:
//
//	the station stood at   7034.5 kHz, thus 10.5 kHz above the center
//	SDRainer reported it at 7013.5 kHz, thus 10.5 kHz below the center
//
// A station **on** the center was correct at the same time, because the mirror of 0 is 0, and the
// speed of each station was correct, because a mirror does not change the time.
//
// The same is true of the openHPSDR protocol for the transmit direction, and the document of that
// protocol names it: "The I&Q samples, relative to receive, are reversed. This is a historical bug
// that goes back to the very first version of PowerSDR."
//
// A conjugation, thus a Q with the other sign, mirrors the spectrum as well and it would do the
// same work here. The pipeline uses the magnitude of the spectrum and the envelope of the keying,
// and neither of the two sees the difference between the two ways.
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
		buffer[2*i] = sample.QFloat()
		buffer[2*i+1] = sample.IFloat()
	}

	return buffer
}

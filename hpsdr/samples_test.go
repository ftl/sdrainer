package hpsdr

import (
	"testing"

	"github.com/jancona/hpsdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sample makes one sample of the device out of the 24 bit values of I and of Q. The device sends
// the most significant byte first, see doc/hpsdr_plan.md, section 1.1.
func sample(i int32, q int32) hpsdr.ReceiveSample {
	return hpsdr.ReceiveSample{
		I2: byte(i >> 16), I1: byte(i >> 8), I0: byte(i),
		Q2: byte(q >> 16), Q1: byte(q >> 8), Q0: byte(q),
	}
}

// TestToIQPutsTheQFieldFirst holds the order of the two parts, which a measurement with a
// Hermes-Lite 2 gave: the field that the protocol calls Q is the real part of the sample, and the
// pipeline takes the real part first. See toIQ.
//
// **This test cannot find that order, it can only hold it.** Each sample of this file comes from
// the same assumption that toIQ uses, so a test of this kind agrees with itself whatever the order
// is. Only a device or a recording of one says which order is right.
func TestToIQPutsTheQFieldFirst(t *testing.T) {
	samples := []hpsdr.ReceiveSample{
		sample(0x7FFFFF, 0),        // the largest positive value in the field of I
		sample(0, -0x800000),       // the largest negative value in the field of Q
		sample(0x400000, 0x400000), // one half of the largest value in both
	}

	result := toIQ(nil, samples)

	require.Len(t, result, 6)
	assert.InDelta(t, 0.0, result[0], 0.001, "the real part of the first sample is its Q field")
	assert.InDelta(t, 1.0, result[1], 0.001, "and the imaginary part is its I field")
	assert.InDelta(t, -1.0, result[2], 0.001, "the real part of the second sample")
	assert.InDelta(t, 0.0, result[3], 0.001, "and its imaginary part")
	assert.InDelta(t, 0.5, result[4], 0.001, "both parts of the third sample")
	assert.InDelta(t, 0.5, result[5], 0.001)
}

// TestToIQKeepsTheSign covers the byte order and the sign: the value of the device is signed, and a
// value that the code reads as unsigned gives a signal that is not there.
func TestToIQKeepsTheSign(t *testing.T) {
	result := toIQ(nil, []hpsdr.ReceiveSample{sample(-1, 1)})

	require.Len(t, result, 2)
	assert.Greater(t, result[0], float32(0), "the Q field of 1 gives the real part")
	assert.Less(t, result[1], float32(0), "the I field of −1 gives the imaginary part")
	assert.InDelta(t, 0.0, result[0], 0.001, "and both must be near zero")
	assert.InDelta(t, 0.0, result[1], 0.001)
}

// TestToIQUsesTheBufferAgain keeps the allocations away from the callback of the device: it comes
// many times for each second.
func TestToIQUsesTheBufferAgain(t *testing.T) {
	buffer := make([]float32, 0, 16)
	samples := []hpsdr.ReceiveSample{sample(1, 2), sample(3, 4)}

	first := toIQ(buffer, samples)
	require.Len(t, first, 4)

	second := toIQ(first, samples)

	require.Len(t, second, 4)
	assert.Equal(t, &first[0], &second[0], "the second call uses the buffer of the first one")
}

// TestToIQGrowsTheBuffer covers the buffer that is too small: the count of the samples of one call
// comes from the count of the receivers, and it changes with that count.
func TestToIQGrowsTheBuffer(t *testing.T) {
	buffer := make([]float32, 0, 2)
	samples := []hpsdr.ReceiveSample{sample(1, 1), sample(2, 2), sample(3, 3)}

	result := toIQ(buffer, samples)

	assert.Len(t, result, 6)
}

func TestToIQWithoutSamples(t *testing.T) {
	assert.Empty(t, toIQ(nil, nil))
}

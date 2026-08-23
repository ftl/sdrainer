package dsp

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNCOMovesAToneToZero is the case that the listen command needs: a tone at +1000 Hz, mixed with
// −1000 Hz, has no more rotation. Its two parts are then constant.
func TestNCOMovesAToneToZero(t *testing.T) {
	const (
		sampleRate = 12000
		frequency  = 1000.0
		count      = 1200
	)

	tone := NewNCO(frequency, sampleRate)
	mixer := NewNCO(-frequency, sampleRate)

	firstI, firstQ := 0.0, 0.0
	for i := range count {
		toneI, toneQ := tone.Mix(1, 0)
		actualI, actualQ := mixer.Mix(toneI, toneQ)

		if i == 0 {
			firstI, firstQ = actualI, actualQ
			continue
		}
		assert.InDeltaf(t, firstI, actualI, 1e-9, "the value of I at %d", i)
		assert.InDeltaf(t, firstQ, actualQ, 1e-9, "the value of Q at %d", i)
	}
}

// TestNCOKeepsTheLevel checks that the mixer changes no level: it turns a complex value and it does
// not make it larger or smaller.
func TestNCOKeepsTheLevel(t *testing.T) {
	nco := NewNCO(700, 12000)

	for range 1000 {
		i, q := nco.Mix(0.6, 0.8)

		assert.InDelta(t, 1.0, math.Hypot(i, q), 1e-9)
	}
}

func TestNCOGivesTheRightFrequency(t *testing.T) {
	const sampleRate = 12000

	// one turn of 600 Hz needs 20 samples at 12 kHz
	nco := NewNCO(600, sampleRate)
	var first, last float64
	for i := range 21 {
		value, _ := nco.Mix(1, 0)
		if i == 0 {
			first = value
		}
		last = value
	}

	assert.InDelta(t, first, last, 1e-9, "after 20 samples the oscillator must be at the same phase")
}

func TestNCOResetRepeatsTheStream(t *testing.T) {
	nco := NewNCO(1234, 12000)

	expected := make([]float64, 100)
	for i := range expected {
		expected[i], _ = nco.Mix(1, 0)
	}

	nco.Reset()
	for i := range expected {
		actual, _ := nco.Mix(1, 0)
		require.InDeltaf(t, expected[i], actual, 1e-12, "the value at %d", i)
	}
}

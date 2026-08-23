package cw

import (
	"bytes"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const errorRateText = "cq cq de dl1abc dl1abc pse k"

// noisyEnvelope converts a keying stream into the sequence of power values that one bin of a
// spectrum sees: the power of a complex signal with additive complex Gaussian noise. The noise
// power is 1, so the given SNR defines the amplitude of the signal.
func noisyEnvelope(stream []string, snrDB float64, seed uint64) []float64 {
	random := rand.New(rand.NewPCG(seed, seed+0x9e3779b9))
	amplitude := math.Sqrt(math.Pow(10, snrDB/10))

	result := make([]float64, len(stream))
	for i, state := range stream {
		inPhase := random.NormFloat64() * math.Sqrt2 / 2
		quadrature := random.NormFloat64() * math.Sqrt2 / 2
		if state == "1" {
			inPhase += amplitude
		}
		result[i] = inPhase*inPhase + quadrature*quadrature
	}
	return result
}

func decodeEnvelope(t *testing.T, sampleRate int, blockSize int, envelope []float64) string {
	t.Helper()

	buffer := bytes.NewBuffer([]byte{})
	d := NewSpectralDemodulator[float64](NewTextSink(buffer), sampleRate, blockSize)
	for _, value := range envelope {
		d.Tick(value)
	}
	d.decoder.stop()

	return decoded(buffer)
}

func characterErrorRate(expected string, actual string) float64 {
	runes := []rune(expected)
	if len(runes) == 0 {
		return 0
	}
	return float64(editDistance(runes, []rune(actual))) / float64(len(runes))
}

func editDistance(a []rune, b []rune) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}

	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}

	return previous[len(b)]
}

// TestDecoder_CharacterErrorRate is the specification of the decoder. It measures the character
// error rate of a long transmission against the speed and the SNR, as the research document asks
// for in section 13. The SNR is the ratio of the signal power to the noise power in one bin of the
// spectrum, not in a bandwidth of 500 Hz.
//
// The limits below are the measured values plus headroom. They are a specification, not a target:
// raise the requirement when the decoder gets better, and never lower it without a reason.
func TestDecoder_CharacterErrorRate(t *testing.T) {
	const (
		sampleRate = 48000
		blockSize  = 512
	)
	text := steadyStateText(8)

	tt := []struct {
		wpm          int
		snr          float64
		maxErrorRate float64
	}{
		{wpm: 5, snr: 30, maxErrorRate: 0.25},
		{wpm: 10, snr: 30, maxErrorRate: 0.08},
		{wpm: 15, snr: 30, maxErrorRate: 0.03},
		{wpm: 20, snr: 30, maxErrorRate: 0.01},
		{wpm: 25, snr: 30, maxErrorRate: 0.01},
		{wpm: 40, snr: 30, maxErrorRate: 0.01},

		{wpm: 5, snr: 20, maxErrorRate: 0.25},
		{wpm: 10, snr: 20, maxErrorRate: 0.08},
		{wpm: 15, snr: 20, maxErrorRate: 0.04},
		{wpm: 20, snr: 20, maxErrorRate: 0.05},
		{wpm: 25, snr: 20, maxErrorRate: 0.10},
	}
	for _, tc := range tt {
		t.Run(fmt.Sprintf("%dwpm_%.0fdB", tc.wpm, tc.snr), func(t *testing.T) {
			stream := generateStream(sampleRate, blockSize, tc.wpm, defaultTiming, text)
			envelope := noisyEnvelope(stream, tc.snr, 23)

			actual := decodeEnvelope(t, sampleRate, blockSize, envelope)

			assert.LessOrEqual(t, characterErrorRate(text, actual), tc.maxErrorRate)
		})
	}
}

// TestDecoder_ErrorsAtALowSpeedAreOnlyAtTheStart shows that the error rate at a low speed is the
// cost of the first characters, while the decoder finds the speed of the signal. A steady error
// would not decrease when the transmission gets longer.
func TestDecoder_ErrorsAtALowSpeedAreOnlyAtTheStart(t *testing.T) {
	const (
		sampleRate = 48000
		blockSize  = 512
		wpm        = 5
		snr        = 20
	)

	var errorRates []float64
	for _, repeat := range []int{1, 2, 4, 8} {
		text := steadyStateText(repeat)
		stream := generateStream(sampleRate, blockSize, wpm, defaultTiming, text)
		envelope := noisyEnvelope(stream, snr, 23)

		actual := decodeEnvelope(t, sampleRate, blockSize, envelope)
		errorRates = append(errorRates, characterErrorRate(text, actual))
	}

	for i := 1; i < len(errorRates); i++ {
		assert.Lessf(t, errorRates[i], errorRates[i-1]*0.75,
			"a transmission of two times the length must have a much lower error rate: %v", errorRates)
	}
}

func steadyStateText(repeat int) string {
	return strings.TrimSpace(strings.Repeat(errorRateText+" ", repeat))
}

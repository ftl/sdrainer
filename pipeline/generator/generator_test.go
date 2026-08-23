package generator

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ftl/digimodes/cw"
	"github.com/ftl/sdrainer/dsp"
)

func TestToElementsCoversTheWholeMorseTable(t *testing.T) {
	require.Greater(t, len(cw.Code), 42, "the table must hold more than the characters of the old local table")

	for r := range cw.Code {
		t.Run(string(r), func(t *testing.T) {
			elements := toElements(string(r), 1)

			assert.NotEmpty(t, elements, "the generator must be able to send this character")
		})
	}
}

func TestToElementsParisTiming(t *testing.T) {
	elements := toElements("PARIS", 1)

	var total int
	for _, e := range elements {
		total += e.samples
	}

	assert.Equal(t, 50, total, "PARIS and one word break are 50 dits")
}

func TestGeneratorPutsSignalsOnTheConfiguredFrequencies(t *testing.T) {
	const (
		sampleRate      = 48000
		blockSize       = 4096
		centerFrequency = 7020000.0
	)
	expectedFrequencies := []float64{7018000, 7021500}

	g := New[float32, float64](GeneratorConfig[float64]{
		SampleRate:      sampleRate,
		CenterFrequency: centerFrequency,
		Signals: []CWSignal[float64]{
			{Frequency: expectedFrequencies[0], Text: "PARIS", WPM: 20},
			{Frequency: expectedFrequencies[1], Text: "CQ TEST", WPM: 25},
		},
	})

	iq := make([]float32, 2*blockSize)
	g.Read(iq)

	spectrum := make(dsp.Block[float32], blockSize)
	psd := make([]float32, blockSize)
	dsp.NewFFT[float32]().IQToSpectrumAndPSD(spectrum, psd, iq, dsp.Magnitude[float32])

	mapping := dsp.NewFrequencyMapping[float64](sampleRate, blockSize, centerFrequency)
	binWidth := float64(sampleRate) / float64(blockSize)
	mean := spectrum.Mean(0, blockSize-1)

	for _, expected := range expectedFrequencies {
		expectedBin := mapping.FrequencyToBin(expected)
		require.Greater(t, spectrum[expectedBin], 10*mean, "%.0f Hz must be far above the mean", expected)

		peakBin := maxBin(spectrum, expectedBin-10, expectedBin+10)
		actual := mapping.BinToFrequency(peakBin, dsp.BinFrom)
		assert.InDelta(t, expected, actual, binWidth, "peak frequency of the signal at %.0f Hz", expected)
	}
}

func maxBin(spectrum dsp.Block[float32], from int, to int) int {
	result := from
	for i := from; i <= to; i++ {
		if spectrum[i] > spectrum[result] {
			result = i
		}
	}
	return result
}

func TestGeneratorKeysTheSignal(t *testing.T) {
	const (
		sampleRate = 48000
		wpm        = 20
	)
	ditSamples := int(math.Round(1.2 * sampleRate / wpm))

	g := New[float32, float64](GeneratorConfig[float64]{
		SampleRate:      sampleRate,
		CenterFrequency: 7020000,
		Signals: []CWSignal[float64]{
			{Frequency: 7020000, Text: "E", WPM: wpm}, // one dit, then a word break
		},
	})

	iq := make([]float32, 2*8*ditSamples)
	g.Read(iq)

	var onSamples int
	for i := 0; i+1 < len(iq); i += 2 {
		magnitude := math.Hypot(float64(iq[i]), float64(iq[i+1]))
		if magnitude > 0.5 {
			onSamples++
		}
	}

	assert.InDelta(t, ditSamples, onSamples, 2, "one dit is on, the word break is off")
}

func TestGeneratorCarrierHasNoGaps(t *testing.T) {
	const (
		sampleRate = 48000
		samples    = 4096
	)

	g := New[float32, float64](GeneratorConfig[float64]{
		SampleRate:      sampleRate,
		CenterFrequency: 7020000,
		Signals: []CWSignal[float64]{
			{Frequency: 7020000, Carrier: true, Text: "this text has no effect"},
		},
	})

	iq := make([]float32, 2*samples)
	g.Read(iq)

	for i := 0; i+1 < len(iq); i += 2 {
		magnitude := math.Hypot(float64(iq[i]), float64(iq[i+1]))
		require.InDeltaf(t, 1, magnitude, 1e-6, "sample %d of a carrier must have the full amplitude", i/2)
	}
}

// TestGeneratorSendsTheTextOneTimeForEachTurn checks the turns of a QSO. "paris" at 20 WPM needs
// exactly 3 s, and the turn is 6 s, so the signal is on in the first half of each turn and silent
// in the second half. The signal with the offset is silent until its first turn begins.
func TestGeneratorSendsTheTextOneTimeForEachTurn(t *testing.T) {
	const (
		sampleRate = 12000
		turn       = 6 * time.Second
		seconds    = 12
	)

	g := New[float32, float64](GeneratorConfig[float64]{
		SampleRate:      sampleRate,
		CenterFrequency: 7028000,
		Signals: []CWSignal[float64]{
			{Frequency: 7028000, Text: "paris", WPM: 20, TurnPeriod: turn},
		},
	})

	iq := make([]float32, 2*seconds*sampleRate)
	g.Read(iq)

	tt := []struct {
		from, to float64
		on       bool
	}{
		{from: 0.5, to: 2.5, on: true},
		{from: 3.5, to: 5.5, on: false},
		{from: 6.5, to: 8.5, on: true},
		{from: 9.5, to: 11.5, on: false},
	}
	for _, tc := range tt {
		power := powerBetween(iq, sampleRate, tc.from, tc.to)
		if tc.on {
			assert.Greaterf(t, power, 0.01, "%.1f s to %.1f s must hold the text", tc.from, tc.to)
		} else {
			assert.Zerof(t, power, "%.1f s to %.1f s must be silent", tc.from, tc.to)
		}
	}
}

func TestGeneratorDelaysTheFirstTurnByTheOffset(t *testing.T) {
	const (
		sampleRate = 12000
		seconds    = 8
	)

	g := New[float32, float64](GeneratorConfig[float64]{
		SampleRate:      sampleRate,
		CenterFrequency: 7028000,
		Signals: []CWSignal[float64]{
			{Frequency: 7028000, Text: "paris", WPM: 20, TurnPeriod: 6 * time.Second, TurnOffset: 3 * time.Second},
		},
	})

	iq := make([]float32, 2*seconds*sampleRate)
	g.Read(iq)

	assert.Zero(t, powerBetween(iq, sampleRate, 0, 2.9), "the signal must be silent before its first turn")
	assert.Greater(t, powerBetween(iq, sampleRate, 3.5, 5.5), 0.01, "the first turn must hold the text")
}

// powerBetween gives the mean power of the IQ stream between the two times, in seconds.
func powerBetween(iq []float32, sampleRate int, from float64, to float64) float64 {
	first := int(from*float64(sampleRate)) * 2
	last := min(int(to*float64(sampleRate))*2, len(iq))

	var sum float64
	for i := first; i+1 < last; i += 2 {
		sum += float64(iq[i])*float64(iq[i]) + float64(iq[i+1])*float64(iq[i+1])
	}
	return 2 * sum / float64(last-first)
}

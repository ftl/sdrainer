package cw

import (
	"bytes"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDebounceThreshold(t *testing.T) {
	tt := []struct {
		desc         string
		tickInterval time.Duration
		expected     int
	}{
		{desc: "a tick is longer than the shortest element", tickInterval: 10667 * time.Microsecond, expected: 1},
		{desc: "a tick is the shortest element", tickInterval: minElementTime, expected: 1},
		{desc: "two ticks are the shortest element", tickInterval: minElementTime / 2, expected: 2},
		{desc: "four ticks are the shortest element", tickInterval: minElementTime / 4, expected: 4},
		{desc: "no tick interval", tickInterval: 0, expected: 1},
	}
	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			assert.Equal(t, tc.expected, debounceThreshold(tc.tickInterval))
		})
	}
}

func TestSpectralDemodulator_FindsTheKeyingWithoutAThreshold(t *testing.T) {
	const (
		sampleRate = 48000
		blockSize  = 512
		markValue  = 100.0
		spaceValue = 10.0
	)
	buffer := bytes.NewBuffer([]byte{})
	d := NewSpectralDemodulator[float64](NewTextSink(buffer), sampleRate, blockSize)

	// A lead-in of gaps, because the demodulator reports a gap until its window holds values: it
	// knows neither level before that. The pipeline gives it the same lead-in, because the tracker
	// confirms a channel more than one second after the signal appeared.
	for range d.minLevelCount {
		d.Tick(spaceValue)
	}

	// "paris" at the speed that the decoder expects
	stream := generateStream(sampleRate, blockSize, defaultWPM, defaultTiming, "paris")
	for _, state := range stream {
		value := spaceValue
		if state == "1" {
			value = markValue
		}
		d.Tick(value)
	}
	d.decoder.stop()

	assert.Equal(t, "paris", decoded(buffer))
	assert.Greater(t, d.MarkLevel(), d.SpaceLevel(), "the mark level must be above the space level")
}

func TestSpectralDemodulator_IgnoresTheAbsoluteLevel(t *testing.T) {
	const (
		sampleRate = 48000
		blockSize  = 512
	)
	stream := generateStream(sampleRate, blockSize, defaultWPM, defaultTiming, "paris")

	tt := []struct {
		desc  string
		mark  float64
		space float64
	}{
		{desc: "a strong signal", mark: 1000, space: 1},
		{desc: "a weak signal", mark: 0.002, space: 0.001},
		{desc: "a small ratio", mark: 11, space: 10},
		{desc: "a large offset", mark: 1e6 + 100, space: 1e6},
	}
	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			buffer := bytes.NewBuffer([]byte{})
			d := NewSpectralDemodulator[float64](NewTextSink(buffer), sampleRate, blockSize)

			// a lead-in of gaps, so the window of the levels holds values before the text starts
			for range d.minLevelCount {
				d.Tick(tc.space)
			}
			for _, state := range stream {
				value := tc.space
				if state == "1" {
					value = tc.mark
				}
				d.Tick(value)
			}
			d.decoder.stop()

			assert.Equal(t, "paris", decoded(buffer))
		})
	}
}

func TestSpectralDemodulator_HysteresisStopsTheChatterOnAnEdge(t *testing.T) {
	const (
		sampleRate = 48000
		blockSize  = 512
	)
	buffer := bytes.NewBuffer([]byte{})
	d := NewSpectralDemodulator[float64](NewTextSink(buffer), sampleRate, blockSize)

	// give the demodulator two clear levels
	for range 200 {
		d.Tick(100)
		d.Tick(0)
	}
	require.Greater(t, d.MarkLevel(), d.SpaceLevel())

	// The demodulator makes its decision in the amplitude, so the limits of this test come from the
	// amplitude too. Tick takes the power, so each limit goes back into that domain with a square.
	markAmplitude := math.Sqrt(d.MarkLevel())
	spaceAmplitude := math.Sqrt(d.SpaceLevel())
	middle := spaceAmplitude + decisionFraction*(markAmplitude-spaceAmplitude)
	margin := defaultHysteresis * (markAmplitude - spaceAmplitude)
	power := func(amplitude float64) float64 { return amplitude * amplitude }

	// The assertions use the state of the trigger and not the value of Tick, because Tick gives the
	// state after the debouncer and that one needs more than one tick of the same value.
	decide := func(value float64) bool {
		d.Tick(value)
		return d.state
	}

	// a value exactly in the middle must not switch the state on
	d.state = false
	assert.False(t, decide(power(middle)), "the middle must not switch on")
	// but a value above the upper limit must
	d.state = false
	assert.True(t, decide(power(middle+2*margin)), "above the upper limit must switch on")
	// a value in the middle must not switch the state off again
	d.state = true
	assert.True(t, decide(power(middle)), "the middle must not switch off")
	// but a value below the lower limit must
	d.state = true
	assert.False(t, decide(power(middle-2*margin)), "below the lower limit must switch off")
}

func TestSpectralDemodulator_Reset(t *testing.T) {
	const (
		sampleRate = 48000
		blockSize  = 512
	)
	buffer := bytes.NewBuffer([]byte{})
	d := NewSpectralDemodulator[float64](NewTextSink(buffer), sampleRate, blockSize)

	for range 100 {
		d.Tick(100)
		d.Tick(0)
	}
	require.Greater(t, d.MarkLevel(), d.SpaceLevel())

	d.Reset()

	assert.Zero(t, d.MarkLevel(), "mark level")
	assert.Zero(t, d.SpaceLevel(), "space level")
	assert.False(t, d.state, "state")
}

// TestSpectralDemodulator_MeasuresTheLengthOfAMark pins the model of the two edges of a mark. The
// window of a spectral analysis slides over an edge, so the power of the bin follows the square of
// the part of the window that the mark covers, and the demodulator decides in the amplitude. A mark
// then begins at a coverage of decisionFraction+hysteresis and it ends at a coverage of
// decisionFraction-hysteresis, thus:
//
//	mark = true mark + window × (1 - 2 × decisionFraction)
//
// The hysteresis cancels, and only decisionFraction moves the edges. A decision in the power domain
// would give a mark that is 3 ticks too short instead of 2 ticks too long, so this test holds that
// correction as well.
func TestSpectralDemodulator_MeasuresTheLengthOfAMark(t *testing.T) {
	const (
		sampleRate = 48000
		blockSize  = 512
		window     = 8
		markTicks  = 20
		gapTicks   = 20
	)

	buffer := bytes.NewBuffer([]byte{})
	d := NewSpectralDemodulator[float64](NewTextSink(buffer), sampleRate, blockSize)

	var states []bool
	for range 20 {
		for _, coverage := range coverageOfOneElement(markTicks, gapTicks, window) {
			// the power is the square of the coverage
			states = append(states, d.Tick(coverage*coverage))
		}
	}

	// take the last complete mark and the last complete gap, when the levels are stable
	marks, gaps := lastRunLengths(states)
	expected := float64(markTicks) + window*(1-2*decisionFraction)
	assert.InDelta(t, expected, marks, 1, "the length of a mark")
	assert.InDelta(t, 2*markTicks-expected, gaps, 1, "the length of a gap")
	assert.Equal(t, markTicks+gapTicks, marks+gaps, "a mark and a gap together must keep the period")
}

// coverageOfOneElement gives the part of the window that the mark covers, for one mark and the gap
// after it. The value rises over the window at the start of the mark, and it falls over the window
// at the end of the mark.
func coverageOfOneElement(markTicks int, gapTicks int, window int) []float64 {
	result := make([]float64, 0, markTicks+gapTicks)
	for i := range markTicks {
		result = append(result, min(float64(i+1)/float64(window), 1))
	}
	for i := range gapTicks {
		result = append(result, max(float64(window-i-1)/float64(window), 0))
	}
	return result
}

// lastRunLengths gives the length of the last complete mark and of the last complete gap. The run at
// the end of the states is not complete, so it does not count.
func lastRunLengths(states []bool) (mark int, gap int) {
	if len(states) == 0 {
		return 0, 0
	}

	current := states[0]
	length := 0
	for _, state := range states {
		if state == current {
			length++
			continue
		}
		if current {
			mark = length
		} else {
			gap = length
		}
		current = state
		length = 1
	}
	return mark, gap
}

func TestSpectralDemodulator_ReportsNoMarkBeforeItKnowsTheLevels(t *testing.T) {
	const (
		sampleRate = 48000
		blockSize  = 512
	)
	buffer := bytes.NewBuffer([]byte{})
	d := NewSpectralDemodulator[float64](NewTextSink(buffer), sampleRate, blockSize)
	require.Greater(t, d.minLevelCount, 1, "the test needs a window that is not full at once")

	// A demodulator that starts knows neither the level of the marks nor the level of the gaps.
	// Without the limit its span is 0, and each value above 0 would be a mark.
	for i := range d.minLevelCount {
		assert.Falsef(t, d.Tick(100), "tick %d: a demodulator that knows no level must report a gap", i)
	}

	// with a gap the window holds the two levels, and the demodulator decides again
	for range d.minLevelCount {
		d.Tick(0)
	}
	// the debouncer needs the same value for more than one tick, see debounceDits
	var mark bool
	for range d.signalDebouncer.Threshold() {
		mark = d.Tick(100)
	}
	assert.True(t, mark, "with two levels a high value is a mark")
}

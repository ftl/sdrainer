package pipeline

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ftl/sdrainer/pipeline/generator"
)

// supportedSampleRates are the sample rates that SDRainer must support. Each one is a power of two
// multiple of the lowest one, so the block size of both tiers stays a power of two and the bin
// width stays the same. doc/architecture.md, section 4.1, gives the reason.
var supportedSampleRates = []int{12000, 24000, 48000, 96000, 192000}

const (
	rateCenter  = 7020000.0
	rateSeconds = 10

	// rateThreshold is higher than the 10 dB of the demo. The noise of one bin has an exponential
	// distribution, so the count of the bins above the threshold grows with the count of the bins,
	// thus with the sample rate at a constant bin width. A measurement gives false channels at
	// 10 dB above 48 kHz, and none at 12 dB at each rate.
	rateThreshold = 12

	// rateNoiseAt48k is the level of the noise at 48 kHz. rateNoiseLevel scales it, so that the
	// **density** of the noise stays the same at each sample rate: the generator makes the noise
	// for each sample, so the same level would give a lower density in a wider band.
	rateNoiseAt48k = 0.01
)

func rateNoiseLevel(sampleRate int) float64 {
	return rateNoiseAt48k * math.Sqrt(float64(sampleRate)/48000)
}

// rateSceneSignals gives five CW signals inside ±5 kHz, so that the scene fits into the narrowest
// bandwidth that we support. The texts are short, so that each signal repeats its text inside
// rateSeconds also at the lowest speed.
func rateSceneSignals(center float64) []generator.CWSignal[float64] {
	return []generator.CWSignal[float64]{
		{Frequency: center - 4000, WPM: 20, Amplitude: 1.0, Text: "de dl1abc", RiseTime: 5 * time.Millisecond},
		{Frequency: center - 2000, WPM: 25, Amplitude: 0.5, Text: "de ok1xyz", RiseTime: 5 * time.Millisecond},
		{Frequency: center + 1000, WPM: 30, Amplitude: 0.25, Text: "de g4abc", RiseTime: 5 * time.Millisecond},
		{Frequency: center + 3000, WPM: 35, Amplitude: 0.1, Text: "de w1aw", RiseTime: 5 * time.Millisecond},
		{Frequency: center + 4500, WPM: 40, Amplitude: 0.05, Text: "de ve3xyz", RiseTime: 5 * time.Millisecond},
	}
}

func rateConfig(sampleRate int) Config[float64] {
	return Config[float64]{
		SampleRate: sampleRate, CenterFrequency: rateCenter, BinWidth: 12,
		MinWPM: 8, MaxWPM: 56,
		PeakThreshold: rateThreshold,
		MinDutyCycle:  0.07, MaxDutyCycle: 0.9, MaxAutocorrelation: 0.4,
		NoiseFloorTime: 3 * time.Second, DeadTimeout: 20 * time.Second, MaxDrift: 2,
		ScopeFrameRate: 10,
	}
}

// runRateScene sends the given number of seconds of the scene through a pipeline at the given
// sample rate, and it gives the collected text and the time that the work needed.
func runRateScene(sampleRate int, seconds int) (*sceneTextListener, time.Duration) {
	config := rateConfig(sampleRate)
	listener := newSceneTextListener()
	p := New[float32, float64](config, nil)
	p.Notify(listener)

	source := generator.New[float32, float64](generator.GeneratorConfig[float64]{
		SampleRate: sampleRate, CenterFrequency: rateCenter, NoiseLevel: rateNoiseLevel(sampleRate),
		Seed: 1, Signals: rateSceneSignals(rateCenter),
	})

	// one chunk is 40 ms, as a sound card or an SDR gives it
	chunk := make([]float32, 2*(sampleRate/25))
	start := time.Now()
	p.Start()
	for range seconds * 25 {
		source.Read(chunk)
		p.IQData(sampleRate, chunk)
	}
	p.Stop()

	return listener, time.Since(start)
}

// TestEachSampleRateGivesTheSameResolution shows that the pipeline calculates the same resolution in
// frequency and in time at each supported sample rate. The sizes in samples scale with the sample
// rate, and each other value stays the same.
func TestEachSampleRateGivesTheSameResolution(t *testing.T) {
	reference := Derive(rateConfig(48000))

	for _, sampleRate := range supportedSampleRates {
		t.Run(fmt.Sprintf("%dHz", sampleRate), func(t *testing.T) {
			derived := Derive(rateConfig(sampleRate))
			factor := float64(sampleRate) / 48000

			assert.Equal(t, int(float64(reference.BlockSize)*factor), derived.BlockSize, "block size")
			assert.Equal(t, int(float64(reference.Hop)*factor), derived.Hop, "hop")
			assert.Equal(t, int(float64(reference.DecodeBlockSize)*factor), derived.DecodeBlockSize, "decode block size")
			assert.Equal(t, int(float64(reference.DecodeHop)*factor), derived.DecodeHop, "decode hop")

			assert.InDelta(t, reference.BinWidth, derived.BinWidth, 0.01, "bin width")
			assert.Equal(t, reference.FrameInterval, derived.FrameInterval, "frame interval")
			assert.Equal(t, reference.DecodeTick, derived.DecodeTick, "tick of the decoder")
			assert.Equal(t, reference.CWWindow, derived.CWWindow, "window of the CW test")
			assert.Equal(t, reference.IdleTimeout, derived.IdleTimeout, "idle timeout")
			assert.Equal(t, reference.ConfirmCount, derived.ConfirmCount, "confirm count")

			// the hop must be one quarter of the window, and the window must not be longer than one
			// dit at the highest speed
			assert.Equal(t, derived.BlockSize/overlapFactor, derived.Hop, "the hop of the detection tier")
			assert.LessOrEqual(t, float64(derived.DecodeBlockSize)/float64(sampleRate), ditSeconds(56),
				"the window of the decode tier must not be longer than one dit")
		})
	}
}

// TestEachSampleRateFindsAndDecodesTheScene runs the same scene at each supported sample rate. The
// result must not depend on the sample rate: the same five channels, no false channel, and the same
// text.
//
// The limits of the error rate come from a measurement. The value at 20 WPM is the highest, because
// the text is short and the characters of the cold start weigh more.
func TestEachSampleRateFindsAndDecodesTheScene(t *testing.T) {
	maxErrorRate := map[int]float64{20: 0.30, 25: 0.20, 30: 0.05, 35: 0.05, 40: 0.05}

	for _, sampleRate := range supportedSampleRates {
		t.Run(fmt.Sprintf("%dHz", sampleRate), func(t *testing.T) {
			listener, elapsed := runRateScene(sampleRate, rateSeconds)
			t.Logf("%d s of audio in %v, thus %.1f times faster than real time",
				rateSeconds, elapsed.Round(time.Millisecond), float64(rateSeconds)/elapsed.Seconds())

			signals := rateSceneSignals(rateCenter)
			for _, signal := range signals {
				actual, found := listener.textOf(signal.Frequency)
				require.Truef(t, found, "%.0f Hz, %d WPM must give a channel", signal.Frequency, signal.WPM)
				assert.LessOrEqualf(t, bestMatchErrorRate(signal.Text, actual), maxErrorRate[signal.WPM],
					"%d WPM: %q", signal.WPM, actual)
			}

			assert.Lenf(t, listener.frequency, len(signals),
				"the scene must give no false channel, got %v", listener.frequency)
		})
	}
}

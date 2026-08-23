package pipeline

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/pipeline/generator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	probeSampleRate = 48000
	probeBlockSize  = 4096
	probeHop        = 1024
	probeChunk      = 2048
	probeCenter     = 7028000.0
	probeSeconds    = 30
)

var probeSignals = []float64{7020000, 7023500, 7028000, 7032000, 7038000}

// probeCarrier is a birdie: a tone without keying. Section 4.3 asks that it never becomes a
// channel, because it carries no CW.
const probeCarrier = 7035000.0

type countingListener struct {
	mutex   sync.Mutex
	created []float64
}

func (l *countingListener) ChannelCreated(channel core.Channel[float64]) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.created = append(l.created, channel.Frequency)
}

func (l *countingListener) ChannelDestroyed(core.Channel[float64]) {}

func (l *countingListener) counts() (real int, false_ int) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	for _, frequency := range l.created {
		matched := false
		for _, signal := range probeSignals {
			if math.Abs(frequency-signal) < 100 {
				matched = true
			}
		}
		if matched {
			real++
		} else {
			false_++
		}
	}
	return real, false_
}

// sceneSignals is the scene: five CW signals of different speed and level, one of them with fading
// and one of them with drift, and a birdie that must give no channel.
func sceneSignals() []generator.CWSignal[float64] {
	return []generator.CWSignal[float64]{
		{Frequency: 7020000, WPM: 15, Amplitude: 1.0, Text: "cq cq de dl1abc dl1abc k", RiseTime: 5 * time.Millisecond},
		{Frequency: 7023500, WPM: 20, Amplitude: 0.5, Text: "cq test de ok1xyz", FadeDepth: 0.5, FadeRate: 0.2, RiseTime: 5 * time.Millisecond},
		{Frequency: 7028000, WPM: 25, Amplitude: 0.25, Text: "cq de g4abc g4abc k", RiseTime: 5 * time.Millisecond},
		{Frequency: 7032000, WPM: 30, Amplitude: 0.1, Text: "test de w1aw", Drift: 2, RiseTime: 5 * time.Millisecond},
		{Frequency: 7038000, WPM: 35, Amplitude: 0.05, Text: "qrl? de ve3xyz", RiseTime: 5 * time.Millisecond},
		{Frequency: probeCarrier, Amplitude: 0.5, Carrier: true},
	}
}

// sceneConfig gives the configuration of the pipeline for the scene. MaxWPM 0 switches the decode
// tier off, and a test that needs no text must use that, because the decode tier makes four times
// more frames than the detection tier.
func sceneConfig(threshold float64, maxWPM int) Config[float64] {
	return Config[float64]{
		SampleRate: probeSampleRate, CenterFrequency: probeCenter, BinWidth: 12,
		MinWPM: 8, MaxWPM: maxWPM,
		PeakThreshold: threshold,
		MinDutyCycle:  0.07, MaxDutyCycle: 0.9, MaxAutocorrelation: 0.4,
		NoiseFloorTime: 3 * time.Second, DeadTimeout: 20 * time.Second, MaxDrift: 2,
		ScopeFrameRate: 10,
	}
}

func sceneSource() *generator.Generator[float32, float64] {
	return generator.New[float32, float64](generator.GeneratorConfig[float64]{
		SampleRate:      probeSampleRate,
		CenterFrequency: probeCenter,
		NoiseLevel:      0.01,
		Seed:            1,
		Signals:         sceneSignals(),
	})
}

// runScene sends the given number of seconds of the scene through the pipeline.
func runScene(p *Pipeline[float32, float64], seconds int) {
	source := sceneSource()
	p.Start()
	defer p.Stop()

	chunk := make([]float32, 2*probeChunk)
	for range seconds * probeSampleRate / probeChunk {
		source.Read(chunk)
		p.IQData(probeSampleRate, chunk)
	}
}

func TestPipelineFindsEachSignalOfTheSceneOnce(t *testing.T) {
	for _, threshold := range []float64{10, 12} {
		listener := &countingListener{}
		p := New[float32, float64](sceneConfig(threshold, 0), nil)
		p.Notify(listener)

		runScene(p, probeSeconds)

		real, falseOnes := listener.counts()
		assert.Equalf(t, len(probeSignals), real, "%.0f dB: each signal of the scene must give one channel", threshold)
		assert.Zerof(t, falseOnes, "%.0f dB: the carrier and the noise must give no channel, got %.0f", threshold, listener.created)
	}
}

// sceneTextListener collects the decoded text of each channel of the scene.
type sceneTextListener struct {
	mutex     sync.Mutex
	frequency map[core.ChannelID]float64
	text      map[core.ChannelID][]rune
}

func newSceneTextListener() *sceneTextListener {
	return &sceneTextListener{
		frequency: make(map[core.ChannelID]float64),
		text:      make(map[core.ChannelID][]rune),
	}
}

func (l *sceneTextListener) ChannelCreated(channel core.Channel[float64]) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.frequency[channel.ID] = channel.Frequency
}

func (l *sceneTextListener) ChannelDestroyed(core.Channel[float64]) {}

func (l *sceneTextListener) ChannelCharacterReceived(channel core.Channel[float64], character rune, _ int64) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.text[channel.ID] = append(l.text[channel.ID], character)
}

// textOf gives the text of the channel at the given frequency.
func (l *sceneTextListener) textOf(frequency float64) (string, bool) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	for id, actual := range l.frequency {
		if math.Abs(actual-frequency) < 100 {
			return string(l.text[id]), true
		}
	}
	return "", false
}

// bestMatchErrorRate gives the smallest edit distance between the expected text and any part of the
// actual text, divided by the length of the expected text. The start and the end of the expected
// text are free, so the value does not depend on the position at which the decode began, and it
// does not count the characters of the cold start.
func bestMatchErrorRate(expected string, actual string) float64 {
	if len(expected) == 0 {
		return 0
	}
	return float64(bestMatchDistance(expected, actual)) / float64(len([]rune(expected)))
}

// bestMatchDistance gives the smallest count of the changes of one character that make the expected
// text out of a part of the actual text. Each insertion, each deletion and each substitution counts
// as one change.
func bestMatchDistance(expected string, actual string) int {
	a := []rune(expected)
	b := []rune(actual)
	if len(a) == 0 {
		return 0
	}

	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
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

	best := previous[0]
	for _, value := range previous {
		best = min(best, value)
	}
	return best
}

// TestPipelineDecodesEachSignalOfTheScene measures the character error rate of each signal of the
// scene. The limits below come from a measurement over four runs, and they are not a target: make
// them smaller when the decode gets better, and never make them larger without a reason.
//
// The text of a run is not the same in each run. The worker takes the frames of the two tiers with
// a select, so the interleaving of the two streams decides at which frame the decoder of a channel
// begins. This changes the characters of the cold start, and thus the error rate of a short text.
func TestPipelineDecodesEachSignalOfTheScene(t *testing.T) {
	tt := []struct {
		frequency    float64
		maxErrorRate float64
		reason       string
	}{
		// 0.083: at 15 WPM one loop of the text needs 19 s, so the 30 s of the scene hold no second
		// complete loop. The value was 0.083 to 0.208 before the limit of the cold start in
		// cw/spectral.go, because the wrong characters of the start changed with each run.
		{frequency: 7020000, maxErrorRate: 0.15},
		// this signal fades by 6 dB with a period of 5 s, and it gave 0.353 before the correction of
		// decisionFraction in cw/spectral.go
		{frequency: 7023500, maxErrorRate: 0.05, reason: "with a fade of 6 dB"},
		{frequency: 7028000, maxErrorRate: 0.05},
		{frequency: 7032000, maxErrorRate: 0.05},
		{frequency: 7038000, maxErrorRate: 0.05},
	}

	listener := newSceneTextListener()
	p := New[float32, float64](sceneConfig(10, 56), nil)
	p.Notify(listener)

	runScene(p, probeSeconds)

	for _, tc := range tt {
		var expected string
		var wpm int
		for _, signal := range sceneSignals() {
			if math.Abs(signal.Frequency-tc.frequency) < 100 {
				expected, wpm = signal.Text, signal.WPM
			}
		}

		actual, found := listener.textOf(tc.frequency)
		require.Truef(t, found, "%.0f Hz must give a channel", tc.frequency)
		assert.LessOrEqualf(t, bestMatchErrorRate(expected, actual), tc.maxErrorRate,
			"%.0f Hz, %d WPM %s: %q", tc.frequency, wpm, tc.reason, actual)
	}
}

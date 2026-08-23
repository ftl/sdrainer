package pipeline

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/dsp"
	"github.com/ftl/sdrainer/pipeline/generator"
)

const (
	// The test uses the lowest sample rate, because it is the cheapest and its band of ±6 kHz makes
	// a small change of the center frequency sufficient.
	retuneSampleRate = 12000
	retuneCenter     = 7020000.0

	// A change of +4000 Hz moves the two signals below the center out of the band, and it keeps the
	// three signals above it. Section 6.3 of doc/architecture.md holds the table.
	retuneShift = 4000

	// A channel needs the idle timeout of 1.57 s and then this time, before it is dead. The value of
	// the demo is 20 s, and the test does not wait that long.
	retuneDeadTimeout = 3 * time.Second

	retuneSecondsBefore = 4
	retuneSecondsAfter  = 5
)

func retuneConfig() Config[float64] {
	config := rateConfig(retuneSampleRate)
	config.DeadTimeout = retuneDeadTimeout
	return config
}

// signalsInBand gives the signals that the SDR still receives after a change of the center
// frequency. The generator calculates the phase of a signal from its offset, so a signal outside the
// band gives an alias inside the band. A real SDR removes such a signal with the filter before its
// decimation, so the test must not give it to the generator.
func signalsInBand(signals []generator.CWSignal[float64], center float64, sampleRate int) []generator.CWSignal[float64] {
	// the limit keeps a distance of one bin from the edge of the band
	limit := float64(sampleRate)/2 - 100

	result := make([]generator.CWSignal[float64], 0, len(signals))
	for _, signal := range signals {
		if math.Abs(signal.Frequency-center) < limit {
			result = append(result, signal)
		}
	}
	return result
}

// retuneListener collects the state and the text of each channel, and it keeps the offset of each
// character. The offset tells if a character came before or after the change.
type retuneListener struct {
	mutex     sync.Mutex
	frequency map[core.ChannelID]float64
	states    map[core.ChannelID][]core.ChannelState
	destroyed map[core.ChannelID]bool
	offsets   map[core.ChannelID][]int64
}

func newRetuneListener() *retuneListener {
	return &retuneListener{
		frequency: make(map[core.ChannelID]float64),
		states:    make(map[core.ChannelID][]core.ChannelState),
		destroyed: make(map[core.ChannelID]bool),
		offsets:   make(map[core.ChannelID][]int64),
	}
}

func (l *retuneListener) ChannelCreated(channel core.Channel[float64]) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.frequency[channel.ID] = channel.Frequency
}

func (l *retuneListener) ChannelDestroyed(channel core.Channel[float64]) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.destroyed[channel.ID] = true
}

func (l *retuneListener) ChannelStateChanged(channel core.Channel[float64]) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.states[channel.ID] = append(l.states[channel.ID], channel.State)
}

func (l *retuneListener) ChannelCharacterReceived(channel core.Channel[float64], _ rune, offset int64) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.offsets[channel.ID] = append(l.offsets[channel.ID], offset)
}

// idOf gives the channel at the given frequency.
func (l *retuneListener) idOf(frequency float64) (core.ChannelID, bool) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	for id, actual := range l.frequency {
		if math.Abs(actual-frequency) < 100 {
			return id, true
		}
	}
	return "", false
}

// charactersAfter gives the count of the characters of a channel from the given offset.
func (l *retuneListener) charactersAfter(id core.ChannelID, offset int64) int {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	result := 0
	for _, actual := range l.offsets[id] {
		if actual >= offset {
			result++
		}
	}
	return result
}

func (l *retuneListener) sawState(id core.ChannelID, state core.ChannelState) bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	for _, actual := range l.states[id] {
		if actual == state {
			return true
		}
	}
	return false
}

// runRetuneScene runs the scene, changes the center frequency, and runs the scene again with the
// new center frequency. It gives the listener and the sample offset of the change.
func runRetuneScene(t *testing.T) (*retuneListener, int64) {
	t.Helper()

	signals := rateSceneSignals(retuneCenter)
	listener := newRetuneListener()
	p := New[float32, float64](retuneConfig(), nil)
	p.Notify(listener)

	chunkSize := retuneSampleRate / 25 // 40 ms
	chunk := make([]float32, 2*chunkSize)
	feed := func(source *generator.Generator[float32, float64], seconds int) {
		for range seconds * 25 {
			source.Read(chunk)
			p.IQData(retuneSampleRate, chunk)
		}
	}

	p.Start()
	defer p.Stop()

	before := generator.New[float32, float64](generator.GeneratorConfig[float64]{
		SampleRate: retuneSampleRate, CenterFrequency: retuneCenter,
		NoiseLevel: rateNoiseLevel(retuneSampleRate), Seed: 1, Signals: signals,
	})
	feed(before, retuneSecondsBefore)

	// the SDR now uses the new center frequency, and it does not give the signals outside its band
	newCenter := retuneCenter + retuneShift
	p.SetCenterFrequency(newCenter)
	after := generator.New[float32, float64](generator.GeneratorConfig[float64]{
		SampleRate: retuneSampleRate, CenterFrequency: newCenter,
		NoiseLevel: rateNoiseLevel(retuneSampleRate), Seed: 2,
		Signals: signalsInBand(signals, newCenter, retuneSampleRate),
	})
	feed(after, retuneSecondsAfter)

	return listener, int64(retuneSecondsBefore * retuneSampleRate)
}

// insideAfterRetune and outsideAfterRetune are the frequencies of the scene, against the band after
// the change of the center frequency.
func insideAfterRetune() []float64 {
	return []float64{retuneCenter + 1000, retuneCenter + 3000, retuneCenter + 4500}
}

func outsideAfterRetune() []float64 {
	return []float64{retuneCenter - 4000, retuneCenter - 2000}
}

func TestPipelineKeepsTheChannelsInsideTheNewBand(t *testing.T) {
	listener, retuneOffset := runRetuneScene(t)

	for _, frequency := range insideAfterRetune() {
		id, found := listener.idOf(frequency)
		require.Truef(t, found, "%.0f Hz must give a channel", frequency)

		assert.Positivef(t, listener.charactersAfter(id, retuneOffset),
			"%.0f Hz must give characters after the change", frequency)
		assert.Falsef(t, listener.destroyed[id], "%.0f Hz must not go away", frequency)
	}
}

func TestPipelineLetsTheChannelsOutsideTheNewBandGoDead(t *testing.T) {
	listener, retuneOffset := runRetuneScene(t)

	for _, frequency := range outsideAfterRetune() {
		id, found := listener.idOf(frequency)
		require.Truef(t, found, "%.0f Hz must give a channel before the change", frequency)

		assert.Zerof(t, listener.charactersAfter(id, retuneOffset),
			"%.0f Hz must give no character after the change", frequency)
		assert.Truef(t, listener.sawState(id, core.IdleChannel), "%.0f Hz must go to IDLE", frequency)
		assert.Truef(t, listener.sawState(id, core.DeadChannel), "%.0f Hz must go to DEAD", frequency)
		assert.Truef(t, listener.destroyed[id], "%.0f Hz must give a destroyed event", frequency)
	}
}

// TestPipelineMakesNoGhostChannelAtAChange guards one failure mode of the change of the center
// frequency: the frames that are already in the buffer come from samples from before the change,
// and the worker would give them the new mapping. Each peak of such a frame then gets a frequency
// that is wrong by the shift, and a group of them makes a channel at that wrong frequency. Those
// ghosts appear at the frequency of a real signal plus the shift.
//
// A candidate needs only ConfirmCount frames, thus approximately 85 ms at 12 kHz, and the buffer
// holds up to 32 frames or 0.68 s. The buffer alone is therefore not short enough, and
// applyCenterFrequency drops the frames of the old band.
//
// This test does not count all channels, because the keying edges of a strong signal also make
// channels beside it. pipeline/qso_test.go measures those.
func TestPipelineMakesNoGhostChannelAtAChange(t *testing.T) {
	listener, _ := runRetuneScene(t)

	for _, signal := range rateSceneSignals(retuneCenter) {
		ghost := signal.Frequency + retuneShift
		_, found := listener.idOf(ghost)
		assert.Falsef(t, found, "%.0f Hz is %.0f Hz plus the shift, and it must give no channel: %v",
			ghost, signal.Frequency, listener.frequency)
	}
}

func TestPipelineTakesTheNewestCenterFrequency(t *testing.T) {
	// the worker does not run, so both values stay in the pipeline
	p := New[float32, float64](retuneConfig(), nil)

	p.SetCenterFrequency(retuneCenter + 1000)
	p.SetCenterFrequency(retuneCenter + 2000)

	assert.Equal(t, retuneCenter+2000, p.CenterFrequency())

	p.Start()
	p.IQData(retuneSampleRate, make([]float32, 2*Derive(retuneConfig()).BlockSize))
	p.Stop()

	assert.Equal(t, retuneCenter+2000, float64(p.mapping.BinToFrequency(Derive(retuneConfig()).BlockSize/2, dsp.BinCenter)),
		"the worker must use the newest value")
}

func TestPipelineTakesACenterFrequencyBeforeStart(t *testing.T) {
	config := retuneConfig()
	p := New[float32, float64](config, nil)
	blockSize := Derive(config).BlockSize

	p.SetCenterFrequency(retuneCenter + 3000)

	p.Start()
	p.IQData(retuneSampleRate, make([]float32, 2*blockSize))
	p.Stop()

	assert.Equal(t, retuneCenter+3000, float64(p.mapping.BinToFrequency(blockSize/2, dsp.BinCenter)),
		"the detection tier must use the new center frequency")
	assert.Equal(t, retuneCenter+3000, float64(p.decode.mapping.BinToFrequency(Derive(config).DecodeBlockSize/2, dsp.BinCenter)),
		"the decode tier must use it too")
}

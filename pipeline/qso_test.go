package pipeline

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/pipeline/generator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file explores what SDRainer does with a QSO: two stations that alternate on almost the same
// frequency, with almost the same speed. Each test changes one property of the QSO and keeps the
// rest, so that the result shows the effect of that one property.
//
// Run `go test -run TestQSO -v ./pipeline/` to see the table of the channels of each run. That
// table is the tool for the exploration: it gives the frequency, the speed, the states and the
// text of each channel that the pipeline made.

const (
	qsoSampleRate = 12000
	qsoCenter     = 7028000.0
	qsoChunk      = 1024

	// qsoNoiseLevel is the level of the noise at 12 kHz, at the same density as
	// rateNoiseLevel gives for the tests of the sample rate.
	qsoNoiseLevel = 0.005

	qsoCallA = "dl1abc"
	qsoCallB = "ok1xyz"
)

// qsoDefaultPrefix begins each over. The decoder of a new channel does not know the speed yet, so
// the first characters of an over are wrong. The callsign comes after the prefix, and it is then a
// clean copy already in the first over.
const qsoDefaultPrefix = "vvv "

// qsoText gives the text of one over. The text must fit into one turn, at the lowest speed that a
// test uses: the text with the prefix needs 138 dits, or 8.3 s at 20 WPM.
func qsoText(prefix string, callsign string) string {
	return prefix + "de " + callsign
}

// qsoScenario describes one QSO. Station A begins, and the two stations alternate: each turn holds
// one over, and a station sends its text one time in its over and is then silent.
type qsoScenario struct {
	// FrequencyDelta is the distance of station B above station A, in Hz. Station A is at the
	// center frequency.
	FrequencyDelta float64

	WPMA       int
	WPMB       int
	AmplitudeA float64
	AmplitudeB float64

	// TextPrefix begins the over of each station, before "de <callsign>".
	TextPrefix string

	// RiseTime shapes the edges of the keying of both stations. A short rise time makes key
	// clicks, thus energy far from the frequency of the station.
	RiseTime time.Duration

	Turn  time.Duration // the time of one over
	Overs int           // the count of the overs of the whole QSO, both stations together

	DeadTimeout   time.Duration
	PeakThreshold float64

	// Seed is the seed of the noise. A different seed gives a different realization of the noise,
	// and it shows if a result comes from the signals or from one realization of the noise.
	Seed uint64
}

// newQSO gives a QSO that SDRainer must handle without a problem: the two stations are 200 Hz
// apart, their speeds are 4 WPM apart, and both are strong.
func newQSO() qsoScenario {
	return qsoScenario{
		FrequencyDelta: 200,
		WPMA:           22,
		WPMB:           26,
		AmplitudeA:     1.0,
		AmplitudeB:     1.0,
		TextPrefix:     qsoDefaultPrefix,
		RiseTime:       5 * time.Millisecond,
		Turn:           9 * time.Second,
		Overs:          2,
		DeadTimeout:    20 * time.Second,
		PeakThreshold:  10,
		Seed:           1,
	}
}

func (s qsoScenario) signals() []generator.CWSignal[float64] {
	period := 2 * s.Turn
	return []generator.CWSignal[float64]{
		{
			Frequency: qsoCenter, Text: qsoText(s.TextPrefix, qsoCallA), WPM: s.WPMA, Amplitude: s.AmplitudeA,
			RiseTime: s.RiseTime, TurnPeriod: period,
		},
		{
			Frequency: qsoCenter + s.FrequencyDelta, Text: qsoText(s.TextPrefix, qsoCallB), WPM: s.WPMB, Amplitude: s.AmplitudeB,
			RiseTime: s.RiseTime, TurnPeriod: period, TurnOffset: s.Turn,
		},
	}
}

func (s qsoScenario) config() Config[float64] {
	return Config[float64]{
		SampleRate: qsoSampleRate, CenterFrequency: qsoCenter, BinWidth: 12,
		MinWPM: 8, MaxWPM: 56,
		PeakThreshold: s.PeakThreshold,
		MinDutyCycle:  0.07, MaxDutyCycle: 0.9, MaxAutocorrelation: 0.4,
		NoiseFloorTime: 3 * time.Second, DeadTimeout: s.DeadTimeout, MaxDrift: 2,
		ScopeFrameRate: 10,
	}
}

// qsoObservation holds what one channel of the pipeline did during the QSO.
type qsoObservation struct {
	ID             core.ChannelID
	FirstFrequency float64
	LastFrequency  float64
	MinFrequency   float64
	MaxFrequency   float64
	Text           []rune
	WPM            []int
	States         []core.ChannelState
	Destroyed      bool
}

func (o *qsoObservation) text() string {
	return string(o.Text)
}

// wpm gives the median of the speed of all characters of this channel. The median is robust
// against the wrong values of the first characters, where the decoder does not know the speed yet.
func (o *qsoObservation) wpm() int {
	if len(o.WPM) == 0 {
		return 0
	}
	sorted := slices.Clone(o.WPM)
	slices.Sort(sorted)
	return sorted[len(sorted)/2]
}

// holds tells if the text of this channel holds the given callsign. Two wrong characters of six are
// allowed. The channel of the station that answers begins in the middle of the run, and the
// pipeline must confirm it before the decode stage takes it, so that station loses more than the
// prefix of its over: a run gave "??1xyz" for "ok1xyz".
//
// The distance between the two callsigns of the QSO is 6 characters, so this limit gives no wrong
// attribution.
func (o *qsoObservation) holds(callsign string) bool {
	return bestMatchErrorRate(callsign, o.text()) <= 1.0/3.0
}

type qsoListener struct {
	mutex        sync.Mutex
	observations []*qsoObservation
	byID         map[core.ChannelID]*qsoObservation
}

func newQSOListener() *qsoListener {
	return &qsoListener{byID: make(map[core.ChannelID]*qsoObservation)}
}

func (l *qsoListener) ChannelCreated(channel core.Channel[float64]) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	observation := &qsoObservation{
		ID:             channel.ID,
		FirstFrequency: channel.Frequency,
		LastFrequency:  channel.Frequency,
		MinFrequency:   channel.Frequency,
		MaxFrequency:   channel.Frequency,
	}
	l.observations = append(l.observations, observation)
	l.byID[channel.ID] = observation
}

func (l *qsoListener) ChannelDestroyed(channel core.Channel[float64]) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if observation, ok := l.byID[channel.ID]; ok {
		observation.Destroyed = true
	}
}

func (l *qsoListener) ChannelStateChanged(channel core.Channel[float64]) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if observation, ok := l.byID[channel.ID]; ok {
		observation.States = append(observation.States, channel.State)
	}
}

func (l *qsoListener) ChannelCharacterReceived(channel core.Channel[float64], character rune, _ int64) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	observation, ok := l.byID[channel.ID]
	if !ok {
		return
	}
	observation.Text = append(observation.Text, character)
	observation.WPM = append(observation.WPM, channel.WPM)
	observation.LastFrequency = channel.Frequency
	observation.MinFrequency = min(observation.MinFrequency, channel.Frequency)
	observation.MaxFrequency = max(observation.MaxFrequency, channel.Frequency)
}

// channels gives all channels of the run, the one with the lowest frequency first.
func (l *qsoListener) channels() []*qsoObservation {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	result := slices.Clone(l.observations)
	slices.SortFunc(result, func(a, b *qsoObservation) int {
		switch {
		case a.FirstFrequency < b.FirstFrequency:
			return -1
		case a.FirstFrequency > b.FirstFrequency:
			return 1
		default:
			return 0
		}
	})
	return result
}

// withCallsign gives all channels whose text holds the given callsign.
func (l *qsoListener) withCallsign(callsign string) []*qsoObservation {
	var result []*qsoObservation
	for _, observation := range l.channels() {
		if observation.holds(callsign) {
			result = append(result, observation)
		}
	}
	return result
}

// decodedNear gives all channels near the given frequency that decoded at least one character. A
// test uses it where the text is too short for the callsign, for example when each over gives its
// own channel.
func (l *qsoListener) decodedNear(frequency float64, tolerance float64) []*qsoObservation {
	var result []*qsoObservation
	for _, observation := range l.channels() {
		if len(observation.Text) > 0 && math.Abs(observation.FirstFrequency-frequency) <= tolerance {
			result = append(result, observation)
		}
	}
	return result
}

// withoutAnyCallsign gives all channels that hold neither callsign of the QSO. Each one is a
// channel that the two stations did not ask for.
func (l *qsoListener) withoutAnyCallsign() []*qsoObservation {
	var result []*qsoObservation
	for _, observation := range l.channels() {
		if !observation.holds(qsoCallA) && !observation.holds(qsoCallB) {
			result = append(result, observation)
		}
	}
	return result
}

func stateName(state core.ChannelState) string {
	switch state {
	case core.NewChannel:
		return "new"
	case core.ConfirmedChannel:
		return "confirmed"
	case core.ActiveChannel:
		return "active"
	case core.IdleChannel:
		return "idle"
	case core.DeadChannel:
		return "dead"
	default:
		return "?"
	}
}

// dump writes the table of the channels of the run. It is the result of the exploration, and a
// human reads it with `go test -run TestQSO -v ./pipeline/`.
func (l *qsoListener) dump(t *testing.T, title string) {
	t.Helper()

	var b strings.Builder
	fmt.Fprintf(&b, "\n%s\n", title)
	fmt.Fprintf(&b, "%-14s %10s %8s %6s %4s  %-28s %s\n", "channel", "frequency", "drift", "wpm", "chr", "states", "text")
	for _, o := range l.channels() {
		states := make([]string, 0, len(o.States))
		for _, state := range o.States {
			states = append(states, stateName(state))
		}
		fmt.Fprintf(&b, "%-14s %10.0f %8.0f %6d %4d  %-28s %q\n",
			o.ID, o.FirstFrequency, o.MaxFrequency-o.MinFrequency, o.wpm(), len(o.Text),
			strings.Join(states, ">"), o.text())
	}
	t.Log(b.String())
}

// runQSO sends the QSO through the pipeline and gives what the pipeline made of it.
func runQSO(t *testing.T, title string, scenario qsoScenario) *qsoListener {
	t.Helper()

	listener := newQSOListener()
	p := New[float32, float64](scenario.config(), nil)
	p.Notify(listener)

	source := generator.New[float32, float64](generator.GeneratorConfig[float64]{
		SampleRate:      qsoSampleRate,
		CenterFrequency: qsoCenter,
		NoiseLevel:      qsoNoiseLevel,
		Seed:            scenario.Seed,
		Signals:         scenario.signals(),
	})

	p.Start()
	chunk := make([]float32, 2*qsoChunk)
	samples := int(float64(scenario.Overs) * scenario.Turn.Seconds() * qsoSampleRate)
	for range samples / qsoChunk {
		source.Read(chunk)
		p.IQData(qsoSampleRate, chunk)
	}
	p.Stop()

	listener.dump(t, title)
	return listener
}

// TestQSOWithADistinctFrequencyGivesOneChannelForEachStation is the case that must work: the two
// stations are 200 Hz apart, and each one gets its own channel with its own text.
func TestQSOWithADistinctFrequencyGivesOneChannelForEachStation(t *testing.T) {
	scenario := newQSO()

	listener := runQSO(t, "two stations, 200 Hz apart", scenario)

	stationA := listener.withCallsign(qsoCallA)
	stationB := listener.withCallsign(qsoCallB)
	require.Lenf(t, stationA, 1, "station A must give exactly one channel")
	require.Lenf(t, stationB, 1, "station B must give exactly one channel")
	assert.NotEqual(t, stationA[0].ID, stationB[0].ID, "the two stations must not share a channel")
	assert.InDelta(t, qsoCenter, stationA[0].FirstFrequency, 30, "the frequency of station A")
	assert.InDelta(t, qsoCenter+scenario.FrequencyDelta, stationB[0].FirstFrequency, 30, "the frequency of station B")
}

// TestQSOOnTheSameFrequencyGivesOneChannelWithBothCallsigns is the other end: the two stations use
// the same frequency, so the pipeline cannot separate them. One channel then holds the text of both
// stations, one over after the other.
func TestQSOOnTheSameFrequencyGivesOneChannelWithBothCallsigns(t *testing.T) {
	scenario := newQSO()
	scenario.FrequencyDelta = 0

	listener := runQSO(t, "two stations on the same frequency", scenario)

	stationA := listener.withCallsign(qsoCallA)
	stationB := listener.withCallsign(qsoCallB)
	require.Len(t, stationA, 1, "the QSO must give one channel with the callsign of station A")
	require.Len(t, stationB, 1, "the QSO must give one channel with the callsign of station B")
	assert.Equalf(t, stationA[0].ID, stationB[0].ID,
		"the two stations must share one channel: %q", stationA[0].text())
}

// TestQSOFrequencyDeltaDecidesTheCountOfTheChannels sweeps the distance of the two stations, and it
// gives the distance at which the pipeline separates them.
//
// The limit is matchWidthHz of the tracker, thus 100 Hz: a peak inside that distance from a channel
// belongs to that channel. The decode tier takes the energy of 3 bins, which are 141 Hz, so two
// channels that stand closer than that would decode almost the same signal anyway.
//
// peakMergeWidthHz of the detection stage is 40 Hz, but it has no effect here: the two stations
// never transmit at the same time, so no frame holds two groups of bins that the stage could merge.
func TestQSOFrequencyDeltaDecidesTheCountOfTheChannels(t *testing.T) {
	tt := []struct {
		delta    float64
		separate bool
	}{
		{delta: 0, separate: false},
		{delta: 25, separate: false},
		{delta: 100, separate: false},
		{delta: 200, separate: true},
	}

	for _, tc := range tt {
		t.Run(fmt.Sprintf("%.0fHz", tc.delta), func(t *testing.T) {
			scenario := newQSO()
			scenario.FrequencyDelta = tc.delta

			listener := runQSO(t, fmt.Sprintf("two stations, %.0f Hz apart", tc.delta), scenario)

			stationA := listener.withCallsign(qsoCallA)
			stationB := listener.withCallsign(qsoCallB)
			require.NotEmpty(t, stationA, "the callsign of station A must appear")
			require.NotEmpty(t, stationB, "the callsign of station B must appear")

			if tc.separate {
				assert.NotEqual(t, stationA[0].ID, stationB[0].ID, "the two stations must get their own channel")
			} else {
				assert.Equal(t, stationA[0].ID, stationB[0].ID, "the two stations must share one channel")
			}
		})
	}
}

// TestQSOChannelReportsTheSpeedOfItsOwnStation checks that the speed of a channel follows its own
// station, and that it does not take the speed of the other station of the QSO.
//
// The tolerance of 4 WPM is large, because the median holds only the characters of two overs, and
// the first characters of an over come from a decoder that does not know the speed yet. A longer
// over gives a better value.
func TestQSOChannelReportsTheSpeedOfItsOwnStation(t *testing.T) {
	tt := []struct{ wpmA, wpmB int }{
		{wpmA: 20, wpmB: 20},
		{wpmA: 20, wpmB: 35},
	}

	for _, tc := range tt {
		t.Run(fmt.Sprintf("%dvs%d", tc.wpmA, tc.wpmB), func(t *testing.T) {
			scenario := newQSO()
			scenario.WPMA, scenario.WPMB = tc.wpmA, tc.wpmB

			listener := runQSO(t, fmt.Sprintf("%d WPM against %d WPM", tc.wpmA, tc.wpmB), scenario)

			stationA := listener.withCallsign(qsoCallA)
			stationB := listener.withCallsign(qsoCallB)
			require.Len(t, stationA, 1, "station A must give one channel")
			require.Len(t, stationB, 1, "station B must give one channel")
			assert.InDeltaf(t, tc.wpmA, stationA[0].wpm(), 4, "the speed of station A: %q", stationA[0].text())
			assert.InDeltaf(t, tc.wpmB, stationB[0].wpm(), 4, "the speed of station B: %q", stationB[0].text())
		})
	}
}

// TestQSOWithAWeakStation sweeps the level of station B while station A stays strong. It gives the
// level at which the weaker station of a QSO still gives its callsign.
//
// The measurement gives the limit between an amplitude of 0.005 and 0.002, thus 46 dB to 54 dB
// below station A. At 0.002 the pipeline still finds a channel, but the channel gives no character.
// At 0.001 there is no channel.
func TestQSOWithAWeakStation(t *testing.T) {
	tt := []struct {
		amplitude float64
		decoded   bool
	}{
		{amplitude: 0.05, decoded: true},
		{amplitude: 0.005, decoded: true},
		{amplitude: 0.002, decoded: false},
	}

	for _, tc := range tt {
		t.Run(fmt.Sprintf("%.3f", tc.amplitude), func(t *testing.T) {
			scenario := newQSO()
			scenario.AmplitudeB = tc.amplitude

			listener := runQSO(t, fmt.Sprintf("station B at an amplitude of %.3f", tc.amplitude), scenario)

			assert.NotEmpty(t, listener.withCallsign(qsoCallA), "station A must always give its callsign")
			if tc.decoded {
				assert.NotEmpty(t, listener.withCallsign(qsoCallB), "station B must give its callsign at this level")
			} else {
				assert.Empty(t, listener.withCallsign(qsoCallB), "station B must give no callsign at this level")
			}
		})
	}
}

// TestQSOChannelSurvivesThePauseOfTheOtherStation looks at the identity of a channel over the whole
// QSO. A station is silent while the other station sends, so its channel goes to IDLE. It stays the
// same channel while DeadTimeout is longer than one over, and the pipeline makes a new channel for
// each over when DeadTimeout is shorter.
//
// A DeadTimeout that is shorter than one over therefore breaks a QSO into one channel for each
// over. Section 5 of the research document asks for 10 s to 30 s, and a real over is longer than
// that, so this is the normal case and not an error.
//
// It costs the copy: each new channel begins with a decoder that does not know the speed, and the
// first characters of each over are then wrong. The run with 3 s gave "u1xyz" and "??xyz" for the
// two overs of "de ok1xyz", and neither one holds the complete callsign.
func TestQSOChannelSurvivesThePauseOfTheOtherStation(t *testing.T) {
	t.Run("dead timeout longer than one over", func(t *testing.T) {
		scenario := newQSO()
		scenario.Overs = 4 // two overs for each station
		scenario.DeadTimeout = 20 * time.Second

		listener := runQSO(t, "dead timeout of 20 s, over of 9 s", scenario)

		assert.Len(t, listener.decodedNear(qsoCenter, 100), 1, "station A must keep one channel over the whole QSO")
		assert.Len(t, listener.decodedNear(qsoCenter+scenario.FrequencyDelta, 100), 1, "station B must keep one channel over the whole QSO")
		assert.Len(t, listener.withCallsign(qsoCallA), 1, "station A must keep one channel over the whole QSO")
		assert.Len(t, listener.withCallsign(qsoCallB), 1, "station B must keep one channel over the whole QSO")
	})

	t.Run("dead timeout shorter than one over", func(t *testing.T) {
		scenario := newQSO()
		scenario.Overs = 4 // two overs for each station
		scenario.DeadTimeout = 3 * time.Second

		listener := runQSO(t, "dead timeout of 3 s, over of 9 s", scenario)

		assert.Len(t, listener.decodedNear(qsoCenter, 100), 2, "each over of station A must give a new channel")
		assert.Len(t, listener.decodedNear(qsoCenter+scenario.FrequencyDelta, 100), 2, "each over of station B must give a new channel")
	})
}

// TestQSOKeyClicksMakeChannelsBesideTheStations shows what this suite found: the keying edges of a
// station make channels beside its own frequency. Those channels are not noise, because they carry
// the speed of the station and they give text.
//
// The measurement with two stations at an amplitude of 1.0, with 4 overs of 9 s:
//
//	rise time    channels beside the stations
//	0 ms         37, over the whole band of 12 kHz, and 12 of them give text
//	5 ms         1, at 467 Hz above station A, and it gives the text "ei eiieehe"
//	20 ms        0
//
// A rise time of 0 is the worst case: the keying is a rectangle, and its spectrum falls with 1/f.
// A real transmitter has a filter and does not reach that. The value of 5 ms is the value that the
// rest of the tests of the pipeline use.
func TestQSOKeyClicksMakeChannelsBesideTheStations(t *testing.T) {
	tt := []struct {
		riseTime time.Duration
		minExtra int // the smallest count of the channels beside the stations
	}{
		{riseTime: 0, minExtra: 10},
		{riseTime: 5 * time.Millisecond, minExtra: 1},
		{riseTime: 20 * time.Millisecond, minExtra: 0},
	}

	for _, tc := range tt {
		t.Run(tc.riseTime.String(), func(t *testing.T) {
			scenario := newQSO()
			scenario.RiseTime = tc.riseTime
			// The over here holds only "de <callsign>", so that a part of each turn is silent. The
			// count of these channels depends on that silence, and not only on the keying edges:
			// the same QSO with the prefix "vvv" fills the turn of 9 s almost completely and gives
			// no such channel, and a turn of 20 s with the prefix gives 99.
			scenario.TextPrefix = ""
			scenario.Overs = 4

			listener := runQSO(t, fmt.Sprintf("a rise time of %s", tc.riseTime), scenario)

			extra := listener.withoutAnyCallsign()
			assert.NotEmpty(t, listener.withCallsign(qsoCallA), "station A must give its callsign")
			assert.NotEmpty(t, listener.withCallsign(qsoCallB), "station B must give its callsign")
			if tc.minExtra > 0 {
				assert.GreaterOrEqualf(t, len(extra), tc.minExtra,
					"the keying edges must give at least %d channels beside the stations", tc.minExtra)
			} else {
				assert.Lenf(t, extra, 0, "a soft keying must give no channel beside the stations")
			}
		})
	}
}

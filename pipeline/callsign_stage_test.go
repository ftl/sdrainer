package pipeline

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ftl/hamradio/callsign"
	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/pipeline/generator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testCallsignChannel = core.ChannelID("1")

// callsignCollector collects the events of the callsign stage.
type callsignCollector struct {
	detected []callsign.Callsign
}

func (c *callsignCollector) emitChannelRunningCallsignDetected(channel core.Channel[float64]) {
	c.detected = append(c.detected, channel.Callsign)
}

// runCallsignStage sends the given text through the stage, one character at a time, and gives the
// callsigns that the stage reported. The optional second argument is the word of the contest.
func runCallsignStage(text string, contest ...string) []callsign.Callsign {
	var word string
	if len(contest) > 0 {
		word = contest[0]
	}

	collector := &callsignCollector{}
	stage := NewCallsignStage[float64](collector, word)
	channel := core.Channel[float64]{ID: testCallsignChannel, Frequency: 7028000, WPM: 25, SNR: 18}
	stage.ChannelCreated(channel)

	for _, character := range text {
		stage.Process(channel, character)
	}

	return collector.detected
}

// TestCallsignStageFindsTheRunningStation takes the patterns of a running station. Each text ends
// with a space, because the stage evaluates a word when the word break arrives.
func TestCallsignStageFindsTheRunningStation(t *testing.T) {
	tt := []struct {
		name string
		text string
	}{
		{name: "one call", text: "cq a1bc a1bc test "},
		{name: "cq with one callsign", text: "cq a1bc test cq a1bc test "},
		{name: "callsign before test", text: "a1bc test a1bc test "},
		{name: "callsign after test", text: "test a1bc test a1bc "},
		{name: "two callsigns before test", text: "a1bc a1bc test a1bc a1bc test "},
		{name: "asking for the frequency", text: "qrl? a1bc qrl? a1bc "},
		{name: "cq with de", text: "cq cq de a1bc a1bc k cq cq de a1bc a1bc k "},
		{name: "finishing a qso", text: "tu a1bc test tu a1bc test "},
		{name: "test before the callsign", text: "test a1bc test a1bc "},
		{name: "cq de", text: "cq de a1bc cq de a1bc "},
		{name: "cq dx", text: "cq dx a1bc cq dx a1bc "},
		{name: "cq dx de", text: "cq dx de a1bc cq dx de a1bc "},
		{name: "cq dx with the callsign in pieces", text: "cq dx a 1 b c cq dx a 1 b c "},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			detected := runCallsignStage(tc.text)

			require.NotEmptyf(t, detected, "%q must give the callsign of the running station", tc.text)
			assert.Equal(t, "A1BC", detected[len(detected)-1].String())
		})
	}
}

// TestCallsignStageIgnoresTheAnsweringStation takes the text of a station that answers a call. The
// answering station uses the frequency of the running station, so its text comes on the same
// channel, and it must give no callsign of a running station.
func TestCallsignStageIgnoresTheAnsweringStation(t *testing.T) {
	tt := []struct {
		name string
		text string
	}{
		{name: "only the callsign", text: "dl0xy dl0xy dl0xy dl0xy "},
		{name: "confirmation and report", text: "r 599 001 r 599 001 "},
		{name: "the running station confirms", text: "dl0xy tu dl0xy tu "},
		{name: "the running station reports", text: "dl0xy 599 123 dl0xy 599 123 "},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			detected := runCallsignStage(tc.text)

			assert.Emptyf(t, detected, "%q must give no callsign of a running station", tc.text)
		})
	}
}

// TestCallsignStageTakesTheRunningStationOfAWholeQSO uses the text of a complete QSO, as it arrives
// on one channel: the two calls of the running station, the answer, the two reports and the
// confirmation.
func TestCallsignStageTakesTheRunningStationOfAWholeQSO(t *testing.T) {
	const qso = "cq a1bc a1bc test " +
		"cq a1bc a1bc test " +
		"dl0xy " +
		"dl0xy 599 123 " +
		"r 599 001 " +
		"tu "

	detected := runCallsignStage(qso)

	// The stage reports the same callsign again when it becomes valid, so the count of the reports
	// is not the count of the stations. Each report must name the running station.
	require.NotEmpty(t, detected)
	for i, found := range detected {
		assert.Equalf(t, "A1BC", found.String(), "the report %d must name the running station", i)
	}
}

// TestCallsignStageReportsACallsignAgainWhenItBecomesValid covers the second reason for a report:
// the quality of a spot belongs to the spot, so a callsign that reaches validCallsignHits gives a
// new spot with the tag V.
func TestCallsignStageReportsACallsignAgainWhenItBecomesValid(t *testing.T) {
	// each call gives 2 hits, so the second call reaches validCallsignHits
	detected := runCallsignStage("cq a1bc a1bc test cq a1bc a1bc test ")

	require.Len(t, detected, 2, "one report at the first hits and one at the valid hits")
	assert.Equal(t, "A1BC", detected[0].String())
	assert.Equal(t, "A1BC", detected[1].String())
}

// TestCallsignStageReportsAValidCallsignOnlyOneTime is the other half: each further hit of a
// callsign that is already valid gives no new spot.
func TestCallsignStageReportsAValidCallsignOnlyOneTime(t *testing.T) {
	detected := runCallsignStage("cq a1bc a1bc test cq a1bc a1bc test cq a1bc a1bc test cq a1bc a1bc test ")

	assert.Len(t, detected, 2, "the reports stop when the callsign is valid")
}

// TestCallsignStageNeedsMoreThanOneHit checks that a single hit is not sufficient. The decoder makes
// wrong characters, and a wrong word can be a valid callsign.
func TestCallsignStageNeedsMoreThanOneHit(t *testing.T) {
	assert.Empty(t, runCallsignStage("qrl? a1bc "), "one hit must give no callsign")
	assert.NotEmpty(t, runCallsignStage("qrl? a1bc qrl? a1bc "), "two hits must give the callsign")
}

// TestCallsignStageCorrectsAWrongCallsign shows what happens when the decoder gives a wrong
// callsign first: the stage reports the wrong one, and it reports the correct one as soon as the
// correct one has more hits.
func TestCallsignStageCorrectsAWrongCallsign(t *testing.T) {
	detected := runCallsignStage("cq a1bd a1bd test cq a1bc a1bc test cq a1bc a1bc test ")

	require.Len(t, detected, 2, "the stage must report the wrong callsign and then the correct one")
	assert.Equal(t, "A1BD", detected[0].String())
	assert.Equal(t, "A1BC", detected[1].String())
}

// TestCallsignStageForgetsAChannelThatIsGone checks that the state of a channel goes away with the
// channel. A later channel with the same ID must begin without the hits of the channel before it.
func TestCallsignStageForgetsAChannelThatIsGone(t *testing.T) {
	collector := &callsignCollector{}
	stage := NewCallsignStage[float64](collector, "")
	channel := core.Channel[float64]{ID: testCallsignChannel, Frequency: 7028000}

	stage.ChannelCreated(channel)
	for _, character := range "cq a1bc " {
		stage.Process(channel, character)
	}
	stage.ChannelDestroyed(channel)

	assert.Equal(t, callsign.NoCallsign, stage.CallsignOf(channel.ID), "the channel is gone")

	stage.ChannelCreated(channel)
	for _, character := range "cq a1bc " {
		stage.Process(channel, character)
	}

	assert.Empty(t, collector.detected, "the hit of the channel before must not count")
}

// runningListener collects the events and the spots of a run of the pipeline.
type runningListener struct {
	mutex     sync.Mutex
	detected  []core.Channel[float64]
	spots     []string
	text      []rune
	qualities []core.ChannelQuality
}

func (l *runningListener) ChannelRunningCallsignDetected(channel core.Channel[float64]) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.detected = append(l.detected, channel)
}

func (l *runningListener) ChannelCharacterReceived(_ core.Channel[float64], character rune, _ int64) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.text = append(l.text, character)
}

func (l *runningListener) Spot(callsign string, frequency float64, msg string, _ time.Time) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.spots = append(l.spots, fmt.Sprintf("%s %.0f %s", callsign, frequency, msg))
}

func (l *runningListener) RemoveSpot(string) {}

func (l *runningListener) ChannelQualityChanged(channel core.Channel[float64]) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.qualities = append(l.qualities, channel.Quality)
}

func (l *runningListener) qualityChanges() []core.ChannelQuality {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return slices.Clone(l.qualities)
}

func (l *runningListener) result() ([]core.Channel[float64], []string, string) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return slices.Clone(l.detected), slices.Clone(l.spots), string(l.text)
}

// runRunningStation sends the given signals through the pipeline for the given time, and gives what
// the pipeline reported. It uses the scene of the QSO tests, at 12 kHz.
func runRunningStation(t *testing.T, seconds int, signals []generator.CWSignal[float64]) *runningListener {
	t.Helper()

	listener := &runningListener{}
	p := New[float32, float64](newQSO().config(), nil)
	p.SetSpotter(listener)
	p.Notify(listener)

	source := generator.New[float32, float64](generator.GeneratorConfig[float64]{
		SampleRate:      qsoSampleRate,
		CenterFrequency: qsoCenter,
		NoiseLevel:      qsoNoiseLevel,
		Seed:            1,
		Signals:         signals,
	})

	p.Start()
	chunk := make([]float32, 2*qsoChunk)
	for range seconds * qsoSampleRate / qsoChunk {
		source.Read(chunk)
		p.IQData(qsoSampleRate, chunk)
	}
	p.Stop()

	return listener
}

// TestPipelineSpotsARunningStation is the main purpose of SDRainer: a station calls CQ, and the
// pipeline gives its callsign with the speed and the SNR.
func TestPipelineSpotsARunningStation(t *testing.T) {
	listener := runRunningStation(t, 24, []generator.CWSignal[float64]{
		{
			Frequency: qsoCenter, Text: "cq a1bc a1bc test", WPM: 30, Amplitude: 1.0,
			RiseTime: 5 * time.Millisecond,
		},
	})

	detected, spots, text := listener.result()
	require.NotEmptyf(t, detected, "the pipeline must find the running station, text %q", text)
	assert.Equal(t, "A1BC", detected[0].Callsign.String())
	assert.InDelta(t, qsoCenter, detected[0].Frequency, 30, "the frequency of the spot")
	assert.InDelta(t, 30, detected[0].WPM, 4, "the speed of the spot")

	require.NotEmpty(t, spots, "the pipeline must make a spot")
	assert.Contains(t, spots[0], "A1BC 7028000 CW ", "the spot holds the callsign, the frequency and the speed")
}

// TestPipelineSpotsOnlyTheRunningStationOfAQSO puts the two stations of a QSO on one frequency, as
// it happens on the band: the station that answers uses the frequency of the running station, so
// both texts arrive on one channel. Only the running station must give a spot.
//
// The answering station repeats its callsign, as it does in a pileup. It therefore sends its
// callsign more often than the running station, and a rule that only counts the callsigns would
// give the wrong result here.
func TestPipelineSpotsOnlyTheRunningStationOfAQSO(t *testing.T) {
	const turn = 10 * time.Second

	listener := runRunningStation(t, 40, []generator.CWSignal[float64]{
		{
			Frequency: qsoCenter, Text: "cq a1bc test", WPM: 30, Amplitude: 1.0,
			RiseTime: 5 * time.Millisecond, TurnPeriod: 2 * turn,
		},
		{
			Frequency: qsoCenter, Text: "dl0xy dl0xy dl0xy", WPM: 30, Amplitude: 0.5,
			RiseTime: 5 * time.Millisecond, TurnPeriod: 2 * turn, TurnOffset: turn,
		},
	})

	detected, spots, text := listener.result()
	require.NotEmptyf(t, detected, "the pipeline must find the running station, text %q", text)
	for _, channel := range detected {
		assert.Equalf(t, "A1BC", channel.Callsign.String(), "only the running station, text %q", text)
	}
	require.NotEmpty(t, spots, "the running station must give a spot")
	for _, spot := range spots {
		assert.Containsf(t, spot, "A1BC", "only the running station gives a spot, text %q", text)
	}
}

// TestCallsignStageTakesTheRunningStationAgainstAnAnswerWithDe covers the answer of a QSO: it names
// the running station, then "de", then its own callsign. Only the running station may get a hit.
func TestCallsignStageTakesTheRunningStationAgainstAnAnswerWithDe(t *testing.T) {
	detected := runCallsignStage("cq a1bc test a1bc de dl0xy cq a1bc test ")

	require.NotEmpty(t, detected)
	assert.Equal(t, "A1BC", detected[len(detected)-1].String())
	for _, found := range detected {
		assert.NotEqual(t, "DL0XY", found.String(), "the station that answers must get no hit")
	}
}

// TestCallsignStageIgnoresTheDeOfAnOver covers the form that an operator uses outside a contest:
// each over begins and ends with "<the other station> de <the own station>". Neither of the two
// callsigns says which station runs, so that "de" must count nothing.
func TestCallsignStageIgnoresTheDeOfAnOver(t *testing.T) {
	tt := []struct {
		name string
		text string
	}{
		{name: "the beginning of an over", text: "a1bc de dl0xy tu fer info a1bc de dl0xy tu fer info "},
		{name: "the end of an over", text: "btu a1bc de dl0xy k btu a1bc de dl0xy k "},
		{name: "a whole over", text: "a1bc de dl0xy tu fer info btu a1bc de dl0xy k "},
		{name: "the callsign before de in pieces", text: "a 1 b c de dl0xy a 1 b c de dl0xy "},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			assert.Emptyf(t, runCallsignStage(tc.text), "%q must give no callsign", tc.text)
		})
	}
}

// TestCallsignStageTakesTheDeOfACall is the other side: a "de" with a call keyword before it
// belongs to the call of the running station.
func TestCallsignStageTakesTheDeOfACall(t *testing.T) {
	tt := []struct {
		name string
		text string
	}{
		{name: "cq de", text: "cq de a1bc cq de a1bc "},
		{name: "cq dx de", text: "cq dx de a1bc cq dx de a1bc "},
		{name: "cq cq de", text: "cq cq de a1bc cq cq de a1bc "},
		{name: "test de", text: "test de a1bc test de a1bc "},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			detected := runCallsignStage(tc.text)

			require.NotEmptyf(t, detected, "%q must give the callsign of the running station", tc.text)
			assert.Equal(t, "A1BC", detected[len(detected)-1].String())
		})
	}
}

// TestCallsignStageIgnoresADeWithoutAKeyword covers the "de" that begins an over. "de" is a filler
// word, so a keyword must stand before it.
func TestCallsignStageIgnoresADeWithoutAKeyword(t *testing.T) {
	tt := []struct {
		name string
		text string
	}{
		{name: "de alone", text: "de dl0xy de dl0xy "},
		{name: "de at the beginning of an over", text: "de dl0xy tu fer info de dl0xy k "},
		{name: "a callsign before de", text: "a1bc de dl0xy a1bc de dl0xy "},
		{name: "a word that is no keyword before de", text: "info de dl0xy info de dl0xy "},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			assert.Emptyf(t, runCallsignStage(tc.text), "%q must give no callsign", tc.text)
		})
	}
}

// TestCallsignStageTakesARepeatedCallsign covers the call that holds no word behind the callsign:
// "cq a1bc a1bc" is complete, and the pattern of a keyword on each side never closes. The
// repetition is the second hit, so the stage gives the spot at the moment of the second callsign.
func TestCallsignStageTakesARepeatedCallsign(t *testing.T) {
	tt := []struct {
		name string
		text string
	}{
		{name: "cq with two callsigns", text: "cq a1bc a1bc "},
		{name: "test with two callsigns", text: "test a1bc a1bc "},
		{name: "cq with three callsigns", text: "cq a1bc a1bc a1bc "},
		{name: "cq cq with two callsigns", text: "cq cq a1bc a1bc "},
		{name: "cq dx with two callsigns", text: "cq dx a1bc a1bc "},
		{name: "cq de with two callsigns", text: "cq de a1bc a1bc "},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			detected := runCallsignStage(tc.text)

			require.NotEmptyf(t, detected, "%q must give the callsign of the running station", tc.text)
			assert.Equal(t, "A1BC", detected[len(detected)-1].String())
		})
	}
}

// TestCallsignStageTakesNoRepetitionWithoutAKeyword is the station that answers a call: it repeats
// its callsign too, and no keyword stands before it.
func TestCallsignStageTakesNoRepetitionWithoutAKeyword(t *testing.T) {
	tt := []struct {
		name string
		text string
	}{
		{name: "the answer of a pileup", text: "dl0xy dl0xy dl0xy dl0xy "},
		{name: "a callsign before the repetition", text: "a1bc dl0xy dl0xy a1bc dl0xy dl0xy "},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			assert.Emptyf(t, runCallsignStage(tc.text), "%q must give no callsign", tc.text)
		})
	}
}

// TestCallsignStageTakesNoRepetitionOfTwoDifferentCallsigns covers the call of a station that gives
// the callsign of another station behind its own one. Only the same callsign, more than one time,
// makes the second hit.
func TestCallsignStageTakesNoRepetitionOfTwoDifferentCallsigns(t *testing.T) {
	assert.Empty(t, runCallsignStage("cq a1bc dl0xy "))
}

// TestCallsignStageJoinsACallsignInPieces takes the text of a station that sends a wide gap between
// the characters of its callsign. The decoder makes a word of each character, and the stage joins
// them again. A measurement with OM1UM in pipeline/testdata gives "o m 1 u m" for "om1um", see
// section 12.3 of doc/architecture.md.
func TestCallsignStageJoinsACallsignInPieces(t *testing.T) {
	tt := []struct {
		name string
		text string
	}{
		{name: "each character alone, after cq", text: "cq o m 1 u m cq o m 1 u m "},
		{name: "each character alone, before test", text: "o m 1 u m test o m 1 u m test "},
		{name: "two pieces", text: "cq om 1um cq om 1um "},
		{name: "the pieces of a call with a prefix", text: "cq d l 1 a b c cq d l 1 a b c "},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			detected := runCallsignStage(tc.text)

			require.NotEmptyf(t, detected, "%q must give the callsign of the running station", tc.text)
			assert.Contains(t, []string{"OM1UM", "DL1ABC"}, detected[len(detected)-1].String())
		})
	}
}

// TestCallsignStageJoinsNoLongWords checks the limit of the rule that joins: only the pieces of a
// callsign are short, and a normal word must stay as it is. Without the limit "dl1abc k" would give
// the callsign DL1ABCK.
func TestCallsignStageJoinsNoLongWords(t *testing.T) {
	detected := runCallsignStage("cq dl1abc k cq dl1abc k ")

	require.NotEmpty(t, detected)
	assert.Equal(t, "DL1ABC", detected[len(detected)-1].String())
}

// TestCallsignStageTakesTheLongestOfThePieces checks that the stage does not stop at a short part of
// a callsign: "m1um" is a valid callsign, and it stands inside "om1um".
func TestCallsignStageTakesTheLongestOfThePieces(t *testing.T) {
	detected := runCallsignStage("o m 1 u m test o m 1 u m test ")

	require.NotEmpty(t, detected)
	assert.Equal(t, "OM1UM", detected[len(detected)-1].String())
}

// spotRecorder collects what the pipeline gives to the spotter.
type spotRecorder struct {
	spots   []string
	removed []string
}

func (r *spotRecorder) Spot(callsign string, _ float64, _ string, _ time.Time) {
	r.spots = append(r.spots, callsign)
}

func (r *spotRecorder) RemoveSpot(callsign string) {
	r.removed = append(r.removed, callsign)
}

// TestPipelineTakesTheSpotBackWhenAChannelGoesAway covers the way from the end of a channel to the
// spotter. The DX cluster gives each spot that it holds to a new connection, so a station that is
// gone must go away there.
func TestPipelineTakesTheSpotBackWhenAChannelGoesAway(t *testing.T) {
	spotter := &spotRecorder{}
	p := New[float32, float64](DefaultConfig(12000, 0.0), nil)
	p.SetSpotter(spotter)

	channel := core.Channel[float64]{ID: "1", Frequency: 700, State: core.ActiveChannel}
	p.emitChannelCreated(channel)
	for _, character := range "cq a1bc a1bc test " {
		p.emitChannelCharacterReceived(channel, character, 0)
	}
	require.Equal(t, []string{"A1BC"}, spotter.spots, "the callsign of the running station")

	p.emitChannelDestroyed(channel)

	assert.Equal(t, []string{"A1BC"}, spotter.removed)
}

// TestPipelineTakesNoSpotBackForAChannelWithoutACallsign covers the normal case: most channels never
// give a callsign, and they must make no work for the spotter.
func TestPipelineTakesNoSpotBackForAChannelWithoutACallsign(t *testing.T) {
	spotter := &spotRecorder{}
	p := New[float32, float64](DefaultConfig(12000, 0.0), nil)
	p.SetSpotter(spotter)

	channel := core.Channel[float64]{ID: "1", Frequency: 700, State: core.ActiveChannel}
	p.emitChannelCreated(channel)

	p.emitChannelDestroyed(channel)

	assert.Empty(t, spotter.removed)
}

// TestCallsignStageIgnoresAFillerWordAlone holds the difference between a filler word and a
// keyword. A filler word only stands between a keyword and the callsign, as in "cq dx a1bc" and
// "cq de a1bc". The same word in the text of a QSO says nothing about the station that runs, so a
// callsign behind it counts nothing.
func TestCallsignStageIgnoresAFillerWordAlone(t *testing.T) {
	tt := []struct {
		name string
		text string
	}{
		{name: "a report that names dx", text: "es dx a1bc es dx a1bc "},
		{name: "thanks for the contact", text: "tnx dx a1bc tnx dx a1bc "},
		{name: "de at the beginning of an over", text: "de a1bc de a1bc "},
		{name: "a filler word twice", text: "dx de a1bc dx de a1bc "},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			assert.Emptyf(t, runCallsignStage(tc.text), "%q must give no callsign", tc.text)
		})
	}
}

// TestCallsignStageDoesNotJoinAFillerWord covers the join of the pieces of a callsign: a filler
// word is no piece, exactly as a keyword is no piece.
func TestCallsignStageDoesNotJoinAFillerWord(t *testing.T) {
	detected := runCallsignStage("cq dx a 1 b c dx test cq dx a 1 b c dx test ")

	for _, found := range detected {
		assert.NotContains(t, found.String(), "DX", "a filler word must not become a part of the callsign")
	}
}

// TestCallsignStageNeedsACallBeforeTu covers the rule of "tu": a running station finishes a QSO with
// "tu a1bc test", and a station that answers thanks with the same words and gives the frequency
// back. Only a channel that already gave a hit with "cq" or "test" holds a station that runs.
func TestCallsignStageNeedsACallBeforeTu(t *testing.T) {
	t.Run("tu alone gives no callsign", func(t *testing.T) {
		assert.Empty(t, runCallsignStage("tu a1bc tu a1bc "))
	})

	t.Run("tu counts after a call on the same channel", func(t *testing.T) {
		detected := runCallsignStage("cq a1bc a1bc tu a1bc ")

		require.NotEmpty(t, detected)
		assert.Equal(t, "A1BC", detected[len(detected)-1].String())
	})
}

// TestCallsignStageIgnoresAnAnswerBehindATrailingTest is the case that a real recording gave: the
// decoder loses the first character of the callsign of the running station, so "a1bc test" becomes
// "1bc test", and the answer of the pileup follows. Without the rule of keywordCounts the "test"
// opens a call for the station that answers.
func TestCallsignStageIgnoresAnAnswerBehindATrailingTest(t *testing.T) {
	detected := runCallsignStage("1bc test dl0xy dl0xy dl0xy cq a1bc test dl0xy dl0xy dl0xy ")

	require.NotEmpty(t, detected)
	assert.Equal(t, "A1BC", detected[len(detected)-1].String())
	for _, found := range detected {
		assert.NotEqual(t, "DL0XY", found.String(), "the station that answers must get no hit")
	}
}

// TestPipelineReportsTheQualityOfAChannel covers the event of the quality: it comes when the
// quality of a channel changes, and the channel of each event carries that value.
func TestPipelineReportsTheQualityOfAChannel(t *testing.T) {
	listener := runRunningStation(t, 40, []generator.CWSignal[float64]{
		{
			Frequency: qsoCenter, Text: "cq a1bc a1bc test", WPM: 30, Amplitude: 1.0,
			RiseTime: 5 * time.Millisecond,
		},
	})

	changes := listener.qualityChanges()
	require.NotEmpty(t, changes, "the quality of the channel must give an event")
	assert.Equal(t, core.UnverifiedQuality, changes[0], "the first answer is thin")
	assert.Equal(t, core.ValidQuality, changes[len(changes)-1], "the station repeats its call")

	detected, _, _ := listener.result()
	require.NotEmpty(t, detected)
	assert.Equal(t, core.ValidQuality, detected[len(detected)-1].Quality,
		"the event of the callsign carries the quality of that moment")
}

// TestPipelineReportsAQualityOnlyWhenItChanges keeps the event quiet: a station that repeats its
// call gives many hits, and the quality stays at V.
func TestPipelineReportsAQualityOnlyWhenItChanges(t *testing.T) {
	listener := runRunningStation(t, 60, []generator.CWSignal[float64]{
		{
			Frequency: qsoCenter, Text: "cq a1bc a1bc test", WPM: 30, Amplitude: 1.0,
			RiseTime: 5 * time.Millisecond,
		},
	})

	changes := listener.qualityChanges()

	require.NotEmpty(t, changes)
	assert.LessOrEqual(t, len(changes), 2, "one change to ? and one to V, and no more: %v", changes)
}

// TestCallsignStageTakesARepetitionInOneWordApart covers the decoder that loses the gap between two
// repetitions of a callsign. Without parseCallsign the spot carried DL1ABCDL1ABC, a callsign that no
// station has, and the callsign of the station got no hit at all.
func TestCallsignStageTakesARepetitionInOneWordApart(t *testing.T) {
	tt := []struct {
		name string
		text string
	}{
		{name: "two times", text: "cq dl1abcdl1abc test cq dl1abcdl1abc test "},
		{name: "three times", text: "cq dl1abcdl1abcdl1abc test cq dl1abcdl1abcdl1abc test "},
		{name: "one repetition beside a clean one", text: "cq dl1abcdl1abc test cq dl1abc test "},
		{name: "in pieces", text: "cq d l1 abc d l1 abc test cq dl1abc test "},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			detected := runCallsignStage(tc.text)

			require.NotEmpty(t, detected)
			assert.Equal(t, "DL1ABC", detected[len(detected)-1].String())
		})
	}
}

// TestCallsignStageKeepsAWordThatIsNoCleanRepetition holds the limit of parseCallsign: a decoder
// that loses one character of one repetition gives a word without a period, and a guess about which
// part is the callsign would make a callsign out of a word that holds none.
func TestCallsignStageKeepsAWordThatIsNoCleanRepetition(t *testing.T) {
	detected := runCallsignStage("cq dl1abcdl1ab test cq dl1abcdl1ab test ")

	require.NotEmpty(t, detected)
	assert.Equal(t, "DL1ABCDL1AB", detected[len(detected)-1].String())
}

// TestParseCallsignKeepsAWordThatIsNoRepetition covers the words that must stay as they are.
func TestParseCallsignKeepsAWordThatIsNoRepetition(t *testing.T) {
	tt := []struct {
		word     string
		expected string
	}{
		{word: "dl1abc", expected: "DL1ABC"},
		{word: "dl1abc/p", expected: "DL1ABC/p"},
		{word: "dl1abcdl1ab", expected: "DL1ABCDL1AB"}, // no period, so no part of it is the callsign
		{word: "dl1abcdl2abc", expected: "DL1ABCDL2ABC"},
	}

	for _, tc := range tt {
		t.Run(tc.word, func(t *testing.T) {
			found, ok := parseCallsign(tc.word)

			require.True(t, ok)
			assert.Equal(t, tc.expected, found.String())
		})
	}
}

// TestParseCallsignTakesTheShortestPart covers the word that holds more than one period: DL1ABC four
// times also holds DL1ABCDL1ABC two times, and the callsign is the shortest of the parts.
func TestParseCallsignTakesTheShortestPart(t *testing.T) {
	found, ok := parseCallsign("dl1abcdl1abcdl1abcdl1abc")

	require.True(t, ok)
	assert.Equal(t, "DL1ABC", found.String())
}

// TestParseCallsignTakesNoWordThatIsNoCallsign holds that the split changes nothing about which
// words are a callsign at all.
func TestParseCallsignTakesNoWordThatIsNoCallsign(t *testing.T) {
	for _, word := range []string{"", "cq", "n1n1", "test"} {
		_, ok := parseCallsign(word)

		assert.Falsef(t, ok, "%q is no callsign", word)
	}
}

// TestCallsignStageTakesTheWordOfAContest covers the word that the operators of one contest put into
// their call. It holds the place between the keyword of the call and the callsign, and without it
// the search stops there: the join of that word and the callsign is no callsign.
func TestCallsignStageTakesTheWordOfAContest(t *testing.T) {
	tt := []struct {
		name    string
		contest string
		text    string
	}{
		{name: "cq and the word", contest: "yo", text: "cq yo a1bc cq yo a1bc "},
		{name: "cq, the word and test", contest: "cwt", text: "cq cwt test a1bc cq cwt test a1bc "},
		{name: "the word before test", contest: "cwt", text: "cq cwt test a1bc a1bc "},
		// This one holds without the configuration too, and only by luck: the join of "cwt" and
		// "a1bc" is no callsign because "a1bc" is longer than maxJoinedWordLength. With the
		// configuration the join rejects "cwt" as a filler word, whatever its length is.
		{name: "the word, the callsign and test", contest: "cwt", text: "cwt a1bc test cwt a1bc test "},
		{name: "beside a filler word", contest: "yo", text: "cq yo de a1bc cq yo de a1bc "},
		{name: "in upper case", contest: "YO", text: "cq yo a1bc cq yo a1bc "},
		{name: "with a space around it", contest: " yo ", text: "cq yo a1bc cq yo a1bc "},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			detected := runCallsignStage(tc.text, tc.contest)

			require.NotEmptyf(t, detected, "%q must give the callsign of the running station", tc.text)
			assert.Equal(t, "A1BC", detected[len(detected)-1].String())
		})
	}
}

// TestCallsignStageWithoutTheWordOfAContest is the other side: the same text without the
// configuration gives no callsign, because the word stands between the keyword and the callsign and
// the stage does not know it.
func TestCallsignStageWithoutTheWordOfAContest(t *testing.T) {
	assert.Empty(t, runCallsignStage("cq yo a1bc cq yo a1bc "))
}

// TestCallsignStageTakesNoWordOfAContestAsACallsign holds that the word is a filler word and no
// piece of a callsign: "cq cwt test" alone holds no station.
func TestCallsignStageTakesNoWordOfAContestAsACallsign(t *testing.T) {
	assert.Empty(t, runCallsignStage("cq cwt test cq cwt test ", "cwt"))
}

// TestCallsignStageClosesACallWithTheWordOfAContest covers the call that holds neither "cq" nor
// "test": "dl1abc dl1abc cwt" is the whole call of a station of that contest, and the word closes it
// exactly as "test" closes "a1bc a1bc test".
func TestCallsignStageClosesACallWithTheWordOfAContest(t *testing.T) {
	tt := []struct {
		name string
		text string
	}{
		{name: "the callsign two times", text: "a1bc a1bc cwt a1bc a1bc cwt "},
		{name: "the callsign one time", text: "a1bc cwt a1bc cwt "},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			detected := runCallsignStage(tc.text, "cwt")

			require.NotEmptyf(t, detected, "%q is a call of that contest", tc.text)
			assert.Equal(t, "A1BC", detected[len(detected)-1].String())

			assert.Empty(t, runCallsignStage(tc.text), "and it gives nothing without the configuration")
		})
	}
}

// TestCallsignStageOpensNoCallWithTheWordOfAContest holds the other side: the word stands for the
// contest and not for the invitation to answer, so it opens no call. A word of a contest at the
// beginning of a text is as often the end of the call before it.
func TestCallsignStageOpensNoCallWithTheWordOfAContest(t *testing.T) {
	assert.Empty(t, runCallsignStage("cwt a1bc cwt a1bc ", "cwt"))
}

// TestCallsignStageNeedsACallBesideTheWordOfAContest holds that a word of a contest inside the text
// of a QSO gives no callsign of its own: the callsign must stand beside it.
func TestCallsignStageNeedsACallBesideTheWordOfAContest(t *testing.T) {
	assert.Empty(t, runCallsignStage("tnx 5nn yo tnx 5nn yo ", "yo"))
}

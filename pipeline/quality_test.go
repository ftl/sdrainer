package pipeline

import (
	"testing"

	"github.com/ftl/hamradio/callsign"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ftl/sdrainer/core"
)

func qualityChannel(t *testing.T, id core.ChannelID, call string, frequency float64) core.Channel[float64] {
	t.Helper()

	found, err := callsign.Parse(call)
	require.NoError(t, err)

	return core.Channel[float64]{ID: id, Callsign: found, Frequency: frequency, WPM: 24, SNR: 25}
}

func TestQualityOfASpotWithThinEvidence(t *testing.T) {
	qualities := newSpotQualities[float64]()

	quality := qualities.tagFor(qualityChannel(t, "1", "dl1abc", 7028000),
		callsignEvidence{hits: minCallsignHits})

	assert.Equal(t, core.UnverifiedQuality, quality.tag)
	assert.Empty(t, quality.correction)
}

func TestQualityOfASpotWithEnoughHits(t *testing.T) {
	qualities := newSpotQualities[float64]()

	quality := qualities.tagFor(qualityChannel(t, "1", "dl1abc", 7028000),
		callsignEvidence{hits: validCallsignHits})

	assert.Equal(t, core.ValidQuality, quality.tag)
}

// TestQualityOfASpotWithACompetitor covers the channel on which the decoder read the callsign in two
// ways: neither of the two is sure, however many hits the leader has.
func TestQualityOfASpotWithACompetitor(t *testing.T) {
	qualities := newSpotQualities[float64]()

	quality := qualities.tagFor(qualityChannel(t, "1", "dl1abc", 7028000),
		callsignEvidence{hits: validCallsignHits, nearCompetitor: true})

	assert.Equal(t, core.UnverifiedQuality, quality.tag)
}

// TestQualityOfABustedCallsign is the case that costs a contest operator a QSO: the decoder loses
// one character of a callsign that the same channel already gave. The spot then names the callsign
// that the receiver made valid.
func TestQualityOfABustedCallsign(t *testing.T) {
	qualities := newSpotQualities[float64]()
	valid := qualities.tagFor(qualityChannel(t, "1", "dl1abc", 7028000),
		callsignEvidence{hits: validCallsignHits})
	require.Equal(t, core.ValidQuality, valid.tag)

	quality := qualities.tagFor(qualityChannel(t, "1", "dl1abd", 7028000),
		callsignEvidence{hits: validCallsignHits})

	assert.Equal(t, core.BustedQuality, quality.tag)
	assert.Equal(t, "DL1ABC", quality.correction)
}

// TestQualityOfACallsignOfAnotherChannel keeps the two channels apart: two stations on two
// frequencies can hold callsigns that stand near each other, and neither is an error of the decoder.
func TestQualityOfACallsignOfAnotherChannel(t *testing.T) {
	qualities := newSpotQualities[float64]()
	qualities.tagFor(qualityChannel(t, "1", "dl1abc", 7028000), callsignEvidence{hits: validCallsignHits})

	quality := qualities.tagFor(qualityChannel(t, "2", "dl1abd", 7035000),
		callsignEvidence{hits: validCallsignHits})

	assert.Equal(t, core.ValidQuality, quality.tag)
}

// TestQualityOfAStationThatMoved covers the station that was valid and appears somewhere else with
// thin evidence. It is a QSY, or the new spot is an image of the old one, and the consumer must know
// that.
func TestQualityOfAStationThatMoved(t *testing.T) {
	qualities := newSpotQualities[float64]()
	qualities.tagFor(qualityChannel(t, "1", "dl1abc", 7028000), callsignEvidence{hits: validCallsignHits})

	quality := qualities.tagFor(qualityChannel(t, "2", "dl1abc", 7035000),
		callsignEvidence{hits: minCallsignHits})

	assert.Equal(t, core.QSYQuality, quality.tag)
}

// TestQualityOfAStationThatMovedAndStays is the case that a QSY must not hold for ever: the station
// stands on the new frequency, the receiver reads its callsign there again and again, and the spot
// then says that the receiver is sure.
//
// The tag stayed at QualityQSY before: the answer of a spot came from the frequency of the station
// before it, and not from the evidence of the frequency where it stands now.
func TestQualityOfAStationThatMovedAndStays(t *testing.T) {
	qualities := newSpotQualities[float64]()
	require.Equal(t, core.ValidQuality,
		qualities.tagFor(qualityChannel(t, "1", "dl1abc", 7028000), callsignEvidence{hits: validCallsignHits}).tag,
		"the station is valid on its first frequency")

	moved := qualityChannel(t, "2", "dl1abc", 7035000)
	require.Equal(t, core.QSYQuality,
		qualities.tagFor(moved, callsignEvidence{hits: minCallsignHits}).tag,
		"the first spot of the new frequency holds thin evidence")

	quality := qualities.tagFor(moved, callsignEvidence{hits: validCallsignHits})

	assert.Equal(t, core.ValidQuality, quality.tag, "the evidence of the new frequency is now enough")

	// and it stays valid there, so a further spot gives no QSY of the frequency before it
	assert.Equal(t, core.ValidQuality,
		qualities.tagFor(moved, callsignEvidence{hits: validCallsignHits}).tag)
}

// TestQualityOfAStationThatMovesBack covers the station that goes to a new frequency and comes back:
// the frequency that it left is now the other one, so it gives a QSY again while the evidence there
// is thin.
func TestQualityOfAStationThatMovesBack(t *testing.T) {
	qualities := newSpotQualities[float64]()
	full := callsignEvidence{hits: validCallsignHits}

	qualities.tagFor(qualityChannel(t, "1", "dl1abc", 7028000), full)
	qualities.tagFor(qualityChannel(t, "2", "dl1abc", 7035000), full)

	quality := qualities.tagFor(qualityChannel(t, "3", "dl1abc", 7028000),
		callsignEvidence{hits: minCallsignHits})

	assert.Equal(t, core.QSYQuality, quality.tag)
}

// TestQualityOfAStationThatDrifts is the other side: the tracker follows a station over a small
// distance, and that is no QSY.
func TestQualityOfAStationThatDrifts(t *testing.T) {
	qualities := newSpotQualities[float64]()
	qualities.tagFor(qualityChannel(t, "1", "dl1abc", 7028000), callsignEvidence{hits: validCallsignHits})

	quality := qualities.tagFor(qualityChannel(t, "1", "dl1abc", 7028000+qsyWidth/2),
		callsignEvidence{hits: validCallsignHits})

	assert.Equal(t, core.ValidQuality, quality.tag)
}

// TestQualityForgetsTheCallsignsOfAChannelThatIsGone covers the channel that goes away: a new
// station on that frequency must not be busted against a callsign of the station before it.
func TestQualityForgetsTheCallsignsOfAChannelThatIsGone(t *testing.T) {
	qualities := newSpotQualities[float64]()
	qualities.tagFor(qualityChannel(t, "1", "dl1abc", 7028000), callsignEvidence{hits: validCallsignHits})

	qualities.channelDestroyed("1")

	quality := qualities.tagFor(qualityChannel(t, "1", "dl1abd", 7028000),
		callsignEvidence{hits: validCallsignHits})
	assert.NotEqual(t, core.BustedQuality, quality.tag)
}

func TestQualityOfAChannelWithoutACallsign(t *testing.T) {
	qualities := newSpotQualities[float64]()

	quality := qualities.tagFor(core.Channel[float64]{ID: "1"}, callsignEvidence{hits: validCallsignHits})

	assert.Equal(t, core.UnverifiedQuality, quality.tag)
}

func TestSpotMessage(t *testing.T) {
	channel := qualityChannel(t, "1", "dl1abc", 7028000)

	assert.Equal(t, "CW 25 dB 24 WPM CQ V", spotMessage(channel, spotQuality{tag: core.ValidQuality}))
	assert.Equal(t, "CW 25 dB 24 WPM CQ ?", spotMessage(channel, spotQuality{tag: core.UnverifiedQuality}))
	assert.Equal(t, "CW 25 dB 24 WPM CQ B (DL1ABC)",
		spotMessage(channel, spotQuality{tag: core.BustedQuality, correction: "DL1ABC"}))
}

func TestLevenshtein(t *testing.T) {
	tt := []struct {
		first    string
		second   string
		expected int
	}{
		{first: "", second: "", expected: 0},
		{first: "DL1ABC", second: "DL1ABC", expected: 0},
		{first: "DL1ABC", second: "DL1ABD", expected: 1},
		{first: "DL1ABC", second: "DL1AB", expected: 1},
		{first: "DL1ABC", second: "DL1ABCD", expected: 1},
		{first: "K3LR", second: "LW3LPL", expected: 4},
		{first: "DL1ABC", second: "", expected: 6},
		{first: "", second: "DL1ABC", expected: 6},
	}

	for _, tc := range tt {
		t.Run(tc.first+"/"+tc.second, func(t *testing.T) {
			assert.Equal(t, tc.expected, levenshtein(tc.first, tc.second))
			assert.Equal(t, tc.expected, levenshtein(tc.second, tc.first), "the distance is symmetric")
		})
	}
}

// TestLevenshteinIsNotTheDistanceOfTheTests holds the difference to bestMatchDistance: that one
// looks for the best part of the second text, and two callsigns need the whole text.
func TestLevenshteinIsNotTheDistanceOfTheTests(t *testing.T) {
	assert.Equal(t, 0, bestMatchDistance("DL1AB", "DL1ABC"), "a part of the text")
	assert.Equal(t, 1, levenshtein("DL1AB", "DL1ABC"), "the whole text")
}

// TestQualityOfAStationOnTwoBands is the normal way of a multi-operator station: it runs on more
// than one band at the same time. That is no QSY and no image, so each band keeps its own answer.
//
// Without the band in the key the two frequencies gave a QSY at each change, and neither of them
// ever became valid again.
func TestQualityOfAStationOnTwoBands(t *testing.T) {
	qualities := newSpotQualities[float64]()
	evidence := callsignEvidence{hits: validCallsignHits}

	tt := []struct {
		frequency float64
		expected  core.ChannelQuality
	}{
		{frequency: 7028000, expected: core.ValidQuality},
		{frequency: 14028000, expected: core.ValidQuality},
		{frequency: 7028000, expected: core.ValidQuality},
		{frequency: 14028000, expected: core.ValidQuality},
		{frequency: 21028000, expected: core.ValidQuality},
	}
	for _, tc := range tt {
		quality := qualities.tagFor(qualityChannel(t, "1", "dl1abc", tc.frequency), evidence)

		assert.Equalf(t, tc.expected.String(), quality.tag.String(), "at %.0f Hz", tc.frequency)
	}
}

// TestQualityOfAStationThatMovesInsideOneBand keeps the case that core.QSYQuality is for: the station
// stays on the band and it changes its frequency there.
func TestQualityOfAStationThatMovesInsideOneBand(t *testing.T) {
	qualities := newSpotQualities[float64]()
	evidence := callsignEvidence{hits: validCallsignHits}

	require.Equal(t, core.ValidQuality, qualities.tagFor(qualityChannel(t, "1", "dl1abc", 7028000), evidence).tag)

	quality := qualities.tagFor(qualityChannel(t, "2", "dl1abc", 7035000),
		callsignEvidence{hits: minCallsignHits})

	assert.Equal(t, core.QSYQuality, quality.tag)
}

func TestBandOfAFrequency(t *testing.T) {
	tt := []struct {
		frequency float64
		expected  int
	}{
		{frequency: 1830000, expected: 1},   // 160 m
		{frequency: 3520000, expected: 3},   // 80 m
		{frequency: 7028000, expected: 7},   // 40 m
		{frequency: 10120000, expected: 10}, // 30 m
		{frequency: 14028000, expected: 14}, // 20 m
		{frequency: 18075000, expected: 18}, // 17 m
		{frequency: 21028000, expected: 21}, // 15 m
		{frequency: 24895000, expected: 24}, // 12 m
		{frequency: 28028000, expected: 28}, // 10 m
		{frequency: 50090000, expected: 50}, // 6 m
	}

	bands := make(map[int]bool, len(tt))
	for _, tc := range tt {
		assert.Equalf(t, tc.expected, bandOf(tc.frequency), "%.0f Hz", tc.frequency)
		assert.Falsef(t, bands[tc.expected], "two bands must not give the same value: %d", tc.expected)
		bands[tc.expected] = true
	}
}

// TestBandOfAFrequencyOverTheBorderOfOneMHz holds the one band that lies over a border of one MHz.
// A station that moves over that border gives no QSY, and that is the direction that costs nothing.
func TestBandOfAFrequencyOverTheBorderOfOneMHz(t *testing.T) {
	assert.Equal(t, 28, bandOf(28500000), "the lower part of 10 m")
	assert.Equal(t, 29, bandOf(29100000), "the upper part of 10 m")
}

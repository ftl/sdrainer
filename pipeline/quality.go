package pipeline

import (
	"math"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/dsp"
)

// The quality tags of a spot are core.ChannelQuality. They are the tags of the algorithm of CT1BOH,
// which AR-Cluster 6 gives to a client that asks for them with "SET DX EXTENSION SKIMMERQUALITY". A
// logger that reads those tags, for example DXLog or N1MM+, therefore needs no change for the spots
// of SDRainer.
//
// doc/spot_quality_concept.md holds the sources and the whole concept.

const (
	// validCallsignHits is the count of the hits at which a spot becomes valid. minCallsignHits is
	// the count that gives a spot at all, and this value must stand above it, otherwise each spot
	// is valid at once.
	//
	// One call of a running station gives 2 hits, for example "cq a1bc a1bc test", so 4 hits are
	// two complete calls. A wrong word of the decoder is different in each repetition, and a call
	// that the decoder read the same way two times is therefore the call of the station.
	//
	// The value is a first one and no measurement stands behind it yet. Section 6 of
	// doc/spot_quality_concept.md says how to measure it.
	validCallsignHits = 4

	// bustDistance is the largest distance of one character at which two callsigns are the same
	// callsign with an error of the decoder. One character is the case that the algorithm of
	// CT1BOH names: "K3LR" that becomes "LW3LPL" is far away, and a call that differs in one
	// character is the usual error of a decoder.
	bustDistance = 1

	// qsyWidth is the distance from which a callsign that was valid before stands on a new
	// frequency. It is far above the width of a match of the tracker, which is 100 Hz, so the drift
	// of a station gives no QSY.
	qsyWidth = 500.0
)

// bandOf gives the band of a frequency, as the count of the whole MHz.
//
// **core.QSYQuality holds inside one band and not over two bands.** A station of a multi-operator group
// runs on more than one band at the same time, and that is the normal way of such a station: it is
// no QSY, and it is no image. Without the band a callsign on 7 MHz and on 14 MHz gives a QSY at each
// change, and neither of the two frequencies ever becomes valid again.
//
// The whole MHz is sufficient for that: no two bands of the amateur service hold the same whole MHz.
// A band that lies over a border of one MHz, thus 10 m, falls into two parts, and a station that
// moves over that border gives no QSY. That is the direction that costs nothing: a QSY that we do
// not see is better than a QSY that is none.
func bandOf(frequency float64) int {
	return int(frequency / 1e6)
}

// validKey holds a callsign on one band. The same callsign on another band is another station of
// the same group, or the same station on another band, and neither says anything about this one.
type validKey struct {
	call string
	band int
}

// spotQuality is the answer for one spot.
type spotQuality struct {
	tag core.ChannelQuality

	// correction holds the callsign that the receiver made valid, for a spot with core.BustedQuality. It
	// is empty for each other tag. A logger takes such a spot as a spot of the corrected callsign,
	// and it drops a busted spot without a correction.
	correction string
}

// spotQualities gives the quality tag of each spot of one receiver. It holds what the receiver made
// valid so far, because core.BustedQuality and core.QSYQuality need that knowledge.
//
// Only the worker of the pipeline uses it, so it needs no lock.
type spotQualities[F dsp.Number] struct {
	// validAt holds the frequency at which each callsign became valid, for each band. core.QSYQuality
	// needs it.
	validAt map[validKey]F

	// validOfChannel holds the callsigns that became valid on one channel. core.BustedQuality needs it:
	// a busted callsign of a channel is near a callsign that the same channel already gave.
	validOfChannel map[core.ChannelID][]string

	// current holds the quality of each channel, so that the pipeline sees a change of it.
	current map[core.ChannelID]core.ChannelQuality
}

func newSpotQualities[F dsp.Number]() *spotQualities[F] {
	return &spotQualities[F]{
		validAt:        make(map[validKey]F),
		validOfChannel: make(map[core.ChannelID][]string),
		current:        make(map[core.ChannelID]core.ChannelQuality),
	}
}

// channelDestroyed forgets the callsigns of a channel that went away. validAt stays: a station that
// comes back on another frequency of the same band is exactly the case of core.QSYQuality.
func (q *spotQualities[F]) channelDestroyed(id core.ChannelID) {
	delete(q.validOfChannel, id)
	delete(q.current, id)
}

// qualityOf gives the quality of a channel, or core.NoQuality while that channel gave no callsign.
// Each event of a channel carries that value.
func (q *spotQualities[F]) qualityOf(id core.ChannelID) core.ChannelQuality {
	return q.current[id]
}

// changed holds the new quality of a channel and tells if it is another one than before.
func (q *spotQualities[F]) changed(id core.ChannelID, quality core.ChannelQuality) bool {
	if q.current[id] == quality {
		return false
	}

	q.current[id] = quality
	return true
}

// tagFor gives the quality of one spot, and it holds what that spot makes valid.
func (q *spotQualities[F]) tagFor(channel core.Channel[F], evidence callsignEvidence) spotQuality {
	call := channel.Callsign.String()
	if call == "" {
		return spotQuality{tag: core.UnverifiedQuality}
	}

	// A callsign that stands beside a callsign of the same channel that is already valid is an
	// error of the decoder, and not a second station: two stations of one channel do not have
	// callsigns that differ in one character.
	if correction, ok := q.nearValidOfChannel(channel.ID, call); ok {
		return spotQuality{tag: core.BustedQuality, correction: correction}
	}

	// **The evidence of this frequency decides.** A station that stands here with enough hits is
	// valid here, whatever it did before: the receiver read its callsign here, again and again, and
	// that is what QualityValid says.
	if evidence.hits >= validCallsignHits && !evidence.nearCompetitor {
		q.makeValid(channel, call)
		return spotQuality{tag: core.ValidQuality}
	}

	// The station was valid before on this band and it stands somewhere else on it now, and the
	// evidence here is still thin. It moved, or this spot is an image of the other frequency, and a
	// consumer must know that. The spot becomes valid as soon as the evidence here is enough.
	key := validKey{call: call, band: bandOf(float64(channel.Frequency))}
	if previous, ok := q.validAt[key]; ok && math.Abs(float64(channel.Frequency-previous)) > qsyWidth {
		return spotQuality{tag: core.QSYQuality}
	}

	return spotQuality{tag: core.UnverifiedQuality}
}

func (q *spotQualities[F]) makeValid(channel core.Channel[F], call string) {
	q.validAt[validKey{call: call, band: bandOf(float64(channel.Frequency))}] = channel.Frequency

	for _, current := range q.validOfChannel[channel.ID] {
		if current == call {
			return
		}
	}
	q.validOfChannel[channel.ID] = append(q.validOfChannel[channel.ID], call)
}

// nearValidOfChannel gives the callsign of the same channel that the given callsign is an error of,
// if there is one.
func (q *spotQualities[F]) nearValidOfChannel(id core.ChannelID, call string) (string, bool) {
	for _, current := range q.validOfChannel[id] {
		if current == call {
			return "", false
		}
		if levenshtein(current, call) <= bustDistance {
			return current, true
		}
	}
	return "", false
}

// levenshtein gives the count of the changes of one character that make one text out of the other
// one. Each insertion, each deletion and each substitution counts as one change.
//
// bestMatchDistance of the tests is another measure: it looks for the best part of the second text,
// so "DL1AB" stands at the distance 0 of "DL1ABC". Two callsigns need the distance of the whole
// text, otherwise each callsign that is a part of another one is an error of the decoder.
func levenshtein(a string, b string) int {
	first := []rune(a)
	second := []rune(b)
	if len(first) == 0 {
		return len(second)
	}
	if len(second) == 0 {
		return len(first)
	}

	previous := make([]int, len(second)+1)
	current := make([]int, len(second)+1)
	for j := range previous {
		previous[j] = j
	}

	for i := 1; i <= len(first); i++ {
		current[0] = i
		for j := 1; j <= len(second); j++ {
			cost := 1
			if first[i-1] == second[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}

	return previous[len(second)]
}

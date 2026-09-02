package pipeline

import (
	"strings"

	"github.com/ftl/hamradio/callsign"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/cw"
	"github.com/ftl/sdrainer/dsp"
)

// minCallsignHits is the count of the hits that a callsign needs before the stage reports it. One
// call of a running station gives 2 hits, for example "cq a1bc a1bc test": the first callsign comes
// after "cq", and the second one comes before "test". A pattern with only one hit, for example
// "qrl? a1bc", therefore needs a repetition.
//
// The value must be more than 1. The decoder makes wrong characters, above all at the beginning of
// a transmission, and a wrong word can be a valid callsign. Such a word is different in each
// repetition, and the correct callsign is always the same.
const minCallsignHits = 2

// leadingKeywords stand before the callsign of the running station. Each of them says by itself
// that the callsign behind it belongs to the station that runs: "cq" and "test" are the call,
// "qrl?" asks for the frequency of a call that follows, and "tu" finishes a QSO of the running
// station.
var leadingKeywords = map[string]bool{
	"cq":   true,
	"qrl":  true,
	"qrl?": true,
	"test": true,
	"tu":   true,
}

// callKeywords are the two words that make a call by themselves. A channel that saw one of them
// with a callsign holds a station that runs, and only then does "tu" count, see countWord.
//
// The word of a contest joins them, see CallsignStage.contest.
var callKeywords = map[string]bool{
	"cq":   true,
	"test": true,
}

// fillerWords stand between a leading keyword and the callsign, as in "cq dx a1bc" and
// "cq de a1bc". The search for the callsign steps over them.
//
// The word of a contest joins them, see CallsignStage.contest. It is not in this map, because it
// belongs to one run of SDRainer and not to the code.
//
// They are **not** keywords: a filler word counts nothing by itself, and it only holds the place
// between a keyword and the callsign.
//
// **"de" is a filler word and not a keyword.** It means "from", and it stands in a call as well as
// in a QSO: "cq de a1bc" is a call, and "a1bc de dl0xy" means "A1BC, this is DL0XY". The second
// form says nothing about which of the two stations runs, and an operator uses it at the beginning
// and at the end of each over, so a "de" that counts by itself would give the station that answers
// a hit with each over. A "de" alone, at the beginning of an over, says as little.
//
// "dx" is the same case: only "cq dx" is a call, and a "dx" in the text of a QSO, as in "tnx dx",
// says nothing.
var fillerWords = map[string]bool{
	"de": true,
	"dx": true,
}

// trailingKeywords stand after the callsign of the running station, as in "a1bc test". The word of a
// contest joins them, see CallsignStage.contest and isTrailing.
//
// "tu" is not in this list. A running station finishes a QSO with "dl0xy tu", where the callsign
// before "tu" belongs to the station that answered.
var trailingKeywords = map[string]bool{
	"test": true,
}

// The decoder writes a space between two characters when the gap between them is wide, and the
// operator of a slow fist sends such a gap inside a callsign: "om1um" then arrives as "o m 1 u m".
// The stage joins such pieces again.
//
// maxJoinedWords is the count of the pieces that it joins, and maxJoinedWordLength is the length
// that a piece can have. A piece of a callsign that the decoder cut apart is one or two characters,
// and a limit on that length keeps the rule away from the words of a normal text: "dl1abc k" holds
// no piece, so it stays two words and gives no callsign "dl1abck".
const (
	maxJoinedWords      = 8
	maxJoinedWordLength = 3
)

// runningCallsignReporter has an unexported method, so only this package can implement it. The
// pipeline implements it and sends the event to the listeners and to the spotter.
type runningCallsignReporter[F dsp.Number] interface {
	emitChannelRunningCallsignDetected(core.Channel[F])
}

// CallsignStage finds the callsign of the running station in the text of a channel.
//
// A running station calls CQ, and an answering station uses the same frequency. The text of one
// channel therefore holds the callsigns of both stations, and a word that is a valid callsign is
// not sufficient. The stage uses the words around the callsign: only the running station puts its
// callsign beside "cq", "de", "qrl?", "test" or "tu".
type CallsignStage[F dsp.Number] struct {
	reporter runningCallsignReporter[F]
	channels map[core.ChannelID]*callsignDetector

	// contest is the word that the operators of one contest put into their call, in lower case, or
	// empty. It is a filler word like "de" and "dx": it holds the place between the keyword of the
	// call and the callsign, and it says nothing by itself.
	//
	// **A contest gives each call a word of its own.** "cq cwt test dl1abc" holds "cwt" for the CWops
	// Mini-CWT, and "cq yo dl1abc" holds "yo" for the YO HF DX contest. Without that word the search
	// for the callsign stops at it: the join of "yo" and "dl1abc" is no callsign, and the pattern of
	// a keyword on each side of the callsign never closes.
	//
	// **It is the "test" of its contest, and it closes a call.** "dl1abc dl1abc cwt" is a call and
	// it holds neither "cq" nor "test", so a word that only fills the place between a keyword and
	// the callsign would leave that station without a spot. The word therefore joins
	// trailingKeywords as well, and a callsign before it counts.
	//
	// **It opens no call.** "cwt dl1abc" alone gives nothing, as "test dl1abc" does give something:
	// the word stands for the contest and not for the invitation to answer, and a word of a contest
	// at the beginning of a text is as often the end of the call before it.
	contest string
}

// callsignDetector holds the state of the search on one channel.
type callsignDetector struct {
	word strings.Builder
	// recent holds the last words, so that the detector can join the pieces of a callsign. The
	// last one is the word that came last.
	recent []string
	words  int // the count of the words so far, for a position that the shift of recent keeps

	// The join of the pieces gives a callsign that grows with each piece: "o m 1 u" gives OM1U and
	// the piece after it gives OM1UM. Both are valid callsigns, so both would count and neither
	// would win. The detector therefore takes the hit of the shorter one back.
	lastJoin      string
	lastJoinStart int

	// sawCall says that this channel already gave a hit with "cq" or "test". Only then does "tu"
	// count.
	sawCall bool

	// contest is the word of the contest, see CallsignStage.contest.
	contest string

	hits map[string]int
	best callsign.Callsign

	// reportedValid holds that the stage already reported the current best callsign as valid, so
	// that one callsign gives one spot of each quality and not one spot for each hit.
	reportedValid bool
}

// NewCallsignStage takes the word of the contest, or an empty string when no contest is running.
// See CallsignStage.contest.
func NewCallsignStage[F dsp.Number](reporter runningCallsignReporter[F], contest string) *CallsignStage[F] {
	return &CallsignStage[F]{
		reporter: reporter,
		channels: make(map[core.ChannelID]*callsignDetector),
		// the decoder gives its text in lower case
		contest: strings.ToLower(strings.TrimSpace(contest)),
	}
}

func (s *CallsignStage[F]) ChannelCreated(channel core.Channel[F]) {
	s.channels[channel.ID] = &callsignDetector{hits: make(map[string]int), contest: s.contest}
}

func (s *CallsignStage[F]) ChannelDestroyed(channel core.Channel[F]) {
	delete(s.channels, channel.ID)
}

// CallsignOf gives the callsign of the running station of the given channel, or
// callsign.NoCallsign while the stage did not find one.
func (s *CallsignStage[F]) CallsignOf(id core.ChannelID) callsign.Callsign {
	detector, ok := s.channels[id]
	if !ok {
		return callsign.NoCallsign
	}
	return detector.best
}

// Process takes one character of the text of a channel. It reports the callsign of the running
// station as soon as it finds one, and again each time it finds a better one.
func (s *CallsignStage[F]) Process(channel core.Channel[F], character rune) {
	detector, ok := s.channels[channel.ID]
	if !ok {
		return
	}

	if character != ' ' && character != cw.OverMarker {
		detector.word.WriteRune(character)
		return
	}

	word := detector.word.String()
	detector.word.Reset()

	counted := word != "" && detector.countWord(word)

	// The over marker ends one transmission of one station. The words of the next transmission must
	// not join with the words of this one: a keyword of a call that ended says nothing about the
	// callsign that comes minutes later.
	if character == cw.OverMarker {
		detector.endOver()
	}

	if !counted {
		return
	}

	found, ok := detector.leader()
	if !ok {
		return
	}

	// The stage reports a new answer, and it reports the same answer again when that answer becomes
	// valid: the quality of a spot belongs to the spot, so a change of the quality is a new spot.
	// Without the second case a spot would never carry V, because the first report of a callsign
	// comes at minCallsignHits and that is below validCallsignHits.
	hits := detector.hits[found.String()]
	switch {
	case found != detector.best:
		detector.best = found
		detector.reportedValid = hits >= validCallsignHits
	case !detector.reportedValid && hits >= validCallsignHits:
		detector.reportedValid = true
	default:
		return
	}

	channel.Callsign = found
	s.reporter.emitChannelRunningCallsignDetected(channel)
}

// evidenceOf gives what the stage knows about the callsign that it reported for the given channel.
// The pipeline uses it for the quality tag of a spot, see quality.go.
func (s *CallsignStage[F]) evidenceOf(id core.ChannelID) callsignEvidence {
	detector, ok := s.channels[id]
	if !ok {
		return callsignEvidence{}
	}

	best := detector.best.String()
	result := callsignEvidence{hits: detector.hits[best]}
	for candidate, hits := range detector.hits {
		if candidate == best || hits < minCallsignHits {
			continue
		}
		if levenshtein(candidate, best) <= bustDistance {
			result.nearCompetitor = true
			break
		}
	}

	return result
}

// callsignEvidence holds what the callsign stage knows about the callsign that it reported.
type callsignEvidence struct {
	// hits is the count of the hits of that callsign. A callsign with many hits stood beside a
	// keyword many times, so the decoder read it the same way many times.
	hits int

	// nearCompetitor says that another callsign of the same channel has at least minCallsignHits
	// and stands within bustDistance of the reported one. The two are then possibly the same
	// station, and the decoder read the call in two ways, so neither of them is sure.
	nearCompetitor bool
}

// countWord takes one complete word and counts a hit when a keyword stands beside the callsign of
// the running station. It looks in both directions: forwards from the last leading keyword, and
// backwards when this word is a trailing keyword. A callsign can also stand in pieces, and the two
// searches join those pieces. It tells if it counted a hit.
func (d *callsignDetector) countWord(word string) bool {
	d.putWord(word)

	// "cq a1bc": a word before the callsign says that the callsign follows. The callsign can also
	// stand in pieces, as in "cq o m 1 u m", and the keyword is then not the word before it: the
	// search goes back over the pieces.
	if index, ok := d.lastLeadingKeyword(); ok {
		if found, ok := d.callsignAfter(index); ok {
			d.countJoin(found.String(), d.words-(len(d.recent)-index-1))
			d.countCall(d.recent[index])
			return true
		}

		// "cq a1bc a1bc": the call holds no word behind the callsign, so the trailing keyword never
		// comes. The repetition itself is the second hit.
		if found, ok := d.repeatedCallsignAfter(index); ok {
			d.hits[found.String()]++
			d.countCall(d.recent[index])
			return true
		}
	}

	// "a1bc test": this word says that the callsign is the word before it, and that callsign can
	// also stand in pieces
	if d.isTrailing(word) {
		if found, ok := d.callsignBefore(len(d.recent) - 1); ok {
			d.hits[found.String()]++
			d.countCall(word)
			return true
		}
	}

	return false
}

// isFiller tells if a word holds only the place between a keyword and the callsign. The word of the
// contest is such a word, see CallsignStage.contest.
// isTrailing tells if a word closes a call, thus if the callsign of the running station stands
// before it. The word of the contest does that, see CallsignStage.contest.
func (d *callsignDetector) isTrailing(word string) bool {
	return trailingKeywords[word] || (d.contest != "" && word == d.contest)
}

func (d *callsignDetector) isFiller(word string) bool {
	return fillerWords[word] || (d.contest != "" && word == d.contest)
}

// countCall holds that this channel saw a call, if the given keyword is one of the words that make a
// call. The word of the contest is one of them, see CallsignStage.contest.
func (d *callsignDetector) countCall(keyword string) {
	if callKeywords[keyword] || (d.contest != "" && keyword == d.contest) {
		d.sawCall = true
	}
}

// lastLeadingKeyword gives the index of the keyword that stands before the callsign, if one lies
// inside the reach of the join and if it counts.
func (d *callsignDetector) lastLeadingKeyword() (int, bool) {
	first := max(0, len(d.recent)-1-maxJoinedWords)
	for i := len(d.recent) - 2; i >= first; i-- {
		if leadingKeywords[d.recent[i]] && d.keywordCounts(i) {
			return i, true
		}
	}
	return 0, false
}

// keywordCounts tells if the leading keyword at the given index may open a call.
//
// **A keyword that also closes a call needs a clean start.** "test" stands on both sides of the
// callsign, so "a1bc test" and "test a1bc" are both calls of A1BC. In a stream of text the two
// forms meet: "cq a1bc test dl0xy dl0xy" is a call of A1BC with the answer of DL0XY behind it, and
// the "test" of that text closes the call and opens nothing. A call begins with its keyword, so an
// ordinary word before such a keyword says that the keyword closes.
//
// A measurement shows the cost of the rule that is missing: the text
// "1bc test dl0xy dl0xy dl0xy cq a1bc test dl0xy dl0xy dl0xy" gave DL0XY, because "test dl0xy" looks
// like a call when the decoder loses the first character of "a1bc".
//
// **"tu" needs a call on the same channel.** "tu a1bc test" is the way a running station finishes a
// QSO, and it is also the way a station that answers thanks and gives the frequency back. Only a
// channel that already gave a hit with "cq" or "test" holds a station that runs, so only there does
// "tu" say something.
func (d *callsignDetector) keywordCounts(index int) bool {
	if d.isTrailing(d.recent[index]) && d.ordinaryWordBefore(index) {
		return false
	}
	if d.recent[index] == "tu" && !d.sawCall {
		return false
	}
	return true
}

// ordinaryWordBefore tells if a word that is neither a keyword nor a filler word stands before the
// word at the given index.
func (d *callsignDetector) ordinaryWordBefore(index int) bool {
	before := index - 1
	for before >= 0 && d.isFiller(d.recent[before]) {
		before--
	}
	if before < 0 {
		return false
	}
	return !leadingKeywords[d.recent[before]] && !d.isTrailing(d.recent[before])
}

// minCallsignRepetitions is the count of the repetitions that repeatedCallsignAfter needs. An
// operator repeats the own callsign in a call, and one time is no repetition.
const minCallsignRepetitions = 2

// repeatedCallsignAfter gives the callsign that stands more than one time behind the keyword at the
// given index, and nothing else stands there.
//
// **This is the call without a word behind the callsign.** "cq a1bc a1bc" and "test a1bc a1bc" are
// complete calls, and the pattern of a keyword on each side of the callsign never closes for them:
// the first callsign gives a hit from "cq", and the second one gives none, because the join of the
// two words is no callsign. The repetition is the second hit, so such a call gives its spot at the
// moment of the second callsign.
//
// A station that answers repeats its callsign too, as in "dl0xy dl0xy dl0xy". That text has no
// leading keyword, so this method never sees it.
func (d *callsignDetector) repeatedCallsignAfter(index int) (callsign.Callsign, bool) {
	from := index + 1
	for from < len(d.recent) && d.isFiller(d.recent[from]) {
		from++
	}
	if len(d.recent)-from < minCallsignRepetitions {
		return callsign.NoCallsign, false
	}

	word := d.recent[from]
	for _, other := range d.recent[from+1:] {
		if other != word {
			return callsign.NoCallsign, false
		}
	}

	return parseCallsign(word)
}

// countJoin counts a hit for a callsign that comes from a join. A join that begins at the same word
// as the join before it holds that one and it is longer, so the shorter one loses its hit.
func (d *callsignDetector) countJoin(found string, start int) {
	if d.lastJoin != "" && d.lastJoinStart == start && d.lastJoin != found {
		d.hits[d.lastJoin]--
		if d.hits[d.lastJoin] <= 0 {
			delete(d.hits, d.lastJoin)
		}
	}
	d.hits[found]++
	d.lastJoin, d.lastJoinStart = found, start
}

// endOver forgets the words of the transmission that ended. The hits stay: they hold what the
// station said over the whole life of the channel, and that is the measure of the callsign.
func (d *callsignDetector) endOver() {
	d.recent = d.recent[:0]
	d.lastJoin, d.lastJoinStart = "", 0
}

func (d *callsignDetector) putWord(word string) {
	d.words++
	d.recent = append(d.recent, word)
	if len(d.recent) > maxJoinedWords+2 {
		d.recent = d.recent[1:]
	}
}

// callsignAfter gives the callsign that begins after the word at the given index and ends with the
// word that came last. It takes the longest one that the words give, and it steps over each filler
// word that stands between the keyword and the callsign.
func (d *callsignDetector) callsignAfter(index int) (callsign.Callsign, bool) {
	from := index + 1
	for from < len(d.recent) && d.isFiller(d.recent[from]) {
		from++
	}
	return d.join(from, len(d.recent))
}

// callsignBefore gives the callsign that ends before the word at the given index. It takes the
// longest one that the words give.
func (d *callsignDetector) callsignBefore(index int) (callsign.Callsign, bool) {
	for from := max(0, index-maxJoinedWords); from < index; from++ {
		if found, ok := d.join(from, index); ok {
			return found, true
		}
	}
	return callsign.NoCallsign, false
}

// join makes one word of the words from `from` to `to` and gives the callsign of it. More than one
// word must each be short, see maxJoinedWordLength.
func (d *callsignDetector) join(from int, to int) (callsign.Callsign, bool) {
	if from < 0 || to > len(d.recent) || to-from < 1 || to-from > maxJoinedWords {
		return callsign.NoCallsign, false
	}

	var joined strings.Builder
	for _, word := range d.recent[from:to] {
		if to-from > 1 && len(word) > maxJoinedWordLength {
			return callsign.NoCallsign, false
		}
		// A keyword and a filler word are no piece of a callsign. Without this rule the join takes
		// the keyword of the next call with it: "cq o m 1 u m cq" gives the callsign OM1UMCQ, which
		// stands beside the correct one and holds it back.
		if leadingKeywords[word] || trailingKeywords[word] || d.isFiller(word) {
			return callsign.NoCallsign, false
		}
		joined.WriteString(word)
	}

	return parseCallsign(joined.String())
}

// maxCallsignRepetitions is the count of the repetitions that parseCallsign takes apart. An operator
// sends the own callsign two or three times in a row, and a longer row costs nothing here.
const maxCallsignRepetitions = 4

// parseCallsign gives the callsign of one word, and it takes a callsign that stands more than one
// time in that word apart.
//
// **The decoder loses the gap between two repetitions of a callsign.** An operator sends the own
// callsign two or three times in a row, and an operator whose gap between the repetitions is no
// wider than the gap between two characters gives one word: "dl1abc dl1abc" arrives as
// "dl1abcdl1abc". The join of the pieces does the same when the decoder cut both repetitions apart,
// as in "cq d l1 abc d l1 abc".
//
// **callsign.Parse takes such a word.** Its expression asks for a prefix, a digit and characters
// after it that end with a letter, and DL1ABCDL1ABC holds all of that. The spot then carries a
// callsign that no station has, and the callsign of the station gets no hit at all: the whole word
// is one candidate of its own.
//
// Only an exact repetition counts. A decoder that loses one character of one repetition gives
// "dl1abcdl1ab", which has no period, and such a word stays as it is: a guess about which part is
// the callsign would make a callsign out of a word that holds none.
func parseCallsign(word string) (callsign.Callsign, bool) {
	found, err := callsign.Parse(word)
	if err != nil {
		return callsign.NoCallsign, false
	}

	// found.String() is the word in upper case, and a form with a prefix or a suffix holds the "/"
	// that no repetition survives
	if single, ok := withoutRepetition(found.String()); ok {
		return single, true
	}

	return found, true
}

// withoutRepetition gives the callsign that stands more than one time in the given text, if the text
// is exactly that repetition and if that part is a callsign by itself. It takes the shortest part,
// thus the most repetitions.
func withoutRepetition(text string) (callsign.Callsign, bool) {
	for count := maxCallsignRepetitions; count > 1; count-- {
		if len(text)%count != 0 {
			continue
		}

		part := text[:len(text)/count]
		if strings.Repeat(part, count) != text {
			continue
		}

		found, err := callsign.Parse(part)
		if err != nil {
			continue
		}
		return found, true
	}

	return callsign.NoCallsign, false
}

// leader gives the callsign with the most hits, if it reaches minCallsignHits. Two callsigns with
// the same count give no result: the stage then waits for the next hit instead of a report that it
// must correct immediately.
func (d *callsignDetector) leader() (callsign.Callsign, bool) {
	var best string
	var bestHits int
	var ambiguous bool
	for candidate, hits := range d.hits {
		switch {
		case hits > bestHits:
			best, bestHits, ambiguous = candidate, hits, false
		case hits == bestHits:
			ambiguous = true
		}
	}
	if bestHits < minCallsignHits || ambiguous {
		return callsign.NoCallsign, false
	}

	return parseCallsign(best)
}

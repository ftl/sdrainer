package rx

import (
	"fmt"
	"io"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/ftl/hamradio/callsign"
	"github.com/ftl/hamradio/dxcc"
	"github.com/ftl/hamradio/scp"
)

const (
	defaultTextWindowSize = 20
	spottingThreshold     = 3

	defaultWriteTimeout = 5 * time.Second
)

var (
	callsignExp = regexp.MustCompile(`\s(?:([a-z0-9]+)/)?(([a-z]|[a-z][a-z]|[0-9][a-z]|[0-9][a-z][a-z])[0-9][a-z0-9]*[a-z])(?:/([a-z0-9]+))?(?:/(p|a|m|mm|am))?`)
)

type SpotIndicator interface {
	ShowSpot(callsign string)
	HideSpot(callsign string)
}

type SpotIndicatorFunc func(callsign string)

func (f SpotIndicatorFunc) ShowSpot(callsign string) {
	f(callsign)
}

func (f SpotIndicatorFunc) HideSpot(callsign string) {}

type dxccFinder interface {
	Find(string) ([]dxcc.Prefix, bool)
}

type scpFinder interface {
	FindStrings(string) ([]string, error)
	Find(string) ([]scp.Match, error)
}

type collectedCallsign struct {
	call   callsign.Callsign
	weight int
	count  int
}

type TextProcessor struct {
	out           io.Writer
	clock         Clock
	spotIndicator SpotIndicator

	lastWrite     time.Time
	lastBestMatch callsign.Callsign

	window *textWindow

	collectedCallsigns map[string]collectedCallsign

	dxccFinder dxccFinder
	scpFinder  scpFinder
}

func NewTextProcessor(out io.Writer, clock Clock, spotIndicator SpotIndicator) *TextProcessor {
	result := &TextProcessor{
		out:                out,
		clock:              clock,
		spotIndicator:      spotIndicator,
		lastWrite:          clock.Now(),
		window:             newTextWindow(defaultTextWindowSize),
		collectedCallsigns: make(map[string]collectedCallsign),
	}

	result.dxccFinder = setupDXCCFinder()
	result.scpFinder = setupSCPFinder()

	return result
}

func setupDXCCFinder() *dxcc.Prefixes {
	localFilename, err := dxcc.LocalFilename()
	if err != nil {
		log.Print(err)
		return nil
	}
	updated, err := dxcc.Update(dxcc.DefaultURL, localFilename)
	if err != nil {
		log.Printf("update of local copy of DXCC prefixes failed: %v", err)
	}
	if updated {
		log.Printf("updated local copy of DXCC prefixes: %v", localFilename)
	}

	result, err := dxcc.LoadLocal(localFilename)
	if err != nil {
		log.Printf("cannot load DXCC prefixes: %v", err)
		return nil
	}
	return result
}

func setupSCPFinder() *scp.Database {
	localFilename, err := scp.LocalFilename()
	if err != nil {
		log.Print(err)
		return nil
	}
	updated, err := scp.Update(scp.DefaultURL, localFilename)
	if err != nil {
		log.Printf("update of local copy of Supercheck database failed: %v", err)
	}
	if updated {
		log.Printf("updated local copy of Supercheck database: %v", localFilename)
	}

	result, err := scp.LoadLocal(localFilename)
	if err != nil {
		log.Printf("cannot load Supercheck database: %v", err)
		return nil
	}
	return result
}

func (p *TextProcessor) Reset() {
	p.lastWrite = p.clock.Now()
	p.window.Reset()
	clear(p.collectedCallsigns)
}

func (p *TextProcessor) LastWrite() time.Time {
	return p.lastWrite
}

func (p *TextProcessor) CheckWriteTimeout() {
	now := p.clock.Now()
	if now.Sub(p.lastWrite) > defaultWriteTimeout {
		p.WriteTimeout()
	}
}

func (p *TextProcessor) Write(bytes []byte) (int, error) {
	p.lastWrite = p.clock.Now()

	bytesForWindow := bytes
	for len(bytesForWindow) > 0 {
		n, err := p.window.Write(bytesForWindow)
		if err != nil {
			panic(err)
		}

		candidate, found := p.window.FindNext(callsignExp, false)
		if found {
			p.collectCallsign(candidate)
		}

		if n <= len(bytesForWindow) {
			bytesForWindow = bytesForWindow[n:]
		}
		if p.window.IsFull() {
			p.window.Shift()
		}
	}

	if p.out != nil {
		return p.out.Write(bytes)
	}
	return len(bytes), nil
}

func (p *TextProcessor) WriteTimeout() {
	candidate, found := p.window.FindNext(callsignExp, true)
	if found {
		p.collectCallsign(candidate)
	}
}

func (p *TextProcessor) collectCallsign(candidate string) {
	candidate = strings.ToLower(strings.TrimSpace(candidate))
	if isFalsePositive(candidate) {
		return
	}

	call, err := callsign.Parse(candidate)
	if err != nil {
		return
	}
	if !p.isValidDXCC(call) {
		return
	}

	collected, found := p.collectedCallsigns[call.String()]
	if !found {
		collected = collectedCallsign{
			call:   call,
			weight: p.callsignWeight(call),
		}
	}
	collected.count++
	p.collectedCallsigns[collected.call.String()] = collected

	bestMatch := p.BestMatch()
	if bestMatch == callsign.NoCallsign {
		return
	}

	if bestMatch != p.lastBestMatch {
		p.spotIndicator.HideSpot(p.lastBestMatch.String())
	}
	p.spotIndicator.ShowSpot(bestMatch.String())
	p.lastBestMatch = bestMatch
}

func isFalsePositive(candidate string) bool {
	var falsePositives = []string{
		"tu5nn",
	}

	for _, falsePrefix := range falsePositives {
		if strings.HasPrefix(candidate, falsePrefix) {
			return true
		}
	}
	return false
}

func (p *TextProcessor) isValidDXCC(call callsign.Callsign) bool {
	if p.dxccFinder == nil {
		return true
	}
	_, found := p.dxccFinder.Find(call.String())
	return found
}

func (p *TextProcessor) BestMatch() callsign.Callsign {
	var bestMatch callsign.Callsign
	maxCount := spottingThreshold - 1

	for _, collected := range p.collectedCallsigns {
		weightedCount := collected.count + collected.weight
		if maxCount < weightedCount {
			maxCount = weightedCount
			bestMatch = collected.call
		}
	}

	return bestMatch
}

func (p *TextProcessor) callsignWeight(call callsign.Callsign) int {
	result := 0
	if p.isSCPCallsign(call) {
		result++
	}
	return result
}

func (p *TextProcessor) isSCPCallsign(call callsign.Callsign) bool {
	if p.scpFinder == nil {
		return false
	}
	matches, err := p.scpFinder.FindStrings(call.String())
	return err == nil && len(matches) == 1
}

type textWindow struct {
	window        [2][]byte
	windowSize    int
	currentWindow int
	searchPoint   int
}

func newTextWindow(windowSize int) *textWindow {
	result := &textWindow{
		windowSize: windowSize,
	}

	for i := range result.window {
		result.window[i] = make([]byte, 0, result.windowSize)
	}

	return result
}

func (w *textWindow) String() string {
	return string(w.window[w.currentWindow])
}

func (w *textWindow) Reset() {
	for i := range w.window {
		w.window[i] = w.window[i][:0]
		w.currentWindow = 0
	}
}

func (w *textWindow) Write(bytes []byte) (n int, err error) {
	appendLen := min(len(bytes), w.windowSize-len(w.window[w.currentWindow]))
	if len(bytes) > 0 && appendLen == 0 {
		return 0, fmt.Errorf("text window is full, use Shift() before writing again")
	}

	w.window[w.currentWindow] = append(w.window[w.currentWindow], bytes[:appendLen]...)
	return appendLen, nil
}

func (w *textWindow) Shift() {
	otherWindow := (w.currentWindow + 1) % 2
	halfSize := w.windowSize / 2
	w.window[otherWindow] = w.window[otherWindow][:0]

	startIndex := max(0, len(w.window[w.currentWindow])-halfSize)
	appendLen := min(halfSize, len(w.window[w.currentWindow])-startIndex)
	if appendLen > 0 {
		w.window[otherWindow] = append(w.window[otherWindow], w.window[w.currentWindow][startIndex:startIndex+appendLen]...)
	}
	w.currentWindow = otherWindow
	w.searchPoint = max(0, w.searchPoint-startIndex)
}

func (w *textWindow) IsFull() bool {
	return len(w.window[w.currentWindow]) == w.windowSize
}

func (w *textWindow) FindNext(exp *regexp.Regexp, includeTail bool) (string, bool) {
	if w.searchPoint >= len(w.window[w.currentWindow]) {
		return "", false
	}

	searchText := w.window[w.currentWindow][w.searchPoint:]
	match := exp.FindIndex(searchText)
	if match == nil {
		return "", false
	}
	if !includeTail && match[1] >= len(searchText) {
		return "", false
	}

	w.searchPoint = w.searchPoint + match[1]

	return string(searchText[match[0]:match[1]]), true
}

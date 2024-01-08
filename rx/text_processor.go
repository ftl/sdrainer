package rx

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	defaultTextWindowSize = 20
	spottingThreshold     = 2
)

var (
	callsignExp = regexp.MustCompile(`\s(?:([a-z0-9]+)/)?(([a-z]|[a-z][a-z]|[0-9][a-z]|[0-9][a-z][a-z])[0-9][a-z0-9]*[a-z])(?:/([a-z0-9]+))?(?:/(p|a|m|mm|am))?\s`)
)

type SpotIndicator interface {
	ShowSpot(callsign string)
}

type TextProcessor struct {
	clock         Clock
	spotIndicator SpotIndicator

	lastWrite time.Time

	window *textWindow

	collectedCallsigns map[string]int
}

func NewTextProcessor(clock Clock, spotIndicator SpotIndicator) *TextProcessor {
	result := &TextProcessor{
		clock:              clock,
		spotIndicator:      spotIndicator,
		lastWrite:          clock.Now(),
		window:             newTextWindow(defaultTextWindowSize),
		collectedCallsigns: make(map[string]int),
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
			p.window.Shift()
		}
	}

	return os.Stdout.Write(bytes)
}

func (p *TextProcessor) collectCallsign(candidate string) {
	candidate = strings.ToLower(strings.TrimSpace(candidate))
	count := p.collectedCallsigns[candidate]
	// TODO check the DXCC entity and MASTER.SCP if this is a valid match
	count++
	p.collectedCallsigns[candidate] = count

	bestMatch := p.BestMatch()
	if bestMatch != "" {
		p.spotIndicator.ShowSpot(bestMatch)
	} else {
		log.Printf("\ncollected callsigns: %v\n", p.collectedCallsigns)
	}
}

func (p *TextProcessor) BestMatch() string {
	bestMatch := ""
	maxCount := spottingThreshold - 1

	for callsign, count := range p.collectedCallsigns {
		if maxCount < count {
			maxCount = count
			bestMatch = callsign
		}
	}

	return bestMatch
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

func (w *textWindow) FindNext(exp *regexp.Regexp, includeTail bool) (string, bool) {
	text := w.window[w.currentWindow]
	if w.searchPoint >= len(text) {
		return "", false
	}

	match := exp.FindIndex(text[w.searchPoint:])
	if match == nil {
		return "", false
	}
	if !includeTail && match[1] >= len(text) {
		return "", false
	}

	w.searchPoint = match[1]

	return string(text[match[0]:match[1]]), true
}

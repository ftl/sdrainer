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

type CallsignReporter interface {
	CallsignDecoded(callsign string, count int, weight int)
	CallsignSpotted(callsign string)
	SpotTimeout(callsign string)
}

type SpotReporterFunc func(callsign string)

func (f SpotReporterFunc) CallsignSpotted(callsign string) {
	f(callsign)
}

func (f SpotReporterFunc) CallsignDecoded(callsign string, count int, weight int) {}
func (f SpotReporterFunc) SpotTimeout(callsign string)                            {}

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
	out      io.Writer
	clock    Clock
	reporter CallsignReporter

	lastWrite     time.Time
	lastBestMatch callsign.Callsign

	op      chan func()
	stop    chan struct{}
	stopped chan struct{}
	window  *textWindow

	collectedCallsigns map[string]collectedCallsign

	dxccFinder dxccFinder
	scpFinder  scpFinder
}

func NewTextProcessor(out io.Writer, clock Clock, reporter CallsignReporter) *TextProcessor {
	result := &TextProcessor{
		out:       out,
		clock:     clock,
		reporter:  reporter,
		lastWrite: clock.Now(),

		op:                 make(chan func(), 10),
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

func (p *TextProcessor) Start() {
	if p.stop != nil {
		return
	}
	p.stop = make(chan struct{})
	p.stopped = make(chan struct{})
	go p.run()
}

func (p *TextProcessor) Stop() {
	if p.stop == nil {
		return
	}
	select {
	case <-p.stop:
	default:
		close(p.stop)
		<-p.stopped
		p.stop = nil
		p.stopped = nil
	}
}

func (p *TextProcessor) run() {
	defer close(p.stopped)
	for {
		select {
		case <-p.stop:
			return
		case f := <-p.op:
			f()
		}
	}
}

func (p *TextProcessor) sync() {
	executed := make(chan struct{})
	p.op <- func() {
		close(executed)
	}
	<-executed
}

func (p *TextProcessor) Restart() {
	p.Stop()
	p.lastWrite = p.clock.Now()
	p.lastBestMatch = callsign.NoCallsign
	p.window.Reset()
	clear(p.collectedCallsigns)
	p.Start()
}

func (p *TextProcessor) LastWrite() time.Time {
	return p.lastWrite
}

func (p *TextProcessor) CheckWriteTimeout() {
	now := p.clock.Now()
	if now.Sub(p.lastWrite) > defaultWriteTimeout {
		p.op <- p.writeTimeout
	}
}

func (p *TextProcessor) writeTimeout() {
	candidate, found := p.window.FindNext(callsignExp, true)
	if found {
		p.collectCallsign(candidate)
	}
}

func (p *TextProcessor) Write(bytes []byte) (int, error) {
	p.lastWrite = p.clock.Now()

	bytesForProcessing := make([]byte, len(bytes))
	copy(bytesForProcessing, bytes)
	p.op <- func() {
		p.findNextCallsign(bytesForProcessing)
	}

	if p.out != nil {
		return p.out.Write(bytes)
	}
	return len(bytes), nil
}

func (p *TextProcessor) findNextCallsign(bytes []byte) {
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
	p.reporter.CallsignDecoded(collected.call.String(), collected.count, collected.weight)

	bestMatch := p.bestMatch()
	if bestMatch == callsign.NoCallsign {
		return
	}

	if bestMatch != p.lastBestMatch && p.lastBestMatch != callsign.NoCallsign {
		p.reporter.SpotTimeout(p.lastBestMatch.String())
	}
	p.reporter.CallsignSpotted(bestMatch.String())
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

func (p *TextProcessor) bestMatch() callsign.Callsign {
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
	if err != nil {
		return false
	}
	if len(matches) == 0 {
		return false
	}
	return call.String() == matches[0]
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

package rx

import (
	"fmt"
	"os"
	"time"
)

const (
	defaultTextWindowSize = 20
)

type TextProcessor struct {
	clock     Clock
	lastWrite time.Time

	window *textWindow
}

func NewTextProcessor(clock Clock) *TextProcessor {
	result := &TextProcessor{
		clock:     clock,
		lastWrite: clock.Now(),
		window:    newTextWindow(defaultTextWindowSize),
	}

	return result
}

func (p *TextProcessor) Reset() {
	p.lastWrite = p.clock.Now()
	p.window.Reset()
}

func (p *TextProcessor) LastWrite() time.Time {
	return p.lastWrite
}

func (p *TextProcessor) Write(bytes []byte) (n int, err error) {
	p.lastWrite = p.clock.Now()
	return os.Stdout.Write(bytes)
}

type textWindow struct {
	window        [2][]byte
	windowSize    int
	currentWindow int
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
}

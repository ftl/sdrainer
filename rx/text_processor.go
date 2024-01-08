package rx

import (
	"os"
	"time"
)

type TextProcessor struct {
	clock Clock

	lastWrite time.Time
}

func NewTextProcessor(clock Clock) *TextProcessor {
	return &TextProcessor{
		clock:     clock,
		lastWrite: clock.Now(),
	}
}

func (p *TextProcessor) Reset() {
	p.lastWrite = p.clock.Now()
}

func (p *TextProcessor) LastWrite() time.Time {
	return p.lastWrite
}

func (p *TextProcessor) Write(bytes []byte) (n int, err error) {
	p.lastWrite = p.clock.Now()
	return os.Stdout.Write(bytes)
}

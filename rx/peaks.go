package rx

import (
	"math/rand"
	"time"

	"github.com/ftl/sdrainer/dsp"
)

const (
	defaultPeakTimeout = 5 * time.Minute
)

type peak[T, F dsp.Number] struct {
	*dsp.Peak[T, F]

	state peakState
	since time.Time
}

type peakState int

const (
	peakNone peakState = iota
	peakNew
	peakActive
	peakInactive
)

type PeaksTable[T, F dsp.Number] struct {
	bins        []*peak[T, F]
	clock       Clock
	peakTimeout time.Duration
}

func NewPeaksTable[T, F dsp.Number](size int, clock Clock) *PeaksTable[T, F] {
	result := &PeaksTable[T, F]{
		bins:        make([]*peak[T, F], size),
		clock:       clock,
		peakTimeout: defaultPeakTimeout,
	}

	return result
}

func (t *PeaksTable[T, F]) Put(p *dsp.Peak[T, F]) {
	clearFrom := -1
	clearTo := -1

	for i := max(0, p.From); i <= min(p.To, len(t.bins)-1); i++ {
		existingPeak := t.bins[i]
		if existingPeak == nil {
			continue
		}
		if existingPeak.state == peakActive || existingPeak.state == peakInactive {
			return
		}
		if clearFrom == -1 {
			clearFrom = existingPeak.From
		}
		clearTo = existingPeak.To
	}

	if clearFrom > -1 && clearTo > -1 {
		t.clear(clearFrom, clearTo)
	}

	internalPeak := &peak[T, F]{
		Peak:  p,
		state: peakNew,
		since: t.clock.Now(),
	}
	t.put(internalPeak)
}

func (t *PeaksTable[T, F]) put(p *peak[T, F]) {
	for i := max(0, p.From); i <= min(p.To, len(t.bins)-1); i++ {
		t.bins[i] = p
	}
}

func (t *PeaksTable[T, F]) clear(from int, to int) {
	for i := max(0, from); i <= min(to, len(t.bins)-1); i++ {
		t.bins[i] = nil
	}
}

func (t *PeaksTable[T, F]) Get(binIndex int) *dsp.Peak[T, F] {
	if binIndex < 0 || binIndex >= len(t.bins) {
		return nil
	}

	p := t.bins[binIndex]
	if p == nil {
		return nil
	}
	return p.Peak
}

func (t *PeaksTable[T, F]) Cleanup() {
	now := t.clock.Now()
	i := 0
	for i < len(t.bins) {
		p := t.bins[i]
		i++

		if p == nil {
			continue
		}
		if p.state == peakActive {
			continue
		}
		if now.Sub(p.since) < t.peakTimeout {
			continue
		}

		t.clear(p.From, p.To)
		i = p.To + 1
	}
}

func (t *PeaksTable[T, F]) Reset() {
	clear(t.bins)
}

func (t *PeaksTable[T, F]) Activate(p *dsp.Peak[T, F]) {
	internalPeak := t.getInternal(p)
	if internalPeak.state != peakNew && internalPeak.state != peakInactive {
		return
	}

	internalPeak.state = peakActive
}

func (t *PeaksTable[T, F]) getInternal(p *dsp.Peak[T, F]) *peak[T, F] {
	internalPeak := t.bins[p.From]
	if internalPeak == nil {
		return nil
	}
	if internalPeak.To != p.To {
		return nil
	}

	return internalPeak
}

func (t *PeaksTable[T, F]) Deactivate(p *dsp.Peak[T, F]) {
	internalPeak := t.getInternal(p)
	if internalPeak.state != peakActive {
		return
	}

	internalPeak.state = peakInactive
}

func (t *PeaksTable[T, F]) FindNext() *dsp.Peak[T, F] {
	for i := 0; i < len(t.bins)/2; i++ {
		i := rand.Intn(len(t.bins))
		p := t.bins[i]
		if p == nil {
			continue
		}
		if p.state != peakNew {
			continue
		}
		return p.Peak
	}

	for _, p := range t.bins {
		if p == nil {
			continue
		}
		if p.state != peakNew {
			continue
		}
		return p.Peak
	}

	return nil
}

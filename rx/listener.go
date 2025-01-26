package rx

import (
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/ftl/sdrainer/cw"
	"github.com/ftl/sdrainer/dsp"
	"github.com/ftl/sdrainer/scope"
)

const (
	defaultSilenceTimeout    = 20 * time.Second
	defaultAttachmentTimeout = 2 * time.Minute
)

type Listener[T, F dsp.Number] struct {
	id       string
	clock    Clock
	reporter Reporter[F]

	demodulator   *cw.SpectralDemodulator[T, F]
	textProcessor *TextProcessor

	peak       *dsp.Peak[T, F]
	lastAttach time.Time

	silenceTimeout    time.Duration
	attachmentTimeout time.Duration
}

func NewListener[T, F dsp.Number](id string, out io.Writer, clock Clock, reporter Reporter[F], sampleRate int, blockSize int) *Listener[T, F] {
	result := &Listener[T, F]{
		id:       id,
		clock:    clock,
		reporter: reporter,

		silenceTimeout:    defaultSilenceTimeout,
		attachmentTimeout: defaultAttachmentTimeout,
	}

	result.textProcessor = NewTextProcessor(out, clock, result)
	result.demodulator = cw.NewSpectralDemodulator[T, F](result.textProcessor, sampleRate, blockSize)

	return result
}

func (l *Listener[T, F]) ID() string {
	return l.id
}

func (l *Listener[T, F]) SetScope(scope scope.Scope) {
	l.demodulator.SetScope(scope)
}

func (l *Listener[T, F]) SetSilenceTimeout(timeout time.Duration) {
	l.silenceTimeout = timeout
}

func (l *Listener[T, F]) SetAttachmentTimeout(timeout time.Duration) {
	l.attachmentTimeout = timeout
}

func (l *Listener[T, F]) SetSignalDebounce(debounce int) {
	l.demodulator.SetSignalDebounce(debounce)
}

func (l *Listener[T, F]) CallsignDecoded(callsign string, count int, weight int) {
	l.reporter.CallsignDecoded(l.id, callsign, l.peak.SignalFrequency, count, weight)
}

func (l *Listener[T, F]) CallsignSpotted(callsign string) {
	callsign = strings.ToUpper(callsign)
	l.reporter.CallsignSpotted(l.id, callsign, l.peak.SignalFrequency)
}

func (l *Listener[T, F]) SpotTimeout(callsign string) {
	callsign = strings.ToUpper(callsign)
	l.reporter.SpotTimeout(l.id, callsign, l.peak.SignalFrequency)
}

func (l *Listener[T, F]) Attach(peak *dsp.Peak[T, F]) {
	l.peak = peak
	l.lastAttach = l.clock.Now()

	l.demodulator.Reset()
	l.textProcessor.Restart()

	l.reporter.ListenerActivated(l.id, l.peak.SignalFrequency)
	// log.Printf("\ndemodulating at %v (%d - %d)\n", peak.CenterFrequency(), peak.From, peak.To)
}

func (l *Listener[T, F]) Attached() bool {
	return l.peak != nil
}

func (l *Listener[T, F]) Detach() {
	frequency := l.peak.SignalFrequency
	l.peak = nil

	l.textProcessor.Stop()
	l.reporter.ListenerDeactivated(l.id, frequency)
	// log.Printf("\ndemodulation stopped\n")
}

func (l *Listener[T, F]) Peak() *dsp.Peak[T, F] {
	return l.peak
}

func (l *Listener[T, F]) PeakRange() (int, int) {
	if !l.Attached() {
		return 0, 0
	}
	return l.peak.From, l.peak.To
}

func (l *Listener[T, F]) SignalBin() int {
	if !l.Attached() {
		return 0
	}
	return l.peak.SignalBin
}

func (l *Listener[T, F]) TimeoutExceeded() bool {
	now := l.clock.Now()
	attachmentExceeded := now.Sub(l.lastAttach) > l.attachmentTimeout
	silenceExceeded := now.Sub(l.textProcessor.LastWrite()) > l.silenceTimeout
	// if attachmentExceeded || silenceExceeded {
	// 	log.Printf("timeout a: %v %t s: %v %t", l.attachmentTimeout, attachmentExceeded, l.silenceTimeout, silenceExceeded)
	// } else {
	// 	log.Printf("waiting for timeout a: %v %v s: %v %v", now.Sub(l.lastAttach), l.attachmentTimeout, now.Sub(l.textProcessor.LastWrite()), l.silenceTimeout)
	// }
	return attachmentExceeded || silenceExceeded
}

func (l *Listener[T, F]) CheckWriteTimeout() {
	l.textProcessor.CheckWriteTimeout()
}

func (l *Listener[T, F]) Listen(value T, noiseFloor T) {
	if !l.Attached() {
		return
	}

	l.demodulator.Tick(value, noiseFloor)
}

type IDPool []string

func NewIDPool(size int, prefix string) IDPool {
	result := make(IDPool, size)

	for i := range result {
		result[i] = prefix + strconv.Itoa(size-i)
	}

	return result
}

func (p *IDPool) Push(id string) {
	*p = append(*p, id)
}

func (p *IDPool) Pop() (string, bool) {
	if len(*p) == 0 {
		return "", false
	}

	slice := *p
	result := slice[len(slice)-1]
	*p = slice[:len(slice)-1]

	return result, true
}

type ListenerFactory[T, F dsp.Number] func(id string) *Listener[T, F]

type ListenerPool[T, F dsp.Number] struct {
	listeners []*Listener[T, F]
	size      int
	ids       IDPool
	factory   ListenerFactory[T, F]
}

func NewListenerPool[T, F dsp.Number](size int, idPrefix string, factory ListenerFactory[T, F]) *ListenerPool[T, F] {
	result := &ListenerPool[T, F]{
		size:      size,
		listeners: make([]*Listener[T, F], 0, size),
		ids:       NewIDPool(size, idPrefix),
		factory:   factory,
	}

	return result
}

func (p *ListenerPool[T, F]) Size() int {
	return p.size
}

func (p *ListenerPool[T, F]) Available() bool {
	return len(p.listeners) < p.size
}

func (p *ListenerPool[T, F]) Reset() {
	for _, l := range p.listeners {
		l.Detach()
		p.ids.Push(l.ID())
	}
	p.listeners = p.listeners[:0]
}

func (p *ListenerPool[T, F]) BindNext() (*Listener[T, F], bool) {
	if len(p.listeners) == p.size {
		return nil, false
	}

	id, ok := p.ids.Pop()
	if !ok {
		return nil, false
	}

	listener := p.factory(id)
	p.listeners = append(p.listeners, listener)

	return listener, true
}

func (p *ListenerPool[T, F]) Release(listeners ...*Listener[T, F]) {
	for _, listener := range listeners {
		p.release(listener)
	}
}

func (p *ListenerPool[T, F]) release(listener *Listener[T, F]) {
	index := p.indexOf(listener)
	if index == -1 {
		return
	}

	p.ids.Push(listener.ID())

	if len(p.listeners) > 1 {
		p.listeners[index] = p.listeners[len(p.listeners)-1]
	}
	p.listeners = p.listeners[:len(p.listeners)-1]
}

func (p *ListenerPool[T, F]) indexOf(listener *Listener[T, F]) int {
	for i, l := range p.listeners {
		if l.id == listener.ID() {
			return i
		}
	}
	return -1
}

func (p *ListenerPool[T, F]) ForEach(f func(listener *Listener[T, F])) {
	for _, l := range p.listeners {
		f(l)
	}
}

func (p *ListenerPool[T, F]) First() (*Listener[T, F], bool) {
	if len(p.listeners) == 0 {
		return nil, false
	}
	return p.listeners[0], true
}

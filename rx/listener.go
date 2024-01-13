package rx

import (
	"io"
	"log"
	"strings"
	"time"

	"github.com/ftl/sdrainer/cw"
	"github.com/ftl/sdrainer/dsp"
	"github.com/ftl/sdrainer/trace"
)

const (
	defaultSilenceTimeout    = 20 * time.Second
	defaultAttachmentTimeout = 2 * time.Minute
)

type ListenerIndicator[T, F dsp.Number] interface {
	ShowDecode(receiver string, peak dsp.Peak[T, F])
	HideDecode(receiver string)
	ShowSpot(receiver string, callsign string, frequency F)
}

type Listener[T, F dsp.Number] struct {
	id        string
	clock     Clock
	indicator ListenerIndicator[T, F]

	demodulator   *cw.SpectralDemodulator[T, F]
	textProcessor *TextProcessor
	lastAttach    time.Time

	silenceTimeout    time.Duration
	attachmentTimeout time.Duration
}

func NewListener[T, F dsp.Number](id string, out io.Writer, clock Clock, indicator ListenerIndicator[T, F], sampleRate int, blockSize int) *Listener[T, F] {
	result := &Listener[T, F]{
		id:        id,
		clock:     clock,
		indicator: indicator,

		silenceTimeout:    defaultSilenceTimeout,
		attachmentTimeout: defaultAttachmentTimeout,
	}

	result.textProcessor = NewTextProcessor(out, clock, result)
	result.demodulator = cw.NewSpectralDemodulator[T, F](result.textProcessor, sampleRate, blockSize)

	return result
}

func (l *Listener[T, F]) SetTracer(tracer trace.Tracer) {
	l.demodulator.SetTracer(tracer)
}

func (l *Listener[T, F]) SetSilenceTimeout(timeout time.Duration) {
	l.silenceTimeout = timeout
}

func (l *Listener[T, F]) SetAttachmentTimeout(timeout time.Duration) {
	l.attachmentTimeout = timeout
}

func (l *Listener[T, F]) SetSignalThreshold(threshold T) {
	l.demodulator.SetSignalThreshold(threshold)
}

func (l *Listener[T, F]) SetSignalDebounce(debounce int) {
	l.demodulator.SetSignalDebounce(debounce)
}

func (l *Listener[T, F]) ShowSpot(callsign string) {
	callsign = strings.ToUpper(callsign)
	l.indicator.ShowSpot(l.id, callsign, l.demodulator.Peak().SignalFrequency)
}

func (l *Listener[T, F]) Attach(peak *dsp.Peak[T, F]) {
	l.demodulator.Attach(peak)
	l.lastAttach = l.clock.Now()
	l.textProcessor.Reset()
	l.indicator.ShowDecode(l.id, *peak)
}

func (l *Listener[T, F]) Attached() bool {
	return l.demodulator.Attached()
}

func (l *Listener[T, F]) Detach() {
	l.demodulator.Detach()
	l.indicator.HideDecode(l.id)
}

func (l *Listener[T, F]) PeakRange() (int, int) {
	return l.demodulator.PeakRange()
}

func (l *Listener[T, F]) Peak() *dsp.Peak[T, F] {
	return l.demodulator.Peak()
}

func (l *Listener[T, F]) TimeoutExceeded() bool {
	now := l.clock.Now()
	attachmentExceeded := now.Sub(l.lastAttach) > l.attachmentTimeout
	silenceExceeded := now.Sub(l.textProcessor.LastWrite()) > l.silenceTimeout
	if attachmentExceeded || silenceExceeded {
		log.Printf("timeout a: %v %t s: %v %t", l.attachmentTimeout, attachmentExceeded, l.silenceTimeout, silenceExceeded)
	}
	return attachmentExceeded || silenceExceeded
}

func (l *Listener[T, F]) CheckWriteTimeout() {
	l.textProcessor.CheckWriteTimeout()
}

func (l *Listener[T, F]) Listen(value T, noiseFloor T) {
	l.demodulator.Tick(value, noiseFloor)
}

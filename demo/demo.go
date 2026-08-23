package demo

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/dsp"
	"github.com/ftl/sdrainer/iq"
	"github.com/ftl/sdrainer/pipeline"
	"github.com/ftl/sdrainer/pipeline/generator"
)

const (
	demoChunkSize      = 2048 // section "Assumed input", thus 42.7 ms for each chunk
	demoReportInterval = 2 * time.Second
	maxTextLineLength  = 40 // characters, a line of the console must stay readable

	demoSampleRate = 48000 // section "Assumed input"
)

func DefaultSignals(centerFrequency float64) []generator.CWSignal[float64] {
	return []generator.CWSignal[float64]{
		{Frequency: centerFrequency, WPM: 15, Amplitude: 1.0, Text: "cq cq de dl1abc dl1abc k", RiseTime: 5 * time.Millisecond},
		{Frequency: centerFrequency + 3500, WPM: 20, Amplitude: 0.5, Text: "cq test de ok1xyz", FadeDepth: 0.5, FadeRate: 0.2, RiseTime: 5 * time.Millisecond},
		{Frequency: centerFrequency + 8000, WPM: 25, Amplitude: 0.25, Text: "cq de g4abc g4abc k", RiseTime: 5 * time.Millisecond},
		{Frequency: centerFrequency + 12000, WPM: 30, Amplitude: 0.1, Text: "test de w1aw", Drift: 2, RiseTime: 5 * time.Millisecond},
		{Frequency: centerFrequency + 18000, WPM: 35, Amplitude: 0.05, Text: "qrl? de ve3xyz", RiseTime: 5 * time.Millisecond},
		{Frequency: centerFrequency + 15000, Amplitude: 0.5, Carrier: true},
	}
}

type (
	ChannelService = core.ChannelService[float64]
	Spotter        = core.Spotter[float64]
)

type Process struct {
	pipeline *pipeline.Pipeline[float32, float64]

	sampleRate int
	chunkSize  int

	source *generator.Generator[float32, float64]
	stop   chan struct{}
}

func New(centerFrequency float64, noise float64, signals []generator.CWSignal[float64], scope core.ScopeService, channelService ChannelService, spotter Spotter, recorder *iq.Writer, debug bool) (*Process, error) {
	result := &Process{
		sampleRate: demoSampleRate,
		chunkSize:  demoChunkSize,
		stop:       make(chan struct{}),
	}

	config := pipeline.DefaultConfig(result.sampleRate, centerFrequency)
	pipelineScope := scope
	if debug {
		pipelineScope = newPeakReporter(scope, demoReportInterval)
	}
	result.pipeline = pipeline.New[float32, float64](config, pipelineScope)
	if recorder != nil {
		// a nil pointer in the interface would not be nil, so the pipeline gets the recorder only
		// when it exists
		result.pipeline.SetRecorder(recorder)
	}
	result.pipeline.SetSpotter(spotter)
	result.pipeline.Notify(channelService)
	if debug {
		result.pipeline.Notify(newChannelReporter[float64](result.sampleRate, maxTextLineLength))
	}

	result.source = generator.New[float32, float64](generator.GeneratorConfig[float64]{
		SampleRate:      result.sampleRate,
		CenterFrequency: centerFrequency,
		NoiseLevel:      noise,
		Seed:            1,
		Signals:         signals,
	})

	go result.run()

	return result, nil
}

func (p *Process) run() {
	ticker := time.NewTicker(time.Duration(p.chunkSize) * time.Second / time.Duration(p.sampleRate))
	defer ticker.Stop()

	p.pipeline.Start()
	defer p.pipeline.Stop()

	chunk := make([]float32, 2*p.chunkSize)
	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			p.source.Read(chunk)
			p.pipeline.IQData(p.sampleRate, chunk)
		}
	}
}

func (p *Process) Stop() {
	select {
	case <-p.stop:
		return
	default:
		close(p.stop)
	}
}

type peakReporter struct {
	scope    core.ScopeService
	interval time.Duration
	last     time.Time
}

func newPeakReporter(scope core.ScopeService, interval time.Duration) *peakReporter {
	if scope == nil {
		scope = &core.NullScopeService{}
	}
	return &peakReporter{scope: scope, interval: interval}
}

func (r *peakReporter) Active() bool { return true }

func (r *peakReporter) SendTimeFrame(frame *core.TimeFrame) {
	if r.scope.Active() {
		r.scope.SendTimeFrame(frame)
	}
}

func (r *peakReporter) SendSpectralFrame(frame *core.SpectralFrame) {
	if r.scope.Active() {
		r.scope.SendSpectralFrame(frame)
	}
	if frame.Stream != pipeline.ScopeSpectrum {
		return
	}

	now := time.Now()
	if now.Sub(r.last) < r.interval {
		return
	}
	r.last = now

	frequencies := make([]float64, 0, len(frame.FrequencyMarkers))
	for _, frequency := range frame.FrequencyMarkers {
		frequencies = append(frequencies, frequency)
	}
	slices.Sort(frequencies)

	fmt.Printf("%s  %d peaks:", now.Format("15:04:05"), len(frequencies))
	for _, frequency := range frequencies {
		fmt.Printf(" %.2f", frequency/1000)
	}
	fmt.Println(" kHz")
}

var (
	_ core.ChannelLifecycleListener[float64] = (*channelReporter[float64])(nil)
	_ core.ChannelStateListener[float64]     = (*channelReporter[float64])(nil)
	_ core.ChannelReceiveListener[float64]   = (*channelReporter[float64])(nil)
)

type channelText struct {
	runes []rune
	start int64 // the sample offset of the first character of the line
}

type channelReporter[F dsp.Number] struct {
	sampleRate    int
	maxLineLength int
	text          map[core.ChannelID]*channelText
}

func newChannelReporter[F dsp.Number](sampleRate int, maxLineLength int) *channelReporter[F] {
	return &channelReporter[F]{
		sampleRate:    sampleRate,
		maxLineLength: maxLineLength,
		text:          make(map[core.ChannelID]*channelText),
	}
}

func (r *channelReporter[F]) ChannelCharacterReceived(channel core.Channel[F], character rune, offset int64) {
	line, ok := r.text[channel.ID]
	if !ok {
		line = &channelText{start: offset}
		r.text[channel.ID] = line
	}
	line.runes = append(line.runes, character)

	if character == ' ' || len(line.runes) >= r.maxLineLength {
		r.flush(channel)
	}
}

// flush writes the collected characters of a channel and forgets them. The time comes from the
// sample offset of the first character, so it is the time in the stream and not the time of the
// clock: the decoder gives a character at the end of the character, and the console would show it
// too late.
func (r *channelReporter[F]) flush(channel core.Channel[F]) {
	line, ok := r.text[channel.ID]
	if !ok {
		return
	}
	delete(r.text, channel.ID)

	text := strings.TrimSpace(string(line.runes))
	if text == "" {
		return
	}

	fmt.Printf("%s  channel %s  %.2f kHz  %2d wpm  t=%6.2f s  %s\n",
		time.Now().Format("15:04:05"), channel.ID, float64(channel.Frequency)/1000.0, channel.WPM,
		float64(line.start)/float64(r.sampleRate), text)
}

func (r *channelReporter[F]) ChannelCreated(channel core.Channel[F]) {
	fmt.Printf("%s  channel %s created  %.2f kHz  %.1f dB\n",
		time.Now().Format("15:04:05"), channel.ID, float64(channel.Frequency)/1000.0, channel.SNR)
}

func (r *channelReporter[F]) ChannelDestroyed(channel core.Channel[F]) {
	r.flush(channel)
	fmt.Printf("%s  channel %s destroyed  %.2f kHz\n",
		time.Now().Format("15:04:05"), channel.ID, float64(channel.Frequency)/1000.0)
}

func (r *channelReporter[F]) ChannelStateChanged(channel core.Channel[F]) {
	fmt.Printf("%s  channel %s %s  %.2f kHz  %d WPM  %.1f dB\n",
		time.Now().Format("15:04:05"), channel.ID, channelStateName(channel.State),
		float64(channel.Frequency)/1000.0, channel.WPM, channel.SNR)
}

func channelStateName(state core.ChannelState) string {
	switch state {
	case core.NewChannel:
		return "new"
	case core.ConfirmedChannel:
		return "confirmed"
	case core.ActiveChannel:
		return "active"
	case core.IdleChannel:
		return "idle"
	case core.DeadChannel:
		return "dead"
	}
	return "unknown"
}

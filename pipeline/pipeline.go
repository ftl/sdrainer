package pipeline

import (
	"fmt"
	"log"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/ftl/hamradio/callsign"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/dsp"
	"github.com/ftl/sdrainer/notify"
)

const (
	// ScopeSpectrum and ScopeNoiseFloor are the stream IDs that the pipeline uses for the scope.
	ScopeSpectrum   core.StreamID = "spectrum"
	ScopeNoiseFloor core.StreamID = "noise_floor"

	// maxScopeMarkers limits the number of peaks that go to the scope. More markers than this give
	// no more information to a human.
	maxScopeMarkers = 16

	frameBufferSize = 32
)

// Config holds the parameters of the pipeline. Each value here is physical: a frequency, a time, a
// speed or a ratio. The pipeline calculates the sizes in samples and the counts in bins and in
// frames itself, so a change of the sample rate needs no change of this structure.
//
// doc/architecture.md, section 4, gives the formula and the measurement behind each value.
type Config[F dsp.Number] struct {
	// SampleRate and CenterFrequency come from the source.
	SampleRate      int
	CenterFrequency F

	// BinWidth is the frequency resolution of the detection tier, thus the smallest distance
	// between two stations that must stay separate. BlockSize and Hop come from it.
	BinWidth F

	// MinWPM and MaxWPM are the range of the speed to decode. The decode tier comes from MaxWPM,
	// and each time of the tracker comes from MinWPM.
	MinWPM int
	MaxWPM int

	PeakThreshold float64 // dB above the local noise floor

	// The limits of the CW discrimination, section 4.3 of the research document.
	MinDutyCycle       float64 // below this a candidate is noise
	MaxDutyCycle       float64 // above this a candidate is a carrier, a beacon or a data mode
	MaxAutocorrelation float64 // above this the envelope repeats too slowly for CW

	NoiseFloorTime time.Duration // the time constant of the noise floor
	DeadTimeout    time.Duration // a channel that is silent for this time goes away
	MaxDrift       F             // Hz for each second

	ScopeFrameRate float64 // frames each second that go to the scope
}

const (
	// overlapFactor is the overlap of the STFT. A Hann window needs 75 % for a result that is
	// smooth in time, thus a hop of one quarter of the window.
	overlapFactor = 4

	// noiseFloorSpanBins is the span of the moving median, in bins. The median breaks down when
	// more than one half of its window holds signal, and one CW signal covers 3 to 10 bins.
	// TestDetectionStageWithASpanThatIsTooSmall shows that 11 bins fail. Section 4.1 of the
	// research document names 500 Hz, which is this count of bins at a bin width of 11.7 Hz.
	noiseFloorSpanBins = 43

	// peakMergeWidthHz is the width of one station, from section 4.2 of the research document. Two
	// groups that are closer than this are one station.
	//
	// The width of a signal grows with its speed: the keying sidebands of a signal at 56 WPM reach
	// approximately 47 Hz. This value is thus possibly too small at a high speed, and
	// doc/architecture.md, section 4, holds that open question. We did not measure it, so the value
	// stays at the value of the research document.
	peakMergeWidthHz = 40

	// matchWidthHz is the distance from a channel inside which a peak belongs to that channel. It
	// is **not** the jitter of a measured peak, which is one or two bins: it is the width inside
	// which two channels would decode the same signal.
	//
	// The decode tier takes the energy of 3 bins around the frequency of a channel (envelopeBins,
	// pipeline/decode_stage.go), and those 3 bins are 141 Hz at each sample rate. Two channels that
	// stand closer than that therefore give almost the same text. With a match width of 2 bins, or
	// 23 Hz, the tracker made 3 channels of one station that drifts or that has a wide signal, and
	// each of them decoded a part of the text: a measurement with a recording of the 20 m band gave
	// 3 channels for OM1UM and 2 channels for IF9/IT9PPG.
	//
	// The value is therefore a physical width, and not a count of bins. It is smaller than the 141
	// Hz of the envelope, so that two stations that a human still separates by ear keep their own
	// channel.
	matchWidthHz = 100.0

	// candidateMatchWidthBins is the same width for a candidate, thus for a signal that is not a
	// channel yet. It stays at the jitter of a measured peak, which is one or two bins.
	//
	// A candidate must not use matchWidthHz: the keying edges of a strong station give peaks over a
	// wide range, and each of them alone is too short for ConfirmCount. With the wide width they
	// become one candidate that reaches ConfirmCount together, and the scene of the demo gave a
	// false channel 225 Hz beside a station. With the narrow width they stay apart and they go away,
	// as they did before.
	candidateMatchWidthBins = 2

	// idleMargin makes the idle timeout longer than one word gap, because an operator makes pauses.
	// Section 5 of the research document asks for that.
	idleMargin = 1.5

	defaultScopeFrameRate = 10
)

// The values of DefaultConfig. Each one is physical, and doc/architecture.md, section 4, gives the
// formula and the measurement behind it.
const (
	// DefaultPeakThreshold is the level above the local noise floor that makes a peak, in dB. 10 dB
	// gives 0 false channels over 30 s of the scene of the demo, 8 dB gives 70 and 6 dB gives 1283.
	// Section 4.2 of the research document asks for 6 dB to 10 dB.
	DefaultPeakThreshold = 10.0

	// the smallest distance between two stations that must stay separate, section 3
	defaultBinWidth = 12.0

	// the range of the speed to decode. The decode tier comes from the highest speed, and each time
	// of the tracker comes from the lowest speed.
	defaultMinWPM = 8
	defaultMaxWPM = 56

	// The limits of the CW discrimination, section 4.3. Real CW of the scene gives a duty cycle of
	// 0.70 to 0.87 and an autocorrelation of 0.09 and below; a carrier gives 1.00 and 1.00.
	//
	// MinDutyCycle also gives ConfirmCount, thus the count of the detections that a candidate needs
	// inside one CW window. The value was 0.07, which is 4 detections of 70 frames, and 4 peaks of
	// the noise inside 1.5 s are common: a recording of 54 s of the 20 m band gave 74 channels,
	// of which 6 carried CW. 65 of the false ones had a duty cycle of 0.06 to 0.13, and the 6 real
	// ones had 0.40 to 0.89. With 0.15 the same recording gives 9 channels, and the character error
	// rate of the 11 transcriptions of the other two recordings does not get worse.
	// doc/architecture.md, section 6.3, holds the measurement.
	defaultMinDutyCycle       = 0.15
	defaultMaxDutyCycle       = 0.9
	defaultMaxAutocorrelation = 0.4

	defaultNoiseFloorTime = 3 * time.Second  // section 4.1 asks for 1 s to 5 s
	defaultDeadTimeout    = 20 * time.Second // section 5 asks for 10 s to 30 s
	defaultMaxDrift       = 2.0              // Hz for each second
)

// DefaultConfig gives the configuration that each source of SDRainer uses. The sample rate and the
// center frequency come from the source, and a caller changes each other value in the result when
// it has a reason: `kiwi` and `replay` take PeakThreshold from a flag.
func DefaultConfig[F dsp.Number](sampleRate int, centerFrequency F) Config[F] {
	return Config[F]{
		SampleRate:      sampleRate,
		CenterFrequency: centerFrequency,
		BinWidth:        defaultBinWidth,

		MinWPM: defaultMinWPM,
		MaxWPM: defaultMaxWPM,

		PeakThreshold: DefaultPeakThreshold,

		MinDutyCycle:       defaultMinDutyCycle,
		MaxDutyCycle:       defaultMaxDutyCycle,
		MaxAutocorrelation: defaultMaxAutocorrelation,

		NoiseFloorTime: defaultNoiseFloorTime,
		DeadTimeout:    defaultDeadTimeout,
		MaxDrift:       defaultMaxDrift,

		ScopeFrameRate: defaultScopeFrameRate,
	}
}

// IQRecorder takes the IQ samples that the pipeline processes. iq.Writer implements it, and the
// root command gives it to the pipeline when the user asks for a recording.
type IQRecorder[S dsp.Number] interface {
	RecordIQ(samples []S) error
}

// Derived holds the values that the pipeline calculates from a Config. A caller uses it to show the
// setup, and a test uses it to know the sizes that the pipeline works with.
type Derived struct {
	BlockSize       int
	Hop             int
	DecodeBlockSize int
	DecodeHop       int

	BinWidth      float64
	FrameInterval time.Duration
	DecodeTick    time.Duration

	NoiseFloorSpan int // bins
	PeakMergeWidth int // bins

	// MatchWidth holds for a channel, and CandidateMatchWidth holds for a signal that is not a
	// channel yet. Both are Hz.
	MatchWidth          float64
	CandidateMatchWidth float64

	CWWindow      time.Duration
	IdleTimeout   time.Duration
	MinKeyingRate float64
	ConfirmCount  int
}

// Derive calculates each value that the pipeline needs from the physical values of the Config.
// doc/architecture.md, section 4, gives the formula behind each one.
func Derive[F dsp.Number](config Config[F]) Derived {
	result := Derived{
		BlockSize:       blockSizeFor(config.SampleRate, float64(config.BinWidth)),
		DecodeBlockSize: decodeBlockSizeFor(config.SampleRate, config.MaxWPM),
		NoiseFloorSpan:  noiseFloorSpanBins,
	}
	result.Hop = result.BlockSize / overlapFactor
	result.DecodeHop = result.DecodeBlockSize / overlapFactor

	if result.BlockSize > 0 {
		result.BinWidth = float64(config.SampleRate) / float64(result.BlockSize)
	}
	result.FrameInterval = frameInterval(config.SampleRate, result.Hop)
	result.DecodeTick = frameInterval(config.SampleRate, result.DecodeHop)

	result.PeakMergeWidth = binsFor(F(peakMergeWidthHz), result.BinWidth)
	result.MatchWidth = matchWidthHz
	result.CandidateMatchWidth = candidateMatchWidthBins * result.BinWidth

	result.CWWindow = characterTime(config.MinWPM)
	result.IdleTimeout = time.Duration(idleMargin * float64(wordGap(config.MinWPM)))
	result.MinKeyingRate = keyingRate(config.MinWPM)
	if result.FrameInterval > 0 {
		result.ConfirmCount = max(1, int(config.MinDutyCycle*result.CWWindow.Seconds()/result.FrameInterval.Seconds()))
	}

	return result
}

// ditSeconds gives the length of one dit at the given speed. PARIS is 50 dits, thus 1.2 s at
// 1 WPM.
func ditSeconds(wpm int) float64 {
	if wpm <= 0 {
		return 0
	}
	return 1.2 / float64(wpm)
}

// blockSizeFor gives the smallest power of two that reaches the wanted bin width or a smaller one.
// The FFT of dsp is a radix-2 FFT, so the size must be a power of two.
func blockSizeFor(sampleRate int, binWidth float64) int {
	if sampleRate <= 0 || binWidth <= 0 {
		return 0
	}
	result := 1
	for float64(sampleRate)/float64(result) > binWidth {
		result *= 2
	}
	return result
}

// decodeBlockSizeFor gives the largest power of two whose window is not longer than one dit at the
// given speed. A window that is longer than one element makes the keying unclear, see
// doc/architecture.md, section 3.1. The hop is one quarter of the window, which keeps the second
// condition of that document without a calculation.
func decodeBlockSizeFor(sampleRate int, maxWPM int) int {
	samples := float64(sampleRate) * ditSeconds(maxWPM)
	if samples < 1 {
		return 0
	}
	result := 1
	for float64(result*2) <= samples {
		result *= 2
	}
	return result
}

// wordGap gives the time of a gap between two words at the given speed, thus 7 dits.
func wordGap(wpm int) time.Duration {
	return time.Duration(7 * ditSeconds(wpm) * float64(time.Second))
}

// characterTime gives the time of one character at the given speed, thus approximately 10 dits.
// The window of the CW discrimination must hold at least one character, otherwise a single long da
// gives a duty cycle of 1 and the test removes a real signal.
func characterTime(wpm int) time.Duration {
	return time.Duration(10 * ditSeconds(wpm) * float64(time.Second))
}

// keyingRate gives the rate at which a signal at the given speed switches on and off, when it sends
// only dits. The period is two dits.
func keyingRate(wpm int) float64 {
	dit := ditSeconds(wpm)
	if dit <= 0 {
		return 0
	}
	return 1 / (2 * dit)
}

// Pipeline for signal processing.
// - S: data type for the IQ sample components
// - F: data type for the frequency in Hz
type Pipeline[S, F dsp.Number] struct {
	config    Config[F]
	derived   Derived
	scope     core.ScopeService
	spotter   core.Spotter[F]
	recorder  IQRecorder[S]
	listeners []any

	framePool *FramePool[S, F]
	stft      *STFTStage[S, F]
	detection *DetectionStage[S, F]
	tracker   *TrackerStage[S, F]
	mapping   *dsp.FrequencyMapping[F]

	qualities       *spotQualities[F]
	decodeFramePool *FramePool[S, F]
	decodeStft      *STFTStage[S, F]
	decode          *DecodeStage[S, F]
	callsigns       *CallsignStage[F]

	// runLock protects the channels of the frames against a source that gives samples while the
	// pipeline stops. IQData takes it for reading, and Start and Stop take it for writing.
	//
	// A source cannot always stop its stream before it stops the pipeline: the TCI client and the
	// KiwiSDR client give their samples in a goroutine of their own, and a callback that already
	// runs continues after the command that stops the stream. Stop closes the channels, and a send
	// on a channel that is closed panics.
	//
	// Only Start and Stop use these three fields, and the worker takes them as parameters. The
	// worker therefore needs no lock for them.
	runLock         sync.RWMutex
	detectionFrames chan *SpectralFrame[S, F]
	decodeFrames    chan *SpectralFrame[S, F]
	stopped         chan struct{}

	// The center frequency can change while the pipeline runs, and the change comes from the
	// goroutine of the SDR. centerLock protects the value, and centerChanged wakes the worker. The
	// worker then applies the newest value between two frames.
	centerLock    sync.Mutex
	centerPending F
	centerChanged chan struct{}

	scopeDecimation int
	scopeCountdown  int
	scopeValues     []float64
	scopeMarkers    map[core.MarkerID]float64
}

func New[S, F dsp.Number](config Config[F], scopeService core.ScopeService) *Pipeline[S, F] {
	if scopeService == nil {
		scopeService = &core.NullScopeService{}
	}
	scopeFrameRate := config.ScopeFrameRate
	if scopeFrameRate <= 0 {
		scopeFrameRate = defaultScopeFrameRate
	}
	derived := Derive(config)

	mapping := dsp.NewFrequencyMapping[F](config.SampleRate, derived.BlockSize, config.CenterFrequency)
	framePool := NewFramePool[S, F](derived.BlockSize)

	result := &Pipeline[S, F]{
		config:  config,
		derived: derived,
		scope:   scopeService,
		spotter: &core.NullSpotter[F]{},

		framePool: framePool,
		stft:      NewSTFTStage[S, F](framePool, config.SampleRate, derived.Hop),
		detection: NewDetectionStage[S, F](
			derived.BlockSize,
			derived.NoiseFloorSpan,
			dsp.SmoothingFactor(config.NoiseFloorTime, derived.FrameInterval),
			config.PeakThreshold,
			derived.PeakMergeWidth,
			mapping,
		),
		mapping: mapping,

		decodeFramePool: NewFramePool[S, F](derived.DecodeBlockSize),

		centerPending: config.CenterFrequency,
		centerChanged: make(chan struct{}, 1),

		scopeDecimation: scopeDecimation(derived.FrameInterval, scopeFrameRate),
		scopeValues:     make([]float64, derived.BlockSize),
		scopeMarkers:    make(map[core.MarkerID]float64, maxScopeMarkers),
	}
	result.decodeStft = NewSTFTStage[S, F](result.decodeFramePool, config.SampleRate, derived.DecodeHop)
	result.decode = NewDecodeStage[S, F](
		dsp.NewFrequencyMapping[F](config.SampleRate, derived.DecodeBlockSize, config.CenterFrequency),
		derived.DecodeBlockSize,
		config.SampleRate,
		derived.DecodeHop,
		result,
	)
	result.callsigns = NewCallsignStage[F](result)
	result.qualities = newSpotQualities[F]()
	result.tracker = NewTrackerStage[S, F](TrackerConfig[F]{
		FrameInterval:       derived.FrameInterval,
		MatchWidth:          F(derived.MatchWidth),
		CandidateMatchWidth: F(derived.CandidateMatchWidth),
		ConfirmCount:        derived.ConfirmCount,
		IdleTimeout:         derived.IdleTimeout,
		DeadTimeout:         config.DeadTimeout,
		MaxDrift:            config.MaxDrift,

		CWWindow:           derived.CWWindow,
		MaxDutyCycle:       config.MaxDutyCycle,
		MinKeyingRate:      derived.MinKeyingRate,
		MaxAutocorrelation: config.MaxAutocorrelation,
	}, result)

	return result
}

// Derived gives the values that the pipeline calculated from its configuration.
func (p *Pipeline[S, F]) Derived() Derived {
	return p.derived
}

func frameInterval(sampleRate int, hop int) time.Duration {
	if sampleRate <= 0 || hop <= 0 {
		return 0
	}
	return time.Duration(float64(hop) / float64(sampleRate) * float64(time.Second))
}

func binsFor[F dsp.Number](width F, binWidth float64) int {
	if binWidth <= 0 {
		return 1
	}
	return max(1, int(math.Round(float64(width)/binWidth)))
}

// scopeDecimation returns the number of frames for one frame that goes to the scope. The STFT makes
// many more frames than a display needs, and each frame for the scope costs one protobuf message
// with one value for each bin.
func scopeDecimation(interval time.Duration, scopeFrameRate float64) int {
	if interval <= 0 || scopeFrameRate <= 0 {
		return 1
	}
	frameRate := 1 / interval.Seconds()

	return max(1, int(math.Round(frameRate/scopeFrameRate)))
}

func (p *Pipeline[_, _]) Notify(listener any) {
	p.listeners = append(p.listeners, listener)
}

// SetRecorder gives the pipeline the recorder of the IQ stream. One recorder covers each source,
// because each source gives its samples to IQData. The caller must use this method before Start.
func (p *Pipeline[S, F]) SetRecorder(recorder IQRecorder[S]) {
	p.recorder = recorder
}

// SetSpotter gives the pipeline the spotter that takes the callsign of each running station. The
// caller must use this method before Start.
//
// A spotter that also needs the life of a channel must go to Notify as well. The DX cluster does
// that: it needs ChannelDestroyed, because the spot of a station that went away must not go to a
// new connection.
func (p *Pipeline[S, F]) SetSpotter(spotter core.Spotter[F]) {
	if spotter == nil {
		spotter = &core.NullSpotter[F]{}
	}
	p.spotter = spotter
}

func (p *Pipeline[S, F]) Channels() []core.Channel[F] {
	return p.tracker.Channels()
}

func (p *Pipeline[S, F]) emitChannelCreated(channel core.Channel[F]) {
	p.decode.ChannelCreated(channel)
	p.callsigns.ChannelCreated(channel)
	notify.Emit(p.listeners, func(l core.ChannelLifecycleListener[F]) {
		l.ChannelCreated(channel)
	})
}

func (p *Pipeline[S, F]) emitChannelDestroyed(channel core.Channel[F]) {
	channel.Callsign = p.callsigns.CallsignOf(channel.ID)
	channel.Quality = p.qualities.qualityOf(channel.ID)
	p.decode.ChannelDestroyed(channel)
	p.callsigns.ChannelDestroyed(channel)
	p.qualities.channelDestroyed(channel.ID)
	notify.Emit(p.listeners, func(l core.ChannelLifecycleListener[F]) {
		l.ChannelDestroyed(channel)
	})

	// the station of this channel is gone, so the spotter must take its spot back
	if channel.Callsign != callsign.NoCallsign {
		p.spotter.RemoveSpot(channel.Callsign.String())
	}
}

func (p *Pipeline[S, F]) emitChannelStateChanged(channel core.Channel[F]) {
	channel.WPM = p.decode.WPMOf(channel.ID)
	channel.Callsign = p.callsigns.CallsignOf(channel.ID)
	channel.Quality = p.qualities.qualityOf(channel.ID)
	notify.Emit(p.listeners, func(l core.ChannelStateListener[F]) {
		l.ChannelStateChanged(channel)
	})
}

func (p *Pipeline[S, F]) emitChannelCharacterReceived(channel core.Channel[F], character rune, offset int64) {
	channel.Callsign = p.callsigns.CallsignOf(channel.ID)
	channel.Quality = p.qualities.qualityOf(channel.ID)
	notify.Emit(p.listeners, func(l core.ChannelReceiveListener[F]) {
		l.ChannelCharacterReceived(channel, character, offset)
	})

	// after the event of the character, so that the character that completes the callsign comes
	// before the event of the callsign
	p.callsigns.Process(channel, character)
}

func (p *Pipeline[S, F]) emitChannelRunningCallsignDetected(channel core.Channel[F]) {
	// The quality comes first, so that each consumer of this moment sees the same value: the event
	// of the callsign, the comment of the spot, and the event of the quality.
	quality := p.qualities.tagFor(channel, p.callsigns.evidenceOf(channel.ID))
	channel.Quality = quality.tag

	notify.Emit(p.listeners, func(l core.ChannelRunningCallsignListener[F]) {
		l.ChannelRunningCallsignDetected(channel)
	})

	p.spotter.Spot(channel.Callsign.String(), channel.Frequency, spotMessage(channel, quality), time.Now())

	// The quality of a channel goes up while the receiver reads the callsign again and again, and a
	// consumer that shows a channel needs that change.
	if p.qualities.changed(channel.ID, quality.tag) {
		notify.Emit(p.listeners, func(l core.ChannelQualityListener[F]) {
			l.ChannelQualityChanged(channel)
		})
	}
}

// spotMessage writes the comment of a spot in the form that a skimmer of AR-Cluster 6 uses:
//
//	CW 25 dB 24 WPM CQ V
//
// The dB stands before the WPM, as each skimmer writes it, and the quality tag stands at the right
// end. "CQ" holds for each spot of SDRainer, because the callsign stage reports the callsign of a
// station that calls and of no other station, so a client can filter on SKIMCQ.
//
// A spot with QualityBusted also holds the callsign that this receiver made valid:
//
//	CW 25 dB 24 WPM CQ B (DL1ABC)
func spotMessage[F dsp.Number](channel core.Channel[F], quality spotQuality) string {
	result := fmt.Sprintf("CW %.0f dB %d WPM CQ %c", channel.SNR, channel.WPM, quality.tag)
	if quality.correction != "" {
		result += " (" + quality.correction + ")"
	}

	return result
}

// SetCenterFrequency changes the center frequency of the stream. An SDR can change it while the
// pipeline runs, for example when the operator turns the dial.
//
// It is safe to call this method from any goroutine, and also before Start. The method does not
// block: the worker applies the newest value between two frames. A channel that is still inside the
// spectrum keeps its frequency and continues. A channel that is now outside the spectrum gets no
// more peaks, so it goes through IDLE to DEAD, as a signal that stopped.
func (p *Pipeline[S, F]) SetCenterFrequency(frequency F) {
	p.centerLock.Lock()
	p.centerPending = frequency
	p.centerLock.Unlock()

	select {
	case p.centerChanged <- struct{}{}:
	default:
		// a signal is already there, and the worker then takes the newest value
	}
}

// CenterFrequency gives the center frequency of the last call of SetCenterFrequency, or the value
// of the configuration.
func (p *Pipeline[S, F]) CenterFrequency() F {
	p.centerLock.Lock()
	defer p.centerLock.Unlock()
	return p.centerPending
}

// applyCenterFrequency moves both tiers to the new center frequency. Only the worker calls it.
//
// The estimate of the noise floor starts again, because each bin has a different content after the
// change. dsp.NoiseFloor gives the floor of the next frame directly, without the smoothing over the
// time, so the floor is correct again with that frame.
func (p *Pipeline[S, F]) applyCenterFrequency(detectionFrames chan *SpectralFrame[S, F], decodeFrames chan *SpectralFrame[S, F]) {
	p.centerLock.Lock()
	frequency := p.centerPending
	p.centerLock.Unlock()

	p.mapping.SetCenterFrequency(frequency) // the detection tier, and the detection stage uses it
	p.decode.SetCenterFrequency(frequency)  // the decode tier
	p.detection.ResetNoiseFloor()

	// The frames that wait in the buffer come from samples of the old band. The new mapping would
	// give each of their peaks a frequency that is wrong by the shift, and the tracker would make a
	// channel at that wrong frequency: a ghost of a real signal, at its frequency plus the shift. A
	// candidate needs only ConfirmCount frames, thus approximately 85 ms, and the buffer holds up
	// to frameBufferSize frames. The frames of the old band must therefore go away.
	drainFrames(detectionFrames, p.framePool)
	drainFrames(decodeFrames, p.decodeFramePool)
}

// drainFrames takes each frame that waits in the given channel and gives it back to its pool. It
// does not block, and it also works with a channel that is closed or nil.
func drainFrames[S, F dsp.Number](frames chan *SpectralFrame[S, F], pool *FramePool[S, F]) {
	for {
		select {
		case frame := <-frames:
			if frame == nil {
				// the channel is closed, and Stop waits for the worker
				return
			}
			pool.ReturnFrame(frame)
		default:
			return
		}
	}
}

// Start begins the work of the pipeline. The caller must call Start before the first call of
// IQData. A second call does nothing.
func (p *Pipeline[S, F]) Start() {
	p.runLock.Lock()
	defer p.runLock.Unlock()

	if p.detectionFrames != nil {
		return
	}

	detectionFrames := make(chan *SpectralFrame[S, F], frameBufferSize)
	decodeFrames := make(chan *SpectralFrame[S, F], frameBufferSize)
	stopped := make(chan struct{})
	p.detectionFrames, p.decodeFrames, p.stopped = detectionFrames, decodeFrames, stopped

	// the worker takes the channels as parameters, so that it never reads the fields
	go p.run(detectionFrames, decodeFrames, stopped)
}

// Stop ends the work of the pipeline and waits for the last frame. A second call does nothing.
//
// It is safe to call IQData at the same time as Stop, and after it: Stop waits for each call that
// runs, and a call after it does nothing. A source that gives its samples in a goroutine of its own
// needs that, because it cannot stop a callback that already runs.
func (p *Pipeline[S, F]) Stop() {
	p.runLock.Lock()
	if p.detectionFrames == nil {
		p.runLock.Unlock()
		return
	}

	// Each call of IQData holds the lock for reading, so no call is inside the STFT now and no call
	// begins before this method gives the lock back. The channels can therefore close.
	close(p.detectionFrames)
	close(p.decodeFrames)
	stopped := p.stopped
	p.detectionFrames, p.decodeFrames, p.stopped = nil, nil, nil
	p.runLock.Unlock()

	<-stopped
}

// IQData is a callback function to ingest the IQ samples in chunks of interleaved I and Q values.
// The STFT runs in the goroutine of the caller, and it copies the samples into its own buffer, so
// the caller can use its slice again after this call. Only the frames go to the worker.
func (p *Pipeline[S, _]) IQData(sampleRate int, samples []S) {
	// for reading, so that many sources can give samples at the same time. Only Stop takes it for
	// writing, and it then waits for each call that runs.
	p.runLock.RLock()
	defer p.runLock.RUnlock()

	if p.detectionFrames == nil {
		return
	}
	if sampleRate != p.config.SampleRate {
		log.Printf("wrong incoming sample rate: %d instead of %d", sampleRate, p.config.SampleRate)
		return
	}

	// before the STFT, so that the recording holds each sample that the pipeline sees
	if p.recorder != nil {
		if err := p.recorder.RecordIQ(samples); err != nil {
			// one message is sufficient: a disk that is full gives this error for each chunk, and
			// the skimmer continues without the recording
			log.Printf("cannot record the IQ data, the recording stops: %v", err)
			p.recorder = nil
		}
	}

	p.stft.Process(p.detectionFrames, samples)
	p.decodeStft.Process(p.decodeFrames, samples)
}

// run takes the frames of the two tiers in one goroutine, so the stages need no lock. The two
// streams are independent: the decode tier makes more frames than the detection tier, and the
// worker can take a decode frame before the detection frame of the same time. A channel needs more
// than one second before the tracker confirms it, so the decode tier starts long after that.
func (p *Pipeline[S, F]) run(detectionFrames chan *SpectralFrame[S, F], decodeFrames chan *SpectralFrame[S, F], stopped chan struct{}) {
	defer close(stopped)

	for detectionFrames != nil || decodeFrames != nil {
		select {
		case <-p.centerChanged:
			p.applyCenterFrequency(detectionFrames, decodeFrames)
		case frame, open := <-detectionFrames:
			if !open {
				detectionFrames = nil
				continue
			}
			p.detection.Process(frame)
			p.tracker.Process(frame)
			// the tracker follows the drift of a signal, and the decode tier must follow it too
			p.decode.FollowChannels(p.tracker)
			p.showFrame(frame)
			p.framePool.ReturnFrame(frame)
		case frame, open := <-decodeFrames:
			if !open {
				decodeFrames = nil
				continue
			}
			p.decode.Process(frame)
			p.decodeFramePool.ReturnFrame(frame)
		}
	}
}

func (p *Pipeline[S, F]) showFrame(frame *SpectralFrame[S, F]) {
	if !p.scope.Active() {
		return
	}

	p.scopeCountdown--
	if p.scopeCountdown > 0 {
		return
	}
	p.scopeCountdown = p.scopeDecimation

	now := time.Now()
	fromFrequency := float64(p.mapping.BinToFrequency(0, dsp.BinFrom))
	toFrequency := float64(p.mapping.BinToFrequency(len(frame.Spectrum)-1, dsp.BinTo))

	clear(p.scopeMarkers)
	for i, peak := range frame.Peaks {
		if i == maxScopeMarkers {
			break
		}
		p.scopeMarkers[core.MarkerID("peak_"+strconv.Itoa(i))] = float64(peak.SignalFrequency)
	}

	p.showSpectralStream(ScopeSpectrum, now, fromFrequency, toFrequency, frame.Spectrum, p.scopeMarkers)
	p.showSpectralStream(ScopeNoiseFloor, now, fromFrequency, toFrequency, frame.NoiseFloor, nil)
}

// showSpectralStream converts a block of linear power into dB and sends it to the scope. dB is the
// correct domain for a display, because a linear power spectrum covers too many orders of magnitude
// for a plot.
func (p *Pipeline[S, F]) showSpectralStream(stream core.StreamID, timestamp time.Time, fromFrequency float64, toFrequency float64, values dsp.Block[S], markers map[core.MarkerID]float64) {
	for i, value := range values {
		p.scopeValues[i] = float64(dsp.PowerIndB(value))
	}

	p.scope.SendSpectralFrame(&core.SpectralFrame{
		Frame: core.Frame{
			Stream:    stream,
			Timestamp: timestamp,
		},
		FromFrequency:    fromFrequency,
		ToFrequency:      toFrequency,
		Values:           p.scopeValues,
		FrequencyMarkers: markers,
	})
}

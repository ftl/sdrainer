package pipeline

import (
	"math"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/cw"
	"github.com/ftl/sdrainer/dsp"
)

// envelopeBins is the number of bins around the frequency of a channel that make its envelope. The
// main lobe of a Hann window is 1.5 bins wide, so 3 bins hold the complete energy of one tone. One
// bin alone is not sufficient: a signal that drifts moves over a bin border, and one bin alone then
// shows a gap in the keying that does not exist.
const envelopeBins = 3

// characterReporter has an unexported method, so only this package can implement it. The pipeline
// implements it and sends the event to the listeners, and the test implements it to collect the
// characters.
type characterReporter[F dsp.Number] interface {
	emitChannelCharacterReceived(core.Channel[F], rune, int64)
}

type DecodeStage[S, F dsp.Number] struct {
	mapping    *dsp.FrequencyMapping[F]
	blockSize  int
	sampleRate int
	hop        int
	reporter   characterReporter[F]

	// the first and the last frequency that a channel can use. They keep a distance of one bin from
	// the edge of the spectrum, so that the 3 bins of an envelope are all inside it.
	fromFrequency F
	toFrequency   F

	channels map[core.ChannelID]*channelDecoder[S, F]
}

// channelSource gives the current data of a channel. The tracker implements it. The method is
// unexported, so only this package can implement it.
type channelSource[F dsp.Number] interface {
	channelOf(core.ChannelID) (core.Channel[F], bool)
}

// channelDecoder holds the state of the decode of one channel.
type channelDecoder[S, F dsp.Number] struct {
	channel  core.Channel[F]
	bin      int
	reporter characterReporter[F]
	hop      int64

	demodulator *cw.SpectralDemodulator[S]
	envelope    S

	// base is the offset of the first frame that this decoder saw. A tick of the decoder is one
	// frame, and the ticks are zero-based, so the character at the tick t begins at base+t*hop.
	base    int64
	started bool
}

// CharacterDecoded takes each character of the decoder of this channel and makes the event. The
// speed comes with the character, so it is the speed at the moment of the character and not the
// speed of now.
func (d *channelDecoder[S, F]) CharacterDecoded(character cw.Character) {
	d.channel.WPM = int(math.Round(character.WPM))
	d.reporter.emitChannelCharacterReceived(d.channel, character.Rune, d.base+character.Start*d.hop)
}

func NewDecodeStage[S, F dsp.Number](mapping *dsp.FrequencyMapping[F], blockSize int, sampleRate int, hop int, reporter characterReporter[F]) *DecodeStage[S, F] {
	result := &DecodeStage[S, F]{
		mapping:    mapping,
		blockSize:  blockSize,
		sampleRate: sampleRate,
		hop:        hop,
		reporter:   reporter,

		channels: make(map[core.ChannelID]*channelDecoder[S, F]),
	}
	result.updateLimits()

	return result
}

// SetCenterFrequency moves the spectrum of the decode tier to a new center frequency. The caller
// must use the goroutine of the worker, because the stages of the pipeline are not safe for
// concurrent use.
func (d *DecodeStage[S, F]) SetCenterFrequency(frequency F) {
	d.mapping.SetCenterFrequency(frequency)
	d.updateLimits()

	// FollowChannels calculates the bin again for each frame of the detection tier, but the decode
	// tier makes four frames in that time. The bins must therefore be correct now.
	for _, decoder := range d.channels {
		decoder.bin = d.mapping.FrequencyToBin(decoder.channel.Frequency)
	}
}

func (d *DecodeStage[S, F]) updateLimits() {
	if d.blockSize < 3 {
		return
	}
	d.fromFrequency = d.mapping.BinToFrequency(1, dsp.BinFrom)
	d.toFrequency = d.mapping.BinToFrequency(d.blockSize-2, dsp.BinTo)
}

// inSpectrum tells if a channel is inside the spectrum of the decode tier. FrequencyToBin limits its
// result to the bins that exist, so a channel outside the spectrum would get the first or the last
// bin and it would decode the noise of that bin.
func (d *DecodeStage[S, F]) inSpectrum(frequency F) bool {
	return frequency >= d.fromFrequency && frequency <= d.toFrequency
}

func (d *DecodeStage[S, F]) ChannelCreated(channel core.Channel[F]) {
	decoder := &channelDecoder[S, F]{
		channel:  channel,
		bin:      d.mapping.FrequencyToBin(channel.Frequency),
		reporter: d.reporter,
		hop:      int64(d.hop),
	}
	// the demodulator needs its sink at the construction, so the decoder of the channel must exist
	// before it
	decoder.demodulator = cw.NewSpectralDemodulator[S](decoder, d.sampleRate, d.hop)

	d.channels[channel.ID] = decoder
}

// WPMOf gives the speed of the given channel, or 0 while the decoder of that channel did not
// decode a character yet. The decode stage owns the speed of a channel.
func (d *DecodeStage[S, F]) WPMOf(id core.ChannelID) int {
	decoder, ok := d.channels[id]
	if !ok {
		return 0
	}
	return decoder.channel.WPM
}

func (d *DecodeStage[S, F]) ChannelDestroyed(channel core.Channel[F]) {
	delete(d.channels, channel.ID)
}

// FollowChannels takes the current data of each channel from the source. The tracker follows
// the drift of a signal, but it reports no event for it, so the bins of the envelope would stay at
// the frequency of the moment of the creation. The 3 bins are 141 Hz at a decode tier of 1024 bins,
// so a signal leaves them at a drift of approximately 70 Hz: a measurement with `sdrainer test`
// before this method showed the wall, because the signal at 7032000 drifts with 2 Hz for each
// second and its text became noise after approximately 42 s.
//
// The caller must use the frames of the detection tier for this, and not the frames of the decode
// tier: the tracker changes a frequency only with a detection frame, and the decode tier makes four
// times more frames.
func (d *DecodeStage[S, F]) FollowChannels(source channelSource[F]) {
	for id, decoder := range d.channels {
		current, ok := source.channelOf(id)
		if !ok {
			continue
		}

		// The tracker owns the frequency, the SNR and the state, and it changes all three over the
		// life of a channel. A copy that is not refreshed here stays at the values of the creation:
		// the event of a character would always give the state CONFIRMED. The decode stage owns the
		// speed, so that value stays.
		wpm := decoder.channel.WPM
		decoder.channel = current
		decoder.channel.WPM = wpm

		decoder.bin = d.mapping.FrequencyToBin(current.Frequency)
	}
}

func (d *DecodeStage[S, F]) Process(frame *SpectralFrame[S, F]) {
	for _, decoder := range d.channels {
		if !decoder.started {
			decoder.base = frame.Offset
			decoder.started = true
		}

		// A channel that is not active has no signal, and the bins of its envelope hold only noise.
		// The demodulator would take that noise as its two levels, and the decoder would make
		// characters from it: a channel that is idle gave text until it was dead, thus for the
		// whole DeadTimeout. The stage gives it a gap instead.
		//
		// The tick must still happen, because the position of a character comes from the count of
		// the ticks of the decoder. A tick that does not happen would move each later character
		// forwards in the stream.
		//
		// The state CONFIRMED is not active, but it lasts only until the first detection of the
		// tracker, thus at most one gap of a character.
		//
		// A channel outside the spectrum comes from a change of the center frequency. The tracker
		// finds no peak for it and it goes to IDLE, but that needs the idle timeout. Until then the
		// channel would decode the bin at the edge of the spectrum.
		if decoder.channel.State != core.ActiveChannel || !d.inSpectrum(decoder.channel.Frequency) {
			decoder.envelope = 0
			decoder.demodulator.Tick(0)
			continue
		}

		decoder.envelope = envelopeOf(frame.Spectrum, decoder.bin)
		decoder.demodulator.Tick(decoder.envelope)
	}
}

// envelopeOf sums the bins around the given center bin. The spectrum holds linear power, and a sum
// is correct in that domain. See the domain of STFTStage.Process.
func envelopeOf[S dsp.Number](spectrum dsp.Block[S], center int) S {
	var result S
	for bin := center - envelopeBins/2; bin <= center+envelopeBins/2; bin++ {
		if bin < 0 || bin >= len(spectrum) {
			continue
		}
		result += spectrum[bin]
	}
	return result
}

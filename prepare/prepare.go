// Package prepare makes the files for a transcription session: one WAV file and one empty
// transcription file for each channel of a recording that carries a signal long enough for a human
// to write it down.
package prepare

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/listen"
	"github.com/ftl/sdrainer/pipeline"
	"github.com/ftl/sdrainer/replay"
)

// minActiveTime is the time that a channel must be ACTIVE before it gets a WAV file. A channel
// below that time holds a few characters at most, and a human cannot transcribe it.
const minActiveTime = 5 * time.Second

type Options struct {
	IQFilename string
	SampleRate int
}

// Run gives the recording to the pipeline, and it then makes one WAV file and one empty
// transcription file for each channel that was ACTIVE longer than minActiveTime. Both files stand
// beside the recording, with the names <iq-filename>_<offset>.wav and <iq-filename>_<offset>.txt.
func Run(ctx context.Context, options Options) error {
	if options.SampleRate <= 0 {
		return fmt.Errorf("the sample rate must be above 0")
	}

	// The center frequency stays 0, so the frequency of a channel is its offset from the center of
	// the recording. That offset is the value that the demodulation and the name of a transcription
	// both need.
	collector := newCollector(options.SampleRate)
	err := replay.Run(ctx, replay.Options{
		Filename:      options.IQFilename,
		SampleRate:    options.SampleRate,
		PeakThreshold: pipeline.DefaultPeakThreshold,
	}, nil, collector, nil, nil)
	if err != nil {
		return err
	}

	channels := collector.transcribable()
	fmt.Printf("\n%d channels are active for more than %s:\n", len(channels), minActiveTime)

	for _, channel := range channels {
		offset := int(math.Round(channel.frequency))
		fmt.Printf("%+8d Hz  %5.1f s  %2d wpm  %5.1f dB  %d characters\n",
			offset, channel.activeTime.Seconds(), channel.wpm, channel.snr, channel.characters)

		err := listen.Run(listen.Options{
			IQFilename:   options.IQFilename,
			SampleRate:   options.SampleRate,
			SignalOffset: float64(offset),
			OutFilename:  fmt.Sprintf("%s_%d.wav", options.IQFilename, offset),
			Pitch:        listen.DefaultPitch,
		})
		if err != nil {
			return err
		}

		err = createTranscription(fmt.Sprintf("%s_%d.txt", options.IQFilename, offset))
		if err != nil {
			return err
		}
	}

	return nil
}

// createTranscription makes an empty transcription file. A file that already exists stays as it is,
// because it holds the work of a human.
func createTranscription(filename string) error {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if os.IsExist(err) {
		fmt.Printf("%s: the transcription exists already and it stays as it is\n", filename)
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot make the transcription file: %w", err)
	}

	return file.Close()
}

// collector measures the time that each channel is ACTIVE, from the events of the pipeline. It is
// the ChannelService of the replay.
//
// **The clock is the position of the last character.** An event of the state holds no position in
// the stream, and the replay runs approximately 120 times faster than real time, so a clock of the
// machine gives no usable time. The event of a character does hold a position: it is the index of
// the first sample of that character. The collector therefore holds the largest position that it
// saw, and each channel uses it. All channels of one recording share that clock, so a channel that
// is silent still gets a correct time while another channel decodes.
//
// The resolution of that clock is the distance between two characters of the whole band, and a
// recording with more than one station gives many characters for each second. A recording where
// nothing decodes gives no clock at all, and each channel then gets the time 0: such a recording
// holds nothing to transcribe.
type collector struct {
	mutex      sync.Mutex
	sampleRate int
	clock      int64 // the largest position of a character so far, in samples

	order    []core.ChannelID
	channels map[core.ChannelID]*activeChannel
}

// activeChannel holds what the collector knows about one channel.
type activeChannel struct {
	frequency  float64
	wpm        int
	snr        float64
	characters int

	activeTime int64 // samples, complete
	activeFrom int64 // samples, the start of the interval that is open
	active     bool
}

// close ends an interval that is open, at the given position of the clock.
func (c *activeChannel) close(clock int64) {
	if !c.active {
		return
	}
	c.activeTime += clock - c.activeFrom
	c.active = false
}

func newCollector(sampleRate int) *collector {
	return &collector{
		sampleRate: sampleRate,
		channels:   make(map[core.ChannelID]*activeChannel),
	}
}

func (c *collector) Active() bool { return true }

func (c *collector) ChannelCreated(channel core.Channel[float64]) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.order = append(c.order, channel.ID)
	c.channels[channel.ID] = &activeChannel{frequency: channel.Frequency, snr: channel.SNR}
}

func (c *collector) ChannelStateChanged(channel core.Channel[float64]) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	current, ok := c.channels[channel.ID]
	if !ok {
		return
	}

	if channel.State == core.ActiveChannel {
		if !current.active {
			current.active = true
			current.activeFrom = c.clock
		}
		return
	}
	current.close(c.clock)
}

func (c *collector) ChannelCharacterReceived(channel core.Channel[float64], _ rune, offset int64) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// The characters of two channels can arrive in another order than their positions, because a
	// character that began earlier can end later. The clock therefore takes the largest position
	// and it never goes back.
	c.clock = max(c.clock, offset)

	current, ok := c.channels[channel.ID]
	if !ok {
		return
	}
	current.characters++
	current.frequency = channel.Frequency
	current.snr = channel.SNR
	if channel.WPM > 0 {
		current.wpm = channel.WPM
	}
}

func (c *collector) ChannelRunningCallsignDetected(core.Channel[float64]) {}
func (c *collector) ChannelQualityChanged(core.Channel[float64])          {}

func (c *collector) ChannelDestroyed(channel core.Channel[float64]) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if current, ok := c.channels[channel.ID]; ok {
		current.close(c.clock)
	}
}

// transcribedChannel is one channel that a human can transcribe.
type transcribedChannel struct {
	frequency  float64
	wpm        int
	snr        float64
	characters int
	activeTime time.Duration
}

// transcribable gives each channel that was ACTIVE longer than minActiveTime, the lowest frequency
// first. It closes each interval that is still open, because the recording ends there.
func (c *collector) transcribable() []transcribedChannel {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	result := make([]transcribedChannel, 0, len(c.order))
	for _, id := range c.order {
		current := c.channels[id]
		current.close(c.clock)

		activeTime := time.Duration(float64(current.activeTime) / float64(c.sampleRate) * float64(time.Second))
		if activeTime <= minActiveTime {
			continue
		}

		result = append(result, transcribedChannel{
			frequency:  current.frequency,
			wpm:        current.wpm,
			snr:        current.snr,
			characters: current.characters,
			activeTime: activeTime,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].frequency < result[j].frequency })

	return result
}

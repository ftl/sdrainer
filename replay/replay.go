package replay

import (
	"context"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/iq"
	"github.com/ftl/sdrainer/pipeline"
)

const (
	// chunkSize is the count of the IQ samples of one call of IQData. It is the size that a real
	// SDR uses, see section "Assumed input" of doc/research_pipeline.md.
	chunkSize = 2048
)

type (
	ChannelService = core.ChannelService[float64]
	Spotter        = core.Spotter[float64]
)

type Options struct {
	Filename        string
	SampleRate      int
	CenterFrequency float64
	PeakThreshold   float64
	Contest         string

	// Realtime gives the chunks with the timing of the recording. Without it the replay runs as
	// fast as the machine allows, which is what a test needs.
	Realtime bool
}

func config(options Options) pipeline.Config[float64] {
	result := pipeline.DefaultConfig(options.SampleRate, options.CenterFrequency)
	result.PeakThreshold = options.PeakThreshold
	result.Contest = options.Contest
	return result
}

// Run gives a recording to the pipeline and writes what the pipeline made of it. It comes back when
// the file is complete or when the context ends.
func Run(ctx context.Context, options Options, scope core.ScopeService, channelService ChannelService, spotter Spotter, recorder *iq.Writer) error {
	reader, err := iq.NewReader(options.Filename)
	if err != nil {
		return err
	}
	defer reader.Close()

	seconds := float64(reader.IQSamples()) / float64(options.SampleRate)
	fmt.Printf("replaying %s: %.1f s at %d Hz, center %.3f kHz\n",
		options.Filename, seconds, options.SampleRate, options.CenterFrequency/1000)

	collector := newCollector()
	p := pipeline.New[float32, float64](config(options), scope)
	if recorder != nil {
		// a nil pointer in the interface would not be nil
		p.SetRecorder(recorder)
	}
	p.SetSpotter(spotter)
	p.Notify(channelService)
	p.Notify(collector)

	p.Start()
	err = feed(ctx, p, reader, options)
	p.Stop()
	if err != nil {
		return err
	}

	collector.print()

	return nil
}

// feed gives the samples of the recording to the pipeline.
func feed(ctx context.Context, p *pipeline.Pipeline[float32, float64], reader *iq.Reader, options Options) error {
	chunk := make([]float32, 2*chunkSize)

	var ticker *time.Ticker
	if options.Realtime {
		ticker = time.NewTicker(time.Duration(chunkSize) * time.Second / time.Duration(options.SampleRate))
		defer ticker.Stop()
	}

	for {
		if ticker != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		} else {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
		}

		count, err := reader.ReadIQ(chunk)
		if count > 0 {
			p.IQData(options.SampleRate, chunk[:count])
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("cannot read the IQ file: %w", err)
		}
	}
}

// collector holds what the pipeline made of the recording, so that Run writes it at the end. A
// human uses this list to find the signals that are worth a transcription.
type collector struct {
	mutex     sync.Mutex
	order     []core.ChannelID
	frequency map[core.ChannelID]float64
	wpm       map[core.ChannelID]int
	snr       map[core.ChannelID]float64
	text      map[core.ChannelID][]rune
	callsign  map[core.ChannelID]string
}

func newCollector() *collector {
	return &collector{
		frequency: make(map[core.ChannelID]float64),
		wpm:       make(map[core.ChannelID]int),
		snr:       make(map[core.ChannelID]float64),
		text:      make(map[core.ChannelID][]rune),
		callsign:  make(map[core.ChannelID]string),
	}
}

func (c *collector) ChannelCreated(channel core.Channel[float64]) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.order = append(c.order, channel.ID)
	c.frequency[channel.ID] = channel.Frequency
	c.snr[channel.ID] = channel.SNR
}

func (c *collector) ChannelDestroyed(core.Channel[float64]) {}

func (c *collector) ChannelCharacterReceived(channel core.Channel[float64], character rune, _ int64) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.text[channel.ID] = append(c.text[channel.ID], character)
	c.frequency[channel.ID] = channel.Frequency
	c.snr[channel.ID] = channel.SNR
	if channel.WPM > 0 {
		c.wpm[channel.ID] = channel.WPM
	}
}

func (c *collector) ChannelRunningCallsignDetected(channel core.Channel[float64]) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.callsign[channel.ID] = channel.Callsign.String()
}

// print writes one line for each channel that gave text, the lowest frequency first. The offset is
// the value that the listen command and the name of a transcription use.
func (c *collector) print() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	ids := make([]core.ChannelID, 0, len(c.order))
	for _, id := range c.order {
		if len(c.text[id]) > 0 {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return c.frequency[ids[i]] < c.frequency[ids[j]] })

	fmt.Printf("\n%d channels with text, of %d channels:\n", len(ids), len(c.order))
	for _, id := range ids {
		fmt.Printf("%+8.0f Hz  %2d wpm  %5.1f dB  %-10s %q\n",
			c.frequency[id], c.wpm[id], c.snr[id], c.callsign[id], strings.TrimSpace(string(c.text[id])))
	}
	if len(ids) == 0 {
		log.Print("no channel gave text: is the sample rate correct?")
	}
}

package cw

import (
	"io"
	"log"
	"math"
	"time"

	"github.com/ftl/sdrainer/dsp"
	"github.com/ftl/sdrainer/scope"
)

const (
	scopeAudio = "audio"

	defaultBufferSize        = 1024 * 1024 // 1k
	defaultDebounceThreshold = 3
	defaultMaxScale          = 12
)

type AudioDemodulator struct {
	filter       *dsp.Goertzel
	debouncer    *dsp.BoolDebouncer
	decoder      *Decoder
	maxScale     float64
	scale        float32
	channelCount int

	in     chan float32
	op     chan func()
	close  chan struct{}
	closed chan struct{}

	scope scope.Scope
}

func NewAudioDemodulator(out io.Writer, pitch float64, sampleRate int, bufferSize int) *AudioDemodulator {
	if bufferSize == 0 {
		bufferSize = defaultBufferSize
	}
	result := &AudioDemodulator{
		filter:       dsp.NewDefaultGoertzel(pitch, sampleRate),
		debouncer:    dsp.NewBoolDebouncer(defaultDebounceThreshold),
		maxScale:     defaultMaxScale,
		scale:        1,
		channelCount: 1,
		in:           make(chan float32, bufferSize),
		op:           make(chan func()),
		close:        make(chan struct{}),
		closed:       make(chan struct{}),
		scope:        scope.NewNullScope(),
	}
	result.decoder = NewDecoder(out, sampleRate, result.filter.Blocksize())

	go result.run()

	return result
}

func (d *AudioDemodulator) Close() {
	select {
	case <-d.close:
		return
	default:
		close(d.close)
		<-d.closed
	}
}

func (d *AudioDemodulator) SetScope(scope scope.Scope) {
	d.do(func() {
		d.scope = scope
		d.decoder.SetScope(scope)
	})
}

func (d *AudioDemodulator) SetMaxScale(scale float64) {
	d.do(func() {
		d.maxScale = scale
	})
}

func (d *AudioDemodulator) MaxScale() float64 {
	var result float64
	d.do(func() {
		result = d.maxScale
	})
	return result
}

func (d *AudioDemodulator) SetScale(scale float64) {
	d.do(func() {
		d.scale = float32(scale)
	})
}

func (d *AudioDemodulator) SetChannelCount(channelCount int) {
	d.do(func() {
		d.channelCount = channelCount
	})
}

func (d *AudioDemodulator) SetDebounceThreshold(threshold int) {
	d.do(func() {
		d.debouncer.SetThreshold(threshold)
	})
}

func (d *AudioDemodulator) DebounceThreshold() int {
	var result int
	d.do(func() {
		result = d.debouncer.Threshold()
	})
	return result
}

func (d *AudioDemodulator) PresetWPM(wpm int) {
	d.do(func() {
		d.decoder.presetWPM(wpm)
	})
}

func (d *AudioDemodulator) WPM() int {
	var result int
	d.do(func() {
		result = int(math.Round(d.decoder.wpm))
	})
	return result
}

func (d *AudioDemodulator) SetMagnitudeThreshold(threshold float64) {
	d.do(func() {
		d.filter.SetMagnitudeThreshold(threshold)
	})
}

func (d *AudioDemodulator) MagnitudeThreshold() float64 {
	var result float64
	d.do(func() {
		result = d.filter.MagnitudeThreshold()
	})
	return result
}

func (d *AudioDemodulator) Blocksize() int {
	return d.filter.Blocksize()
}

func (d *AudioDemodulator) Write(buf []float32) (int, error) {
	n := 0
	for i, sample := range buf {
		if (i % d.channelCount) == 0 {
			d.in <- sample
		}
		n++
	}
	return n, nil
}

func (d *AudioDemodulator) do(f func()) {
	select {
	case <-d.closed:
		return
	default:
		d.op <- f
	}
}

func (d *AudioDemodulator) run() {
	defer close(d.closed)
	blocksize := d.filter.Blocksize()
	block := make(dsp.FilterBlock, 0)

	for {
		select {
		case op := <-d.op:
			op()
		case sample := <-d.in:
			block = append(block, sample)
			if len(block) < blocksize {
				continue
			}

			scale := d.scale
			if scale == 0 {
				max := block.Max()
				scale = float32(math.Min(1/float64(max), d.maxScale))
			}
			if scale != 1 {
				for i := range block {
					block[i] = truncate(block[i] * scale)
				}
			}

			magnitude, state, _, err := d.filter.Detect(block)
			if err != nil {
				log.Printf("cannot detect signal: %v", err)
				continue
			}
			block = block[:0]

			debounced := d.debouncer.Debounce(state)
			d.decoder.Tick(debounced)

			d.scopeAudio(d.filter.MagnitudeThreshold(), magnitude, state, debounced)
		case <-d.close:
			d.decoder.stop()
			return
		}
	}
}

func truncate(value float32) float32 {
	if value > 1 {
		return 1
	} else if value < -1 {
		return -1
	} else {
		return value
	}
}

func (d *AudioDemodulator) scopeAudio(magnitudeThreshold float64, magnitude float64, state bool, debounced bool) {
	if !d.scope.Active() {
		return
	}

	stateInt := 0
	if state {
		stateInt = 1
	}
	debouncedInt := 0
	if debounced {
		debouncedInt = 1
	}

	d.scope.ShowTimeFrame(&scope.TimeFrame{
		Frame: scope.Frame{
			Stream:    scopeAudio,
			Timestamp: time.Now(),
		},
		Values: map[scope.ChannelID]float64{
			"magnitude_threshold": magnitudeThreshold * 50,
			"magnitude":           magnitude * 50,
			"state":               float64(stateInt) * 30,
			"debounced":           float64(debouncedInt) * 40,
		},
	})
}

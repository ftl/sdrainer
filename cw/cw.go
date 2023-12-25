package cw

import (
	"io"
	"log"
	"math"
	"time"

	"github.com/ftl/sdrainer/dsp"
)

const (
	defaultBufferSize        = 1024 * 1024 // 1k
	defaultDebounceThreshold = 3
	defaultMaxScale          = 12
)

type Clock interface {
	Now() time.Time
}

type Decoder struct {
	clock        *manualClock
	filter       *dsp.Goertzel
	debouncer    *dsp.BoolDebouncer
	demodulator  *demodulator
	maxScale     float64
	scale        float32
	channelCount int

	in     chan float32
	op     chan func()
	close  chan struct{}
	closed chan struct{}
}

func NewDecoder(out io.Writer, pitch float64, sampleRate int, bufferSize int) *Decoder {
	if bufferSize == 0 {
		bufferSize = defaultBufferSize
	}
	clock := &manualClock{now: time.Now()}
	result := &Decoder{
		clock:        clock,
		filter:       dsp.NewDefaultGoertzel(pitch, sampleRate),
		debouncer:    dsp.NewBoolDebouncer(defaultDebounceThreshold),
		demodulator:  newDemodulator(out, clock),
		maxScale:     defaultMaxScale,
		scale:        1,
		channelCount: 1,
		in:           make(chan float32, bufferSize),
		op:           make(chan func()),
		close:        make(chan struct{}),
		closed:       make(chan struct{}),
	}

	go result.run()

	return result
}

func (d *Decoder) Close() {
	select {
	case <-d.close:
		return
	default:
		close(d.close)
		<-d.closed
	}
}

func (d *Decoder) SetMaxScale(scale float64) {
	d.do(func() {
		d.maxScale = scale
	})
}

func (d *Decoder) MaxScale() float64 {
	var result float64
	d.do(func() {
		result = d.maxScale
	})
	return result
}

func (d *Decoder) SetScale(scale float64) {
	d.do(func() {
		d.scale = float32(scale)
	})
}

func (d *Decoder) SetChannelCount(channelCount int) {
	d.do(func() {
		d.channelCount = channelCount
	})
}

func (d *Decoder) SetDebounceThreshold(threshold int) {
	d.do(func() {
		d.debouncer.SetThreshold(threshold)
	})
}

func (d *Decoder) DebounceThreshold() int {
	var result int
	d.do(func() {
		result = d.debouncer.Threshold()
	})
	return result
}

func (d *Decoder) PresetWPM(wpm int) {
	d.do(func() {
		d.demodulator.presetWPM(wpm)
	})
}

func (d *Decoder) WPM() int {
	var result int
	d.do(func() {
		result = int(math.Round(d.demodulator.wpm))
	})
	return result
}

func (d *Decoder) SetMagnitudeThreshold(threshold float64) {
	d.do(func() {
		d.filter.SetMagnitudeThreshold(threshold)
	})
}

func (d *Decoder) MagnitudeThreshold() float64 {
	var result float64
	d.do(func() {
		result = d.filter.MagnitudeThreshold()
	})
	return result
}

func (d *Decoder) Blocksize() int {
	return d.filter.Blocksize()
}

func (d *Decoder) Write(buf []float32) (int, error) {
	n := 0
	for i, sample := range buf {
		if (i % d.channelCount) == 0 {
			d.in <- sample
		}
		n++
	}
	return n, nil
}

func (d *Decoder) do(f func()) {
	select {
	case <-d.closed:
		return
	default:
		d.op <- f
	}
}

func (d *Decoder) run() {
	defer close(d.closed)
	blocksize := d.filter.Blocksize()
	tick := d.filter.Tick()
	block := make(dsp.FilterBlock, 0)

	// f, err := os.Create("stream.csv")
	// if err != nil {
	// 	log.Printf("cannot open stream file: %v", err)
	// 	return
	// }
	// defer f.Close()

	for {
		select {
		case op := <-d.op:
			op()
		case sample := <-d.in:
			// _, err := fmt.Fprintf(f, "%f\n", sample)
			// if err != nil {
			// 	log.Printf("cannot write stream file: %v", err)
			// }

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

			// for _, smpl := range block {
			// 	_, err := fmt.Fprintf(f, "%f\n", smpl)
			// 	if err != nil {
			// 		log.Printf("cannot write stream file: %v", err)
			// 	}
			// }

			d.clock.Add(tick)

			magnitude, state, _, err := d.filter.Detect(block)
			if err != nil {
				log.Printf("cannot detect signal: %v", err)
				continue
			}
			_ = magnitude

			stateInt := 0
			_ = stateInt
			if state {
				stateInt = 1
			}

			block = block[:0]

			debounced := d.debouncer.Debounce(state)
			_ = debounced
			debouncedInt := 0
			_ = debouncedInt
			if debounced {
				debouncedInt = 1
			}
			// _, err := fmt.Fprintf(f, "%f;%d;%d\n", magnitude, stateInt, debouncedInt)
			// if err != nil {
			// 	log.Printf("cannot write stream file: %v", err)
			// }

			d.demodulator.tick(debounced)
		case <-d.close:
			d.demodulator.stop()
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

type manualClock struct {
	now time.Time
}

func (c *manualClock) Now() time.Time {
	return c.now
}

func (c *manualClock) Add(d time.Duration) {
	c.now = c.now.Add(d)
}

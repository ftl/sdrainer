package cw

import (
	"io"
	"log"
	"math"

	"github.com/ftl/sdrainer/dsp"
)

const (
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

	// f, err := os.Create("frame.csv")
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

			d.decoder.Tick(debounced)
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

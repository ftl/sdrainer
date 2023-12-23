package cw

import (
	"io"
	"time"
)

const defaultBufferSize = 1024 * 1024 // 1k
type Clock interface {
	Now() time.Time
}

type Decoder struct {
	filter      *filter
	demodulator *demodulator

	in     chan float32
	close  chan struct{}
	closed chan struct{}
}

func NewDecoder(out io.Writer, clock Clock, pitch float64, sampleRate int, bufferSize int) *Decoder {
	if bufferSize == 0 {
		bufferSize = defaultBufferSize
	}
	result := &Decoder{
		filter:      newFilter(pitch, sampleRate),
		demodulator: newDemodulator(out, clock),
		in:          make(chan float32, bufferSize),
		close:       make(chan struct{}),
		closed:      make(chan struct{}),
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

func (d *Decoder) Write(buf []float32) (int, error) {
	n := 0
	for _, sample := range buf {
		d.in <- sample
		n++
	}
	return n, nil
}

func (d *Decoder) run() {
	defer close(d.closed)
	block := make(filterBlock, 0, d.filter.blocksize)
	lastScale := float32(1.0)
	_ = lastScale
	for {
		select {
		case sample := <-d.in:
			block = append(block, sample)
			if len(block) < d.filter.blocksize {
				continue
			}
			// max := filterBlock(block).max()
			// if max == 0 {
			// 	max = 0.1
			// }
			// scale := ((1.0/max)/2 + lastScale + lastScale) / 3
			// lastScale = scale
			// fmt.Println(scale, max, scale*max)
			// for i := range block {
			// 	block[i] *= scale
			// }

			state := d.filter.signalState(block)
			magnitude := d.filter.normalizedMagnitude(block)
			_ = magnitude
			// fmt.Println(state, magnitude)
			// state, n, err := d.filter.Detect(block)
			// if err != nil {
			// 	log.Printf("unable to detect signal: %v", err)
			// 	continue
			// }
			// if n != d.filter.blocksize {
			// 	log.Printf("filter not using the blocksize: %d != %d", n, d.filter.blocksize)
			// 	continue
			// }
			block = block[:0]

			d.demodulator.tick(state)
		case <-d.close:
			d.demodulator.stop()
			return
		}
	}
}

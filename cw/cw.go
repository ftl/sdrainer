package cw

import "fmt"

const defaultBufferSize = 1024 * 1024 // 1k

type Decoder struct {
	in     chan float32
	closed chan struct{}
}

func NewDecoder(bufferSize int) *Decoder {
	if bufferSize == 0 {
		bufferSize = defaultBufferSize
	}
	result := &Decoder{
		in:     make(chan float32, bufferSize),
		closed: make(chan struct{}),
	}

	go result.run()

	return result
}

func (d *Decoder) Close() {
	select {
	case <-d.closed:
		return
	default:
		close(d.closed)
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
	for {
		select {
		case sample := <-d.in:
			_ = sample
			fmt.Println(sample)
		case <-d.closed:
			return
		}
	}
}

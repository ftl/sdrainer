package cw

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/ftl/digimodes/cw"
	"github.com/ftl/patrix/osc"
	"github.com/ftl/sdrainer/dsp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecoder(t *testing.T) {
	sampleRate := 48000
	pitch := 700.0
	wpm := 20
	text := "hello world"

	oscillator := osc.New(sampleRate)
	modulator := cw.NewModulator(pitch, wpm)
	defer modulator.Close()
	oscillator.Modulator = modulator

	filter := dsp.NewDefaultGoertzel(pitch, sampleRate)
	require.True(t, filter.Blocksize() > 0)

	blockTick := time.Duration(float64(filter.Blocksize()) / float64(sampleRate) * float64(time.Second))
	require.True(t, blockTick > 0)

	buffer := bytes.NewBuffer([]byte{})
	decoder := NewDecoder(buffer, sampleRate, filter.Blocksize())

	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)

		block := make(dsp.FilterBlock, filter.Blocksize())
		for {
			select {
			case <-stop:
				return
			default:
				n, err := oscillator.Synth32(block)
				require.NoError(t, err)
				require.Equal(t, filter.Blocksize(), n)

				_, state, n, err := filter.Detect(block)
				require.NoError(t, err)
				require.Equal(t, filter.Blocksize(), n)

				decoder.Tick(state)
			}
		}
	}()

	_, err := fmt.Fprintln(modulator, text)
	require.NoError(t, err)

	close(stop)
	<-stopped
	decoder.stop()
	assert.Equal(t, text, buffer.String())
}

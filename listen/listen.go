// Package listen makes an audio file of one CW signal of a recorded IQ stream, so that a human can
// transcribe it. doc/architecture.md, section 9, gives the concept and the chain.
package listen

import (
	"fmt"
	"io"
	"math"

	"github.com/ftl/sdrainer/dsp"
	"github.com/ftl/sdrainer/iq"
)

const (
	// DefaultPitch is the frequency of the tone of the demodulated signal. 600 Hz is the usual
	// sidetone of a CW operator.
	DefaultPitch = 600.0

	// cwBandwidth is the width of the filter that takes one signal out of the stream. The keying
	// sidebands of a CW signal reach approximately 47 Hz at 56 WPM, so 300 Hz holds each signal
	// that SDRainer decodes, and it is the width that an operator uses.
	cwBandwidth = 300.0

	// transitionWidth is the width between the passband and the stopband of the filter. It decides
	// the count of the taps, see dsp.NewLowPass.
	transitionWidth = 100.0

	// targetLevel is the level of the loudest sample of the audio, thus −3 dBFS.
	targetLevel = 0.708

	// chunkSize is the count of the IQ samples of one read.
	chunkSize = 4096
)

type Options struct {
	IQFilename   string
	SampleRate   int
	SignalOffset float64
	OutFilename  string
	Pitch        float64
}

// Run takes the CW signal at the offset of the options and writes it as a WAV file.
func Run(options Options) error {
	if options.Pitch <= 0 {
		options.Pitch = DefaultPitch
	}
	if err := validate(options); err != nil {
		return err
	}

	// The first pass gives the loudest sample of the whole file, and the second pass scales the
	// audio with it. A recording of a weak signal is then still audible, and no sample clips. Two
	// passes are possible because a file is not a live stream.
	peak, count, err := walk(options, func(float64) {})
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("the IQ file %s holds no sample", options.IQFilename)
	}

	scale := targetLevel
	if peak > 0 {
		scale = targetLevel / peak
	}

	out, err := newWAVWriter(options.OutFilename, options.SampleRate, count)
	if err != nil {
		return err
	}
	defer out.Close()

	var writeErr error
	_, _, err = walk(options, func(value float64) {
		if writeErr != nil {
			return
		}
		writeErr = out.write(toPCM(value * scale))
	})
	if err != nil {
		return err
	}
	if writeErr != nil {
		return fmt.Errorf("cannot write the WAV file: %w", writeErr)
	}

	fmt.Printf("%s: %.1f s of the signal at %+.0f Hz, at a pitch of %.0f Hz\n",
		options.OutFilename, float64(count)/float64(options.SampleRate), options.SignalOffset, options.Pitch)

	return out.Close()
}

func validate(options Options) error {
	if options.SampleRate <= 0 {
		return fmt.Errorf("the sample rate must be above 0")
	}
	limit := float64(options.SampleRate) / 2
	if math.Abs(options.SignalOffset) >= limit {
		return fmt.Errorf("the offset %+.0f Hz is outside the stream of %d Hz, which holds %+.0f Hz to %+.0f Hz",
			options.SignalOffset, options.SampleRate, -limit, limit)
	}
	if options.Pitch >= limit {
		return fmt.Errorf("the pitch %.0f Hz is above one half of the sample rate", options.Pitch)
	}
	if options.OutFilename == "" {
		return fmt.Errorf("no name for the WAV file")
	}
	return nil
}

// demodulator is the chain of a CW receiver: it moves the wanted signal to 0 Hz, it removes each
// other signal, and it moves what is left to the pitch of a sidetone.
type demodulator struct {
	down    *dsp.NCO
	up      *dsp.NCO
	filterI *dsp.LowPass
	filterQ *dsp.LowPass
}

func newDemodulator(options Options) *demodulator {
	return &demodulator{
		down:    dsp.NewNCO(-options.SignalOffset, options.SampleRate),
		up:      dsp.NewNCO(options.Pitch, options.SampleRate),
		filterI: dsp.NewLowPass(cwBandwidth/2, transitionWidth, options.SampleRate),
		filterQ: dsp.NewLowPass(cwBandwidth/2, transitionWidth, options.SampleRate),
	}
}

// next takes one IQ sample and gives one sample of audio.
func (d *demodulator) next(inPhase float64, quadrature float64) float64 {
	i, q := d.down.Mix(inPhase, quadrature)
	i, q = d.filterI.Filter(i), d.filterQ.Filter(q)
	audio, _ := d.up.Mix(i, q)
	return audio
}

// walk takes each IQ sample of the file through the chain and gives each sample of audio to the
// sink. It gives the loudest sample and the count of the samples.
func walk(options Options, sink func(float64)) (float64, int, error) {
	reader, err := iq.NewReader(options.IQFilename)
	if err != nil {
		return 0, 0, err
	}
	defer reader.Close()

	demodulator := newDemodulator(options)
	chunk := make([]float32, 2*chunkSize)

	var peak float64
	var count int
	for {
		read, err := reader.ReadIQ(chunk)
		for i := 0; i+1 < read; i += 2 {
			audio := demodulator.next(float64(chunk[i]), float64(chunk[i+1]))
			peak = math.Max(peak, math.Abs(audio))
			count++
			sink(audio)
		}

		if err == io.EOF {
			return peak, count, nil
		}
		if err != nil {
			return peak, count, fmt.Errorf("cannot read the IQ file: %w", err)
		}
	}
}

// toPCM makes a value of 16 bit out of a value between −1 and 1. A value outside that range would
// turn around without the limit.
func toPCM(value float64) int16 {
	result := math.Round(value * math.MaxInt16)
	if result > math.MaxInt16 {
		return math.MaxInt16
	}
	if result < math.MinInt16 {
		return math.MinInt16
	}
	return int16(result)
}

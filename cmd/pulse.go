package cmd

import (
	"context"
	"log"
	"os"

	"github.com/ftl/sdrainer/cw"
	"github.com/jfreymuth/pulse"
	"github.com/spf13/cobra"
)

var pulseFlags = struct {
	source string
	pitch  int

	scale              float64
	debounceThreshold  int
	magnitudeThreshold float64
	wpm                int
}{}

var pulseCmd = &cobra.Command{
	Use:   "pulse",
	Short: "decode CW from a Pulseaudio source",
	Run:   runWithCtx(runPulse),
}

func init() {
	rootCmd.AddCommand(pulseCmd)

	pulseCmd.Flags().StringVar(&pulseFlags.source, "source", "", "Pulseaudio source ID to use")
	pulseCmd.Flags().IntVar(&pulseFlags.pitch, "pitch", 700, "pitch in Hz")

	pulseCmd.Flags().Float64Var(&pulseFlags.scale, "scale", 0, "scale the audio signal (0 = autoscale, 1 = no scaling)")
	pulseCmd.Flags().IntVar(&pulseFlags.debounceThreshold, "debounce", 2, "debounce threshold for the signal detector")
	pulseCmd.Flags().Float64Var(&pulseFlags.magnitudeThreshold, "magnitude", 0.75, "magnitude threshold for the signal detector")
	pulseCmd.Flags().IntVar(&pulseFlags.wpm, "wpm", 20, "preset speed in WpM")
}

func runPulse(ctx context.Context, cmd *cobra.Command, args []string) {
	client, err := pulse.NewClient(pulse.ClientApplicationName("SDRainer"))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	var source *pulse.Source
	if pulseFlags.source == "" {
		source, err = client.DefaultSource()
	} else {
		source, err = client.SourceByID(pulseFlags.source)
	}
	if err != nil {
		log.Fatal(err)
	}

	demodulator := cw.NewAudioDemodulator(os.Stdout, 700, source.SampleRate(), 0)
	defer demodulator.Close()
	demodulator.SetScale(pulseFlags.scale)
	demodulator.SetDebounceThreshold(pulseFlags.debounceThreshold)
	demodulator.SetMagnitudeThreshold(pulseFlags.magnitudeThreshold)

	stream, err := client.NewRecord(pulse.Float32Writer(demodulator.Write), pulse.RecordBufferFragmentSize(2*uint32(demodulator.Blocksize())))
	if err != nil {
		log.Fatal(err)
	}
	demodulator.SetChannelCount(stream.Channels())

	stream.Start()
	<-ctx.Done()
	stream.Stop()
}

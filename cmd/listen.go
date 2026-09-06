package cmd

import (
	"github.com/ftl/sdrainer/listen"
	"github.com/spf13/cobra"
)

var listenFlags = struct {
	iqFilename   string
	iqSampleRate int
	signalOffset float64
	outFilename  string
	pitch        float64
}{}

var listenCmd = &cobra.Command{
	Use:   "listen",
	Short: "EXPERIMENTAL: make an audio file of one CW signal of a recorded IQ stream",
	Long: "Make an audio file of one CW signal of a recorded IQ stream, to transcribe it by ear.\n\n" +
		"The transcription belongs beside the IQ file, with the name <iq-filename>_<signal-offset>.txt.",
	Run: runListen,
}

func init() {
	rootCmd.AddCommand(listenCmd)

	listenCmd.Flags().StringVar(&listenFlags.iqFilename, "iq-filename", "", "the file with the recorded IQ stream")
	listenCmd.Flags().IntVar(&listenFlags.iqSampleRate, "iq-sample-rate", 12_000, "the sample rate of the recorded IQ stream")
	listenCmd.Flags().Float64Var(&listenFlags.signalOffset, "signal-offset", 0, "the offset of the signal from the center of the recording, in Hz")
	listenCmd.Flags().StringVar(&listenFlags.outFilename, "out-filename", "", "the WAV file for the demodulated signal")
	listenCmd.Flags().Float64Var(&listenFlags.pitch, "pitch", listen.DefaultPitch, "the frequency of the tone of the demodulated CW signal, in Hz")

	listenCmd.MarkFlagRequired("iq-filename")
	listenCmd.MarkFlagRequired("out-filename")
}

func runListen(cmd *cobra.Command, args []string) {
	err := listen.Run(listen.Options{
		IQFilename:   listenFlags.iqFilename,
		SampleRate:   listenFlags.iqSampleRate,
		SignalOffset: listenFlags.signalOffset,
		OutFilename:  listenFlags.outFilename,
		Pitch:        listenFlags.pitch,
	})
	if err != nil {
		fatalTermination(err)
	}
}

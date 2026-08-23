package cmd

import (
	"log"

	"github.com/spf13/cobra"

	"github.com/ftl/sdrainer/prepare"
)

var prepareFlags = struct {
	iqFilename   string
	iqSampleRate int
}{}

var prepareCmd = &cobra.Command{
	Use:   "prepare",
	Short: "EXPERIMENTAL: prepare a transcription session for a recorded IQ stream",
	Long: "Prepare a transcription session for a recorded IQ stream.\n\n" +
		"The command finds each channel of the recording that carries a signal long enough to " +
		"transcribe, and it makes a WAV file and an empty transcription file for each of them, " +
		"beside the recording, with the names <iq-filename>_<offset>.wav and " +
		"<iq-filename>_<offset>.txt. A transcription file that exists already stays as it is.",
	Run: runPrepare,
}

func init() {
	rootCmd.AddCommand(prepareCmd)

	prepareCmd.Flags().StringVar(&prepareFlags.iqFilename, "iq-filename", "", "the file with the recorded IQ stream")
	prepareCmd.Flags().IntVar(&prepareFlags.iqSampleRate, "iq-sample-rate", 12_000, "the sample rate of the recorded IQ stream")

	prepareCmd.MarkFlagRequired("iq-filename")
}

func runPrepare(cmd *cobra.Command, args []string) {
	err := prepare.Run(cmd.Context(), prepare.Options{
		IQFilename: prepareFlags.iqFilename,
		SampleRate: prepareFlags.iqSampleRate,
	})
	if err != nil {
		log.Fatal(err)
	}
}

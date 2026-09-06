package cmd

import (
	"context"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/iq"
	"github.com/ftl/sdrainer/pipeline"
	"github.com/ftl/sdrainer/replay"
	"github.com/spf13/cobra"
)

var replayFlags = struct {
	filename        string
	sampleRate      int
	centerFrequency float64
	threshold       float64
	contest         string
	realtime        bool
}{}

var replayCmd = &cobra.Command{
	Use:   "replay",
	Short: "EXPERIMENTAL: detect and decode CW signals from a recorded IQ stream",
	Run:   runPipeline(runReplay),
}

func init() {
	rootCmd.AddCommand(replayCmd)

	replayCmd.Flags().StringVar(&replayFlags.filename, "iq-filename", "", "the file with the recorded IQ stream")
	replayCmd.Flags().IntVar(&replayFlags.sampleRate, "iq-sample-rate", 12_000, "the sample rate of the recorded IQ stream")
	replayCmd.Flags().Float64Var(&replayFlags.centerFrequency, "center", 0, "the center frequency of the recording, 0 gives each channel as an offset")
	replayCmd.Flags().Float64Var(&replayFlags.threshold, "threshold", pipeline.DefaultPeakThreshold, "the level above the noise floor that makes a peak, in dB")
	replayCmd.Flags().StringVar(&replayFlags.contest, "contest", "", "the additional trigger word of a contest, for example cwt in \"cq cwt test dl1abc\"")
	replayCmd.Flags().BoolVar(&replayFlags.realtime, "realtime", false, "give the samples with the timing of the recording, instead of as fast as possible")

	replayCmd.MarkFlagRequired("iq-filename")
}

func runReplay(ctx context.Context, scope core.ScopeService, channelService replay.ChannelService, spotter replay.Spotter, recorder *iq.Writer, cmd *cobra.Command, args []string) {
	err := replay.Run(ctx, replay.Options{
		Filename:        replayFlags.filename,
		SampleRate:      replayFlags.sampleRate,
		CenterFrequency: replayFlags.centerFrequency,
		PeakThreshold:   replayFlags.threshold,
		Contest:         replayFlags.contest,
		Realtime:        replayFlags.realtime,
	}, scope, channelService, spotter, recorder)
	if err != nil {
		fatalTermination(err)
	}
}

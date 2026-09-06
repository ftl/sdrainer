package cmd

import (
	"context"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/iq"
	"github.com/ftl/sdrainer/kiwi"
	"github.com/ftl/sdrainer/pipeline"
	"github.com/spf13/cobra"
)

var kiwiFlags = struct {
	host            string
	username        string
	password        string
	centerFrequency float64
	threshold       float64
	contest         string
}{}

var kiwiCmd = &cobra.Command{
	Use:   "kiwi",
	Short: "EXPERIMENTAL: detect and decode CW signals from a KiwiSDR IQ stream",
	Run:   runPipeline(runKiwi),
}

func init() {
	rootCmd.AddCommand(kiwiCmd)

	kiwiCmd.Flags().StringVar(&kiwiFlags.host, "host", "localhost:8073", "the KiwiSDR host and port")
	kiwiCmd.Flags().StringVar(&kiwiFlags.username, "username", "", "the KiwiSDR username")
	kiwiCmd.Flags().StringVar(&kiwiFlags.password, "password", "", "the KiwiSDR password")
	kiwiCmd.Flags().Float64Var(&kiwiFlags.centerFrequency, "center", 7_020_000, "the center frequency")
	kiwiCmd.Flags().Float64Var(&kiwiFlags.threshold, "threshold", pipeline.DefaultPeakThreshold, "the level above the noise floor that makes a peak, in dB")
	kiwiCmd.Flags().StringVar(&kiwiFlags.contest, "contest", "", "the additional trigger word of a contest, for example cwt in \"cq cwt test dl1abc\"")
}

func runKiwi(ctx context.Context, scope core.ScopeService, channelService kiwi.ChannelService, spotter kiwi.Spotter, recorder *iq.Writer, cmd *cobra.Command, args []string) {
	process, err := kiwi.New(kiwiFlags.host, kiwiFlags.username, kiwiFlags.password,
		kiwiFlags.centerFrequency, kiwiFlags.threshold, kiwiFlags.contest,
		scope, channelService, spotter, recorder)
	if err != nil {
		fatalTermination(err)
	}

	<-ctx.Done()
	process.Close()
}

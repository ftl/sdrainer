package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/demo"
	"github.com/ftl/sdrainer/iq"
)

var demoFlags = struct {
	centerFrequency float64
	noise           float64
}{}

var demoCmd = &cobra.Command{
	Use:   "demo",
	Short: "run SDRainer with a stream of demo CW signals",
	Run:   runPipeline(runDemo),
}

func init() {
	rootCmd.AddCommand(demoCmd)

	demoCmd.Flags().Float64Var(&demoFlags.centerFrequency, "center", 7_020_000, "the center frequency")
	demoCmd.Flags().Float64Var(&demoFlags.noise, "noise", 0.01, "the level of the white noise in the generated IQ stream")
}

func runDemo(ctx context.Context, scope core.ScopeService, channelService demo.ChannelService, spotter demo.Spotter, recorder *iq.Writer, cmd *cobra.Command, args []string) {
	process, err := demo.New(demoFlags.centerFrequency, demoFlags.noise, demo.DefaultSignals(demoFlags.centerFrequency), scope, channelService, spotter, recorder, rootFlags.debug)
	if err != nil {
		fatalTermination(err)
	}

	<-ctx.Done()
	process.Stop()
}

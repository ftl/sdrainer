package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/iq"
	"github.com/ftl/sdrainer/pipeline"
	"github.com/ftl/sdrainer/tci"
)

var tciFlags = struct {
	host      string
	trx       int
	allTRX    bool
	threshold float64
	contest   string

	showSpots bool

	traceTCI bool
}{}

var tciCmd = &cobra.Command{
	Use:   "tci",
	Short: "detect and decode CW signals from a TCI IQ stream",
	Run:   runPipeline(runTCI),
}

func init() {
	rootCmd.AddCommand(tciCmd)

	tciCmd.Flags().StringVar(&tciFlags.host, "host", "localhost:40001", "the TCI host and port")
	tciCmd.Flags().IntVar(&tciFlags.trx, "trx", 0, "the zero-based index of the TCI trx")
	tciCmd.Flags().BoolVar(&tciFlags.allTRX, "all-trx", false, "decode the IQ stream of each trx of the TCI device, and ignore --trx")
	tciCmd.Flags().Float64Var(&tciFlags.threshold, "threshold", pipeline.DefaultPeakThreshold, "the level above the noise floor that makes a peak, in dB")
	tciCmd.Flags().StringVar(&tciFlags.contest, "contest", "", "the additional trigger word of a contest, for example cwt in \"cq cwt test dl1abc\"")
	tciCmd.Flags().BoolVar(&tciFlags.showSpots, "show-spots", false, "show the spotted callsigns as spots on the TCI device's spectrum display")

	tciCmd.Flags().BoolVar(&tciFlags.traceTCI, "trace-tci", false, "trace the TCI communication on the console")

	tciCmd.Flags().MarkHidden("trace-tci")
}

func runTCI(ctx context.Context, scope core.ScopeService, channelService tci.ChannelService, spotter tci.Spotter, recorder *iq.Writer, cmd *cobra.Command, args []string) {
	process, err := tci.New(tciFlags.host, tciFlags.trx, tciFlags.allTRX, tciFlags.threshold, tciFlags.contest,
		scope, channelService, spotter, recorder, tciFlags.showSpots, tciFlags.traceTCI)
	if err != nil {
		fatalTermination(err)
	}

	<-ctx.Done()
	process.Close()
}

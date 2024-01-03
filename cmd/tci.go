package cmd

import (
	"context"
	"log"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ftl/sdrainer/tci"
	"github.com/ftl/sdrainer/trace"
)

var tciFlags = struct {
	host      string
	trx       int
	mode      string
	threshold int
	debounce  int

	traceTCI     bool
	traceContext string
}{}

var tciCmd = &cobra.Command{
	Use:   "tci",
	Short: "decode CW from a TCI IQ stream",
	Run:   runWithCtx(runTCI),
}

func init() {
	rootCmd.AddCommand(tciCmd)

	tciCmd.Flags().StringVar(&tciFlags.host, "host", "localhost:40001", "the TCI host and port")
	tciCmd.Flags().IntVar(&tciFlags.trx, "trx", 0, "the zero-based index of the TCI trx")
	tciCmd.Flags().StringVar(&tciFlags.mode, "mode", "vfo", "vfo: decode at the frequency of VFO A, random: decode a random signal in the spectrum")
	tciCmd.Flags().IntVar(&tciFlags.threshold, "threshold", 15, "the threshold in dB over noise that a signal must exceed to be detected")
	tciCmd.Flags().IntVar(&tciFlags.debounce, "debounce", 1, "the debounce threshold for the CW signal")

	tciCmd.Flags().BoolVar(&tciFlags.traceTCI, "trace_tci", false, "trace the TCI communication on the console")
	tciCmd.Flags().StringVar(&tciFlags.traceContext, "trace", "", "spectrum | signal | cw")
}

func runTCI(ctx context.Context, cmd *cobra.Command, args []string) {
	process, err := tci.New(tciFlags.host, tciFlags.trx, tci.Mode(strings.ToLower(tciFlags.mode)), tciFlags.traceTCI)
	if err != nil {
		log.Fatal(err)
	}
	// process.SetTracer(NewFileTracer(tciFlags.traceContext, "trace.csv"))
	process.SetTracer(trace.NewUDPTracer(tciFlags.traceContext, "localhost:3536"))
	process.SetThreshold(tciFlags.threshold)

	<-ctx.Done()
	process.Close()
}

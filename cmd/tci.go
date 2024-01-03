package cmd

import (
	"context"
	"log"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ftl/sdrainer/tci"
)

var tciFlags = struct {
	host      string
	trx       int
	mode      string
	threshold int
	debounce  int
	trace     bool
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

	tciCmd.Flags().BoolVar(&tciFlags.trace, "trace", false, "trace the TCI communication on the console")
}

func runTCI(ctx context.Context, cmd *cobra.Command, args []string) {
	process, err := tci.New(tciFlags.host, tciFlags.trx, tci.Mode(strings.ToLower(tciFlags.mode)), tciFlags.trace)
	if err != nil {
		log.Fatal(err)
	}
	// process.SetTracer(NewFileTracer("trace.csv"))
	// process.SetTracer(NewUDPTracer("localhost:3536"))
	process.SetThreshold(tciFlags.threshold)

	<-ctx.Done()
	process.Close()
}

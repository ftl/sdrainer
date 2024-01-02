package cmd

import (
	"context"
	"log"

	"github.com/spf13/cobra"

	"github.com/ftl/sdrainer/tci"
)

var tciFlags = struct {
	host  string
	trx   int
	trace bool
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
	tciCmd.Flags().BoolVar(&tciFlags.trace, "trace", false, "trace the TCI communication on the console")
}

func runTCI(ctx context.Context, cmd *cobra.Command, args []string) {
	process, err := tci.New(tciFlags.host, tciFlags.trx, tciFlags.trace)
	if err != nil {
		log.Fatal(err)
	}

	<-ctx.Done()
	process.Close()
}

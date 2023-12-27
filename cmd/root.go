package cmd

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

const (
	version string = "develop"
	build   string = "-"
)

var rootFlags = struct {
}{}

var rootCmd = &cobra.Command{
	Use:   "sdrainer",
	Short: "SDRainer - combine a pasta strainer with an SDR...",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

func init() {
}

func runWithCtx(f func(ctx context.Context, cmd *cobra.Command, args []string)) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithCancel(context.Background())
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
		go handleCancelation(signals, cancel)

		f(ctx, cmd, args)
	}
}

func handleCancelation(signals <-chan os.Signal, cancel context.CancelFunc) {
	count := 0
	for range signals {
		count++
		if count == 1 {
			cancel()
		} else {
			log.Fatal("hard shutdown")
		}
	}
}

package cmd

import (
	"context"
	"fmt"
	"log"

	"github.com/ftl/sdrainer/kiwi"
	"github.com/ftl/sdrainer/scope"
	"github.com/ftl/sdrainer/telnet"
	"github.com/spf13/cobra"
)

var kiwiFlags = struct {
	host            string
	username        string
	password        string
	centerFrequency float64
	rxFrequency     float64
	bandwidth       int
	threshold       int
}{}

var strainKiwiCmd = &cobra.Command{
	Use:   "kiwi",
	Short: "EXPERIMENTAL: detect and decode CW signals from a KiwiSDR IQ stream",
	Run:   runWithCtx(runStrainKiwi),
}

func init() {
	strainCmd.AddCommand(strainKiwiCmd)

	strainKiwiCmd.Flags().StringVar(&kiwiFlags.host, "host", "localhost:8073", "the KiwiSDR host and port")
	strainKiwiCmd.Flags().StringVar(&kiwiFlags.username, "username", "", "the KiwiSDR username")
	strainKiwiCmd.Flags().StringVar(&kiwiFlags.password, "password", "", "the KiwiSDR password")
	strainKiwiCmd.Flags().Float64Var(&kiwiFlags.centerFrequency, "center", 7_020_000, "the center frequency")
	strainKiwiCmd.Flags().Float64Var(&kiwiFlags.rxFrequency, "rx", 0, "the rx frequency")
	strainKiwiCmd.Flags().IntVar(&kiwiFlags.bandwidth, "bandwidth", 10_000, "the bandwidth that is observed to find CW signals (max 12000)")
}

func runStrainKiwi(ctx context.Context, scope scope.Scope, cmd *cobra.Command, args []string) {
	spotter, err := telnet.NewServer(fmt.Sprintf(":%d", strainFlags.telnetPort), strainFlags.telnetCall, formatVersion())
	if err != nil {
		log.Fatal(err)
	}
	spotter.SetSilencePeriod(strainFlags.spotSilencePeriod)

	process, err := kiwi.New(kiwiFlags.host, kiwiFlags.username, kiwiFlags.password, kiwiFlags.centerFrequency, kiwiFlags.bandwidth, spotter)
	if err != nil {
		log.Fatal(err)
	}
	process.SetThreshold(kiwiFlags.threshold)
	process.SetSignalDebounce(strainFlags.debounce)
	process.SetSilenceTimeout(strainFlags.silenceTimeout)
	process.SetAttachmentTimeout(strainFlags.attachmentTimeout)
	process.SetRXFrequency(kiwiFlags.rxFrequency)
	process.SetScope(scope)

	<-ctx.Done()
	process.Close()
	spotter.Stop()
}

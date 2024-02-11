package cmd

import (
	"context"
	"fmt"
	"log"

	"github.com/spf13/cobra"

	"github.com/ftl/sdrainer/rx"
	"github.com/ftl/sdrainer/tci"
	"github.com/ftl/sdrainer/telnet"
)

var tciFlags = struct {
	host      string
	trx       int
	threshold int

	showListeners bool
	showSpots     bool

	traceTCI bool
}{}

var strainTCICmd = &cobra.Command{
	Use:   "tci",
	Short: "detect and decode CW signals from a TCI IQ stream",
	Run:   runWithCtx(runStrainTCI),
}

var decodeTCICmd = &cobra.Command{
	Use:   "tci",
	Short: "decode a CW signal at the current VFO A frequency of a TCI IQ stream",
	Run:   runWithCtx(runDecodeTCI),
}

func init() {
	strainCmd.AddCommand(strainTCICmd)
	decodeCmd.AddCommand(decodeTCICmd)

	strainTCICmd.Flags().StringVar(&tciFlags.host, "host", "localhost:40001", "the TCI host and port")
	strainTCICmd.Flags().IntVar(&tciFlags.trx, "trx", 0, "the zero-based index of the TCI trx")
	strainTCICmd.Flags().IntVar(&tciFlags.threshold, "threshold", 15, "the threshold in dB over noise that a signal must exceed to be detected")

	strainTCICmd.Flags().BoolVar(&tciFlags.showListeners, "show_listeners", false, "report the listener frequencies as spots over TCI")
	strainTCICmd.Flags().BoolVar(&tciFlags.showSpots, "show_spots", false, "report the spots over TCI")
	strainTCICmd.Flags().BoolVar(&tciFlags.traceTCI, "trace_tci", false, "trace the TCI communication on the console")
	strainTCICmd.Flags().MarkHidden("show_listeners")
	strainTCICmd.Flags().MarkHidden("show_spots")
	strainTCICmd.Flags().MarkHidden("trace_tci")

	decodeTCICmd.Flags().StringVar(&tciFlags.host, "host", "localhost:40001", "the TCI host and port")
	decodeTCICmd.Flags().IntVar(&tciFlags.trx, "trx", 0, "the zero-based index of the TCI trx")
	decodeTCICmd.Flags().IntVar(&tciFlags.threshold, "threshold", 15, "the threshold in dB over noise that a signal must exceed to be detected")

	decodeTCICmd.Flags().BoolVar(&tciFlags.traceTCI, "trace_tci", false, "trace the TCI communication on the console")
	decodeTCICmd.Flags().MarkHidden("trace_tci")
}

func runStrainTCI(ctx context.Context, cmd *cobra.Command, args []string) {
	spotter, err := telnet.NewServer(fmt.Sprintf(":%d", strainFlags.telnetPort), strainFlags.telnetCall, formatVersion())
	if err != nil {
		log.Fatal(err)
	}
	spotter.SetSilencePeriod(strainFlags.spotSilencePeriod)

	process, err := tci.New(tciFlags.host, tciFlags.trx, rx.StrainMode, spotter, tciFlags.traceTCI)
	if err != nil {
		log.Fatal(err)
	}
	process.SetThreshold(tciFlags.threshold)
	process.SetSignalDebounce(strainFlags.debounce)
	process.SetSilenceTimeout(strainFlags.silenceTimeout)
	process.SetAttachmentTimeout(strainFlags.attachmentTimeout)
	process.SetShow(tciFlags.showListeners, tciFlags.showSpots)

	tracer, ok := createTracer()
	if ok {
		log.Printf("set tracer %#v", tracer)
		process.SetTracer(tracer)
	}

	<-ctx.Done()
	process.Close()
	spotter.Stop()
}

func runDecodeTCI(ctx context.Context, cmd *cobra.Command, args []string) {
	process, err := tci.New(tciFlags.host, tciFlags.trx, rx.DecodeMode, nil, tciFlags.traceTCI)
	if err != nil {
		log.Fatal(err)
	}
	process.SetThreshold(tciFlags.threshold)
	process.SetSignalDebounce(decodeFlags.debounce)

	tracer, ok := createTracer()
	if ok {
		log.Printf("set tracer %#v", tracer)
		process.SetTracer(tracer)
	}

	<-ctx.Done()
	process.Close()
}

package cmd

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ftl/sdrainer/rx"
	"github.com/ftl/sdrainer/tci"
	"github.com/ftl/sdrainer/telnet"
)

var tciFlags = struct {
	host              string
	trx               int
	mode              string
	threshold         int
	debounce          int
	silenceTimeout    time.Duration
	attachmentTimeout time.Duration

	telnetPort int
	telnetCall string

	traceTCI bool
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
	tciCmd.Flags().StringVar(&tciFlags.mode, "mode", "vfo", "vfo: decode at the frequency of VFO A, strain: decode all available signals and find callsigns")
	tciCmd.Flags().IntVar(&tciFlags.threshold, "threshold", 15, "the threshold in dB over noise that a signal must exceed to be detected")
	tciCmd.Flags().IntVar(&tciFlags.debounce, "debounce", 1, "the debounce threshold for the CW signal detection")
	tciCmd.Flags().DurationVar(&tciFlags.silenceTimeout, "silence", 10*time.Second, "the time of silence until the next random peak is selected")
	tciCmd.Flags().DurationVar(&tciFlags.attachmentTimeout, "busy", 1*time.Minute, "the time of decoding a busy signal until the next random peak is selected")

	tciCmd.Flags().IntVar(&tciFlags.telnetPort, "telnet_port", 7373, "the port of the telnet cluster interface")
	tciCmd.Flags().StringVar(&tciFlags.telnetCall, "telnet_call", "local-#", "the reporter callsign of the cluster spots")

	tciCmd.Flags().BoolVar(&tciFlags.traceTCI, "trace_tci", false, "trace the TCI communication on the console")
	tciCmd.Flags().MarkHidden("trace_tci")
}

func runTCI(ctx context.Context, cmd *cobra.Command, args []string) {
	spotter, err := telnet.NewServer(fmt.Sprintf(":%d", tciFlags.telnetPort), tciFlags.telnetCall, formatVersion())
	if err != nil {
		log.Fatal(err)
	}

	process, err := tci.New(tciFlags.host, tciFlags.trx, rx.ReceiverMode(strings.ToLower(tciFlags.mode)), spotter, tciFlags.traceTCI)
	if err != nil {
		log.Fatal(err)
	}
	process.SetThreshold(tciFlags.threshold)
	process.SetSilenceTimeout(tciFlags.silenceTimeout)
	process.SetAttachmentTimeout(tciFlags.attachmentTimeout)
	process.SetSignalDebounce(tciFlags.debounce)

	tracer, ok := createTracer()
	if ok {
		log.Printf("set tracer %#v", tracer)
		process.SetTracer(tracer)
	}

	<-ctx.Done()
	process.Close()
	spotter.Stop()
}

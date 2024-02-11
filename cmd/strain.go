package cmd

import (
	"time"

	"github.com/spf13/cobra"
)

var strainFlags = struct {
	debounce          int
	silenceTimeout    time.Duration
	attachmentTimeout time.Duration

	telnetPort        int
	telnetCall        string
	spotSilencePeriod time.Duration
}{}

var strainCmd = &cobra.Command{
	Use:   "strain",
	Short: "detect and decode calling CW signals from an IQ stream",
}

func init() {
	rootCmd.AddCommand(strainCmd)

	strainCmd.PersistentFlags().IntVar(&strainFlags.debounce, "debounce", 1, "the debounce threshold for the CW signal detection")
	strainCmd.PersistentFlags().DurationVar(&strainFlags.silenceTimeout, "silence", 10*time.Second, "the time of silence until the next random peak is selected")
	strainCmd.PersistentFlags().DurationVar(&strainFlags.attachmentTimeout, "busy", 1*time.Minute, "the time of decoding a busy signal until the next random peak is selected")

	strainCmd.PersistentFlags().IntVar(&strainFlags.telnetPort, "telnet_port", 7373, "the port of the telnet cluster interface")
	strainCmd.PersistentFlags().StringVar(&strainFlags.telnetCall, "telnet_call", "local-#", "the reporter callsign of the cluster spots")
	strainCmd.PersistentFlags().DurationVar(&strainFlags.spotSilencePeriod, "spot_every", 1*time.Minute, "the time period after an active callsign is spotted again")
}

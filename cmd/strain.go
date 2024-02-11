package cmd

import (
	"time"

	"github.com/spf13/cobra"
)

var strainFlags = struct {
	debounce          int
	silenceTimeout    time.Duration
	attachmentTimeout time.Duration

	telnetPort int
	telnetCall string
}{}

var strainCmd = &cobra.Command{
	Use:   "strain",
	Short: "detect and decode calling CW signals from an IQ stream",
}

func init() {
	rootCmd.AddCommand(strainCmd)

	strainCmd.Flags().IntVar(&strainFlags.debounce, "debounce", 1, "the debounce threshold for the CW signal detection")
	strainCmd.Flags().DurationVar(&strainFlags.silenceTimeout, "silence", 10*time.Second, "the time of silence until the next random peak is selected")
	strainCmd.Flags().DurationVar(&strainFlags.attachmentTimeout, "busy", 1*time.Minute, "the time of decoding a busy signal until the next random peak is selected")

	strainCmd.Flags().IntVar(&strainFlags.telnetPort, "telnet_port", 7373, "the port of the telnet cluster interface")
	strainCmd.Flags().StringVar(&strainFlags.telnetCall, "telnet_call", "local-#", "the reporter callsign of the cluster spots")
}

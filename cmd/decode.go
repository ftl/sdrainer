package cmd

import "github.com/spf13/cobra"

var decodeFlags = struct {
	debounce int
}{}

var decodeCmd = &cobra.Command{
	Use:   "decode",
	Short: "decode a CW signal at the current VFO frequency",
}

func init() {
	rootCmd.AddCommand(decodeCmd)

	decodeCmd.PersistentFlags().IntVar(&decodeFlags.debounce, "debounce", 1, "the debounce threshold for the CW signal detection")
}

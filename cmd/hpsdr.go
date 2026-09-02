package cmd

import (
	"context"
	"log"

	"github.com/spf13/cobra"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/hpsdr"
	"github.com/ftl/sdrainer/iq"
	"github.com/ftl/sdrainer/pipeline"
)

var hpsdrFlags = struct {
	host       string
	center     []int
	sampleRate int
	threshold  float64
	contest    string
}{}

var hpsdrCmd = &cobra.Command{
	Use:   "hpsdr",
	Short: "EXPERIMENTAL: detect and decode CW signals from an openHPSDR device",
	Long: "Detect and decode CW signals from a device that speaks the openHPSDR protocol 1, for\n" +
		"example a Hermes-Lite 2 or an original openHPSDR device.\n\n" +
		"--center names one frequency for each receiver of the device. With more than one frequency\n" +
		"the command runs one pipeline for each receiver, and the id of a channel then carries the\n" +
		"number of its receiver. Only the first receiver writes to the scope and into a recording.",
	Run: runPipeline(runHPSDR),
}

func init() {
	rootCmd.AddCommand(hpsdrCmd)

	hpsdrCmd.Flags().StringVar(&hpsdrFlags.host, "host", "", "the address of the device, empty looks for one on the local network")
	hpsdrCmd.Flags().IntSliceVar(&hpsdrFlags.center, "center", nil, "the center frequency of each receiver, in Hz, separated by a comma")
	hpsdrCmd.Flags().IntVar(&hpsdrFlags.sampleRate, "sample-rate", 48000, "the sample rate of each receiver: 48000, 96000 or 192000")
	hpsdrCmd.Flags().Float64Var(&hpsdrFlags.threshold, "threshold", pipeline.DefaultPeakThreshold, "the level above the noise floor that makes a peak, in dB")
	hpsdrCmd.Flags().StringVar(&hpsdrFlags.contest, "contest", "", "the additional trigger word of a contest, for example cwt in \"cq cwt test dl1abc\"")

	hpsdrCmd.MarkFlagRequired("center")
}

func runHPSDR(ctx context.Context, scope core.ScopeService, channelService hpsdr.ChannelService, spotter hpsdr.Spotter, recorder *iq.Writer, cmd *cobra.Command, args []string) {
	process, err := hpsdr.New(hpsdr.Options{
		Host:              hpsdrFlags.host,
		CenterFrequencies: hpsdrFlags.center,
		SampleRate:        hpsdrFlags.sampleRate,
		PeakThreshold:     hpsdrFlags.threshold,
		Contest:           hpsdrFlags.contest,
	}, scope, channelService, spotter, recorder)
	if err != nil {
		log.Fatal(err)
	}

	<-ctx.Done()
	process.Close()
}

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jfreymuth/pulse"

	"github.com/ftl/sdrainer/cw"
)

func main() {
	client, err := pulse.NewClient(pulse.ClientApplicationName("SDRainer"))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	source, err := client.DefaultSource()
	if err != nil {
		log.Fatal(err)
	}

	decoder := cw.NewDecoder(os.Stdout, 700, source.SampleRate(), 0)
	defer decoder.Close()
	decoder.SetScale(0) // TODO 0 == autoscale

	stream, err := client.NewRecord(pulse.Float32Writer(decoder.Write), pulse.RecordBufferFragmentSize(2*uint32(decoder.Blocksize())))
	if err != nil {
		log.Fatal(err)
	}
	decoder.SetChannelCount(stream.Channels())

	ctx, cancel := context.WithCancel(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go handleCancelation(signals, cancel, func() error { os.Exit(1); return nil })

	stream.Start()
	<-ctx.Done()
	stream.Stop()
}

func handleCancelation(signals <-chan os.Signal, cancel context.CancelFunc, shutdown func() error) {
	count := 0
	for range signals {
		count++
		if count == 1 {
			cancel()
		} else {
			shutdown()
			log.Fatal("hard shutdown")
		}
	}
}

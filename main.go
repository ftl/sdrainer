package main

import (
	"log"
	"os"
	"time"

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

	decoder := cw.NewDecoder(os.Stdout, new(clock), 700, source.SampleRate(), 0)
	defer decoder.Close()

	stream, err := client.NewRecord(pulse.Float32Writer(decoder.Write))
	if err != nil {
		log.Fatal(err)
	}

	stream.Start()
	time.Sleep(30 * time.Second)
	stream.Stop()
}

type clock struct{}

func (c clock) Now() time.Time {
	return time.Now()
}

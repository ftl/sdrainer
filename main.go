package main

import (
	"log"
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

	decoder := cw.NewDecoder(0)
	defer decoder.Close()

	stream, err := client.NewRecord(pulse.Float32Writer(decoder.Write))
	if err != nil {
		log.Fatal(err)
	}

	stream.Start()
	time.Sleep(10 * time.Second)
	stream.Stop()
}

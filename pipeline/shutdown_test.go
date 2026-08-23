package pipeline

import (
	"sync"
	"testing"
	"time"
)

// shutdownSampleRate and shutdownChunk are the values of a real source: a chunk of 2048 samples at
// 12 kHz, thus one chunk each 171 ms.
const (
	shutdownSampleRate = 12000
	shutdownChunk      = 2048
)

// TestStopWhileASourceGivesSamples covers the way that a real source stops.
//
// A source cannot always stop its stream before it stops the pipeline: the TCI client and the
// KiwiSDR client both give their samples in a goroutine of their own, and a callback that is
// already running continues after the command that stops the stream. Stop closes the channels of
// the frames, and a send on a channel that is closed panics.
//
// The test therefore gives samples in one goroutine while it stops the pipeline in another one.
// Without the lock of Stop it panics with "send on closed channel" in STFTStage.Process.
//
// **One goroutine gives the samples**, as a real source does: the STFT holds the samples that are
// left over in a buffer of its own, so IQData is not safe for two callers at the same time. This
// test measures Stop against IQData, and not IQData against itself.
func TestStopWhileASourceGivesSamples(t *testing.T) {
	for range 20 {
		p := New[float32, float64](DefaultConfig(shutdownSampleRate, 0.0), nil)
		p.Start()

		var running sync.WaitGroup
		running.Add(1)
		done := make(chan struct{})
		go func() {
			defer running.Done()

			samples := make([]float32, 2*shutdownChunk)
			for {
				select {
				case <-done:
					return
				default:
				}
				p.IQData(shutdownSampleRate, samples)
			}
		}()

		// let the source fill the buffer of the frames, so that Stop meets a send that waits
		time.Sleep(time.Millisecond)

		p.Stop()
		close(done)
		running.Wait()
	}
}

// TestIQDataAfterStopDoesNothing is the other half: a callback of a source that arrives after Stop
// must be a quiet no-op, and not a panic.
func TestIQDataAfterStopDoesNothing(t *testing.T) {
	p := New[float32, float64](DefaultConfig(shutdownSampleRate, 0.0), nil)
	p.Start()
	p.Stop()

	p.IQData(shutdownSampleRate, make([]float32, 2*shutdownChunk))
}

// TestStopIsIdempotent covers a source that stops the pipeline more than one time.
func TestStopIsIdempotent(t *testing.T) {
	p := New[float32, float64](DefaultConfig(shutdownSampleRate, 0.0), nil)
	p.Start()

	p.Stop()
	p.Stop()
}

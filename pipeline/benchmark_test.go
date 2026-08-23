package pipeline

import (
	"fmt"
	"testing"
	"time"

	"github.com/ftl/sdrainer/pipeline/generator"
)

// benchmarkSeconds is the audio time that one iteration of a benchmark processes. One second gives
// a value that is easy to read: the metric realtime is how many times faster than real time the
// pipeline works, thus how many streams one core can carry.
const benchmarkSeconds = 1

// benchmarkPipeline sends benchmarkSeconds of the scene through a pipeline for each iteration. The
// generator makes its samples before the measurement, so the benchmark measures the pipeline and
// not the generator.
func benchmarkPipeline(b *testing.B, sampleRate int, maxWPM int) {
	config := rateConfig(sampleRate)
	config.MaxWPM = maxWPM

	source := generator.New[float32, float64](generator.GeneratorConfig[float64]{
		SampleRate: sampleRate, CenterFrequency: rateCenter, NoiseLevel: rateNoiseLevel(sampleRate),
		Seed: 1, Signals: rateSceneSignals(rateCenter),
	})
	chunkSize := sampleRate / 25 // 40 ms, as a sound card or an SDR gives it
	chunks := make([][]float32, benchmarkSeconds*25)
	for i := range chunks {
		chunks[i] = make([]float32, 2*chunkSize)
		source.Read(chunks[i])
	}

	b.ResetTimer()
	for b.Loop() {
		p := New[float32, float64](config, nil)
		p.Start()
		for _, chunk := range chunks {
			p.IQData(sampleRate, chunk)
		}
		p.Stop()
	}
	b.StopTimer()

	// how many times faster than real time one core carries this stream
	seconds := b.Elapsed().Seconds() / float64(b.N)
	b.ReportMetric(float64(benchmarkSeconds)/seconds, "x-realtime")
	b.ReportMetric(seconds*1000, "ms/s-audio")
}

// BenchmarkPipeline measures the whole pipeline, thus the detection tier and the decode tier.
func BenchmarkPipeline(b *testing.B) {
	for _, sampleRate := range supportedSampleRates {
		b.Run(fmt.Sprintf("%dHz", sampleRate), func(b *testing.B) {
			benchmarkPipeline(b, sampleRate, 56)
		})
	}
}

// BenchmarkPipelineDetectionOnly measures the pipeline without the decode tier. The difference to
// BenchmarkPipeline is the cost of the decode tier: its own STFT, and one demodulator for each
// channel and for each frame.
func BenchmarkPipelineDetectionOnly(b *testing.B) {
	for _, sampleRate := range supportedSampleRates {
		b.Run(fmt.Sprintf("%dHz", sampleRate), func(b *testing.B) {
			benchmarkPipeline(b, sampleRate, 0)
		})
	}
}

// BenchmarkSTFT measures the STFT alone, thus the FFT and the window. It shows how much of the load
// of the pipeline comes from the spectral analysis.
func BenchmarkSTFT(b *testing.B) {
	for _, sampleRate := range supportedSampleRates {
		b.Run(fmt.Sprintf("%dHz", sampleRate), func(b *testing.B) {
			derived := Derive(rateConfig(sampleRate))
			samples := make([]float32, 2*benchmarkSeconds*sampleRate)

			framePool := NewFramePool[float32, float64](derived.BlockSize)
			frames := make(chan *SpectralFrame[float32, float64], frameBufferSize)
			done := make(chan struct{})
			go func() {
				defer close(done)
				for frame := range frames {
					framePool.ReturnFrame(frame)
				}
			}()

			b.ResetTimer()
			for b.Loop() {
				stft := NewSTFTStage[float32, float64](framePool, sampleRate, derived.Hop)
				stft.Process(frames, samples)
			}
			b.StopTimer()
			close(frames)
			<-done

			seconds := b.Elapsed().Seconds() / float64(b.N)
			b.ReportMetric(float64(benchmarkSeconds)/seconds, "x-realtime")
			b.ReportMetric(seconds*1000, "ms/s-audio")
		})
	}
}

// The channel capacity of the pipeline has two limits, and the smaller one decides:
//
//   - The **band** holds bandwidth/spacing channels. This is a property of the band and of the
//     operators on it, and not of the code.
//   - The **core** carries (1 s - the cost of the detection) / the cost of one channel. The cost of
//     the detection does not depend on the count of the channels, and the cost of one channel does
//     not depend on the sample rate, because the demodulator of a channel makes one tick for each
//     frame of the decode tier and that frame rate is the same at each sample rate.
const (
	// channelSpacing is a usual distance between two stations in a contest. The pipeline itself
	// separates two channels that are further apart than peakMergeWidthHz, thus 40 Hz, but a band
	// with stations at that distance does not exist.
	channelSpacing = 300

	// usableBandwidth is the part of the band that holds signals. The edges stay free, because the
	// noise floor of the moving median needs bins on both sides of a signal.
	usableBandwidth = 0.8

	// benchmarkChannelSeconds is the audio time for one measurement of the load. It must be longer
	// than the time that the tracker needs to confirm a channel.
	benchmarkChannelSeconds = 6
)

// bandChannels gives the count of the channels that the band holds at channelSpacing.
func bandChannels(sampleRate int) int {
	return int(usableBandwidth * float64(sampleRate) / channelSpacing)
}

// measureChannelLoad gives the time that one core needs for one second of audio, in milliseconds,
// with the given count of signals in the band.
func measureChannelLoad(b *testing.B, sampleRate int, signals int) float64 {
	b.Helper()

	config := rateConfig(sampleRate)
	source := generator.New[float32, float64](generator.GeneratorConfig[float64]{
		SampleRate: sampleRate, CenterFrequency: rateCenter, NoiseLevel: rateNoiseLevel(sampleRate),
		Seed: 1, Signals: manyCWSignals(rateCenter, signals),
	})

	chunkSize := sampleRate / 25
	chunks := make([][]float32, benchmarkChannelSeconds*25)
	for i := range chunks {
		chunks[i] = make([]float32, 2*chunkSize)
		source.Read(chunks[i])
	}

	listener := newSceneTextListener()
	p := New[float32, float64](config, nil)
	p.Notify(listener)

	start := time.Now()
	p.Start()
	for _, chunk := range chunks {
		p.IQData(sampleRate, chunk)
	}
	p.Stop()
	elapsed := time.Since(start)

	if len(listener.frequency) < signals {
		b.Logf("%d Hz: %d of %d signals became a channel", sampleRate, len(listener.frequency), signals)
	}
	return elapsed.Seconds() / benchmarkChannelSeconds * 1000
}

// manyCWSignals gives count CW signals at channelSpacing, around the center.
func manyCWSignals(center float64, count int) []generator.CWSignal[float64] {
	result := make([]generator.CWSignal[float64], 0, count)
	first := center - float64(count-1)*channelSpacing/2
	for i := range count {
		result = append(result, generator.CWSignal[float64]{
			Frequency: first + float64(i)*channelSpacing,
			WPM:       20 + (i%4)*5,
			Amplitude: 0.5,
			Text:      "de dl1abc",
			RiseTime:  5 * time.Millisecond,
		})
	}
	return result
}

// BenchmarkMaxChannels gives the maximum count of the channels for each sample rate. The maximum is
// the smaller value of two limits, and the measurement shows which one decides:
//
//   - the **band** holds bandChannels(sampleRate) channels at channelSpacing,
//   - the **core** carries the channels whose load stays below 1000 ms for each second of audio.
//
// The benchmark fills the band completely and measures the load. It also measures one half of the
// band, so that the cost of one channel in that range comes out.
//
// Do not calculate a maximum from the cost of one channel: the load is **not** linear in the count
// of the channels. Each channel holds a window of the levels of approximately 4 kB, so the working
// set leaves the cache of the CPU above approximately 100 channels, and the cost of one channel
// then grows. A measurement at 96 kHz gives 0.38 ms for each channel between 10 and 100 channels,
// and 0.70 ms between 100 and 200.
//
// One iteration needs several seconds, and the value at 192 kHz needs approximately half a minute:
//
//	go test -run XXX -bench BenchmarkMaxChannels -benchtime 1x -timeout 600s ./pipeline/
func BenchmarkMaxChannels(b *testing.B) {
	for _, sampleRate := range supportedSampleRates {
		b.Run(fmt.Sprintf("%dHz", sampleRate), func(b *testing.B) {
			full := bandChannels(sampleRate)
			half := full / 2

			for b.Loop() {
				halfLoad := measureChannelLoad(b, sampleRate, half)
				fullLoad := measureChannelLoad(b, sampleRate, full)

				b.ReportMetric(float64(full), "channels")
				b.ReportMetric(fullLoad, "ms/s-audio")
				b.ReportMetric(1000/fullLoad, "x-realtime")
				b.ReportMetric((fullLoad-halfLoad)/float64(full-half), "ms/channel")
			}
		})
	}
}

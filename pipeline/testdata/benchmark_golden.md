# Golden Benchmark of the Pipeline

This file holds the values of one measurement. A later measurement can show a change against these
values.

These values are **not** a limit that a test compares. The values depend on the machine, so such a
test fails on a different machine.

## 1. The machine and the commands

The first column gives the property of the machine. The second column gives its value.

| Property | Value |
|---|---|
| CPU | 11th Gen Intel Core i5-1145G7 at 2.60 GHz, 8 threads |
| Go | go1.26.5 linux/amd64 |
| Date | 2026-08-18 |

To measure the load again, use this command. Then take the median of the 3 runs.

```
go test -run XXX -bench 'BenchmarkPipeline$|BenchmarkPipelineDetectionOnly|BenchmarkSTFT' \
    -benchtime 2s -count 3 ./pipeline/
```

To measure the maximum count of the channels again, use this command. Then take the median of the
2 runs. One run needs approximately 1 minute, because it makes 512 CW signals for the widest band.

```
go test -run XXX -bench BenchmarkMaxChannels -benchtime 1x -count 2 -timeout 3600s ./pipeline/
```

A difference of approximately 10 % is the noise of the measurement. A machine that does other work
at the same time gives a larger difference.

## 2. The units

One iteration of a benchmark processes one second of audio. All units below refer to **one core**.

The first column gives the unit. The second column tells you what the unit measures. The third
column shows you how to read a value in that unit.

| Unit | What it measures | How to read it |
|---|---|---|
| `ms/s-audio` | The CPU time in milliseconds that the pipeline needs for one second of audio. | This is the main unit. 39 ms/s-audio means that the pipeline uses 3.9 % of one core for one stream. |
| `x-realtime` | How many times faster than real time the pipeline works. It is `1000 / ms/s-audio`. | 26x means that one core carries 26 streams of this sample rate. |
| `ms/channel` | The CPU time in milliseconds for one second of audio, for **one more** channel. | 0.5 ms/channel means that each new channel adds 0.5 ms to the value in `ms/s-audio`. Read the warning in section 6 before you calculate with this unit. |
| `channels` | The count of the channels that the pipeline decodes at the same time. | Each channel is one CW station. |
| `ns/op` | Go writes this unit for each benchmark. It is the time of one iteration. | Ignore it here. One iteration also includes work that we do not measure, for example the pipeline that the benchmark makes. |

These units come from the signal, and not from the benchmark. The first column gives the unit, and
the second column tells you what it is.

| Unit | What it is |
|---|---|
| `Hz`, `kHz` | The sample rate of the stream, or a frequency in the spectrum. |
| `WPM` | Words each minute. This is the speed of the CW, and PARIS is one word. |
| `block size` | The count of the samples in one FFT of the detection tier. |

## 3. The benchmarks

The first column gives the name of the benchmark. The second column tells you what it measures.

| Benchmark | What it measures |
|---|---|
| `Pipeline` | The complete pipeline: the two STFT tiers, the detection, the tracker, and the decode of 5 channels. |
| `PipelineDetectionOnly` | The same pipeline with `MaxWPM: 0`, which removes the decode tier. The difference to `Pipeline` is the cost of the decode tier. |
| `STFT` | The spectral analysis of the detection tier alone, which is the window and the FFT. |
| `MaxChannels` | The load with a band that is full of channels. Section 6 gives the result. |

## 4. The scene

The scene is the same at each sample rate. It has 5 CW signals inside ±5 kHz, so that it also fits
into the narrowest band of 12 kHz. The speeds are 20 WPM to 40 WPM, and the amplitudes are 1.0 to
0.05.

The **density** of the noise stays the same at each sample rate. The generator makes the noise for
each sample, so the same level of the noise would give a lower density in a wider band.
`pipeline/samplerate_test.go` holds the scene.

## 5. The load

Each column of the table below gives one measurement with 5 channels.

| Column | What it gives |
|---|---|
| Sample rate | The sample rate of the IQ stream. |
| Block size | The count of the samples in one FFT of the detection tier. The pipeline calculates this value from the bin width. |
| Pipeline | The load of the complete pipeline, in `ms/s-audio`. The value in the brackets is `x-realtime`. |
| DetectionOnly | The load of the same pipeline without the decode tier, in `ms/s-audio`. |
| STFT | The load of the spectral analysis of the detection tier alone, in `ms/s-audio`. |
| the decode tier | The difference between the columns Pipeline and DetectionOnly. This is the cost of the decode tier for 5 channels. |

| Sample rate | Block size | Pipeline | DetectionOnly | STFT | the decode tier |
|---|---|---|---|---|---|
| 12 kHz | 1024 | **11.5 ms** (87x) | 10.5 ms (96x) | 1.9 ms (514x) | 1.0 ms |
| 24 kHz | 2048 | **21.9 ms** (46x) | 19.4 ms (52x) | 4.1 ms (246x) | 2.5 ms |
| 48 kHz | 4096 | **39.1 ms** (26x) | 33.9 ms (29x) | 9.2 ms (109x) | 5.2 ms |
| 96 kHz | 8192 | **65.8 ms** (15x) | 51.4 ms (19x) | 17.1 ms (58x) | 14.3 ms |
| 192 kHz | 16384 | **105.4 ms** (9x) | 84.6 ms (12x) | 30.8 ms (32x) | 20.8 ms |

What the table shows:

- **The load grows with the sample rate, but more slowly.** The sample rate grows 16 times from
  12 kHz to 192 kHz, and the load grows 9.2 times. One FFT costs `n·log(n)`, but the count of the
  frames for each second stays the same. The work for each second of audio therefore grows with
  `log(n)`, and not with the width of the band.
- **The decode tier is the smaller part.** It adds 9 % of the load at 12 kHz, and 20 % at 192 kHz,
  with 5 channels. Its cost grows with the count of the channels. The cost of the detection tier
  does not.
- **The STFT of the detection tier is 17 % of the load at 12 kHz, and 29 % at 192 kHz.** The other
  part is the noise floor, the peak search, the tracker and the decode. The part of the STFT grows
  with the sample rate, because one FFT costs `n·log(n)` while the work for each bin grows only
  with `n`.
- **Each supported sample rate works far above real time on one core.** The values are 87 times at
  12 kHz, and 9 times at 192 kHz. These values hold 5 channels. Section 6 gives the values for a
  full band.

## 6. The maximum count of the channels

The maximum count of the channels is the smaller value of two limits:

- the count that the **band** holds, which is `0.8 × sample rate / 300 Hz`,
- the count that one **core** decodes in real time.

`BenchmarkMaxChannels` fills the band completely with CW signals at a distance of 300 Hz, which is a
usual distance in a contest. Then it measures the load.

Each column of the table below gives one property of a band that is full of channels.

| Column | What it gives |
|---|---|
| Sample rate | The sample rate of the IQ stream. |
| Channels of the full band | The count of the channels in the band, at a distance of 300 Hz. |
| Load | The load with that count of the channels, in `ms/s-audio`. |
| x-realtime | The margin against real time with that count of the channels. |
| one channel | The cost of one more channel, in `ms/channel`. The benchmark measures it between one half of the band and the full band. |

| Sample rate | Channels of the full band | Load | x-realtime | one channel |
|---|---|---|---|---|
| 12 kHz | 32 | 23.4 ms | 42.7x | 0.41 ms |
| 24 kHz | 64 | 45.1 ms | 22.2x | 0.63 ms |
| 48 kHz | 128 | 81.8 ms | 12.2x | 0.38 ms |
| 96 kHz | 256 | 177.8 ms | 5.6x | 0.56 ms |
| 192 kHz | 512 | 330.1 ms | 3.0x | 0.50 ms |

**The band gives the limit, and not the core.** One core carries a full band at each supported
sample rate. The margin is 3.0 times at 192 kHz, and 43 times at 12 kHz. A faster CPU therefore does
not increase the maximum count of the channels.

**The cost of one channel does not depend on the sample rate.** It is approximately 0.5 ms for each
second of audio. The demodulator of a channel makes one tick for each frame of the decode tier, and
that frame rate is 187.5 Hz at each sample rate. The cost of the **detection** does depend on the
sample rate, and it is the larger part when there are few channels.

> **Do not calculate a maximum from `ms/channel`.** The load is not linear against the count of the
> channels. Each channel holds a window of the levels of approximately 4 kB, so the data of more
> than approximately 100 channels does not stay in the cache of the CPU. A measurement at 96 kHz
> gives 0.38 ms for each channel between 10 and 100 channels, and 0.70 ms between 100 and 200
> channels. A calculation with the value from a low count gives approximately 2400 channels, and
> that result is wrong.

package pipeline

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/cw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

/*
The tests of this file measure the decode against recordings of real signals.

A fixture is an IQ file in testdata, with the name <name>_<center in kHz>_<sample rate>.iq. The
sample rate is not in the file, so the name holds it: "12k" is 12000. The center frequency in the
name is for a human, because the test replays with a center frequency of 0 and each frequency is
then an offset.

Beside each IQ file lies one text file for each signal that a human transcribed, with the name
<iq-filename>_<offset>.txt. The `sdrainer listen` command makes the WAV file for that work.
doc/architecture.md, section 9, gives the whole way from the recording to the transcription.

**Each line of a transcription is one over**, thus one transmission of one station. A channel holds
the text of each station inside the match width of the tracker, so more than one station stands in
one channel, and their overs come one after the other. The test compares each line for itself
against the text of the channel, and the error rate is the count of the changes of all lines,
divided by the count of the characters of all lines. One line gives the same value as a
transcription without a line break.

A recording holds more signals than transcriptions, and a signal without a transcription is no
failure: an operator transcribes the signals that are worth it.
*/

const (
	// transcriptionTolerance is the distance in Hz from the offset of a transcription to a channel
	// of the pipeline. It is one half of the filter of the listen command (cwBandwidth of the
	// listen package), because a human hears each signal inside that filter.
	transcriptionTolerance = 150.0

	// transcriptionChunkSize is the count of the IQ samples of one call of IQData.
	transcriptionChunkSize = 2048

	// defaultMaxTranscriptionErrorRate holds for a transcription that maxTranscriptionErrorRate does
	// not name. It is loose, and the test writes the measured value, so a new transcription gets its
	// own limit from that number.
	defaultMaxTranscriptionErrorRate = 0.8
)

// maxTranscriptionErrorRate is the limit for each transcription. The limits are **not** a target:
// make them smaller when the decode gets better, and never make one larger without a reason.
//
// The values differ a lot, and one limit for all of them would guard nothing: the signal at
// −2040 Hz is strong and gives 0.22, and the station at −3244 Hz stands in 3 channels.
//
// A measurement over 25 runs on 2026-08-20 gave these values for test_14020_12k.iq. The limits stand approximately 15 %
// above the largest one, because the result of a run is not always the same: the worker takes the
// frames of the two tiers with a select, so the decode of a channel begins at another frame in each
// run.
//
//	transcription   measured     limit  what is left of the error
//	_-2040.txt      0.185–0.222  0.26   the first characters of the transmission
//	_-3244.txt      0.619        0.71   accepted, the signal cannot be decoded correctly
//	_-3954.txt      0.244–0.268  0.31   the copy of a weak signal
//	_0.txt          0.356–0.378  0.43   3 of its 5 overs, see below
//	_196.txt        0.571–0.643  0.74   a weak signal at 10.7 dB
//
// **_-3244.txt is accepted as it is.** The whole error is a space between each two characters, and
// not a wrong character: the decoder gives "o m 1 u m" for "om1um". The operator sends the gap
// between two characters of his callsign so wide that it is a word gap, and 7 ways to correct it
// leave the value at 0.619. Section 12.3 of doc/architecture.md holds each measurement.
//
// Do not try to make this value smaller. A change that moves it moves each other signal too, and
// each of those is worse afterwards.
//
// The values before markDecayTime (section 7.1 of doc/architecture.md) were 0.185–0.222,
// 0.619–0.667, 0.439–0.488, 0.356 and 0.714. The level of the marks that goes down made 3 of the 5
// better, and _0.txt 0.02 worse.
//
// _0.txt holds 5 overs of 3 stations, and the pipeline gives them in one channel. Two of the overs
// are correct without one wrong character:
//
//	over  rate   text
//	1     0.600  "p 599"
//	2     0.500  "599 tu"
//	3     0.000  "tu if9/it9ppg"
//	4     0.625  "cq 8b81jb 8b81jb"
//	5     0.000  "df3na"
//
// A measurement over 15 runs on 2026-08-21 gave these values for the second recording,
// test_14018_12k.iq. Its signals are weaker than the ones of the first recording, and each of them
// stands beside another signal inside the width of a match, so the text of two stations comes into
// one channel:
//
//	transcription   measured     limit  what is left of the error
//	_-1224.txt      0.649–0.676  0.78   two weak stations at 13.1 and 11.7 dB
//	_-2728.txt      0.300–0.350  0.40   the callsign is correct, the text around it is not
//	_-2986.txt      0.341–0.439  0.51   the copy of a station at 10.3 dB
//	_-3315.txt      0.467        0.54   2 of its 4 overs are correct, see below
//	_-3667.txt      0.231        0.27   the best copy of this recording
//	_-4962.txt      0.645–0.710  0.82   a fast station at 36 wpm that fades
//
// _-3315.txt holds 4 overs of 3 stations, and the channel at −3452 Hz gives the best value. Both
// overs of IZ1DXS are correct, and the overs of HB9AVE and F6EYB stand in the other channel at
// −3322 Hz:
//
//	over  rate   text
//	1     0.769  "hb9ave hb9ave"
//	2     0.000  "iz1dxs"
//	3     0.800  "f6eyb"
//	4     0.000  "iz1dxs"
//
// A measurement over 15 runs on 2026-08-21 gave these values for the third recording,
// test_14024_12k.iq. A human listened to each of its channels, and it holds 6 signals that are
// readable. Section 6.3 of doc/architecture.md holds what the other channels of that recording are:
//
//	transcription   measured     limit  what is left of the error
//	_-1056.txt      0.250        0.29   the first characters of the transmission
//	_-45.txt        0.319–0.330  0.38   a fast station at 39 wpm
//	_482.txt        0.444        0.51   the report of a QSO, with numbers
//	_1951.txt       0.711–0.737  0.85   a weak station with a text that is not a call
//	_3457.txt       0.314        0.36   the best copy of this recording
//	_3828.txt       0.625–0.667  0.77   a weak station at 16.4 dB
//
// A measurement over 15 runs on 2026-08-28 gave these values for the fourth recording,
// test_yo-hf-dx_1_48k.iq. It is a recording of a real contest, the YO HF DX contest, over 31 s of
// the 20 m band at a sample rate of 48000, and it is the first fixture that does not use 12000. It
// holds 54 transcriptions, thus more signals than the three recordings before it together, and the
// band is full: a station stands beside a station over the whole spectrum.
//
// The limits stand approximately 15 % above the largest measured value, as for each other fixture.
//
//	_-19218.txt      0.778–0.778  0.89   35 wpm, 19.1 dB
//	_-18438.txt      0.661–0.661  0.76   53 wpm, 10.0 dB
//	_-17466.txt      0.358–0.377  0.43   28 wpm, 18.1 dB
//	_-16474.txt      0.528–0.528  0.61   31 wpm, 27.5 dB
//	_-14599.txt      0.244–0.244  0.28   25 wpm, 22.8 dB
//	_-13875.txt      0.167–0.333  0.38   32 wpm, 20.0 dB
//	_-13084.txt      0.292–0.292  0.34   34 wpm, 20.5 dB
//	_-12472.txt      0.571–0.714  0.82   30 wpm, 16.2 dB
//	_-11578.txt      0.200–0.200  0.23   39 wpm, 30.6 dB
//	_-11573.txt      0.200–0.200  0.23   39 wpm, 30.6 dB
//	_-10666.txt      0.096–0.115  0.13   30 wpm, 33.6 dB
//	_-10264.txt      0.352–0.352  0.40   28 wpm, 37.4 dB
//	_-9571.txt       0.122–0.135  0.16   34 wpm, 16.4 dB
//	_-8474.txt       0.316–0.316  0.48   30 wpm, 10.1 dB, see the outlier below
//	_-7463.txt       0.208–0.333  0.38   25 wpm, 28.2 dB
//	_-7447.txt       0.208–0.333  0.38   25 wpm, 28.2 dB
//	_-7222.txt       0.274–0.290  0.33   28 wpm, 32.6 dB
//	_-6771.txt       0.196–0.196  0.23   28 wpm, 20.4 dB
//	_-5977.txt       0.311–0.333  0.38   23 wpm, 27.0 dB
//	_-5464.txt       0.291–0.309  0.36   34 wpm, 40.0 dB
//	_-4913.txt       0.214–0.214  0.25   29 wpm, 19.6 dB
//	_-3978.txt       0.143–0.161  0.19   34 wpm, 28.2 dB
//	_-3478.txt       0.488–0.488  0.56   26 wpm, 13.3 dB
//	_-3364.txt       0.488–0.488  0.56   26 wpm, 13.3 dB
//	_-2888.txt       0.244–0.267  0.31   24 wpm, 20.8 dB
//	_-2569.txt       0.286–0.286  0.33   29 wpm, 26.7 dB
//	_-1971.txt       0.255–0.277  0.32   30 wpm, 21.9 dB
//	_-1958.txt       0.255–0.277  0.32   30 wpm, 21.9 dB
//	_-1178.txt       0.438–0.438  0.50   33 wpm, 14.0 dB
//	_-442.txt        0.172–0.207  0.24   36 wpm, 18.3 dB
//	_521.txt         0.159–0.175  0.20   30 wpm, 37.1 dB
//	_1198.txt        0.196–0.235  0.27   33 wpm, 32.7 dB
//	_2028.txt        0.164–0.164  0.19   29 wpm, 29.7 dB
//	_4158.txt        0.185–0.204  0.23   31 wpm, 21.6 dB
//	_4928.txt        0.085–0.085  0.10   32 wpm, 23.7 dB
//	_5637.txt        0.305–0.305  0.35   31 wpm, 22.0 dB
//	_6823.txt        0.390–0.390  0.45   32 wpm, 14.3 dB
//	_7277.txt        0.146–0.146  0.17   26 wpm, 10.7 dB
//	_7527.txt        0.231–0.269  0.31   27 wpm, 22.9 dB
//	_8795.txt        0.400–0.440  0.51   29 wpm, 12.0 dB
//	_9527.txt        0.200–0.200  0.29   13 wpm, 24.5 dB, see the outlier below
//	_11323.txt       0.846–0.846  0.97   42 wpm, 10.0 dB
//	_11499.txt       0.415–0.415  0.48   26 wpm, 14.5 dB
//	_12758.txt       0.261–0.283  0.33   26 wpm, 18.7 dB
//	_13532.txt       0.389–0.426  0.49   29 wpm, 14.1 dB
//	_14034.txt       0.328–0.328  0.38   35 wpm, 32.1 dB
//	_14124.txt       0.328–0.328  0.38   35 wpm, 32.1 dB
//	_15028.txt       0.516–0.516  0.59   32 wpm, 34.1 dB
//	_15516.txt       0.429–0.429  0.49   25 wpm, 28.1 dB
//	_16515.txt       0.281–0.281  0.32   27 wpm, 14.6 dB
//	_16544.txt       0.281–0.281  0.32   27 wpm, 14.6 dB
//	_17026.txt       0.452–0.452  0.52   29 wpm, 18.3 dB
//	_17530.txt       0.275–0.353  0.41   30 wpm, 17.9 dB
//	_18981.txt       0.593–0.593  0.68   34 wpm, 16.1 dB
//
// **Twelve of the 54 transcriptions come in six pairs with the same text**, and both limits of a
// pair are the same value for the same reason: the tracker made two channels of one station, and
// `sdrainer prepare` therefore wrote two WAV files of the same signal. Section 6.6 of
// doc/architecture.md holds the analysis.
//
// The tracker merges two of the six pairs, −11573/−11578 and −7447/−7463. The four that are left
// are −1958/−1971, −3364/−3478, 14034/14124 and 16515/16544. Both files of a pair keep their limit,
// whether the pair merged or not: the test takes each channel inside transcriptionTolerance of an
// offset, so both files of a merged pair point at the one channel that is left.
//
// **Two limits hold a rare outlier of the scheduling.** _-8474.txt and _9527.txt gave one value over
// 30 runs each, 0.316 and 0.200, and one run of 15 under load gave 0.421 and 0.250. The reason is
// the source of the variance that this file names above: the worker takes the frames of the two
// tiers with a select, so the decode of a channel begins at another frame in each run. Both
// transcriptions are short, 19 and 22 characters, so one character is 0.05 of the error rate and one
// frame of difference moves the value far. The limits hold the outlier, and they are 0.48 and 0.29.
//
// **Two limits guard almost nothing**, because their transcription is very short: _11323.txt holds
// 13 characters and gives 0.846, and _-19218.txt holds 9 characters and gives 0.778. One wrong
// character of a text of that length is 0.08 of the error rate. They stay in the set because the
// test also asks that a channel exists at that offset at all.
// A measurement over 15 runs on 2026-08-30 gave these values for the fifth recording,
// test_yo-hf-dx_3_48k.iq. It is the second recording of the YO HF DX contest, 89 s of the 20 m band
// at 48000, and it is the recording that showed the noise outside the passband of the receiver: see
// section 6.7 of doc/architecture.md and TrackerStage.reviewSNR. Its 22 transcriptions are what
// fixes the limit of that rule: they gave the weakest signal against which it must not fire.
//
//	_-20248.txt      0.576–0.576  0.66
//	_-14030.txt      0.236–0.236  0.27
//	_-11003.txt      0.547–0.623  0.72
//	_-9054.txt       0.138–0.172  0.20
//	_-7895.txt       0.414–0.448  0.52
//	_-7504.txt       0.567–0.567  0.65
//	_-5508.txt       0.071–0.077  0.09
//	_-4731.txt       0.667–0.667  0.77
//	_-2027.txt       0.444–0.463  0.53
//	_926.txt         0.542–0.542  0.62
//	_2397.txt        0.500–0.556  0.64
//	_2954.txt        0.600–0.600  0.69
//	_3532.txt        0.357–0.429  0.49
//	_4507.txt        0.447–0.479  0.55
//	_4943.txt        0.833–0.833  0.96
//	_5487.txt        0.552–0.581  0.67
//	_6207.txt        0.238–0.254  0.29
//	_7387.txt        0.213–0.235  0.27
//	_7717.txt        0.462–0.538  0.62
//	_9003.txt        0.118–0.142  0.16
//	_12503.txt       0.450–0.457  0.53
//	_18811.txt       0.647–0.647  0.74
//
// **Two of its transcriptions lie outside the passband of the receiver**, thus beyond ±18.1 kHz,
// and they hold the same callsign: _-20248.txt and _18811.txt both say "ea5itt ea5itt". One station
// does not send on two frequencies at the same time, so at least one of the two frequencies is not
// the frequency of that station. What the tracker does with them is right either way: the peaks of
// both are far above the threshold, so both keep their channel, and section 6.7 holds why a rule
// over the place in the band is wrong.
//
// _4943.txt holds 8 characters, so its limit guards nothing beyond the question whether a channel
// exists at that offset at all.
var maxTranscriptionErrorRate = map[string]float64{
	"test_yo-hf-dx_3_48k.iq_-20248.txt": 0.66,
	"test_yo-hf-dx_3_48k.iq_-14030.txt": 0.27,
	"test_yo-hf-dx_3_48k.iq_-11003.txt": 0.72,
	"test_yo-hf-dx_3_48k.iq_-9054.txt":  0.20,
	"test_yo-hf-dx_3_48k.iq_-7895.txt":  0.52,
	"test_yo-hf-dx_3_48k.iq_-7504.txt":  0.65,
	"test_yo-hf-dx_3_48k.iq_-5508.txt":  0.09,
	"test_yo-hf-dx_3_48k.iq_-4731.txt":  0.77,
	"test_yo-hf-dx_3_48k.iq_-2027.txt":  0.53,
	"test_yo-hf-dx_3_48k.iq_926.txt":    0.62,
	"test_yo-hf-dx_3_48k.iq_2397.txt":   0.64,
	"test_yo-hf-dx_3_48k.iq_2954.txt":   0.69,
	"test_yo-hf-dx_3_48k.iq_3532.txt":   0.49,
	"test_yo-hf-dx_3_48k.iq_4507.txt":   0.55,
	"test_yo-hf-dx_3_48k.iq_4943.txt":   0.96,
	"test_yo-hf-dx_3_48k.iq_5487.txt":   0.67,
	"test_yo-hf-dx_3_48k.iq_6207.txt":   0.29,
	"test_yo-hf-dx_3_48k.iq_7387.txt":   0.27,
	"test_yo-hf-dx_3_48k.iq_7717.txt":   0.62,
	"test_yo-hf-dx_3_48k.iq_9003.txt":   0.16,
	"test_yo-hf-dx_3_48k.iq_12503.txt":  0.53,
	"test_yo-hf-dx_3_48k.iq_18811.txt":  0.74,
	"test_yo-hf-dx_1_48k.iq_-19218.txt": 0.89,
	"test_yo-hf-dx_1_48k.iq_-18438.txt": 0.76,
	"test_yo-hf-dx_1_48k.iq_-17466.txt": 0.43,
	"test_yo-hf-dx_1_48k.iq_-16474.txt": 0.61,
	"test_yo-hf-dx_1_48k.iq_-14599.txt": 0.28,
	"test_yo-hf-dx_1_48k.iq_-13875.txt": 0.38,
	"test_yo-hf-dx_1_48k.iq_-13084.txt": 0.34,
	"test_yo-hf-dx_1_48k.iq_-12472.txt": 0.82,
	"test_yo-hf-dx_1_48k.iq_-11578.txt": 0.23,
	"test_yo-hf-dx_1_48k.iq_-11573.txt": 0.23,
	"test_yo-hf-dx_1_48k.iq_-10666.txt": 0.13,
	"test_yo-hf-dx_1_48k.iq_-10264.txt": 0.40,
	"test_yo-hf-dx_1_48k.iq_-9571.txt":  0.16,
	"test_yo-hf-dx_1_48k.iq_-8474.txt":  0.48,
	"test_yo-hf-dx_1_48k.iq_-7463.txt":  0.38,
	"test_yo-hf-dx_1_48k.iq_-7447.txt":  0.38,
	"test_yo-hf-dx_1_48k.iq_-7222.txt":  0.33,
	"test_yo-hf-dx_1_48k.iq_-6771.txt":  0.23,
	"test_yo-hf-dx_1_48k.iq_-5977.txt":  0.38,
	"test_yo-hf-dx_1_48k.iq_-5464.txt":  0.36,
	"test_yo-hf-dx_1_48k.iq_-4913.txt":  0.25,
	"test_yo-hf-dx_1_48k.iq_-3978.txt":  0.19,
	"test_yo-hf-dx_1_48k.iq_-3478.txt":  0.56,
	"test_yo-hf-dx_1_48k.iq_-3364.txt":  0.56,
	"test_yo-hf-dx_1_48k.iq_-2888.txt":  0.31,
	"test_yo-hf-dx_1_48k.iq_-2569.txt":  0.33,
	"test_yo-hf-dx_1_48k.iq_-1971.txt":  0.32,
	"test_yo-hf-dx_1_48k.iq_-1958.txt":  0.32,
	"test_yo-hf-dx_1_48k.iq_-1178.txt":  0.50,
	"test_yo-hf-dx_1_48k.iq_-442.txt":   0.24,
	"test_yo-hf-dx_1_48k.iq_521.txt":    0.20,
	"test_yo-hf-dx_1_48k.iq_1198.txt":   0.27,
	"test_yo-hf-dx_1_48k.iq_2028.txt":   0.19,
	"test_yo-hf-dx_1_48k.iq_4158.txt":   0.23,
	"test_yo-hf-dx_1_48k.iq_4928.txt":   0.10,
	"test_yo-hf-dx_1_48k.iq_5637.txt":   0.35,
	"test_yo-hf-dx_1_48k.iq_6823.txt":   0.45,
	"test_yo-hf-dx_1_48k.iq_7277.txt":   0.17,
	"test_yo-hf-dx_1_48k.iq_7527.txt":   0.31,
	"test_yo-hf-dx_1_48k.iq_8795.txt":   0.51,
	"test_yo-hf-dx_1_48k.iq_9527.txt":   0.29,
	"test_yo-hf-dx_1_48k.iq_11323.txt":  0.97,
	"test_yo-hf-dx_1_48k.iq_11499.txt":  0.48,
	"test_yo-hf-dx_1_48k.iq_12758.txt":  0.33,
	"test_yo-hf-dx_1_48k.iq_13532.txt":  0.49,
	"test_yo-hf-dx_1_48k.iq_14034.txt":  0.38,
	"test_yo-hf-dx_1_48k.iq_14124.txt":  0.38,
	"test_yo-hf-dx_1_48k.iq_15028.txt":  0.59,
	"test_yo-hf-dx_1_48k.iq_15516.txt":  0.49,
	"test_yo-hf-dx_1_48k.iq_16515.txt":  0.32,
	"test_yo-hf-dx_1_48k.iq_16544.txt":  0.32,
	"test_yo-hf-dx_1_48k.iq_17026.txt":  0.52,
	"test_yo-hf-dx_1_48k.iq_17530.txt":  0.41,
	"test_yo-hf-dx_1_48k.iq_18981.txt":  0.68,
	"test_14018_12k.iq_-1224.txt":       0.78,
	"test_14018_12k.iq_-2728.txt":       0.40,
	"test_14018_12k.iq_-2986.txt":       0.51,
	"test_14018_12k.iq_-3315.txt":       0.54,
	"test_14018_12k.iq_-3667.txt":       0.27,
	"test_14018_12k.iq_-4962.txt":       0.82,
	"test_14020_12k.iq_-2040.txt":       0.26,
	"test_14020_12k.iq_-3244.txt":       0.71,
	"test_14020_12k.iq_-3954.txt":       0.34,
	"test_14020_12k.iq_0.txt":           0.43,
	"test_14020_12k.iq_196.txt":         0.74,
	"test_14024_12k.iq_-1056.txt":       0.29,
	"test_14024_12k.iq_-45.txt":         0.38,
	"test_14024_12k.iq_482.txt":         0.49,
	"test_14024_12k.iq_1951.txt":        0.85,
	"test_14024_12k.iq_3457.txt":        0.36,
	"test_14024_12k.iq_3828.txt":        0.77,
}

// Two limits changed with the character of the decoder for a character that it cannot read, on
// 2026-08-22. The decoder gave "?" for that case before, and "?" is also ..--.. and a character of
// the code table, and the transcriptions used "?" for a prosign as well. Three meanings stood on
// one character, and two of them were wrong:
//
//   - _-3954.txt went from 0.244–0.268 to 0.268–0.293, so its limit went from 0.31 to 0.34. It
//     holds an SN, and the decoder cannot read that prosign: it gives the unknown character there.
//     The old "?" of the transcription and the old "?" of the decoder met by luck, so a real error
//     counted as a hit. The value is worse and it is now true.
//   - _482.txt went from 0.444 to 0.429, so its limit went from 0.51 to 0.49. It holds an AR, and
//     the decoder reads that prosign correctly as "+". The transcription said "?", so a correct
//     decode counted as an error.
//
// fixtureName takes the sample rate from the name of a fixture.
var fixtureName = regexp.MustCompile(`_(\d+)(k?)\.iq$`)

func TestDecodeOfTranscribedRecordings(t *testing.T) {
	fixtures, err := filepath.Glob(filepath.Join("testdata", "*.iq"))
	require.NoError(t, err)
	require.NotEmpty(t, fixtures, "no recording in testdata")

	for _, fixture := range fixtures {
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			sampleRate, err := sampleRateOf(fixture)
			require.NoError(t, err)

			transcriptions, err := transcriptionsWithText(fixture)
			require.NoError(t, err)
			if len(transcriptions) == 0 {
				t.Skipf("%s has no transcription that holds text", filepath.Base(fixture))
			}

			channels := decodeRecording(t, fixture, sampleRate)
			t.Logf("%s: %d Hz, %d channels, %d transcriptions", filepath.Base(fixture), sampleRate, len(channels), len(transcriptions))

			for _, transcription := range transcriptions {
				t.Run(filepath.Base(transcription), func(t *testing.T) {
					checkTranscription(t, transcription, channels)
				})
			}
		})
	}
}

// transcriptionsWithText gives the transcriptions of a recording that hold text.
//
// **A recording without such a transcription is no failure of this test.** Two ways lead to one:
//
//   - A recording that another test uses. test_yo-hf-dx_3_48k.iq is such a recording, and it holds
//     no transcription at all.
//   - A session that a human did not finish. `sdrainer prepare` makes an empty file for each channel
//     of a recording, and a human then fills the files of the signals that are worth it, so an empty
//     file is no expectation.
//
// The test skips a recording of either kind, and it does not decode it: a recording is large, and
// the decode of one that says nothing is a minute of the suite for nothing.
func transcriptionsWithText(fixture string) ([]string, error) {
	candidates, err := filepath.Glob(fixture + "_*.txt")
	if err != nil {
		return nil, err
	}

	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		expected, err := readTranscription(candidate)
		if err != nil {
			return nil, err
		}
		if len(expected.overs) == 0 {
			continue
		}
		result = append(result, candidate)
	}

	return result, nil
}

// checkTranscription compares the text of the channels around the offset of one transcription
// against the text that a human wrote.
func checkTranscription(t *testing.T, transcription string, channels []transcribedChannel) {
	t.Helper()

	offset, err := offsetOf(transcription)
	require.NoError(t, err)
	expected, err := readTranscription(transcription)
	require.NoError(t, err)
	require.NotEmpty(t, expected.overs, "the transcription must hold text")

	// The pipeline gives more than one channel inside the filter of one transcription: it makes a
	// channel of each station, and it also splits one station into more than one channel. The best
	// of them answers the question of this test, which is if the decoder copied this signal.
	var candidates []transcribedChannel
	best := math.Inf(1)
	var bestText string
	for _, channel := range channels {
		if math.Abs(channel.frequency-offset) > transcriptionTolerance {
			continue
		}
		candidates = append(candidates, channel)

		rate := expected.errorRate(channel.text)
		if rate < best {
			best, bestText = rate, channel.text
		}
	}

	for _, candidate := range candidates {
		t.Logf("  channel at %+8.1f Hz  %2d wpm  %5.1f dB  rate %.3f  %q",
			candidate.frequency, candidate.wpm, candidate.snr,
			expected.errorRate(candidate.text), candidate.text)
	}

	require.NotEmptyf(t, candidates, "%+.0f Hz must give a channel: the transcription says that a CW signal is there", offset)

	t.Logf("%+.0f Hz: error rate %.3f\n  actual %q", offset, best, bestText)
	for _, over := range expected.overs {
		t.Logf("    over %.3f  %q", bestMatchErrorRate(over, bestText), over)
	}

	limit, ok := maxTranscriptionErrorRate[filepath.Base(transcription)]
	if !ok {
		limit = defaultMaxTranscriptionErrorRate
		t.Logf("no limit for this transcription yet, %.2f is the measured value", best)
	}
	assert.LessOrEqualf(t, best, limit, "the copy of %+.0f Hz got worse: %q", offset, bestText)
}

// transcribedChannel holds what the pipeline made of one channel of a recording.
type transcribedChannel struct {
	frequency float64
	wpm       int
	snr       float64
	text      string
}

// decodeRecording gives the whole recording to the pipeline. The center frequency is 0, so the
// frequency of a channel is the offset from the center of the recording, which is the number in the
// name of a transcription.
func decodeRecording(t *testing.T, filename string, sampleRate int, options ...func(*Config[float64])) []transcribedChannel {
	t.Helper()

	samples, err := readIQFile(filename)
	require.NoError(t, err)
	require.NotEmpty(t, samples)

	config := DefaultConfig(sampleRate, 0.0)
	for _, option := range options {
		option(&config)
	}

	listener := newTranscriptionListener()
	p := New[float32, float64](config, nil)
	p.Notify(listener)

	p.Start()
	for i := 0; i+2*transcriptionChunkSize <= len(samples); i += 2 * transcriptionChunkSize {
		p.IQData(sampleRate, samples[i:i+2*transcriptionChunkSize])
	}
	p.Stop()

	return listener.channels()
}

// readIQFile takes the samples of a recording: interleaved float32, little endian, without a
// header. iq.Reader does the same for the commands, and the test of the pipeline uses no other
// package of SDRainer.
func readIQFile(filename string) ([]float32, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	result := make([]float32, len(content)/4)
	for i := range result {
		result[i] = math.Float32frombits(binary.LittleEndian.Uint32(content[4*i:]))
	}
	return result, nil
}

// sampleRateOf takes the sample rate from the name of a fixture, for example 12000 from
// "test_14020_12k.iq".
func sampleRateOf(filename string) (int, error) {
	matches := fixtureName.FindStringSubmatch(filepath.Base(filename))
	if matches == nil {
		return 0, fmt.Errorf("%s does not end with the sample rate, for example _12k.iq", filename)
	}

	result, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, err
	}
	if matches[2] == "k" {
		result *= 1000
	}
	return result, nil
}

// offsetOf takes the offset from the name of a transcription, for example -3954 from
// "test_14020_12k.iq_-3954.txt".
func offsetOf(filename string) (float64, error) {
	base := strings.TrimSuffix(filepath.Base(filename), ".txt")
	index := strings.LastIndex(base, "_")
	if index < 0 {
		return 0, fmt.Errorf("%s holds no offset", filename)
	}
	return strconv.ParseFloat(base[index+1:], 64)
}

// transcribedText holds the overs that a human wrote, one for each line of the transcription.
type transcribedText struct {
	overs []string
}

// errorRate gives the count of the changes that make each over out of a part of the given text,
// divided by the count of the characters of all overs.
//
// Each over gets its own part of the text, because the stations of one channel transmit one after
// the other and the text of the channel holds their overs in that order. The test does not check
// that order: an over can match a part that lies before the part of the over before it.
func (t transcribedText) errorRate(actual string) float64 {
	// The decoder marks the end of an over, and that marker is a word break. A transcription holds
	// the overs as its lines, and it has no such character, so the comparison takes it as a space.
	actual = strings.ReplaceAll(actual, string(cw.OverMarker), " ")

	var changes, characters int
	for _, over := range t.overs {
		changes += bestMatchDistance(over, actual)
		characters += len([]rune(over))
	}
	if characters == 0 {
		return 0
	}
	return float64(changes) / float64(characters)
}

// readTranscription gives the text that a human wrote. A line that begins with # is a comment, and
// each other line is one over.
//
// A prosign stands as the character that the code table gives for it, thus "+" for AR and "%" for
// SN, because the decoder gives one rune for one character of the code.
func readTranscription(filename string) (transcribedText, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return transcribedText{}, err
	}

	var result transcribedText
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		over := strings.ToLower(line)
		result.overs = append(result.overs, strings.Join(strings.Fields(over), " "))
	}

	return result, nil
}

// transcriptionListener collects the text of each channel of a recording.
type transcriptionListener struct {
	mutex     sync.Mutex
	order     []core.ChannelID
	frequency map[core.ChannelID]float64
	wpm       map[core.ChannelID]int
	snr       map[core.ChannelID]float64
	text      map[core.ChannelID][]rune
}

func newTranscriptionListener() *transcriptionListener {
	return &transcriptionListener{
		frequency: make(map[core.ChannelID]float64),
		wpm:       make(map[core.ChannelID]int),
		snr:       make(map[core.ChannelID]float64),
		text:      make(map[core.ChannelID][]rune),
	}
}

func (l *transcriptionListener) ChannelCreated(channel core.Channel[float64]) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.order = append(l.order, channel.ID)
	l.frequency[channel.ID] = channel.Frequency
	l.snr[channel.ID] = channel.SNR
}

func (l *transcriptionListener) ChannelDestroyed(core.Channel[float64]) {}

func (l *transcriptionListener) ChannelCharacterReceived(channel core.Channel[float64], character rune, _ int64) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.text[channel.ID] = append(l.text[channel.ID], character)
	l.frequency[channel.ID] = channel.Frequency
	l.snr[channel.ID] = channel.SNR
	if channel.WPM > 0 {
		l.wpm[channel.ID] = channel.WPM
	}
}

func (l *transcriptionListener) channels() []transcribedChannel {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	result := make([]transcribedChannel, 0, len(l.order))
	for _, id := range l.order {
		result = append(result, transcribedChannel{
			frequency: l.frequency[id],
			wpm:       l.wpm[id],
			snr:       l.snr[id],
			text:      strings.TrimSpace(string(l.text[id])),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].frequency < result[j].frequency })

	return result
}

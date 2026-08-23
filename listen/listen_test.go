package listen

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ftl/sdrainer/iq"
	"github.com/ftl/sdrainer/pipeline/generator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testSampleRate = 12000
	testWanted     = 1000.0
	testOther      = 2000.0
	testSeconds    = 8
)

// writeTwoSignals makes a recording with two CW signals that are 1000 Hz apart. The wanted signal
// sends "paris" and the other one sends "test", so the two are also different in their keying.
func writeTwoSignals(t *testing.T, filename string) {
	t.Helper()

	writer, err := iq.NewWriter(filename)
	require.NoError(t, err)

	source := generator.New[float32, float64](generator.GeneratorConfig[float64]{
		SampleRate:      testSampleRate,
		CenterFrequency: 0,
		NoiseLevel:      0.001,
		Seed:            1,
		Signals: []generator.CWSignal[float64]{
			{Frequency: testWanted, Text: "paris", WPM: 20, Amplitude: 1.0, RiseTime: 5 * time.Millisecond},
			{Frequency: testOther, Text: "test", WPM: 20, Amplitude: 1.0, RiseTime: 5 * time.Millisecond},
		},
	})

	chunk := make([]float32, 2*1024)
	for range testSeconds * testSampleRate / 1024 {
		source.Read(chunk)
		require.NoError(t, writer.RecordIQ(chunk))
	}
	require.NoError(t, writer.Close())
}

// readWAV gives the samples of a WAV file with 16 bit mono, and the sample rate of its header.
func readWAV(t *testing.T, filename string) ([]float64, int) {
	t.Helper()

	content, err := os.ReadFile(filename)
	require.NoError(t, err)
	require.Greater(t, len(content), wavHeaderSize)

	assert.Equal(t, "RIFF", string(content[0:4]))
	assert.Equal(t, "WAVE", string(content[8:12]))
	assert.Equal(t, "fmt ", string(content[12:16]))
	assert.Equal(t, uint16(1), binary.LittleEndian.Uint16(content[20:22]), "PCM")
	assert.Equal(t, uint16(1), binary.LittleEndian.Uint16(content[22:24]), "one channel")
	assert.Equal(t, uint16(16), binary.LittleEndian.Uint16(content[34:36]), "16 bit")
	assert.Equal(t, "data", string(content[36:40]))

	sampleRate := int(binary.LittleEndian.Uint32(content[24:28]))
	dataSize := int(binary.LittleEndian.Uint32(content[40:44]))
	assert.Equal(t, dataSize, len(content)-wavHeaderSize, "the size in the header must be the size of the data")

	samples := make([]float64, dataSize/2)
	for i := range samples {
		samples[i] = float64(int16(binary.LittleEndian.Uint16(content[wavHeaderSize+2*i:]))) / math.MaxInt16
	}
	return samples, sampleRate
}

// levelAt gives the level of the given frequency in the samples, with a Goertzel over the whole
// signal.
func levelAt(samples []float64, frequency float64, sampleRate int) float64 {
	var real, imaginary float64
	for i, sample := range samples {
		phase := 2 * math.Pi * frequency * float64(i) / float64(sampleRate)
		real += sample * math.Cos(phase)
		imaginary += sample * math.Sin(phase)
	}
	return 2 * math.Hypot(real, imaginary) / float64(len(samples))
}

// TestRunGivesTheSignalOfTheOffsetAtThePitch is the loop of the whole chain: the wanted signal
// arrives at the pitch, and the station 1000 Hz beside it is gone.
func TestRunGivesTheSignalOfTheOffsetAtThePitch(t *testing.T) {
	const pitch = 600.0

	directory := t.TempDir()
	iqFilename := filepath.Join(directory, "test.iq")
	outFilename := filepath.Join(directory, "test.wav")
	writeTwoSignals(t, iqFilename)

	err := Run(Options{
		IQFilename:   iqFilename,
		SampleRate:   testSampleRate,
		SignalOffset: testWanted,
		OutFilename:  outFilename,
		Pitch:        pitch,
	})
	require.NoError(t, err)

	samples, sampleRate := readWAV(t, outFilename)
	assert.Equal(t, testSampleRate, sampleRate)
	assert.InDelta(t, testSeconds*testSampleRate, len(samples), 2048, "one sample of audio for each IQ sample")

	atPitch := levelAt(samples, pitch, sampleRate)
	assert.Greaterf(t, atPitch, 0.1, "the wanted signal must be at the pitch of %.0f Hz", pitch)

	// the other station is 1000 Hz above the wanted one, so it would arrive at pitch+1000
	atOther := levelAt(samples, pitch+(testOther-testWanted), sampleRate)
	assert.Lessf(t, atOther, atPitch/100, "the station beside the wanted one must be gone: %f against %f", atOther, atPitch)
}

// TestRunTakesTheOtherSignalWithTheOtherOffset uses the same recording and takes the other station,
// so that the result cannot come from the position of the signals alone.
func TestRunTakesTheOtherSignalWithTheOtherOffset(t *testing.T) {
	const pitch = 600.0

	directory := t.TempDir()
	iqFilename := filepath.Join(directory, "test.iq")
	outFilename := filepath.Join(directory, "test.wav")
	writeTwoSignals(t, iqFilename)

	err := Run(Options{
		IQFilename:   iqFilename,
		SampleRate:   testSampleRate,
		SignalOffset: testOther,
		OutFilename:  outFilename,
		Pitch:        pitch,
	})
	require.NoError(t, err)

	samples, _ := readWAV(t, outFilename)

	atPitch := levelAt(samples, pitch, testSampleRate)
	atWanted := levelAt(samples, pitch-(testOther-testWanted), testSampleRate)
	assert.Greater(t, atPitch, 0.1, "the other station must now be at the pitch")
	assert.Less(t, atWanted, atPitch/100, "the first station must now be gone")
}

// TestRunNormalizesTheLevel checks that a weak signal gives an audio file that a human can hear: the
// loudest sample is at the target level, whatever the level of the signal was.
func TestRunNormalizesTheLevel(t *testing.T) {
	directory := t.TempDir()
	iqFilename := filepath.Join(directory, "test.iq")

	writer, err := iq.NewWriter(iqFilename)
	require.NoError(t, err)
	source := generator.New[float32, float64](generator.GeneratorConfig[float64]{
		SampleRate: testSampleRate, CenterFrequency: 0, NoiseLevel: 0.00001, Seed: 1,
		Signals: []generator.CWSignal[float64]{
			{Frequency: testWanted, Text: "paris", WPM: 20, Amplitude: 0.001, RiseTime: 5 * time.Millisecond},
		},
	})
	chunk := make([]float32, 2*1024)
	for range testSeconds * testSampleRate / 1024 {
		source.Read(chunk)
		require.NoError(t, writer.RecordIQ(chunk))
	}
	require.NoError(t, writer.Close())

	outFilename := filepath.Join(directory, "test.wav")
	require.NoError(t, Run(Options{
		IQFilename: iqFilename, SampleRate: testSampleRate, SignalOffset: testWanted,
		OutFilename: outFilename, Pitch: DefaultPitch,
	}))

	samples, _ := readWAV(t, outFilename)
	var peak float64
	for _, sample := range samples {
		peak = math.Max(peak, math.Abs(sample))
	}

	assert.InDelta(t, targetLevel, peak, 0.01, "a weak signal must still reach the target level")
}

func TestRunRefusesAnOffsetOutsideTheStream(t *testing.T) {
	directory := t.TempDir()
	iqFilename := filepath.Join(directory, "test.iq")
	writeTwoSignals(t, iqFilename)

	err := Run(Options{
		IQFilename:   iqFilename,
		SampleRate:   testSampleRate,
		SignalOffset: testSampleRate, // far above one half of the sample rate
		OutFilename:  filepath.Join(directory, "test.wav"),
		Pitch:        DefaultPitch,
	})

	assert.ErrorContains(t, err, "outside the stream")
}

func TestRunGivesAnErrorForAMissingFile(t *testing.T) {
	directory := t.TempDir()

	err := Run(Options{
		IQFilename:  filepath.Join(directory, "no-such-file.iq"),
		SampleRate:  testSampleRate,
		OutFilename: filepath.Join(directory, "test.wav"),
		Pitch:       DefaultPitch,
	})

	assert.Error(t, err)
}

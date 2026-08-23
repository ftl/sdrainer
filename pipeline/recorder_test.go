package pipeline

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingRecorder collects the samples that the pipeline gives it.
type recordingRecorder struct {
	samples []float32
	calls   int
	err     error
}

func (r *recordingRecorder) RecordIQ(samples []float32) error {
	r.calls++
	if r.err != nil {
		return r.err
	}
	r.samples = append(r.samples, samples...)
	return nil
}

func TestPipelineGivesEachSampleToTheRecorder(t *testing.T) {
	first := noisyTone(testBlockSize, testOffset, 0.5, 0.001, 1)
	second := noisyTone(testBlockSize, testOffset, 0.5, 0.001, 2)

	recorder := &recordingRecorder{}
	p := New[float32, float64](testConfig(), nil)
	p.SetRecorder(recorder)

	p.Start()
	p.IQData(testSampleRate, first)
	p.IQData(testSampleRate, second)
	p.Stop()

	require.Equal(t, 2, recorder.calls)
	assert.Equal(t, append(append([]float32{}, first...), second...), recorder.samples,
		"the recording must hold each sample of the stream, in the order of the stream")
}

func TestPipelineRecordsNoSampleOfAWrongSampleRate(t *testing.T) {
	recorder := &recordingRecorder{}
	p := New[float32, float64](testConfig(), nil)
	p.SetRecorder(recorder)

	p.Start()
	p.IQData(testSampleRate/2, noisyTone(testBlockSize, testOffset, 0.5, 0.001, 1))
	p.Stop()

	assert.Zero(t, recorder.calls, "the pipeline drops such a chunk, so the recording must not hold it")
}

// TestPipelineStopsTheRecordingAfterAnError checks that a disk that is full does not stop the
// skimmer, and that it does not give a message for each chunk.
func TestPipelineStopsTheRecordingAfterAnError(t *testing.T) {
	recorder := &recordingRecorder{err: errors.New("no space left on device")}
	p := New[float32, float64](testConfig(), nil)
	p.SetRecorder(recorder)

	p.Start()
	assert.NotPanics(t, func() {
		for range 3 {
			p.IQData(testSampleRate, noisyTone(testBlockSize, testOffset, 0.5, 0.001, 1))
		}
	})
	p.Stop()

	assert.Equal(t, 1, recorder.calls, "the pipeline must use the recorder no more after an error")
}

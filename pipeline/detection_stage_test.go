package pipeline

import (
	"fmt"
	"testing"

	"github.com/ftl/sdrainer/dsp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	detectionSampleRate = 48000
	detectionBlockSize  = 1024 // 46.875 Hz for each bin
	detectionCenter     = 7020000.0
	// 43 bins are approximately 500 Hz, as section 4.1 asks for. The span must be much wider than
	// the signals: a median breaks down when more than half of its window holds signal.
	detectionSpan       = 43
	detectionThreshold  = 8 // dB
	detectionFloorLevel = 1.0
	detectionPeakLevel  = 1000.0
)

func newTestDetectionStage(t *testing.T, mergeWidth int) (*DetectionStage[float32, float64], *dsp.FrequencyMapping[float64]) {
	t.Helper()

	mapping := dsp.NewFrequencyMapping[float64](detectionSampleRate, detectionBlockSize, detectionCenter)
	stage := NewDetectionStage[float32, float64](detectionBlockSize, detectionSpan, 1, detectionThreshold, mergeWidth, mapping)

	return stage, mapping
}

// newTestFrame gives a frame with a flat noise floor and one symmetric peak of three bins at each
// of the given bins.
func newTestFrame(peakBins ...int) *SpectralFrame[float32, float64] {
	frame := NewFramePool[float32, float64](detectionBlockSize).NewFrame()
	for i := range frame.Spectrum {
		frame.Spectrum[i] = detectionFloorLevel
	}
	for _, bin := range peakBins {
		frame.Spectrum[bin-1] = detectionPeakLevel / 2
		frame.Spectrum[bin] = detectionPeakLevel
		frame.Spectrum[bin+1] = detectionPeakLevel / 2
	}
	return frame
}

func TestDetectionStageFindsEachPeak(t *testing.T) {
	stage, mapping := newTestDetectionStage(t, 0)
	peakBins := []int{200, 500, 800}
	frame := newTestFrame(peakBins...)

	stage.Process(frame)

	require.Len(t, frame.Peaks, len(peakBins))
	for i, peak := range frame.Peaks {
		t.Run(fmt.Sprintf("bin_%d", peakBins[i]), func(t *testing.T) {
			assert.Equal(t, peakBins[i], peak.SignalBin, "signal bin")
			assert.Equal(t, peakBins[i]-1, peak.From, "from")
			assert.Equal(t, peakBins[i]+1, peak.To, "to")
			assert.Equal(t, float32(detectionPeakLevel), peak.SignalValue, "signal value")

			// the peak is symmetric, so the interpolation must not move the center
			expected := mapping.BinToFrequency(peakBins[i], dsp.BinCenter)
			assert.InDelta(t, expected, peak.SignalFrequency, 1e-6, "signal frequency")
		})
	}
}

func TestDetectionStageFindsTheNoiseFloor(t *testing.T) {
	stage, _ := newTestDetectionStage(t, 0)
	frame := newTestFrame(500)

	stage.Process(frame)

	// the median of the window is the floor level, and the estimator corrects it to the mean of
	// the noise, which is above the median
	for bin, floor := range frame.NoiseFloor {
		assert.Greaterf(t, floor, float32(detectionFloorLevel), "bin %d must be above the median", bin)
		assert.Lessf(t, floor, float32(2*detectionFloorLevel), "bin %d must stay near the median", bin)
	}
}

func TestDetectionStageMergesNearGroups(t *testing.T) {
	// the two peaks give the groups 499 to 501 and 504 to 506, thus a gap of two bins
	tt := []struct {
		desc          string
		mergeWidth    int
		expectedPeaks int
	}{
		{desc: "no merge", mergeWidth: 0, expectedPeaks: 2},
		{desc: "the gap is too large", mergeWidth: 1, expectedPeaks: 2},
		{desc: "the gap fits", mergeWidth: 2, expectedPeaks: 1},
		{desc: "a large merge width", mergeWidth: 10, expectedPeaks: 1},
	}
	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			stage, _ := newTestDetectionStage(t, tc.mergeWidth)
			frame := newTestFrame(500, 505)

			stage.Process(frame)

			require.Len(t, frame.Peaks, tc.expectedPeaks)
			if tc.expectedPeaks == 1 {
				assert.Equal(t, 499, frame.Peaks[0].From, "the merged peak starts at the first group")
				assert.Equal(t, 506, frame.Peaks[0].To, "the merged peak ends at the second group")
			}
		})
	}
}

// TestDetectionStageWithASpanThatIsTooSmall shows the limit of the moving median. If the signals
// fill more than half of the median window, the median follows the signals, the local floor rises,
// and the detection loses the signals.
func TestDetectionStageWithASpanThatIsTooSmall(t *testing.T) {
	mapping := dsp.NewFrequencyMapping[float64](detectionSampleRate, detectionBlockSize, detectionCenter)
	frame := newTestFrame(500, 505)

	wideEnough := NewDetectionStage[float32, float64](detectionBlockSize, 43, 1, detectionThreshold, 0, mapping)
	wideEnough.Process(frame)
	require.Len(t, frame.Peaks, 2, "a wide window finds both signals")

	tooSmall := NewDetectionStage[float32, float64](detectionBlockSize, 11, 1, detectionThreshold, 0, mapping)
	tooSmall.Process(frame)

	assert.Less(t, widthOf(frame.Peaks), 6, "a narrow window makes the signals smaller than they are")
}

func widthOf(peaks []dsp.Peak[float32, float64]) int {
	var result int
	for _, peak := range peaks {
		result += peak.Width()
	}
	return result
}

func TestDetectionStageFindsNoPeakInAFlatSpectrum(t *testing.T) {
	stage, _ := newTestDetectionStage(t, 0)
	frame := newTestFrame()

	stage.Process(frame)

	assert.Empty(t, frame.Peaks)
}

func TestDetectionStageWithAPeakAtTheEdge(t *testing.T) {
	stage, _ := newTestDetectionStage(t, 0)
	frame := newTestFrame(500)
	frame.Spectrum[0] = detectionPeakLevel
	frame.Spectrum[detectionBlockSize-1] = detectionPeakLevel

	assert.NotPanics(t, func() {
		stage.Process(frame)
	})

	require.Len(t, frame.Peaks, 3)
	assert.Equal(t, 0, frame.Peaks[0].SignalBin, "the first bin")
	assert.Equal(t, detectionBlockSize-1, frame.Peaks[2].SignalBin, "the last bin")
}

func TestDetectionStageInterpolatesAnAsymmetricPeak(t *testing.T) {
	stage, mapping := newTestDetectionStage(t, 0)
	frame := newTestFrame(500)
	// move the energy towards the next higher bin
	frame.Spectrum[501] = detectionPeakLevel * 0.8

	stage.Process(frame)

	require.Len(t, frame.Peaks, 1)
	center := mapping.BinToFrequency(500, dsp.BinCenter)
	upperLimit := mapping.BinToFrequency(500, dsp.BinTo)

	assert.Greater(t, frame.Peaks[0].SignalFrequency, center, "the center must move up")
	assert.LessOrEqual(t, frame.Peaks[0].SignalFrequency, upperLimit, "but it must stay inside the bin")
}

func TestDetectionStageUsesTheFrameAgain(t *testing.T) {
	stage, _ := newTestDetectionStage(t, 0)
	pool := NewFramePool[float32, float64](detectionBlockSize)

	frame := pool.NewFrame()
	for i := range frame.Spectrum {
		frame.Spectrum[i] = detectionFloorLevel
	}
	for _, bin := range []int{200, 500, 800} {
		frame.Spectrum[bin] = detectionPeakLevel
	}
	stage.Process(frame)
	require.Len(t, frame.Peaks, 3, "the test needs a frame with peaks")
	pool.ReturnFrame(frame)

	empty := pool.NewFrame()
	for i := range empty.Spectrum {
		empty.Spectrum[i] = detectionFloorLevel
	}
	stage.Process(empty)

	assert.Empty(t, empty.Peaks, "a frame without peaks must not keep the peaks of the last frame")
}

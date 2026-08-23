package pipeline

import (
	"math"

	"github.com/ftl/sdrainer/dsp"
)

// DetectionStage finds the noise floor and the peaks in a spectral frame. It is the detection tier
// of the research document, section 4.
//
// The threshold is a ratio against the floor of each single bin, not one value for the whole
// block. This is the reason for the moving median in dsp.NoiseFloor: a band with a slope, or a
// band with a strong station at one end, has a different floor at each frequency.
type DetectionStage[S, F dsp.Number] struct {
	noiseFloor *dsp.NoiseFloor[S]
	mapping    *dsp.FrequencyMapping[F]

	ratio      float64
	mergeWidth int

	scratch dsp.Block[S]
}

// NewDetectionStage for a spectrum with the given block size.
//   - span is the width of the median window in bins, section 4.1 asks for approximately 500 Hz.
//   - smoothing is the weight of a new value in the time filter, use dsp.SmoothingFactor.
//   - threshold is the distance above the local floor in dB, section 4.2 asks for 6 dB to 10 dB.
//   - mergeWidth is the largest gap in bins between two groups that still belong to one signal,
//     section 4.2 asks for 40 Hz.
func NewDetectionStage[S, F dsp.Number](blockSize int, span int, smoothing float64, threshold float64, mergeWidth int, mapping *dsp.FrequencyMapping[F]) *DetectionStage[S, F] {
	return &DetectionStage[S, F]{
		noiseFloor: dsp.NewNoiseFloor[S](blockSize, span, smoothing),
		mapping:    mapping,
		// the spectrum holds linear power, so a distance in dB becomes a factor
		ratio:      math.Pow(10, threshold/10),
		mergeWidth: max(0, mergeWidth),
		scratch:    make(dsp.Block[S], 3),
	}
}

// Process fills the noise floor and the peaks into the given frame.
// ResetNoiseFloor makes the estimate of the noise floor start again. A change of the center
// frequency gives each bin a different content, so the old floor is wrong. The next frame gives the
// new floor directly, without the smoothing over the time, so no time to settle is necessary.
func (d *DetectionStage[S, F]) ResetNoiseFloor() {
	d.noiseFloor.Reset()
}

func (d *DetectionStage[S, F]) Process(frame *SpectralFrame[S, F]) {
	if len(frame.Spectrum) != len(frame.NoiseFloor) {
		return
	}

	copy(frame.NoiseFloor, d.noiseFloor.Update(frame.Spectrum))

	frame.Peaks = d.findGroups(frame, frame.Peaks[:0])
	frame.Peaks = d.mergeGroups(frame.Peaks)
	d.addFrequencies(frame)
}

// findGroups collects each run of bins that is above the local threshold into one peak.
func (d *DetectionStage[S, F]) findGroups(frame *SpectralFrame[S, F], peaks []dsp.Peak[S, F]) []dsp.Peak[S, F] {
	var current dsp.Peak[S, F]
	inGroup := false

	for bin, value := range frame.Spectrum {
		floor := float64(frame.NoiseFloor[bin])
		if floor <= 0 || float64(value) < d.ratio*floor {
			if inGroup {
				peaks = append(peaks, current)
				inGroup = false
			}
			continue
		}

		if !inGroup {
			current = dsp.Peak[S, F]{From: bin, To: bin, SignalValue: value, SignalBin: bin}
			inGroup = true
			continue
		}

		current.To = bin
		if value > current.SignalValue {
			current.SignalValue = value
			current.SignalBin = bin
		}
	}
	if inGroup {
		peaks = append(peaks, current)
	}

	return peaks
}

// mergeGroups joins two groups that are nearer to each other than mergeWidth. The keying sidebands
// of one CW signal can give more than one group.
func (d *DetectionStage[S, F]) mergeGroups(peaks []dsp.Peak[S, F]) []dsp.Peak[S, F] {
	if len(peaks) < 2 {
		return peaks
	}

	result := peaks[:1]
	for _, peak := range peaks[1:] {
		last := &result[len(result)-1]
		if peak.From-last.To-1 > d.mergeWidth {
			result = append(result, peak)
			continue
		}

		last.To = peak.To
		if peak.SignalValue > last.SignalValue {
			last.SignalValue = peak.SignalValue
			last.SignalBin = peak.SignalBin
		}
	}

	return result
}

func (d *DetectionStage[S, F]) addFrequencies(frame *SpectralFrame[S, F]) {
	for i := range frame.Peaks {
		peak := &frame.Peaks[i]
		peak.FromFrequency = d.mapping.BinToFrequency(peak.From, dsp.BinFrom)
		peak.ToFrequency = d.mapping.BinToFrequency(peak.To, dsp.BinTo)
		peak.SignalFrequency = d.mapping.BinToFrequency(peak.SignalBin, d.centerCorrection(frame, peak.SignalBin))
	}
}

// centerCorrection interpolates the true center of a peak between its bins. The parabola of the
// interpolation fits the logarithm of a spectrum much better than the linear values, so only the
// three bins around the peak go into the decibel domain.
func (d *DetectionStage[S, F]) centerCorrection(frame *SpectralFrame[S, F], bin int) dsp.BinLocation {
	if bin <= 0 || bin >= len(frame.Spectrum)-1 {
		return dsp.BinCenter
	}

	d.scratch[0] = dsp.PowerIndB(frame.Spectrum[bin-1])
	d.scratch[1] = dsp.PowerIndB(frame.Spectrum[bin])
	d.scratch[2] = dsp.PowerIndB(frame.Spectrum[bin+1])

	return dsp.PeakCenterCorrection[S, F](1, d.scratch)
}

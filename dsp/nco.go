package dsp

import "math"

// NCO is a numerically controlled oscillator. It gives the two factors of e^(j2π·frequency·t) for
// each sample, and Mix multiplies a complex sample with them.
//
// A negative frequency moves a signal at that frequency to 0 Hz, and a positive frequency moves a
// signal at 0 Hz up. doc/architecture.md, section 9, gives the chain that uses both.
type NCO struct {
	phase float64
	step  float64
}

// NewNCO makes an oscillator for the given frequency, which can be negative.
func NewNCO(frequency float64, sampleRate int) *NCO {
	if sampleRate <= 0 {
		return &NCO{}
	}
	return &NCO{step: 2 * math.Pi * frequency / float64(sampleRate)}
}

// Mix multiplies the given complex value with the current value of the oscillator and moves the
// oscillator one sample forwards.
func (n *NCO) Mix(inPhase float64, quadrature float64) (float64, float64) {
	cos, sin := math.Cos(n.phase), math.Sin(n.phase)

	// the phase stays inside one turn, so that it loses no precision over a long stream
	n.phase = math.Mod(n.phase+n.step, 2*math.Pi)

	return inPhase*cos - quadrature*sin, inPhase*sin + quadrature*cos
}

// Reset puts the oscillator back to the phase of the first sample. A second pass over the same
// stream then gives the same result as the first one.
func (n *NCO) Reset() {
	n.phase = 0
}

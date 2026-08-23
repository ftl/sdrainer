package generator

import (
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/ftl/digimodes/cw"
	"github.com/ftl/sdrainer/dsp"
)

type element struct {
	on      bool
	samples int
}

// CWSignal describes one CW transmission in the generated IQ stream. The
// generator repeats the text endlessly, with one word break between the
// repetitions.
type CWSignal[F dsp.Number] struct {
	Frequency F
	Text      string
	WPM       int
	Amplitude float64 // 0 means 1.0

	Drift     float64       // Hz for each second, 0 means no drift
	FadeDepth float64       // 0.0 (no fading) to 1.0 (full fading)
	FadeRate  float64       // Hz, the speed of the fading
	RiseTime  time.Duration // 0 means hard keying, which makes key clicks
	Carrier   bool          // a tone without keying, for example a beacon or a birdie; Text has no effect

	// TurnPeriod and TurnOffset make the signal transmit in turns, for a QSO where two stations
	// alternate. The signal sends its text one time at the beginning of each turn, and then it is
	// silent until the next turn begins. The text therefore always stops at a word break, and the
	// end of a turn makes no key click.
	//
	// TurnPeriod 0 means that the signal repeats its text without an end. A Carrier ignores both
	// values.
	TurnPeriod time.Duration
	TurnOffset time.Duration
}

type GeneratorConfig[F dsp.Number] struct {
	SampleRate      int
	CenterFrequency F
	NoiseLevel      float64 // standard deviation of the white noise, 0 means no noise
	Seed            uint64
	Signals         []CWSignal[F]
}

type Generator[S, F dsp.Number] struct {
	config GeneratorConfig[F]
	states []*signalState[F]
	random *rand.Rand
}

func New[S, F dsp.Number](config GeneratorConfig[F]) *Generator[S, F] {
	result := &Generator[S, F]{
		config: config,
		random: rand.New(rand.NewPCG(config.Seed, config.Seed+0x9e3779b9)),
		states: make([]*signalState[F], 0, len(config.Signals)),
	}
	for _, signal := range config.Signals {
		result.states = append(result.states, newSignalState(signal, config.SampleRate))
	}
	return result
}

// Read fills the given slice with interleaved IQ samples, in the same layout
// that dsp.FFT expects. The stream is continuous over the calls.
func (g *Generator[S, F]) Read(iq []S) {
	sampleRate := float64(g.config.SampleRate)
	centerFrequency := float64(g.config.CenterFrequency)
	for i := 0; i+1 < len(iq); i += 2 {
		var inPhase, quadrature float64
		for _, state := range g.states {
			re, im := state.next(sampleRate, centerFrequency)
			inPhase += re
			quadrature += im
		}
		if g.config.NoiseLevel > 0 {
			inPhase += g.random.NormFloat64() * g.config.NoiseLevel
			quadrature += g.random.NormFloat64() * g.config.NoiseLevel
		}
		iq[i] = S(inPhase)
		iq[i+1] = S(quadrature)
	}
}

type signalState[F dsp.Number] struct {
	signal    CWSignal[F]
	amplitude float64

	elements []element
	index    int
	left     int

	envelope float64
	rise     float64
	phase    float64
	elapsed  float64

	// turnPeriod and turnOffset are TurnPeriod and TurnOffset of the signal, in samples. turn is
	// the number of the turn that runs now, and silent tells that the text of this turn is
	// complete.
	turnPeriod  int64
	turnOffset  int64
	turn        int64
	silent      bool
	sampleCount int64
}

func newSignalState[F dsp.Number](signal CWSignal[F], sampleRate int) *signalState[F] {
	amplitude := signal.Amplitude
	if amplitude == 0 {
		amplitude = 1
	}
	wpm := signal.WPM
	if wpm == 0 {
		wpm = 20
	}

	ditSamples := int(math.Round(1.2 * float64(sampleRate) / float64(wpm)))
	elements := toElements(signal.Text, ditSamples)
	if signal.Carrier {
		// one element that is on, and the ring of the elements repeats it without an end
		elements = []element{{on: true, samples: 1}}
	}

	// ponytail: a one-pole filter shapes the keying edges, not a raised cosine.
	// The shape is exponential, so the sideband level is not exact. Replace it
	// with a raised cosine if a test needs a correct click spectrum.
	rise := 1.0
	if signal.RiseTime > 0 {
		rise = 1 - math.Exp(-1/(signal.RiseTime.Seconds()*float64(sampleRate)))
	}

	result := &signalState[F]{
		signal:    signal,
		amplitude: amplitude,
		elements:  elements,
		rise:      rise,

		// turn -1 makes the first call of updateTurn see turn 0 as a new turn
		turn: -1,
	}
	if len(elements) > 0 {
		result.left = elements[0].samples
	}
	if signal.TurnPeriod > 0 && !signal.Carrier {
		result.turnPeriod = samplesFor(signal.TurnPeriod, sampleRate)
		result.turnOffset = samplesFor(signal.TurnOffset, sampleRate)
	}
	return result
}

// samplesFor gives the count of the samples of the given time at the given sample rate.
func samplesFor(duration time.Duration, sampleRate int) int64 {
	return int64(math.Round(duration.Seconds() * float64(sampleRate)))
}

func (s *signalState[F]) next(sampleRate float64, centerFrequency float64) (float64, float64) {
	s.envelope += (s.key() - s.envelope) * s.rise

	offset := float64(s.signal.Frequency) - centerFrequency + s.signal.Drift*s.elapsed
	s.phase = math.Mod(s.phase+2*math.Pi*offset/sampleRate, 2*math.Pi)
	s.elapsed += 1 / sampleRate
	s.sampleCount++

	amplitude := s.amplitude * s.envelope * s.fade()
	return amplitude * math.Cos(s.phase), amplitude * math.Sin(s.phase)
}

func (s *signalState[F]) key() float64 {
	if len(s.elements) == 0 {
		return 0
	}
	if s.turnPeriod > 0 {
		s.updateTurn()
		if s.silent {
			return 0
		}
	}
	if s.left == 0 {
		s.index = (s.index + 1) % len(s.elements)
		if s.index == 0 && s.turnPeriod > 0 {
			// the ring is at its beginning again, so the text of this turn is complete
			s.silent = true
			return 0
		}
		s.left = s.elements[s.index].samples
	}
	s.left--
	if s.elements[s.index].on {
		return 1
	}
	return 0
}

// updateTurn looks if a new turn began. The signal then sends its text one more time, from the
// beginning. Before the first turn, and after the end of the text of a turn, the signal is silent.
func (s *signalState[F]) updateTurn() {
	if s.sampleCount < s.turnOffset {
		s.silent = true
		return
	}

	turn := (s.sampleCount - s.turnOffset) / s.turnPeriod
	if turn == s.turn {
		return
	}
	s.turn = turn
	s.index = 0
	s.left = s.elements[0].samples
	s.silent = false
}

func (s *signalState[F]) fade() float64 {
	if s.signal.FadeDepth <= 0 {
		return 1
	}
	return 1 - s.signal.FadeDepth*0.5*(1+math.Sin(2*math.Pi*s.signal.FadeRate*s.elapsed))
}

func toElements(text string, ditSamples int) []element {
	result := make([]element, 0, len(text)*10)
	add := func(symbol cw.Symbol) {
		result = append(result, element{on: symbol.KeyDown, samples: symbol.Weight * ditSamples})
	}

	gap := cw.CharBreak
	for _, r := range strings.ToLower(text) {
		if r == ' ' {
			gap = cw.WordBreak
			continue
		}
		symbols, ok := cw.Code[r]
		if !ok {
			continue
		}

		if len(result) > 0 {
			add(gap)
		}
		gap = cw.CharBreak

		for i, symbol := range symbols {
			if i > 0 {
				add(cw.SymbolBreak)
			}
			add(symbol)
		}
	}
	if len(result) > 0 {
		add(cw.WordBreak)
	}
	return result
}

package rx

import (
	"fmt"
	"io"
	"os"

	"github.com/ftl/sdrainer/dsp"
)

type Reporter[F dsp.Number] interface {
	ListenerActivated(listener string, frequency F)
	ListenerDeactivated(listener string, frequency F)
	CallsignDecoded(listener string, callsign string, frequency F, count int, weight int)
	CallsignSpotted(listener string, callsign string, frequency F)
	SpotTimeout(listener string, callsign string, frequency F)
}

type TextReporter[F dsp.Number] struct {
	out io.Writer
}

func NewTextReporter[F dsp.Number](out io.Writer) *TextReporter[F] {
	if out == nil {
		out = os.Stdout
	}
	return &TextReporter[F]{out: out}
}

func (r *TextReporter[F]) ListenerActivated(listener string, frequency F) {
	fmt.Fprintf(r.out, "listener %s activated on %.2fkHz\n", listener, float64(frequency)/1000)
}

func (r *TextReporter[F]) ListenerDeactivated(listener string, frequency F) {
	fmt.Fprintf(r.out, "listener %s deactivated on %.2fkHz\n", listener, float64(frequency)/1000)
}

func (r *TextReporter[F]) CallsignDecoded(listener string, callsign string, frequency F, count int, weight int) {
	fmt.Fprintf(r.out, "callsign %s heard %d times on %.2fkHz, weight is %d\n", callsign, count, float64(frequency)/1000, weight)
}

func (r *TextReporter[F]) CallsignSpotted(listener string, callsign string, frequency F) {
	fmt.Fprintf(r.out, "callsign %s spotted on %.2fkHz\n", callsign, float64(frequency)/1000)
}

func (r *TextReporter[F]) SpotTimeout(listener string, callsign string, frequency F) {
	fmt.Fprintf(r.out, "spot of %s on %.2fkHz timed out\n", callsign, float64(frequency)/1000)
}

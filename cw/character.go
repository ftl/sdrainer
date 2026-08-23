package cw

import (
	"fmt"
	"io"
)

// Character is one decoded character, with the state of the decoder at the moment of the decode.
// Start and End count the ticks from the first call of Decoder.Tick, so a consumer with its own
// time base converts them itself: one tick is the hop of its spectral analysis.
type Character struct {
	Rune  rune
	WPM   float64
	Start int64
	End   int64
}

// CharacterSink takes the characters of a decoder. CharacterDecoded runs in the goroutine of the
// caller of Decoder.Tick, so an implementation must not block.
type CharacterSink interface {
	CharacterDecoded(Character)
}

// TextSink gives the characters to an io.Writer, for a consumer that wants only the text.
type TextSink struct {
	out io.Writer
}

func NewTextSink(out io.Writer) *TextSink {
	return &TextSink{out: out}
}

func (s *TextSink) CharacterDecoded(character Character) {
	if s.out == nil {
		return
	}
	fmt.Fprint(s.out, string(character.Rune))
}

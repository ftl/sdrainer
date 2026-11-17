package core

import (
	"fmt"
	"sync"
	"time"

	"github.com/ftl/hamradio/callsign"

	"github.com/ftl/sdrainer/dsp"
)

type IgnoreList map[string]bool

func ParseIgnoreList(callsigns []string) (IgnoreList, error) {
	result := make(IgnoreList, len(callsigns))
	for _, s := range callsigns {
		call, err := callsign.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("cannot ignore callsign: %w", err)
		}
		// the base call covers each variant: DL1ABC covers DL1ABC/P and F/DL1ABC
		result[call.BaseCall] = true
	}
	return result, nil
}

func (l IgnoreList) contains(call callsign.Callsign) bool {
	return l[call.BaseCall]
}

func (l IgnoreList) containsString(s string) bool {
	call, err := callsign.Parse(s)
	return err == nil && l.contains(call)
}

func IgnoringSpotter[F dsp.Number](ignored IgnoreList, next Spotter[F]) Spotter[F] {
	return &ignoringSpotter[F]{ignored: ignored, next: next}
}

type ignoringSpotter[F dsp.Number] struct {
	ignored IgnoreList
	next    Spotter[F]
}

func (s *ignoringSpotter[F]) Spot(call string, frequency F, msg string, timestamp time.Time) {
	if s.ignored.containsString(call) {
		return
	}
	s.next.Spot(call, frequency, msg, timestamp)
}

func (s *ignoringSpotter[F]) RemoveSpot(call string) {
	s.next.RemoveSpot(call)
}

func IgnoringChannelService[F dsp.Number](ignored IgnoreList, next ChannelService[F]) ChannelService[F] {
	return &ignoringChannelService[F]{
		ignored: ignored,
		next:    next,
		hidden:  make(map[ChannelID]bool),
	}
}

type ignoringChannelService[F dsp.Number] struct {
	ignored IgnoreList
	next    ChannelService[F]

	// more than one pipeline can use the same service
	lock sync.Mutex
	// A channel gets its callsign after it was created, so the next service gets ChannelDestroyed
	// when the channel shows an ignored callsign, and ChannelCreated again when it shows a different
	// callsign later.
	hidden map[ChannelID]bool
}

func (s *ignoringChannelService[F]) isHidden(id ChannelID) bool {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.hidden[id]
}

func (s *ignoringChannelService[F]) setHidden(id ChannelID, hidden bool) (wasHidden bool) {
	s.lock.Lock()
	defer s.lock.Unlock()
	wasHidden = s.hidden[id]
	if hidden {
		s.hidden[id] = true
	} else {
		delete(s.hidden, id)
	}
	return wasHidden
}

func (s *ignoringChannelService[F]) Active() bool {
	return s.next.Active()
}

func (s *ignoringChannelService[F]) ChannelCreated(channel Channel[F]) {
	s.next.ChannelCreated(channel)
}

func (s *ignoringChannelService[F]) ChannelDestroyed(channel Channel[F]) {
	if s.setHidden(channel.ID, false) {
		return
	}
	s.next.ChannelDestroyed(channel)
}

func (s *ignoringChannelService[F]) ChannelStateChanged(channel Channel[F]) {
	if s.isHidden(channel.ID) {
		return
	}
	s.next.ChannelStateChanged(channel)
}

func (s *ignoringChannelService[F]) ChannelCharacterReceived(channel Channel[F], character rune, offset int64) {
	if s.isHidden(channel.ID) {
		return
	}
	s.next.ChannelCharacterReceived(channel, character, offset)
}

func (s *ignoringChannelService[F]) ChannelQualityChanged(channel Channel[F]) {
	if s.isHidden(channel.ID) {
		return
	}
	s.next.ChannelQualityChanged(channel)
}

func (s *ignoringChannelService[F]) ChannelRunningCallsignDetected(channel Channel[F]) {
	hide := s.ignored.contains(channel.Callsign)
	wasHidden := s.setHidden(channel.ID, hide)
	switch {
	case hide && !wasHidden:
		// the ignored callsign must not reach the client, not even in this event
		channel.Callsign = callsign.NoCallsign
		channel.Quality = NoQuality
		s.next.ChannelDestroyed(channel)
	case !hide && wasHidden:
		s.next.ChannelCreated(channel)
		s.next.ChannelRunningCallsignDetected(channel)
	case !hide:
		s.next.ChannelRunningCallsignDetected(channel)
	}
}

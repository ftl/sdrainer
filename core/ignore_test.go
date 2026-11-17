package core

import (
	"testing"
	"time"

	"github.com/ftl/hamradio/callsign"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingSpotter struct {
	spots []string
}

func (s *recordingSpotter) Spot(call string, _ int, _ string, _ time.Time) {
	s.spots = append(s.spots, call)
}

func (s *recordingSpotter) RemoveSpot(string) {}

type recordingChannelService struct {
	NullChannelService[int]
	events []string
}

func (s *recordingChannelService) record(event string, channel Channel[int]) {
	s.events = append(s.events, event+" "+string(channel.ID)+" "+channel.Callsign.String())
}

func (s *recordingChannelService) ChannelCreated(c Channel[int])      { s.record("created", c) }
func (s *recordingChannelService) ChannelDestroyed(c Channel[int])    { s.record("destroyed", c) }
func (s *recordingChannelService) ChannelStateChanged(c Channel[int]) { s.record("state", c) }
func (s *recordingChannelService) ChannelQualityChanged(c Channel[int]) {
	s.record("quality", c)
}
func (s *recordingChannelService) ChannelCharacterReceived(c Channel[int], _ rune, _ int64) {
	s.record("character", c)
}
func (s *recordingChannelService) ChannelRunningCallsignDetected(c Channel[int]) {
	s.record("callsign", c)
}

func TestParseIgnoreList(t *testing.T) {
	_, err := ParseIgnoreList([]string{"DL1ABC", "no call"})
	assert.Error(t, err)
}

func TestIgnoringSpotter(t *testing.T) {
	ignored, err := ParseIgnoreList([]string{" dl1abc "})
	require.NoError(t, err)
	next := &recordingSpotter{}
	spotter := IgnoringSpotter[int](ignored, next)

	spotter.Spot("DL1ABC", 7020000, "", time.Now())
	spotter.Spot("DL1ABC/P", 7020000, "", time.Now())
	spotter.Spot("F/DL1ABC", 7020000, "", time.Now())
	spotter.Spot("DL2XYZ", 7021000, "", time.Now())

	assert.Equal(t, []string{"DL2XYZ"}, next.spots)
}

func TestIgnoringChannelService(t *testing.T) {
	ignoreList, err := ParseIgnoreList([]string{"DL1ABC"})
	require.NoError(t, err)
	next := &recordingChannelService{}
	service := IgnoringChannelService[int](ignoreList, next)
	plain := Channel[int]{ID: "1"}
	ignored := plain
	ignored.Callsign = callsign.MustParse("DL1ABC/P")
	other := plain
	other.Callsign = callsign.MustParse("DL2XYZ")

	service.ChannelCreated(plain)
	service.ChannelCharacterReceived(plain, 'd', 0)
	service.ChannelRunningCallsignDetected(ignored)
	service.ChannelQualityChanged(ignored)
	service.ChannelCharacterReceived(ignored, 'd', 0)
	service.ChannelStateChanged(ignored)
	service.ChannelRunningCallsignDetected(other)
	service.ChannelRunningCallsignDetected(ignored)
	service.ChannelDestroyed(ignored)

	assert.Equal(t, []string{
		"created 1 ",
		"character 1 ",
		"destroyed 1 ",
		"created 1 DL2XYZ",
		"callsign 1 DL2XYZ",
		"destroyed 1 ",
	}, next.events)
}

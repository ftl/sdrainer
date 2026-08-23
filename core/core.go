package core

import (
	"time"

	"github.com/ftl/hamradio/callsign"
	"github.com/ftl/sdrainer/dsp"
)

/* Spotter */

// Spotter takes the callsign of a station that runs, and it takes it back when that station is
// gone. A skimmer that only announces a new station gives a wrong picture of the band: a consumer
// that connects later must know which stations are there now, and a station that stopped must not
// stand in that list.
type Spotter[F dsp.Number] interface {
	Spot(callsign string, frequency F, msg string, timestamp time.Time)

	// RemoveSpot says that the station with this callsign has no channel any more. The pipeline
	// calls it when a channel goes away, and only for a channel that gave a callsign.
	RemoveSpot(callsign string)
}

type NullSpotter[F dsp.Number] struct{}

func (*NullSpotter[F]) Spot(string, F, string, time.Time) {}
func (*NullSpotter[F]) RemoveSpot(string)                 {}

/* Scope Service */

type StreamID string
type MarkerID string
type ValueID string

type Frame struct {
	Stream    StreamID
	Timestamp time.Time
}

type TimeFrame struct {
	Frame
	Values map[ValueID]float64
}

type SpectralFrame struct {
	Frame
	FromFrequency    float64
	ToFrequency      float64
	Values           []float64
	FrequencyMarkers map[MarkerID]float64
	MagnitudeMarkers map[MarkerID]float64
}

type ScopeService interface {
	Active() bool
	SendTimeFrame(timeFrame *TimeFrame)
	SendSpectralFrame(spectralFrame *SpectralFrame)
}

type NullScopeService struct{}

func (s *NullScopeService) Active() bool                                   { return false }
func (s *NullScopeService) SendTimeFrame(timeFrame *TimeFrame)             {}
func (s *NullScopeService) SendSpectralFrame(spectralFrame *SpectralFrame) {}

/* Channel Service */

type ChannelID string

type ChannelState int

const (
	NewChannel ChannelState = iota
	ConfirmedChannel
	ActiveChannel
	IdleChannel
	DeadChannel
)

// Channel represents a CW signal that the pipeline found and follows.
type Channel[F dsp.Number] struct {
	ID        ChannelID
	Frequency F
	WPM       int
	SNR       float64
	State     ChannelState
	Callsign  callsign.Callsign
}

type ChannelLifecycleListener[F dsp.Number] interface {
	ChannelCreated(Channel[F])
	ChannelDestroyed(Channel[F])
}

type ChannelStateListener[F dsp.Number] interface {
	ChannelStateChanged(Channel[F])
}

type ChannelReceiveListener[F dsp.Number] interface {
	ChannelCharacterReceived(Channel[F], rune, int64)
}

type ChannelRunningCallsignListener[F dsp.Number] interface {
	ChannelRunningCallsignDetected(Channel[F])
}

type ChannelService[F dsp.Number] interface {
	Active() bool
	ChannelCreated(channel Channel[F])
	ChannelDestroyed(channel Channel[F])
	ChannelStateChanged(channel Channel[F])
	ChannelCharacterReceived(channel Channel[F], character rune, offset int64)
	ChannelRunningCallsignDetected(channel Channel[F])
}

type NullChannelService[F dsp.Number] struct{}

func (s *NullChannelService[F]) Active() bool                           { return false }
func (s *NullChannelService[F]) ChannelCreated(channel Channel[F])      {}
func (s *NullChannelService[F]) ChannelDestroyed(channel Channel[F])    {}
func (s *NullChannelService[F]) ChannelStateChanged(channel Channel[F]) {}
func (s *NullChannelService[F]) ChannelCharacterReceived(channel Channel[F], character rune, offset int64) {
}
func (s *NullChannelService[F]) ChannelRunningCallsignDetected(channel Channel[F]) {}

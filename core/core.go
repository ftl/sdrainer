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

// ChannelQuality says how much a consumer can trust the callsign of a channel. The values are the
// tags of the algorithm of CT1BOH, which AR-Cluster 6 gives to a client that asks for them, so a
// consumer that already reads those tags needs no new knowledge.
//
// doc/architecture.md, section 9.1, holds the rule behind each value.
type ChannelQuality rune

const (
	// NoQuality holds for a channel that gave no callsign yet.
	NoQuality ChannelQuality = 0

	// UnverifiedQuality says that the evidence is thin: the callsign reached the smallest count of
	// the hits that gives a spot at all, and no more.
	UnverifiedQuality ChannelQuality = '?'

	// ValidQuality says that the receiver is sure. It is not the V of AR-Cluster 6, which says that
	// three receivers at three places agree: here one receiver read the same callsign many times,
	// over more than one transmission.
	ValidQuality ChannelQuality = 'V'

	// QSYQuality says that this callsign was valid before on another frequency of the same band.
	QSYQuality ChannelQuality = 'Q'

	// BustedQuality says that this callsign stands one character beside a callsign that the same
	// channel already made valid.
	BustedQuality ChannelQuality = 'B'
)

func (q ChannelQuality) String() string {
	if q == NoQuality {
		return ""
	}
	return string(rune(q))
}

// Channel represents a CW signal that the pipeline found and follows.
type Channel[F dsp.Number] struct {
	ID        ChannelID
	Frequency F
	WPM       int
	SNR       float64
	State     ChannelState
	Callsign  callsign.Callsign
	Quality   ChannelQuality
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

// ChannelQualityListener is notified when the quality of the callsign of a channel changes. The
// quality of a channel goes up while the receiver reads the callsign again and again, and it can go
// down when the receiver finds that the callsign is an error.
type ChannelQualityListener[F dsp.Number] interface {
	ChannelQualityChanged(Channel[F])
}

type ChannelService[F dsp.Number] interface {
	Active() bool
	ChannelCreated(channel Channel[F])
	ChannelDestroyed(channel Channel[F])
	ChannelStateChanged(channel Channel[F])
	ChannelCharacterReceived(channel Channel[F], character rune, offset int64)
	ChannelRunningCallsignDetected(channel Channel[F])
	ChannelQualityChanged(channel Channel[F])
}

type NullChannelService[F dsp.Number] struct{}

func (s *NullChannelService[F]) Active() bool                           { return false }
func (s *NullChannelService[F]) ChannelCreated(channel Channel[F])      {}
func (s *NullChannelService[F]) ChannelDestroyed(channel Channel[F])    {}
func (s *NullChannelService[F]) ChannelStateChanged(channel Channel[F]) {}
func (s *NullChannelService[F]) ChannelCharacterReceived(channel Channel[F], character rune, offset int64) {
}
func (s *NullChannelService[F]) ChannelRunningCallsignDetected(channel Channel[F]) {}
func (s *NullChannelService[F]) ChannelQualityChanged(channel Channel[F])          {}

// Package scope provides a visualisation of the inner workings of SDRainer in form of
// spectral and time domain plots.
package scope

import (
	"time"
)

type StreamID string
type ChannelID string
type MarkerID string

type Frame struct {
	Stream    StreamID
	Timestamp time.Time
}

type TimeFrame struct {
	Frame
	Values map[ChannelID]float64
}

type SpectralFrame struct {
	Frame
	FromFrequency    float64
	ToFrequency      float64
	Values           []float64
	FrequencyMarkers map[MarkerID]float64
	MagnitudeMarkers map[MarkerID]float64
}

// Scope shows time and spectral frames for a visualisation of the inner workings of SDRainer.
type Scope interface {
	Active() bool
	ShowTimeFrame(timeFrame *TimeFrame)
	ShowSpectralFrame(spectralFrame *SpectralFrame)
}

// NullScope is a scope that does nothing. It can be used if no scope is configured.
type NullScope struct{}

func NewNullScope() *NullScope                                      { return &NullScope{} }
func (s *NullScope) Active() bool                                   { return false }
func (s *NullScope) ShowTimeFrame(timeFrame *TimeFrame)             {}
func (s *NullScope) ShowSpectralFrame(spectralFrame *SpectralFrame) {}

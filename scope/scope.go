// Package scope provides a visualisation of the inner workings of SDRainer in form of
// spectral and time domain plots.
package scope

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/ftl/sdrainer/scope/pb"
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

type Scope struct {
	server *grpcServer
}

func NewScope(address string) (*Scope, error) {
	server, err := newGRPCServer(address, defaultOutBufferSize)
	if err != nil {
		return nil, err
	}
	return &Scope{server: server}, nil
}

func (s *Scope) Start() error {
	return s.server.Start()
}

func (s *Scope) Stop() {
	s.server.Stop()
}

func (s *Scope) SendTimeFrame(timeFrame *TimeFrame) {
	grpcTimeFrame := &pb.TimeFrame{
		StreamId:  string(timeFrame.Stream),
		Timestamp: timestamppb.New(timeFrame.Timestamp),
		Values:    make(map[string]float32),
	}
	for channel, value := range timeFrame.Values {
		grpcTimeFrame.Values[string(channel)] = float32(value)
	}

	frame := &pb.Frame{
		Frame: &pb.Frame_TimeFrame{
			TimeFrame: grpcTimeFrame,
		},
	}
	s.server.SendFrame(frame)
}

func (s *Scope) SendSpectralFrame(spectralFrame *SpectralFrame) {
	grpcSpectralFrame := &pb.SpectralFrame{
		StreamId:         string(spectralFrame.Stream),
		Timestamp:        timestamppb.New(spectralFrame.Timestamp),
		FromFrequency:    float32(spectralFrame.FromFrequency),
		ToFrequency:      float32(spectralFrame.ToFrequency),
		Values:           make([]float32, len(spectralFrame.Values)),
		FrequencyMarkers: make(map[string]float32),
		MagnitudeMarkers: make(map[string]float32),
	}
	for i, value := range spectralFrame.Values {
		grpcSpectralFrame.Values[i] = float32(value)
	}
	for marker, value := range spectralFrame.FrequencyMarkers {
		grpcSpectralFrame.FrequencyMarkers[string(marker)] = float32(value)
	}
	for marker, value := range spectralFrame.MagnitudeMarkers {
		grpcSpectralFrame.MagnitudeMarkers[string(marker)] = float32(value)
	}

	frame := &pb.Frame{
		Frame: &pb.Frame_SpectralFrame{
			SpectralFrame: grpcSpectralFrame,
		},
	}
	s.server.SendFrame(frame)
}

package scope

import (
	"fmt"
	"log"
	"net"
	"sync"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/ftl/sdrainer/scope/pb"
)

// ScopeServer is scope that serves frames over a network connection to remote clients.
type ScopeServer struct {
	address string

	server     *grpcServer
	serverLock *sync.Mutex
}

// NewScopeServer creates a new scope server that listens on the given address.
func NewScopeServer(address string) *ScopeServer {
	return &ScopeServer{
		address:    address,
		server:     nil,
		serverLock: &sync.Mutex{},
	}
}

func (s *ScopeServer) Active() bool {
	s.serverLock.Lock()
	defer s.serverLock.Unlock()
	return s.server != nil
}

func (s *ScopeServer) Addr() net.Addr {
	s.serverLock.Lock()
	defer s.serverLock.Unlock()
	if s.server != nil {
		return s.server.Addr()
	}
	return nil
}

func (s *ScopeServer) Start() error {
	if s.Active() {
		return fmt.Errorf("scope was already started")
	}

	server, err := newGRPCServer(s.address, defaultOutBufferSize)
	if err != nil {
		return err
	}

	go func() {
		s.serverLock.Lock()
		if s.server != nil {
			s.serverLock.Unlock()
			return
		}
		s.server = server
		s.serverLock.Unlock()

		err := s.server.Start()
		if err != nil {
			log.Printf("Scope server failed: %v", err)
		}

		s.serverLock.Lock()
		s.server = nil
		s.serverLock.Unlock()
	}()

	return nil
}

func (s *ScopeServer) Stop() {
	if !s.Active() {
		return
	}

	s.server.Stop()
}

func (s *ScopeServer) ShowTimeFrame(timeFrame *TimeFrame) {
	if !s.Active() {
		return
	}

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

func (s *ScopeServer) ShowSpectralFrame(spectralFrame *SpectralFrame) {
	if !s.Active() {
		return
	}

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

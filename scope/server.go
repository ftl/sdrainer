package scope

import (
	"fmt"
	"log"
	"net"
	"sync"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/dsp"
	"github.com/ftl/sdrainer/scope/pb"
)

// ScopeServer is a scope that serves frames over a network connection to remote clients.
type ScopeServer[F dsp.Number] struct {
	address string

	server     *grpcServer
	serverLock *sync.Mutex
}

// NewScopeServer creates a new scope server that listens on the given address.
func NewScopeServer[F dsp.Number](address string) *ScopeServer[F] {
	return &ScopeServer[F]{
		address:    address,
		server:     nil,
		serverLock: &sync.Mutex{},
	}
}

func (s *ScopeServer[_]) Active() bool {
	s.serverLock.Lock()
	defer s.serverLock.Unlock()
	return s.server != nil
}

func (s *ScopeServer[_]) Addr() net.Addr {
	s.serverLock.Lock()
	defer s.serverLock.Unlock()
	if s.server != nil {
		return s.server.Addr()
	}
	return nil
}

func (s *ScopeServer[_]) Start() error {
	if s.Active() {
		return fmt.Errorf("scope was already started")
	}

	server, err := newGRPCServer(s.address, defaultScopeOutBufferSize, defaultChannelOutBufferSize)
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

		// the local variable and not the field, because Stop can set the field to nil at any time
		err := server.Start()
		if err != nil {
			log.Printf("Scope server failed: %v", err)
		}

		s.serverLock.Lock()
		s.server = nil
		s.serverLock.Unlock()
	}()

	return nil
}

// activeServer gives the running server, or nil if no server runs. Each method that uses the server
// must take it from here: Active and a read of the field are two steps, and the goroutine of Start
// can change the field between them.
func (s *ScopeServer[_]) activeServer() *grpcServer {
	s.serverLock.Lock()
	defer s.serverLock.Unlock()
	return s.server
}

func (s *ScopeServer[_]) Stop() {
	server := s.activeServer()
	if server == nil {
		return
	}

	server.Stop()
}

func (s *ScopeServer[_]) SendSpectralFrame(spectralFrame *core.SpectralFrame) {
	server := s.activeServer()
	if server == nil {
		return
	}

	frame := &pb.SpectralFrame{
		StreamId:         string(spectralFrame.Stream),
		Timestamp:        timestamppb.New(spectralFrame.Timestamp),
		FromFrequency:    float32(spectralFrame.FromFrequency),
		ToFrequency:      float32(spectralFrame.ToFrequency),
		Values:           make([]float32, len(spectralFrame.Values)),
		FrequencyMarkers: make(map[string]float32),
		MagnitudeMarkers: make(map[string]float32),
	}
	for i, value := range spectralFrame.Values {
		frame.Values[i] = float32(value)
	}
	for marker, value := range spectralFrame.FrequencyMarkers {
		frame.FrequencyMarkers[string(marker)] = float32(value)
	}
	for marker, value := range spectralFrame.MagnitudeMarkers {
		frame.MagnitudeMarkers[string(marker)] = float32(value)
	}

	server.SendSpectralFrame(frame)
}

func (s *ScopeServer[_]) SendTimeFrame(timeFrame *core.TimeFrame) {
	server := s.activeServer()
	if server == nil {
		return
	}

	frame := &pb.TimeFrame{
		StreamId:  string(timeFrame.Stream),
		Timestamp: timestamppb.New(timeFrame.Timestamp),
		Values:    make(map[string]float32),
	}
	for channel, value := range timeFrame.Values {
		frame.Values[string(channel)] = float32(value)
	}

	server.SendTimeFrame(frame)
}

func (s *ScopeServer[F]) ChannelCreated(channel core.Channel[F]) {
	server := s.activeServer()
	if server == nil {
		return
	}

	server.SendChannelCreated(&pb.ChannelCreated{Channel: toPBChannel(channel)})
}

func (s *ScopeServer[F]) ChannelDestroyed(channel core.Channel[F]) {
	server := s.activeServer()
	if server == nil {
		return
	}

	server.SendChannelDestroyed(&pb.ChannelDestroyed{Channel: toPBChannel(channel)})
}

func (s *ScopeServer[F]) ChannelStateChanged(channel core.Channel[F]) {
	server := s.activeServer()
	if server == nil {
		return
	}

	server.SendChannelStateChanged(&pb.ChannelStateChanged{Channel: toPBChannel(channel)})
}

func (s *ScopeServer[F]) ChannelCharacterReceived(channel core.Channel[F], character rune, offset int64) {
	server := s.activeServer()
	if server == nil {
		return
	}

	server.SendChannelCharacterReceived(&pb.ChannelCharacterReceived{
		Channel:   toPBChannel(channel),
		Character: string(character),
		Offset:    offset,
	})
}

func (s *ScopeServer[F]) ChannelRunningCallsignDetected(channel core.Channel[F]) {
	server := s.activeServer()
	if server == nil {
		return
	}

	server.SendChannelRunningCallsignDetected(&pb.ChannelRunningCallsignDetected{
		Channel: toPBChannel(channel),
	})
}

func toPBChannel[F dsp.Number](channel core.Channel[F]) *pb.Channel {
	return &pb.Channel{
		Id:        string(channel.ID),
		Frequency: float32(channel.Frequency),
		Wpm:       int32(channel.WPM),
		Snr:       float32(channel.SNR),
		State:     toPBChannelState(channel.State),
		Callsign:  channel.Callsign.String(),
	}
}

func toPBChannelState(state core.ChannelState) pb.ChannelState {
	switch state {
	case core.NewChannel:
		return pb.ChannelState_CHANNEL_STATE_NEW
	case core.ConfirmedChannel:
		return pb.ChannelState_CHANNEL_STATE_CONFIRMED
	case core.ActiveChannel:
		return pb.ChannelState_CHANNEL_STATE_ACTIVE
	case core.IdleChannel:
		return pb.ChannelState_CHANNEL_STATE_IDLE
	case core.DeadChannel:
		return pb.ChannelState_CHANNEL_STATE_DEAD
	}
	return pb.ChannelState_CHANNEL_STATE_NEW
}

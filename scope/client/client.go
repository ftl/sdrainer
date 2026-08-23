package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/ftl/hamradio/callsign"
	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/scope/pb"
)

// Client allows to connect to a scope server and receive frames.
type Client struct {
	address string

	conn          *grpc.ClientConn
	scopeClient   pb.ScopeServiceClient
	channelClient pb.ChannelServiceClient
}

// NewClient creates a new client for the given address.
func NewClient(address string) *Client {
	return &Client{
		address: address,
	}
}

// Open the connection to the scope server.
func (c *Client) Open() error {
	if c.conn != nil {
		return fmt.Errorf("already connected")
	}

	conn, err := grpc.NewClient(c.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("cannot connect to scope server: %v", err)
	}
	c.conn = conn
	c.scopeClient = pb.NewScopeServiceClient(conn)
	c.channelClient = pb.NewChannelServiceClient(conn)

	return nil
}

// Close the connection to the scope server.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// GetScopeEvents provides a channel to receive spectral and time frames from the scope service.
// To stop reading use a cancelable context.
func (c *Client) GetScopeEvents(ctx context.Context) (chan any, error) {
	stream, err := c.scopeClient.GetScopeEvents(context.Background(), nil)
	if err != nil {
		return nil, fmt.Errorf("cannot open spectral frame stream: %v", err)
	}
	result := make(chan any, 1)

	go c.readFromScopeStream(result, stream)

	return result, nil
}

func (c *Client) readFromScopeStream(out chan<- any, stream grpc.ServerStreamingClient[pb.ScopeEvent]) {
	defer close(out)

	for {
		grpcEvent, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			log.Print("incoming scope stream is closed")
			return
		}
		if err != nil {
			st, ok := status.FromError(err)
			if ok && st.Code() == codes.Canceled {
				log.Println("scope stream stopped: context canceled by client")
			} else {
				log.Printf("scope stream error: %v", err)
			}
			return
		}

		switch event := grpcEvent.Event.(type) {
		case *pb.ScopeEvent_SpectralFrame:
			out <- readSpectralFrame(event.SpectralFrame)
		case *pb.ScopeEvent_TimeFrame:
			out <- readTimeFrame(event.TimeFrame)
		default:
			// ignore unknown events
		}
	}
}

func readSpectralFrame(spectralFrame *pb.SpectralFrame) *core.SpectralFrame {
	result := &core.SpectralFrame{
		Frame: core.Frame{
			Stream:    core.StreamID(spectralFrame.StreamId),
			Timestamp: spectralFrame.Timestamp.AsTime(),
		},
		FromFrequency:    float64(spectralFrame.FromFrequency),
		ToFrequency:      float64(spectralFrame.ToFrequency),
		Values:           make([]float64, len(spectralFrame.Values)),
		FrequencyMarkers: make(map[core.MarkerID]float64),
		MagnitudeMarkers: make(map[core.MarkerID]float64),
	}
	for i, v := range spectralFrame.Values {
		result.Values[i] = float64(v)
	}
	for k, v := range spectralFrame.FrequencyMarkers {
		result.FrequencyMarkers[core.MarkerID(k)] = float64(v)
	}
	for k, v := range spectralFrame.MagnitudeMarkers {
		result.MagnitudeMarkers[core.MarkerID(k)] = float64(v)
	}
	return result
}

func readTimeFrame(timeFrame *pb.TimeFrame) *core.TimeFrame {
	result := &core.TimeFrame{
		Frame: core.Frame{
			Stream:    core.StreamID(timeFrame.StreamId),
			Timestamp: timeFrame.Timestamp.AsTime(),
		},
		Values: make(map[core.ValueID]float64),
	}
	for k, v := range timeFrame.Values {
		result.Values[core.ValueID(k)] = float64(v)
	}
	return result
}

// GetChannelEvents provides a channel to receive channel-related events from the channel service.
// To stop reading use a cancelable context.
func (c *Client) GetChannelEvents(ctx context.Context) (chan any, error) {
	stream, err := c.channelClient.GetChannelEvents(context.Background(), nil)
	if err != nil {
		return nil, fmt.Errorf("cannot open spectral frame stream: %v", err)
	}
	result := make(chan any, 1)

	go c.readFromChannelStream(result, stream)

	return result, nil
}

func (c *Client) readFromChannelStream(out chan<- any, stream grpc.ServerStreamingClient[pb.ChannelEvent]) {
	defer close(out)

	for {
		grpcEvent, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			log.Print("incoming stream is closed")
			return
		}
		if err != nil {
			st, ok := status.FromError(err)
			if ok && st.Code() == codes.Canceled {
				log.Println("stream stopped: context canceled by client")
			} else {
				log.Printf("stream error: %v", err)
			}
			return
		}

		switch event := grpcEvent.Event.(type) {
		case *pb.ChannelEvent_ChannelCreated:
			out <- ChannelCreated{Channel: readChannel(event.ChannelCreated.Channel)}
		case *pb.ChannelEvent_ChannelDestroyed:
			out <- ChannelDestroyed{Channel: readChannel(event.ChannelDestroyed.Channel)}
		case *pb.ChannelEvent_ChannelStateChanged:
			out <- ChannelStateChanged{Channel: readChannel(event.ChannelStateChanged.Channel)}
		case *pb.ChannelEvent_ChannelCharacterReceived:
			out <- ChannelCharacterReceived{
				Channel:   readChannel(event.ChannelCharacterReceived.Channel),
				Character: firstRune(event.ChannelCharacterReceived.Character),
				Offset:    event.ChannelCharacterReceived.Offset,
			}
		case *pb.ChannelEvent_ChannelRunningCallsignDetected:
			out <- ChannelRunningCallsignDetected{
				Channel: readChannel(event.ChannelRunningCallsignDetected.Channel),
			}
		default:
			// ignore unknown events
		}
	}
}

// The events of the channels that GetChannelEvents puts into its channel. They are values and not
// pointers, so a type switch of the consumer needs no nil check.
type (
	Channel = core.Channel[float64]

	ChannelCreated   struct{ Channel Channel }
	ChannelDestroyed struct{ Channel Channel }

	ChannelStateChanged struct{ Channel Channel }

	ChannelCharacterReceived struct {
		Channel   Channel
		Character rune
		Offset    int64
	}

	ChannelRunningCallsignDetected struct{ Channel Channel }
)

func readChannel(channel *pb.Channel) Channel {
	if channel == nil {
		return Channel{}
	}
	return Channel{
		ID:        core.ChannelID(channel.Id),
		Frequency: float64(channel.Frequency),
		WPM:       int(channel.Wpm),
		SNR:       float64(channel.Snr),
		State:     readChannelState(channel.State),
		Callsign:  readCallsign(channel.Callsign),
	}
}

func readCallsign(s string) callsign.Callsign {
	result, err := callsign.Parse(s)
	if err != nil {
		return callsign.NoCallsign
	}
	return result
}

func readChannelState(state pb.ChannelState) core.ChannelState {
	switch state {
	case pb.ChannelState_CHANNEL_STATE_NEW:
		return core.NewChannel
	case pb.ChannelState_CHANNEL_STATE_CONFIRMED:
		return core.ConfirmedChannel
	case pb.ChannelState_CHANNEL_STATE_ACTIVE:
		return core.ActiveChannel
	case pb.ChannelState_CHANNEL_STATE_IDLE:
		return core.IdleChannel
	case pb.ChannelState_CHANNEL_STATE_DEAD:
		return core.DeadChannel
	}
	return core.NewChannel
}

// firstRune gives the first rune of the character field. The field is a string, because protobuf
// has no rune type, and a prosign can need more than one rune later.
func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

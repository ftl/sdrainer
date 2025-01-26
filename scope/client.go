package scope

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/ftl/sdrainer/scope/pb"
)

// Client allows to connect to a scope server and receive frames.
type Client struct {
	address string

	conn   *grpc.ClientConn
	client pb.ScopeClient
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
	c.client = pb.NewScopeClient(conn)

	return nil
}

// Close the connection to the scope server.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// GetFrames provides a set of channels to receive frames from the scope server.
func (c *Client) GetFrames() (chan *TimeFrame, chan *SpectralFrame, error) {
	stream, err := c.client.GetFrames(context.Background(), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot open frame stream: %v", err)
	}

	timeFrames := make(chan *TimeFrame, 1)
	spectralFrames := make(chan *SpectralFrame, 1)
	go func() {
		for {
			frame, err := stream.Recv()
			if err != nil {
				close(timeFrames)
				close(spectralFrames)
				return
			}

			switch rawFrame := frame.Frame.(type) {
			case *pb.Frame_TimeFrame:
				timeFrames <- readTimeFrame(rawFrame.TimeFrame)
			case *pb.Frame_SpectralFrame:
				spectralFrames <- readSpectralFrame(rawFrame.SpectralFrame)
			}
		}
	}()

	return timeFrames, spectralFrames, nil
}

func readTimeFrame(timeFrame *pb.TimeFrame) *TimeFrame {
	result := &TimeFrame{
		Frame: Frame{
			Stream:    StreamID(timeFrame.StreamId),
			Timestamp: timeFrame.Timestamp.AsTime(),
		},
		Values: make(map[ChannelID]float64),
	}
	for k, v := range timeFrame.Values {
		result.Values[ChannelID(k)] = float64(v)
	}
	return result
}

func readSpectralFrame(spectralFrame *pb.SpectralFrame) *SpectralFrame {
	result := &SpectralFrame{
		Frame: Frame{
			Stream:    StreamID(spectralFrame.StreamId),
			Timestamp: spectralFrame.Timestamp.AsTime(),
		},
		FromFrequency:    float64(spectralFrame.FromFrequency),
		ToFrequency:      float64(spectralFrame.ToFrequency),
		Values:           make([]float64, len(spectralFrame.Values)),
		FrequencyMarkers: make(map[MarkerID]float64),
		MagnitudeMarkers: make(map[MarkerID]float64),
	}
	for i, v := range spectralFrame.Values {
		result.Values[i] = float64(v)
	}
	for k, v := range spectralFrame.FrequencyMarkers {
		result.FrequencyMarkers[MarkerID(k)] = float64(v)
	}
	for k, v := range spectralFrame.MagnitudeMarkers {
		result.MagnitudeMarkers[MarkerID(k)] = float64(v)
	}
	return result
}

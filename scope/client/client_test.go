package client

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ftl/hamradio/callsign"
	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/scope"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFrameRoundTrip(t *testing.T) {
	server := scope.NewScopeServer[float64]("localhost:")

	err := server.Start()
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)
	defer server.Stop()

	client := NewClient(server.Addr().String())
	err = client.Open()
	require.NoError(t, err)
	defer client.Close()

	eventsReceived := &sync.WaitGroup{}
	eventsReceived.Add(2)
	var timeFrame *core.TimeFrame
	var spectralFrame *core.SpectralFrame
	go func() {
		events, err := client.GetScopeEvents(context.Background())
		require.NoError(t, err)
		for range 2 {
			event := <-events
			switch e := event.(type) {
			case *core.SpectralFrame:
				spectralFrame = e
			case *core.TimeFrame:
				timeFrame = e
			}
			eventsReceived.Done()
		}
	}()
	time.Sleep(100 * time.Millisecond)

	server.SendTimeFrame(&core.TimeFrame{Frame: core.Frame{Stream: "frame1"}})
	server.SendSpectralFrame(&core.SpectralFrame{Frame: core.Frame{Stream: "frame2"}})
	eventsReceived.Wait()

	assert.Equal(t, core.StreamID("frame1"), timeFrame.Stream)
	assert.Equal(t, core.StreamID("frame2"), spectralFrame.Stream)
}

func TestChannelEventRoundTrip(t *testing.T) {
	server := scope.NewScopeServer[float64]("localhost:")

	err := server.Start()
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)
	defer server.Stop()

	client := NewClient(server.Addr().String())
	err = client.Open()
	require.NoError(t, err)
	defer client.Close()

	channel := core.Channel[float64]{
		ID: "3", Frequency: 7020000, WPM: 25, SNR: 17.5, State: core.ActiveChannel,
		Callsign: callsign.MustParse("DL1ABC"), Quality: core.ValidQuality,
	}
	idleChannel := channel
	idleChannel.State = core.IdleChannel
	expected := []any{
		ChannelCreated{Channel: channel},
		ChannelStateChanged{Channel: idleChannel},
		ChannelCharacterReceived{Channel: channel, Character: 'ü', Offset: 4711},
		ChannelRunningCallsignDetected{Channel: channel},
		ChannelQualityChanged{Channel: channel},
		ChannelDestroyed{Channel: channel},
	}

	received := make([]any, 0, len(expected))
	eventsReceived := &sync.WaitGroup{}
	eventsReceived.Add(len(expected))
	go func() {
		events, err := client.GetChannelEvents(context.Background())
		require.NoError(t, err)
		for range len(expected) {
			received = append(received, <-events)
			eventsReceived.Done()
		}
	}()
	time.Sleep(100 * time.Millisecond)

	server.ChannelCreated(channel)
	server.ChannelStateChanged(idleChannel)
	// a rune of more than one byte proves that the string field of the protobuf keeps it
	server.ChannelCharacterReceived(channel, 'ü', 4711)
	server.ChannelRunningCallsignDetected(channel)
	server.ChannelQualityChanged(channel)
	server.ChannelDestroyed(channel)
	eventsReceived.Wait()

	assert.Equal(t, expected, received)
}

// TestChannelWithoutAQualityRoundTrip covers the channel that gave no callsign: its quality is
// core.NoQuality, the field of the protobuf is then empty, and the client must give the same value
// back and not a rune of the value 0 that means something else.
func TestChannelWithoutAQualityRoundTrip(t *testing.T) {
	server := scope.NewScopeServer[float64]("localhost:")
	require.NoError(t, server.Start())
	time.Sleep(10 * time.Millisecond)
	defer server.Stop()

	client := NewClient(server.Addr().String())
	require.NoError(t, client.Open())
	defer client.Close()

	channel := core.Channel[float64]{ID: "1", Frequency: 7020000, State: core.ActiveChannel}

	received := make(chan any, 1)
	go func() {
		events, err := client.GetChannelEvents(context.Background())
		require.NoError(t, err)
		received <- <-events
	}()
	time.Sleep(100 * time.Millisecond)

	server.ChannelCreated(channel)

	select {
	case event := <-received:
		assert.Equal(t, ChannelCreated{Channel: channel}, event)
	case <-time.After(time.Second):
		t.Fatal("the event did not arrive")
	}
}

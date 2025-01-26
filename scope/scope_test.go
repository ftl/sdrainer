package scope

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartStopScope(t *testing.T) {
	scope := NewScope("localhost:")

	err := scope.Start()
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)
	assert.True(t, scope.Active())

	scope.Stop()
	time.Sleep(10 * time.Millisecond)
	assert.False(t, scope.Active())
}

func TestFrameRoundTrip(t *testing.T) {
	scope := NewScope("localhost:")

	err := scope.Start()
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)
	defer scope.Stop()

	client := NewClient(scope.Addr().String())
	err = client.Open()
	require.NoError(t, err)
	defer client.Close()

	framesReceived := &sync.WaitGroup{}
	framesReceived.Add(2)
	var timeFrame *TimeFrame
	var spectralFrame *SpectralFrame
	go func() {
		timeFrames, spectralFrames, err := client.GetFrames()
		require.NoError(t, err)
		for range 2 {
			select {
			case frame := <-timeFrames:
				timeFrame = frame
			case frame := <-spectralFrames:
				spectralFrame = frame
			}
			framesReceived.Done()
		}
	}()
	time.Sleep(100 * time.Millisecond)

	scope.SendTimeFrame(&TimeFrame{Frame: Frame{Stream: "frame1"}})
	scope.SendSpectralFrame(&SpectralFrame{Frame: Frame{Stream: "frame2"}})
	framesReceived.Wait()

	assert.Equal(t, StreamID("frame1"), timeFrame.Stream)
	assert.Equal(t, StreamID("frame2"), spectralFrame.Stream)
}

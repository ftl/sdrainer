package scope

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ftl/sdrainer/scope/pb"
)

func TestStartStopGRPCServer(t *testing.T) {
	server, err := newGRPCServer("localhost:", defaultOutBufferSize)
	require.NoError(t, err)

	serverResult := make(chan error)
	go func() {
		serverResult <- server.Start()
	}()

	time.Sleep(10 * time.Millisecond)
	server.Stop()
	assert.NoError(t, <-serverResult)
	assert.Nil(t, server.server)
}

func TestCloseUnresponsiveStreams(t *testing.T) {
	server, err := newGRPCServer("localhost:", 1)
	require.NoError(t, err)

	go func() {
		server.Start()
	}()
	defer func() {
		time.Sleep(10 * time.Millisecond)
		server.Stop()
	}()

	frames := server.getFrameStream()

	frame1 := &pb.Frame{}
	server.SendFrame(frame1)

	frame2 := &pb.Frame{}
	server.SendFrame(frame2)

	frame, open := <-frames
	assert.NotNil(t, frame)
	assert.Same(t, frame1, frame)
	assert.True(t, open)

	frame, open = <-frames
	assert.Nil(t, frame)
	assert.NotSame(t, frame2, frame)
	assert.False(t, open)

}

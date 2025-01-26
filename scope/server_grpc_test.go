package scope

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

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

func TestSendFramesToClient(t *testing.T) {
	server, err := newGRPCServer("localhost:", 1)
	require.NoError(t, err)
	require.NotNil(t, server)

	go func() {
		server.Start()
	}()
	time.Sleep(100 * time.Millisecond)
	defer server.Stop()

	conn, err := grpc.NewClient(server.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	client := pb.NewScopeClient(conn)
	stream, err := client.GetFrames(context.Background(), nil)
	require.NoError(t, err)

	frames := make([]*pb.Frame, 0)
	framesReceived := &sync.WaitGroup{}
	framesReceived.Add(2)
	go func() {
		for range 2 {
			frame, err := stream.Recv()
			require.NoError(t, err)
			frames = append(frames, frame)
			framesReceived.Done()
		}
	}()
	time.Sleep(100 * time.Millisecond)

	frame1 := &pb.Frame{Frame: &pb.Frame_TimeFrame{TimeFrame: &pb.TimeFrame{StreamId: "frame1"}}}
	server.SendFrame(frame1)

	frame2 := &pb.Frame{Frame: &pb.Frame_TimeFrame{TimeFrame: &pb.TimeFrame{StreamId: "frame2"}}}
	server.SendFrame(frame2)

	framesReceived.Wait()
	assert.Len(t, frames, 2)
}

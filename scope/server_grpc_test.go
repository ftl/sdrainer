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
	server, err := newGRPCServer("localhost:", defaultScopeOutBufferSize, defaultChannelOutBufferSize)
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

func TestSlowClientLosesTheOldestEventAndNotItsStream(t *testing.T) {
	// addStream and send belong to the run goroutine. This test calls them without Run, so it is the
	// only goroutine and the result depends on no timing.
	//
	// The test must not use Put: Put gives the event to Run and it comes back when Run has taken the
	// event, and not when Run has sent it.
	events := newGRPCStream[pb.ScopeEvent]("scope", 1)
	out := make(chan *pb.ScopeEvent, 1)
	events.addStream(out)

	event1 := &pb.ScopeEvent{}
	events.send(event1)
	require.Len(t, events.out, 1, "a stream that takes the event must stay")

	// out holds only one event, so the buffer is full for the second one
	event2 := &pb.ScopeEvent{}
	events.send(event2)

	require.Len(t, events.out, 1, "the client keeps its stream")
	event := <-out
	assert.Same(t, event2, event, "the oldest event went away, and the newest one is there")
	assert.Empty(t, out, "the buffer holds no more than the newest event")
}

func TestAClientThatGoesAwayLosesItsStream(t *testing.T) {
	events := newGRPCStream[pb.ScopeEvent]("scope", 1)
	out := make(chan *pb.ScopeEvent, 1)
	events.addStream(out)

	events.dropStream(out)

	assert.Empty(t, events.out)
	_, open := <-out
	assert.False(t, open, "the channel of the client is closed")
}

func TestDropStreamTakesOnlyTheStreamOfThatClient(t *testing.T) {
	events := newGRPCStream[pb.ScopeEvent]("scope", 1)
	first := make(chan *pb.ScopeEvent, 1)
	second := make(chan *pb.ScopeEvent, 1)
	events.addStream(first)
	events.addStream(second)

	events.dropStream(first)

	require.Len(t, events.out, 1)
	assert.Equal(t, second, events.out[0], "the stream of the other client stays")
}

// TestReleaseComesBackAfterTheStreamStopped covers the client that goes away while the server stops.
// Run then closed each channel already, and Release must not wait for a goroutine that ended.
func TestReleaseComesBackAfterTheStreamStopped(t *testing.T) {
	events := newGRPCStream[pb.ScopeEvent]("scope", 1)
	out := make(chan *pb.ScopeEvent, 1)
	events.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		events.Release(out)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Release waits although the stream stopped")
	}
}

func TestSendFramesToClient(t *testing.T) {
	server, err := newGRPCServer("localhost:", 1, 1)
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

	client := pb.NewScopeServiceClient(conn)
	stream, err := client.GetScopeEvents(context.Background(), nil)
	require.NoError(t, err)

	events := make([]*pb.ScopeEvent, 0)
	eventsReceived := &sync.WaitGroup{}
	eventsReceived.Add(2)
	go func() {
		for range 2 {
			event, err := stream.Recv()
			require.NoError(t, err)
			events = append(events, event)
			eventsReceived.Done()
		}
	}()
	time.Sleep(100 * time.Millisecond)

	event1 := &pb.ScopeEvent{Event: &pb.ScopeEvent_TimeFrame{TimeFrame: &pb.TimeFrame{StreamId: "frame1"}}}
	server.scopeEvents.Put(event1)

	event2 := &pb.ScopeEvent{Event: &pb.ScopeEvent_TimeFrame{TimeFrame: &pb.TimeFrame{StreamId: "frame2"}}}
	server.scopeEvents.Put(event2)

	eventsReceived.Wait()
	assert.Len(t, events, 2)
}

// TestStopWhileEventsArrive covers the way that the server stops. The pipeline gives its events in a
// goroutine of its own, and it cannot stop between two events, so Put meets Stop. The stream must
// close neither in nor register for that reason: without the select of Put it panics with
// "send on closed channel".
func TestStopWhileEventsArrive(t *testing.T) {
	for range 50 {
		events := newGRPCStream[pb.ScopeEvent]("scope", 1)
		go events.Run()

		var running sync.WaitGroup
		running.Add(1)
		done := make(chan struct{})
		go func() {
			defer running.Done()

			for {
				select {
				case <-done:
					return
				default:
				}
				events.Put(&pb.ScopeEvent{})
			}
		}()

		time.Sleep(time.Millisecond)

		events.Stop()
		close(done)
		running.Wait()
	}
}

// TestPutAfterStopDoesNothing is the other half: an event that arrives after the end must wait for
// nothing and it must not panic.
func TestPutAfterStopDoesNothing(t *testing.T) {
	events := newGRPCStream[pb.ScopeEvent]("scope", 1)
	go events.Run()
	events.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		events.Put(&pb.ScopeEvent{})
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Put waits although the stream stopped")
	}
}

// TestGetAfterStopGivesAChannelThatIsClosed covers the client that connects while the server stops.
// Its loop must end at once, and its call must not wait for a goroutine that ended.
func TestGetAfterStopGivesAChannelThatIsClosed(t *testing.T) {
	events := newGRPCStream[pb.ScopeEvent]("scope", 1)
	go events.Run()
	events.Stop()

	result := make(chan chan *pb.ScopeEvent)
	go func() { result <- events.Get() }()

	select {
	case out := <-result:
		_, open := <-out
		assert.False(t, open, "the channel of the client is closed")
	case <-time.After(time.Second):
		t.Fatal("Get waits although the stream stopped")
	}
}

package scope

import (
	"fmt"
	"log"
	"net"
	"sync"

	"google.golang.org/grpc"

	"github.com/ftl/sdrainer/scope/pb"
)

const (
	// defaultScopeOutBufferSize is the count of the events that one client of the scope stream can
	// be behind. One spectral frame holds one value for each bin, thus up to 16384 values, so this
	// buffer costs memory and it stays small.
	defaultScopeOutBufferSize = 10

	// defaultChannelOutBufferSize is the same for the stream of the channels. Its events are small,
	// and there are many more of them: each decoded character makes one. A band with 40 channels at
	// 25 wpm gives approximately 100 events for each second, so this buffer holds approximately 10
	// seconds.
	defaultChannelOutBufferSize = 1000
)

// grpcServer runs in the goroutine of its caller of Start, and Stop, Addr and the Send methods come
// from other goroutines. The lock protects each field that Start writes.
type grpcServer struct {
	pb.UnimplementedScopeServiceServer
	pb.UnimplementedChannelServiceServer

	address *net.TCPAddr

	scopeEvents   *grpcStream[pb.ScopeEvent]
	channelEvents *grpcStream[pb.ChannelEvent]

	lock          sync.Mutex
	server        *grpc.Server
	activeAddress net.Addr
}

func newGRPCServer(address string, scopeOutBufferSize int, channelOutBufferSize int) (*grpcServer, error) {
	result := &grpcServer{
		scopeEvents:   newGRPCStream[pb.ScopeEvent]("scope", scopeOutBufferSize),
		channelEvents: newGRPCStream[pb.ChannelEvent]("channel", channelOutBufferSize),
	}

	localAddress, err := net.ResolveTCPAddr("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve address %s: %w", address, err)
	}
	result.address = localAddress

	return result, nil
}

func (s *grpcServer) Start() error {
	s.lock.Lock()
	if s.server != nil {
		s.lock.Unlock()
		return fmt.Errorf("server already running")
	}
	// each use after this point takes the local variable and not the field, so the goroutine of
	// Stop can set the field to nil at any time
	server := grpc.NewServer()
	s.server = server
	s.lock.Unlock()

	pb.RegisterScopeServiceServer(server, s)
	pb.RegisterChannelServiceServer(server, s)

	listener, err := net.Listen("tcp", s.address.String())
	if err != nil {
		s.forgetServer()
		return fmt.Errorf("cannot listen on address %s: %w", s.address, err)
	}

	s.lock.Lock()
	s.activeAddress = listener.Addr()
	s.lock.Unlock()

	go s.scopeEvents.Run()
	go s.channelEvents.Run()

	err = server.Serve(listener)
	s.forgetServer()
	s.scopeEvents.Stop()
	s.channelEvents.Stop()
	return err
}

func (s *grpcServer) forgetServer() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.server = nil
}

// activeServer gives the running server, or nil if no server runs.
func (s *grpcServer) activeServer() *grpc.Server {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.server
}

func (s *grpcServer) Stop() {
	server := s.activeServer()
	if server == nil {
		return
	}
	server.Stop()
}

func (s *grpcServer) Addr() net.Addr {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.activeAddress
}

func (s *grpcServer) GetScopeEvents(_ *pb.GetScopeEventsRequest, stream grpc.ServerStreamingServer[pb.ScopeEvent]) error {
	events := s.scopeEvents.Get()
	defer s.scopeEvents.Release(events)
	for {
		select {
		case event, open := <-events:
			if !open {
				return nil
			}
			if err := stream.Send(event); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return nil
		}
	}
}

func (s *grpcServer) SendSpectralFrame(frame *pb.SpectralFrame) {
	if s.activeServer() == nil {
		return
	}
	event := &pb.ScopeEvent{
		Event: &pb.ScopeEvent_SpectralFrame{
			SpectralFrame: frame,
		},
	}
	s.scopeEvents.Put(event)
}

func (s *grpcServer) SendTimeFrame(frame *pb.TimeFrame) {
	if s.activeServer() == nil {
		return
	}
	event := &pb.ScopeEvent{
		Event: &pb.ScopeEvent_TimeFrame{
			TimeFrame: frame,
		},
	}
	s.scopeEvents.Put(event)
}

func (s *grpcServer) GetChannelEvents(_ *pb.GetChannelEventsRequest, stream grpc.ServerStreamingServer[pb.ChannelEvent]) error {
	events := s.channelEvents.Get()
	defer s.channelEvents.Release(events)
	for {
		select {
		case event, open := <-events:
			if !open {
				return nil
			}
			if err := stream.Send(event); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return nil
		}
	}
}

func (s *grpcServer) SendChannelCreated(created *pb.ChannelCreated) {
	if s.activeServer() == nil {
		return
	}
	s.channelEvents.Put(&pb.ChannelEvent{Event: &pb.ChannelEvent_ChannelCreated{ChannelCreated: created}})
}

func (s *grpcServer) SendChannelDestroyed(destroyed *pb.ChannelDestroyed) {
	if s.activeServer() == nil {
		return
	}
	s.channelEvents.Put(&pb.ChannelEvent{Event: &pb.ChannelEvent_ChannelDestroyed{ChannelDestroyed: destroyed}})
}

func (s *grpcServer) SendChannelStateChanged(changed *pb.ChannelStateChanged) {
	if s.activeServer() == nil {
		return
	}
	s.channelEvents.Put(&pb.ChannelEvent{Event: &pb.ChannelEvent_ChannelStateChanged{ChannelStateChanged: changed}})
}

func (s *grpcServer) SendChannelCharacterReceived(received *pb.ChannelCharacterReceived) {
	if s.activeServer() == nil {
		return
	}
	s.channelEvents.Put(&pb.ChannelEvent{Event: &pb.ChannelEvent_ChannelCharacterReceived{ChannelCharacterReceived: received}})
}

func (s *grpcServer) SendChannelQualityChanged(changed *pb.ChannelQualityChanged) {
	if s.activeServer() == nil {
		return
	}
	s.channelEvents.Put(&pb.ChannelEvent{Event: &pb.ChannelEvent_ChannelQualityChanged{ChannelQualityChanged: changed}})
}

func (s *grpcServer) SendChannelRunningCallsignDetected(detected *pb.ChannelRunningCallsignDetected) {
	if s.activeServer() == nil {
		return
	}
	s.channelEvents.Put(&pb.ChannelEvent{Event: &pb.ChannelEvent_ChannelRunningCallsignDetected{ChannelRunningCallsignDetected: detected}})
}

/*

grpcStream

*/

type grpcStream[F any] struct {
	name          string
	outBufferSize int
	in            chan *F
	register      chan chan *F
	unregister    chan chan *F
	out           []chan *F
	shutdown      chan struct{}

	// dropLogged holds that this stream already said that it drops events. One message is
	// sufficient: a client that is too slow is too slow for each event that follows.
	dropLogged bool
}

func newGRPCStream[F any](name string, outBufferSize int) *grpcStream[F] {
	return &grpcStream[F]{
		name:          name,
		outBufferSize: outBufferSize,
		in:            make(chan *F),
		register:      make(chan chan *F),
		unregister:    make(chan chan *F),
		shutdown:      make(chan struct{}),
	}
}

// Run takes the events and gives them to each client. It ends with Stop.
//
// **It closes neither in nor register.** Put and Get send on those two channels from the goroutine
// of the pipeline and from the goroutine of a client, and a send on a channel that is closed panics.
// Both methods take the shutdown instead, so a call that comes after the end waits for nothing.
func (s *grpcStream[_]) Run() {
	for {
		select {
		case <-s.shutdown:
			for _, out := range s.out {
				close(out)
			}
			s.out = nil
			return
		case out := <-s.register:
			s.addStream(out)
		case out := <-s.unregister:
			s.dropStream(out)
		case o := <-s.in:
			s.send(o)
		}
	}
}

func (s *grpcStream[_]) Stop() {
	select {
	case <-s.shutdown:
	default:
		close(s.shutdown)
	}
}

// addStream must only be called from the run goroutine.
func (s *grpcStream[F]) addStream(out chan *F) {
	s.out = append(s.out, out)
}

// send must only be called from the run goroutine.
//
// **A client that is too slow loses the oldest event, and not its stream.** The scope is a view for
// debugging, and one event that goes away is a gap in that view. A stream that ends is the end of
// the view: the client sees nothing at all afterwards, and it must connect again to see anything.
func (s *grpcStream[F]) send(o *F) {
	for _, out := range s.out {
		select {
		case out <- o:
		default:
			s.dropOldest(out)
			select {
			case out <- o:
			default:
				// the client emptied the buffer between the two steps, which cannot happen while
				// only this goroutine writes, but a lost event is better than a send that waits
			}
		}
	}
}

// dropOldest takes the oldest event of a buffer that is full. It does not wait: the client reads
// from the same channel, and a receive that waits would hold the whole stream while that client
// empties its buffer.
func (s *grpcStream[F]) dropOldest(out chan *F) {
	select {
	case <-out:
	default:
	}

	if !s.dropLogged {
		s.dropLogged = true
		log.Printf("a client of the %s stream is too slow, events go away", s.name)
	}
}

// dropStream takes the channel of a client that went away and closes it. Without it a client that
// disconnects stays in the list for ever, because send no longer removes a client.
//
// It must only be called from the run goroutine.
func (s *grpcStream[F]) dropStream(out chan *F) {
	for i, current := range s.out {
		if current != out {
			continue
		}

		s.out[i] = s.out[len(s.out)-1]
		s.out = s.out[:len(s.out)-1]
		close(out)
		return
	}
}

// Put gives one event to the stream. A stream that stopped takes no event, and the event goes away:
// the server is on its way out, and the events of a scope are not worth a call that waits.
func (s *grpcStream[F]) Put(o *F) {
	select {
	case s.in <- o:
	case <-s.shutdown:
	}
}

// Get gives the channel of a new client. A stream that stopped gives a channel that is closed, so
// the loop of that client ends at once.
func (s *grpcStream[F]) Get() chan *F {
	result := make(chan *F, s.outBufferSize)

	select {
	case s.register <- result:
	case <-s.shutdown:
		close(result)
	}

	return result
}

// Release gives the channel of a client back when that client goes away. A stream that stopped
// closed each channel already, so this method then does nothing.
func (s *grpcStream[F]) Release(out chan *F) {
	select {
	case s.unregister <- out:
	case <-s.shutdown:
	}
}

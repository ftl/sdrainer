package scope

import (
	"fmt"
	"net"

	"google.golang.org/grpc"

	"github.com/ftl/sdrainer/scope/pb"
)

const defaultOutBufferSize = 10

type grpcServer struct {
	address *net.TCPAddr
	server  *grpc.Server

	outBufferSize int
	in            chan *pb.Frame
	register      chan chan *pb.Frame
	out           []chan *pb.Frame
	shutdown      chan struct{}
}

func newGRPCServer(address string, outBufferSize int) (*grpcServer, error) {
	result := &grpcServer{
		outBufferSize: outBufferSize,
		in:            make(chan *pb.Frame),
		register:      make(chan chan *pb.Frame),
		shutdown:      make(chan struct{}),
	}

	localAddress, err := net.ResolveTCPAddr("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve address %s: %w", address, err)
	}
	result.address = localAddress

	return result, nil
}

func (s *grpcServer) run() {
	defer close(s.in)
	defer close(s.register)

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
		case frame := <-s.in:
			s.sendFrameToStreams(frame)
		}
	}
}

func (s *grpcServer) addStream(out chan *pb.Frame) {
	s.out = append(s.out, out)
}

func (s *grpcServer) removeStream(i int) {
	if len(s.out) == 1 {
		s.out = nil
		return
	}
	s.out[i] = s.out[len(s.out)-1]
	s.out = s.out[:len(s.out)-1]
}

func (s *grpcServer) sendFrameToStreams(frame *pb.Frame) {
	for i, out := range s.out {
		select {
		case out <- frame:
		default:
			close(out)
			s.removeStream(i)
		}
	}
}

func (s *grpcServer) getFrameStream() chan *pb.Frame {
	result := make(chan *pb.Frame, s.outBufferSize)
	s.register <- result
	return result
}

func (s *grpcServer) Start() error {
	if s.server != nil {
		return fmt.Errorf("server already running")
	}

	listener, err := net.Listen("tcp", s.address.String())
	if err != nil {
		return fmt.Errorf("cannot listen on address %s: %w", s.address, err)
	}

	go s.run()

	s.server = grpc.NewServer()
	pb.RegisterScopeServer(s.server, s)
	err = s.server.Serve(listener)
	close(s.shutdown)
	return err
}

func (s *grpcServer) Stop() {
	if s.server == nil {
		return
	}
	s.server.Stop()
	s.server = nil
}

func (s *grpcServer) GetFrames(_ *pb.GetScopeRequest, stream pb.Scope_GetFramesServer) error {
	frames := s.getFrameStream()
	for {
		select {
		case frame, open := <-frames:
			if !open {
				return nil
			}
			if err := stream.Send(frame); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return nil
		}
	}
}

func (s *grpcServer) SendFrame(frame *pb.Frame) {
	if s.server == nil {
		return
	}
	s.in <- frame
}

// Code generated by protoc-gen-go. DO NOT EDIT.
// source: scope.proto

package pb

import (
	context "context"
	fmt "fmt"
	proto "github.com/golang/protobuf/proto"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	math "math"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion3 // please upgrade the proto package

type TimeFrame struct {
	StreamId             string                 `protobuf:"bytes,1,opt,name=stream_id,json=streamId,proto3" json:"stream_id,omitempty"`
	Timestamp            *timestamppb.Timestamp `protobuf:"bytes,2,opt,name=timestamp,proto3" json:"timestamp,omitempty"`
	Values               map[string]float32     `protobuf:"bytes,3,rep,name=values,proto3" json:"values,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"fixed32,2,opt,name=value,proto3"`
	XXX_NoUnkeyedLiteral struct{}               `json:"-"`
	XXX_unrecognized     []byte                 `json:"-"`
	XXX_sizecache        int32                  `json:"-"`
}

func (m *TimeFrame) Reset()         { *m = TimeFrame{} }
func (m *TimeFrame) String() string { return proto.CompactTextString(m) }
func (*TimeFrame) ProtoMessage()    {}
func (*TimeFrame) Descriptor() ([]byte, []int) {
	return fileDescriptor_c67276d5d71daf81, []int{0}
}

func (m *TimeFrame) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_TimeFrame.Unmarshal(m, b)
}
func (m *TimeFrame) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_TimeFrame.Marshal(b, m, deterministic)
}
func (m *TimeFrame) XXX_Merge(src proto.Message) {
	xxx_messageInfo_TimeFrame.Merge(m, src)
}
func (m *TimeFrame) XXX_Size() int {
	return xxx_messageInfo_TimeFrame.Size(m)
}
func (m *TimeFrame) XXX_DiscardUnknown() {
	xxx_messageInfo_TimeFrame.DiscardUnknown(m)
}

var xxx_messageInfo_TimeFrame proto.InternalMessageInfo

func (m *TimeFrame) GetStreamId() string {
	if m != nil {
		return m.StreamId
	}
	return ""
}

func (m *TimeFrame) GetTimestamp() *timestamppb.Timestamp {
	if m != nil {
		return m.Timestamp
	}
	return nil
}

func (m *TimeFrame) GetValues() map[string]float32 {
	if m != nil {
		return m.Values
	}
	return nil
}

type SpectralFrame struct {
	StreamId             string                 `protobuf:"bytes,1,opt,name=stream_id,json=streamId,proto3" json:"stream_id,omitempty"`
	Timestamp            *timestamppb.Timestamp `protobuf:"bytes,2,opt,name=timestamp,proto3" json:"timestamp,omitempty"`
	FromFrequency        float32                `protobuf:"fixed32,3,opt,name=from_frequency,json=fromFrequency,proto3" json:"from_frequency,omitempty"`
	ToFrequency          float32                `protobuf:"fixed32,4,opt,name=to_frequency,json=toFrequency,proto3" json:"to_frequency,omitempty"`
	Values               []float32              `protobuf:"fixed32,5,rep,packed,name=values,proto3" json:"values,omitempty"`
	FrequencyMarkers     map[string]float32     `protobuf:"bytes,6,rep,name=frequency_markers,json=frequencyMarkers,proto3" json:"frequency_markers,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"fixed32,2,opt,name=value,proto3"`
	MagnitudeMarkers     map[string]float32     `protobuf:"bytes,7,rep,name=magnitude_markers,json=magnitudeMarkers,proto3" json:"magnitude_markers,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"fixed32,2,opt,name=value,proto3"`
	XXX_NoUnkeyedLiteral struct{}               `json:"-"`
	XXX_unrecognized     []byte                 `json:"-"`
	XXX_sizecache        int32                  `json:"-"`
}

func (m *SpectralFrame) Reset()         { *m = SpectralFrame{} }
func (m *SpectralFrame) String() string { return proto.CompactTextString(m) }
func (*SpectralFrame) ProtoMessage()    {}
func (*SpectralFrame) Descriptor() ([]byte, []int) {
	return fileDescriptor_c67276d5d71daf81, []int{1}
}

func (m *SpectralFrame) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_SpectralFrame.Unmarshal(m, b)
}
func (m *SpectralFrame) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_SpectralFrame.Marshal(b, m, deterministic)
}
func (m *SpectralFrame) XXX_Merge(src proto.Message) {
	xxx_messageInfo_SpectralFrame.Merge(m, src)
}
func (m *SpectralFrame) XXX_Size() int {
	return xxx_messageInfo_SpectralFrame.Size(m)
}
func (m *SpectralFrame) XXX_DiscardUnknown() {
	xxx_messageInfo_SpectralFrame.DiscardUnknown(m)
}

var xxx_messageInfo_SpectralFrame proto.InternalMessageInfo

func (m *SpectralFrame) GetStreamId() string {
	if m != nil {
		return m.StreamId
	}
	return ""
}

func (m *SpectralFrame) GetTimestamp() *timestamppb.Timestamp {
	if m != nil {
		return m.Timestamp
	}
	return nil
}

func (m *SpectralFrame) GetFromFrequency() float32 {
	if m != nil {
		return m.FromFrequency
	}
	return 0
}

func (m *SpectralFrame) GetToFrequency() float32 {
	if m != nil {
		return m.ToFrequency
	}
	return 0
}

func (m *SpectralFrame) GetValues() []float32 {
	if m != nil {
		return m.Values
	}
	return nil
}

func (m *SpectralFrame) GetFrequencyMarkers() map[string]float32 {
	if m != nil {
		return m.FrequencyMarkers
	}
	return nil
}

func (m *SpectralFrame) GetMagnitudeMarkers() map[string]float32 {
	if m != nil {
		return m.MagnitudeMarkers
	}
	return nil
}

type Frame struct {
	// Types that are valid to be assigned to Frame:
	//
	//	*Frame_TimeFrame
	//	*Frame_SpectralFrame
	Frame                isFrame_Frame `protobuf_oneof:"frame"`
	XXX_NoUnkeyedLiteral struct{}      `json:"-"`
	XXX_unrecognized     []byte        `json:"-"`
	XXX_sizecache        int32         `json:"-"`
}

func (m *Frame) Reset()         { *m = Frame{} }
func (m *Frame) String() string { return proto.CompactTextString(m) }
func (*Frame) ProtoMessage()    {}
func (*Frame) Descriptor() ([]byte, []int) {
	return fileDescriptor_c67276d5d71daf81, []int{2}
}

func (m *Frame) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_Frame.Unmarshal(m, b)
}
func (m *Frame) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_Frame.Marshal(b, m, deterministic)
}
func (m *Frame) XXX_Merge(src proto.Message) {
	xxx_messageInfo_Frame.Merge(m, src)
}
func (m *Frame) XXX_Size() int {
	return xxx_messageInfo_Frame.Size(m)
}
func (m *Frame) XXX_DiscardUnknown() {
	xxx_messageInfo_Frame.DiscardUnknown(m)
}

var xxx_messageInfo_Frame proto.InternalMessageInfo

type isFrame_Frame interface {
	isFrame_Frame()
}

type Frame_TimeFrame struct {
	TimeFrame *TimeFrame `protobuf:"bytes,1,opt,name=time_frame,json=timeFrame,proto3,oneof"`
}

type Frame_SpectralFrame struct {
	SpectralFrame *SpectralFrame `protobuf:"bytes,2,opt,name=spectral_frame,json=spectralFrame,proto3,oneof"`
}

func (*Frame_TimeFrame) isFrame_Frame() {}

func (*Frame_SpectralFrame) isFrame_Frame() {}

func (m *Frame) GetFrame() isFrame_Frame {
	if m != nil {
		return m.Frame
	}
	return nil
}

func (m *Frame) GetTimeFrame() *TimeFrame {
	if x, ok := m.GetFrame().(*Frame_TimeFrame); ok {
		return x.TimeFrame
	}
	return nil
}

func (m *Frame) GetSpectralFrame() *SpectralFrame {
	if x, ok := m.GetFrame().(*Frame_SpectralFrame); ok {
		return x.SpectralFrame
	}
	return nil
}

// XXX_OneofWrappers is for the internal use of the proto package.
func (*Frame) XXX_OneofWrappers() []interface{} {
	return []interface{}{
		(*Frame_TimeFrame)(nil),
		(*Frame_SpectralFrame)(nil),
	}
}

type GetScopeRequest struct {
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *GetScopeRequest) Reset()         { *m = GetScopeRequest{} }
func (m *GetScopeRequest) String() string { return proto.CompactTextString(m) }
func (*GetScopeRequest) ProtoMessage()    {}
func (*GetScopeRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_c67276d5d71daf81, []int{3}
}

func (m *GetScopeRequest) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_GetScopeRequest.Unmarshal(m, b)
}
func (m *GetScopeRequest) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_GetScopeRequest.Marshal(b, m, deterministic)
}
func (m *GetScopeRequest) XXX_Merge(src proto.Message) {
	xxx_messageInfo_GetScopeRequest.Merge(m, src)
}
func (m *GetScopeRequest) XXX_Size() int {
	return xxx_messageInfo_GetScopeRequest.Size(m)
}
func (m *GetScopeRequest) XXX_DiscardUnknown() {
	xxx_messageInfo_GetScopeRequest.DiscardUnknown(m)
}

var xxx_messageInfo_GetScopeRequest proto.InternalMessageInfo

func init() {
	proto.RegisterType((*TimeFrame)(nil), "pb.TimeFrame")
	proto.RegisterMapType((map[string]float32)(nil), "pb.TimeFrame.ValuesEntry")
	proto.RegisterType((*SpectralFrame)(nil), "pb.SpectralFrame")
	proto.RegisterMapType((map[string]float32)(nil), "pb.SpectralFrame.FrequencyMarkersEntry")
	proto.RegisterMapType((map[string]float32)(nil), "pb.SpectralFrame.MagnitudeMarkersEntry")
	proto.RegisterType((*Frame)(nil), "pb.Frame")
	proto.RegisterType((*GetScopeRequest)(nil), "pb.GetScopeRequest")
}

func init() {
	proto.RegisterFile("scope.proto", fileDescriptor_c67276d5d71daf81)
}

var fileDescriptor_c67276d5d71daf81 = []byte{
	// 437 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0xb4, 0x93, 0x4f, 0x8f, 0xd3, 0x30,
	0x10, 0xc5, 0x9b, 0x84, 0x74, 0xc9, 0x84, 0x2e, 0x5b, 0xf3, 0x47, 0x21, 0x1c, 0x28, 0x91, 0x10,
	0x39, 0xb9, 0x10, 0x2e, 0x65, 0x8f, 0x20, 0x76, 0x97, 0xc3, 0x5e, 0xb2, 0x15, 0xd7, 0x28, 0x69,
	0x9d, 0x2a, 0xda, 0xba, 0x0e, 0xb6, 0x83, 0x54, 0x89, 0x0f, 0xc9, 0x27, 0x42, 0xc8, 0x76, 0x92,
	0x6d, 0xcb, 0x72, 0xe8, 0x81, 0x5b, 0xfd, 0xfa, 0x9b, 0xe7, 0xf1, 0x9b, 0x09, 0xf8, 0x62, 0xc1,
	0x6a, 0x82, 0x6b, 0xce, 0x24, 0x43, 0x76, 0x5d, 0x84, 0xaf, 0x56, 0x8c, 0xad, 0xd6, 0x64, 0xaa,
	0x95, 0xa2, 0x29, 0xa7, 0xb2, 0xa2, 0x44, 0xc8, 0x9c, 0xd6, 0x06, 0x8a, 0x7e, 0x59, 0xe0, 0xcd,
	0x2b, 0x4a, 0x2e, 0x78, 0x4e, 0x09, 0x7a, 0x09, 0x9e, 0x90, 0x9c, 0xe4, 0x34, 0xab, 0x96, 0x81,
	0x35, 0xb1, 0x62, 0x2f, 0x7d, 0x68, 0x84, 0xaf, 0x4b, 0x34, 0x03, 0xaf, 0xaf, 0x0e, 0xec, 0x89,
	0x15, 0xfb, 0x49, 0x88, 0x8d, 0x3f, 0xee, 0xfc, 0xf1, 0xbc, 0x23, 0xd2, 0x3b, 0x18, 0xbd, 0x87,
	0xe1, 0x8f, 0x7c, 0xdd, 0x10, 0x11, 0x38, 0x13, 0x27, 0xf6, 0x93, 0x17, 0xb8, 0x2e, 0x70, 0x7f,
	0x2b, 0xfe, 0xa6, 0xff, 0xfb, 0xb2, 0x91, 0x7c, 0x9b, 0xb6, 0x60, 0xf8, 0x11, 0xfc, 0x1d, 0x19,
	0x9d, 0x81, 0x73, 0x4b, 0xb6, 0x6d, 0x4b, 0xea, 0x27, 0x7a, 0x0a, 0xae, 0x46, 0x75, 0x27, 0x76,
	0x6a, 0x0e, 0xe7, 0xf6, 0xcc, 0x8a, 0x7e, 0x3b, 0x30, 0xba, 0xa9, 0xc9, 0x42, 0xf2, 0x7c, 0xfd,
	0x5f, 0x9f, 0xf5, 0x06, 0x4e, 0x4b, 0xce, 0x68, 0x56, 0x72, 0xf2, 0xbd, 0x21, 0x9b, 0xc5, 0x36,
	0x70, 0x74, 0x2f, 0x23, 0xa5, 0x5e, 0x74, 0x22, 0x7a, 0x0d, 0x8f, 0x24, 0xdb, 0x81, 0x1e, 0x68,
	0xc8, 0x97, 0xec, 0x0e, 0x79, 0xde, 0x07, 0xe4, 0x4e, 0x9c, 0xd8, 0xee, 0x52, 0x40, 0x73, 0x18,
	0xf7, 0x75, 0x19, 0xcd, 0xf9, 0x2d, 0xe1, 0x22, 0x18, 0xea, 0x0c, 0xdf, 0xaa, 0x0c, 0xf7, 0x9e,
	0x89, 0x7b, 0xbf, 0x6b, 0x43, 0x9a, 0x44, 0xcf, 0xca, 0x03, 0x59, 0xb9, 0xd2, 0x7c, 0xb5, 0xa9,
	0x64, 0xb3, 0x24, 0xbd, 0xeb, 0xc9, 0xbf, 0x5c, 0xaf, 0x3b, 0x74, 0xdf, 0x95, 0x1e, 0xc8, 0xe1,
	0x67, 0x78, 0x76, 0x6f, 0x03, 0xc7, 0xcc, 0x4e, 0x99, 0xdc, 0x7b, 0xdf, 0x51, 0x0b, 0xf0, 0x13,
	0x5c, 0x33, 0x77, 0x0c, 0xa0, 0xa6, 0x95, 0x95, 0xea, 0xa4, 0x6b, 0xfd, 0x64, 0xb4, 0xb7, 0x7b,
	0x57, 0x03, 0x33, 0x50, 0xc3, 0x9f, 0xc3, 0xa9, 0x68, 0xdf, 0xde, 0xd6, 0x98, 0x7d, 0x18, 0xff,
	0x95, 0xca, 0xd5, 0x20, 0x1d, 0x89, 0x5d, 0xe1, 0xd3, 0x09, 0xb8, 0xba, 0x24, 0x1a, 0xc3, 0xe3,
	0x4b, 0x22, 0x6f, 0xd4, 0x87, 0x98, 0xaa, 0x38, 0x84, 0x4c, 0x66, 0xe0, 0xea, 0x33, 0x9a, 0x82,
	0x77, 0x49, 0xa4, 0x2e, 0x10, 0xe8, 0x89, 0x72, 0x3d, 0x40, 0x43, 0x4f, 0x89, 0x1a, 0x88, 0x06,
	0xef, 0xac, 0x62, 0xa8, 0x37, 0xf0, 0xc3, 0x9f, 0x00, 0x00, 0x00, 0xff, 0xff, 0xfe, 0x07, 0x48,
	0x1e, 0xd9, 0x03, 0x00, 0x00,
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConnInterface

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion6

// ScopeClient is the client API for Scope service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type ScopeClient interface {
	GetFrames(ctx context.Context, in *GetScopeRequest, opts ...grpc.CallOption) (Scope_GetFramesClient, error)
}

type scopeClient struct {
	cc grpc.ClientConnInterface
}

func NewScopeClient(cc grpc.ClientConnInterface) ScopeClient {
	return &scopeClient{cc}
}

func (c *scopeClient) GetFrames(ctx context.Context, in *GetScopeRequest, opts ...grpc.CallOption) (Scope_GetFramesClient, error) {
	stream, err := c.cc.NewStream(ctx, &_Scope_serviceDesc.Streams[0], "/pb.Scope/GetFrames", opts...)
	if err != nil {
		return nil, err
	}
	x := &scopeGetFramesClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type Scope_GetFramesClient interface {
	Recv() (*Frame, error)
	grpc.ClientStream
}

type scopeGetFramesClient struct {
	grpc.ClientStream
}

func (x *scopeGetFramesClient) Recv() (*Frame, error) {
	m := new(Frame)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// ScopeServer is the server API for Scope service.
type ScopeServer interface {
	GetFrames(*GetScopeRequest, Scope_GetFramesServer) error
}

// UnimplementedScopeServer can be embedded to have forward compatible implementations.
type UnimplementedScopeServer struct {
}

func (*UnimplementedScopeServer) GetFrames(req *GetScopeRequest, srv Scope_GetFramesServer) error {
	return status.Errorf(codes.Unimplemented, "method GetFrames not implemented")
}

func RegisterScopeServer(s *grpc.Server, srv ScopeServer) {
	s.RegisterService(&_Scope_serviceDesc, srv)
}

func _Scope_GetFrames_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(GetScopeRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(ScopeServer).GetFrames(m, &scopeGetFramesServer{stream})
}

type Scope_GetFramesServer interface {
	Send(*Frame) error
	grpc.ServerStream
}

type scopeGetFramesServer struct {
	grpc.ServerStream
}

func (x *scopeGetFramesServer) Send(m *Frame) error {
	return x.ServerStream.SendMsg(m)
}

var _Scope_serviceDesc = grpc.ServiceDesc{
	ServiceName: "pb.Scope",
	HandlerType: (*ScopeServer)(nil),
	Methods:     []grpc.MethodDesc{},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "GetFrames",
			Handler:       _Scope_GetFrames_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "scope.proto",
}

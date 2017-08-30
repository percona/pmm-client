// Code generated by protoc-gen-go. DO NOT EDIT.
// source: alerts.proto

/*
Package api is a generated protocol buffer package.

It is generated from these files:
	alerts.proto
	base.proto
	scrape_jobs.proto

It has these top-level messages:
	AlertRule
	AlertsListRequest
	AlertsListResponse
	AlertsGetRequest
	AlertsGetResponse
	BaseVersionRequest
	BaseVersionResponse
	ScrapeJob
	ScrapeJobsListRequest
	ScrapeJobsListResponse
	ScrapeJobsGetRequest
	ScrapeJobsGetResponse
	ScrapeJobsCreateRequest
	ScrapeJobsCreateResponse
	ScrapeJobsDeleteRequest
	ScrapeJobsDeleteResponse
*/
package api

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"
import _ "google.golang.org/genproto/googleapis/api/annotations"

import (
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion2 // please upgrade the proto package

type AlertRule struct {
	Name     string `protobuf:"bytes,1,opt,name=name" json:"name,omitempty"`
	Text     string `protobuf:"bytes,2,opt,name=text" json:"text,omitempty"`
	Disabled bool   `protobuf:"varint,3,opt,name=disabled" json:"disabled,omitempty"`
}

func (m *AlertRule) Reset()                    { *m = AlertRule{} }
func (m *AlertRule) String() string            { return proto.CompactTextString(m) }
func (*AlertRule) ProtoMessage()               {}
func (*AlertRule) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{0} }

func (m *AlertRule) GetName() string {
	if m != nil {
		return m.Name
	}
	return ""
}

func (m *AlertRule) GetText() string {
	if m != nil {
		return m.Text
	}
	return ""
}

func (m *AlertRule) GetDisabled() bool {
	if m != nil {
		return m.Disabled
	}
	return false
}

type AlertsListRequest struct {
}

func (m *AlertsListRequest) Reset()                    { *m = AlertsListRequest{} }
func (m *AlertsListRequest) String() string            { return proto.CompactTextString(m) }
func (*AlertsListRequest) ProtoMessage()               {}
func (*AlertsListRequest) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{1} }

type AlertsListResponse struct {
	AlertRules []*AlertRule `protobuf:"bytes,1,rep,name=alert_rules,json=alertRules" json:"alert_rules,omitempty"`
}

func (m *AlertsListResponse) Reset()                    { *m = AlertsListResponse{} }
func (m *AlertsListResponse) String() string            { return proto.CompactTextString(m) }
func (*AlertsListResponse) ProtoMessage()               {}
func (*AlertsListResponse) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{2} }

func (m *AlertsListResponse) GetAlertRules() []*AlertRule {
	if m != nil {
		return m.AlertRules
	}
	return nil
}

type AlertsGetRequest struct {
	Name string `protobuf:"bytes,1,opt,name=name" json:"name,omitempty"`
}

func (m *AlertsGetRequest) Reset()                    { *m = AlertsGetRequest{} }
func (m *AlertsGetRequest) String() string            { return proto.CompactTextString(m) }
func (*AlertsGetRequest) ProtoMessage()               {}
func (*AlertsGetRequest) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{3} }

func (m *AlertsGetRequest) GetName() string {
	if m != nil {
		return m.Name
	}
	return ""
}

type AlertsGetResponse struct {
	AlertRule *AlertRule `protobuf:"bytes,1,opt,name=alert_rule,json=alertRule" json:"alert_rule,omitempty"`
}

func (m *AlertsGetResponse) Reset()                    { *m = AlertsGetResponse{} }
func (m *AlertsGetResponse) String() string            { return proto.CompactTextString(m) }
func (*AlertsGetResponse) ProtoMessage()               {}
func (*AlertsGetResponse) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{4} }

func (m *AlertsGetResponse) GetAlertRule() *AlertRule {
	if m != nil {
		return m.AlertRule
	}
	return nil
}

func init() {
	proto.RegisterType((*AlertRule)(nil), "api.AlertRule")
	proto.RegisterType((*AlertsListRequest)(nil), "api.AlertsListRequest")
	proto.RegisterType((*AlertsListResponse)(nil), "api.AlertsListResponse")
	proto.RegisterType((*AlertsGetRequest)(nil), "api.AlertsGetRequest")
	proto.RegisterType((*AlertsGetResponse)(nil), "api.AlertsGetResponse")
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// Client API for Alerts service

type AlertsClient interface {
	// List returns all alert rules.
	List(ctx context.Context, in *AlertsListRequest, opts ...grpc.CallOption) (*AlertsListResponse, error)
	// Get returns an alert rule by name.
	Get(ctx context.Context, in *AlertsGetRequest, opts ...grpc.CallOption) (*AlertsGetResponse, error)
}

type alertsClient struct {
	cc *grpc.ClientConn
}

func NewAlertsClient(cc *grpc.ClientConn) AlertsClient {
	return &alertsClient{cc}
}

func (c *alertsClient) List(ctx context.Context, in *AlertsListRequest, opts ...grpc.CallOption) (*AlertsListResponse, error) {
	out := new(AlertsListResponse)
	err := grpc.Invoke(ctx, "/api.Alerts/List", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *alertsClient) Get(ctx context.Context, in *AlertsGetRequest, opts ...grpc.CallOption) (*AlertsGetResponse, error) {
	out := new(AlertsGetResponse)
	err := grpc.Invoke(ctx, "/api.Alerts/Get", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Server API for Alerts service

type AlertsServer interface {
	// List returns all alert rules.
	List(context.Context, *AlertsListRequest) (*AlertsListResponse, error)
	// Get returns an alert rule by name.
	Get(context.Context, *AlertsGetRequest) (*AlertsGetResponse, error)
}

func RegisterAlertsServer(s *grpc.Server, srv AlertsServer) {
	s.RegisterService(&_Alerts_serviceDesc, srv)
}

func _Alerts_List_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(AlertsListRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AlertsServer).List(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/api.Alerts/List",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AlertsServer).List(ctx, req.(*AlertsListRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Alerts_Get_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(AlertsGetRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AlertsServer).Get(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/api.Alerts/Get",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AlertsServer).Get(ctx, req.(*AlertsGetRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _Alerts_serviceDesc = grpc.ServiceDesc{
	ServiceName: "api.Alerts",
	HandlerType: (*AlertsServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "List",
			Handler:    _Alerts_List_Handler,
		},
		{
			MethodName: "Get",
			Handler:    _Alerts_Get_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "alerts.proto",
}

func init() { proto.RegisterFile("alerts.proto", fileDescriptor0) }

var fileDescriptor0 = []byte{
	// 300 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x6c, 0x91, 0x4f, 0x4a, 0xc3, 0x40,
	0x14, 0xc6, 0x49, 0x53, 0x4a, 0xfb, 0x5a, 0xc4, 0xbe, 0x62, 0x8d, 0xc1, 0x45, 0x98, 0x85, 0x64,
	0x63, 0x46, 0xea, 0x09, 0x14, 0xa4, 0x0b, 0x85, 0x42, 0x2e, 0x20, 0x53, 0xfa, 0x28, 0x03, 0x31,
	0x13, 0x33, 0x13, 0x11, 0xc4, 0x8d, 0x57, 0xf0, 0x0e, 0x5e, 0xc8, 0x2b, 0x78, 0x10, 0xc9, 0xe4,
	0x2f, 0xd6, 0xdd, 0x9b, 0x8f, 0x6f, 0x7e, 0xdf, 0xf7, 0x78, 0x30, 0x13, 0x09, 0xe5, 0x46, 0x47,
	0x59, 0xae, 0x8c, 0x42, 0x57, 0x64, 0xd2, 0x3f, 0xdf, 0x2b, 0xb5, 0x4f, 0x88, 0x8b, 0x4c, 0x72,
	0x91, 0xa6, 0xca, 0x08, 0x23, 0x55, 0x5a, 0x5b, 0xd8, 0x06, 0x26, 0x37, 0xe5, 0x97, 0xb8, 0x48,
	0x08, 0x11, 0x86, 0xa9, 0x78, 0x22, 0xcf, 0x09, 0x9c, 0x70, 0x12, 0xdb, 0xb9, 0xd4, 0x0c, 0xbd,
	0x1a, 0x6f, 0x50, 0x69, 0xe5, 0x8c, 0x3e, 0x8c, 0x77, 0x52, 0x8b, 0x6d, 0x42, 0x3b, 0xcf, 0x0d,
	0x9c, 0x70, 0x1c, 0xb7, 0x6f, 0xb6, 0x80, 0xb9, 0x05, 0xea, 0x07, 0xa9, 0x4d, 0x4c, 0xcf, 0x05,
	0x69, 0xc3, 0xee, 0x00, 0xfb, 0xa2, 0xce, 0x54, 0xaa, 0x09, 0x39, 0x4c, 0x6d, 0xdd, 0xc7, 0xbc,
	0x48, 0x48, 0x7b, 0x4e, 0xe0, 0x86, 0xd3, 0xd5, 0x51, 0x24, 0x32, 0x19, 0xb5, 0x9d, 0x62, 0x10,
	0xcd, 0xa8, 0xd9, 0x05, 0x1c, 0x57, 0x98, 0x35, 0x35, 0xe8, 0xff, 0x3a, 0xb3, 0xdb, 0xa6, 0x83,
	0xf5, 0xd5, 0x69, 0x97, 0x00, 0x5d, 0x9a, 0xb5, 0x1f, 0x86, 0x4d, 0xda, 0xb0, 0xd5, 0x97, 0x03,
	0xa3, 0x0a, 0x82, 0xf7, 0x30, 0x2c, 0x7b, 0xe3, 0xb2, 0x73, 0xf7, 0xb7, 0xf3, 0x4f, 0x0f, 0xf4,
	0x2a, 0x92, 0xe1, 0xc7, 0xf7, 0xcf, 0xe7, 0x60, 0x86, 0xc0, 0x5f, 0xae, 0x78, 0x75, 0x19, 0xdc,
	0x80, 0xbb, 0x26, 0x83, 0x27, 0xbd, 0x3f, 0xdd, 0x36, 0xfe, 0xf2, 0xaf, 0x5c, 0x93, 0xce, 0x2c,
	0x69, 0x81, 0xf3, 0x8e, 0xc4, 0xdf, 0xca, 0x5d, 0xdf, 0xb7, 0x23, 0x7b, 0xc8, 0xeb, 0xdf, 0x00,
	0x00, 0x00, 0xff, 0xff, 0x89, 0x27, 0x89, 0x67, 0xfb, 0x01, 0x00, 0x00,
}
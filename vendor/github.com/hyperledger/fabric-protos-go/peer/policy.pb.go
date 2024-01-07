// Code generated by protoc-gen-go. DO NOT EDIT.
// source: peer/policy.proto

package peer

import (
	fmt "fmt"
	proto "github.com/golang/protobuf/proto"
	common "github.com/VoneChain-CS/fabric-gm-protos-go/common"
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

// ApplicationPolicy captures the diffenrent policy types that
// are set and evaluted at the application level.
type ApplicationPolicy struct {
	// Types that are valid to be assigned to Type:
	//	*ApplicationPolicy_SignaturePolicy
	//	*ApplicationPolicy_ChannelConfigPolicyReference
	Type                 isApplicationPolicy_Type `protobuf_oneof:"Type"`
	XXX_NoUnkeyedLiteral struct{}                 `json:"-"`
	XXX_unrecognized     []byte                   `json:"-"`
	XXX_sizecache        int32                    `json:"-"`
}

func (m *ApplicationPolicy) Reset()         { *m = ApplicationPolicy{} }
func (m *ApplicationPolicy) String() string { return proto.CompactTextString(m) }
func (*ApplicationPolicy) ProtoMessage()    {}
func (*ApplicationPolicy) Descriptor() ([]byte, []int) {
	return fileDescriptor_17aa1dd1e55c3e19, []int{0}
}

func (m *ApplicationPolicy) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_ApplicationPolicy.Unmarshal(m, b)
}
func (m *ApplicationPolicy) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_ApplicationPolicy.Marshal(b, m, deterministic)
}
func (m *ApplicationPolicy) XXX_Merge(src proto.Message) {
	xxx_messageInfo_ApplicationPolicy.Merge(m, src)
}
func (m *ApplicationPolicy) XXX_Size() int {
	return xxx_messageInfo_ApplicationPolicy.Size(m)
}
func (m *ApplicationPolicy) XXX_DiscardUnknown() {
	xxx_messageInfo_ApplicationPolicy.DiscardUnknown(m)
}

var xxx_messageInfo_ApplicationPolicy proto.InternalMessageInfo

type isApplicationPolicy_Type interface {
	isApplicationPolicy_Type()
}

type ApplicationPolicy_SignaturePolicy struct {
	SignaturePolicy *common.SignaturePolicyEnvelope `protobuf:"bytes,1,opt,name=signature_policy,json=signaturePolicy,proto3,oneof"`
}

type ApplicationPolicy_ChannelConfigPolicyReference struct {
	ChannelConfigPolicyReference string `protobuf:"bytes,2,opt,name=channel_config_policy_reference,json=channelConfigPolicyReference,proto3,oneof"`
}

func (*ApplicationPolicy_SignaturePolicy) isApplicationPolicy_Type() {}

func (*ApplicationPolicy_ChannelConfigPolicyReference) isApplicationPolicy_Type() {}

func (m *ApplicationPolicy) GetType() isApplicationPolicy_Type {
	if m != nil {
		return m.Type
	}
	return nil
}

func (m *ApplicationPolicy) GetSignaturePolicy() *common.SignaturePolicyEnvelope {
	if x, ok := m.GetType().(*ApplicationPolicy_SignaturePolicy); ok {
		return x.SignaturePolicy
	}
	return nil
}

func (m *ApplicationPolicy) GetChannelConfigPolicyReference() string {
	if x, ok := m.GetType().(*ApplicationPolicy_ChannelConfigPolicyReference); ok {
		return x.ChannelConfigPolicyReference
	}
	return ""
}

// XXX_OneofWrappers is for the internal use of the proto package.
func (*ApplicationPolicy) XXX_OneofWrappers() []interface{} {
	return []interface{}{
		(*ApplicationPolicy_SignaturePolicy)(nil),
		(*ApplicationPolicy_ChannelConfigPolicyReference)(nil),
	}
}

func init() {
	proto.RegisterType((*ApplicationPolicy)(nil), "protos.ApplicationPolicy")
}

func init() { proto.RegisterFile("peer/policy.proto", fileDescriptor_17aa1dd1e55c3e19) }

var fileDescriptor_17aa1dd1e55c3e19 = []byte{
	// 243 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x54, 0x90, 0xc1, 0x4a, 0xc3, 0x40,
	0x10, 0x86, 0x1b, 0x91, 0x82, 0xeb, 0x41, 0x1b, 0x10, 0x8a, 0x08, 0x2d, 0x3d, 0xf5, 0x60, 0x37,
	0xa0, 0x4f, 0x60, 0x45, 0xec, 0xc1, 0x83, 0x44, 0x4f, 0x5e, 0x42, 0xb2, 0x4e, 0x36, 0x0b, 0xdb,
	0x9d, 0x61, 0x36, 0x15, 0xf2, 0x5a, 0x3e, 0xa1, 0x24, 0xd3, 0x80, 0x3d, 0xed, 0xe1, 0xfb, 0xfe,
	0x9f, 0x9d, 0x5f, 0xcd, 0x08, 0x80, 0x33, 0x42, 0xef, 0x4c, 0xa7, 0x89, 0xb1, 0xc5, 0x74, 0x3a,
	0x3c, 0xf1, 0xf6, 0xc6, 0xe0, 0x7e, 0x8f, 0x41, 0xa0, 0x83, 0x28, 0x78, 0xf5, 0x9b, 0xa8, 0xd9,
	0x13, 0x91, 0x77, 0xa6, 0x6c, 0x1d, 0x86, 0xf7, 0x21, 0x9a, 0xbe, 0xa9, 0xeb, 0xe8, 0x6c, 0x28,
	0xdb, 0x03, 0x43, 0x21, 0x75, 0xf3, 0x64, 0x99, 0xac, 0x2f, 0x1f, 0x16, 0x5a, 0x7a, 0xf4, 0xc7,
	0xc8, 0x25, 0xf2, 0x12, 0x7e, 0xc0, 0x23, 0xc1, 0x6e, 0x92, 0x5f, 0xc5, 0x53, 0x94, 0xbe, 0xaa,
	0x85, 0x69, 0xca, 0x10, 0xc0, 0x17, 0x06, 0x43, 0xed, 0xec, 0xb1, 0xb2, 0x60, 0xa8, 0x81, 0x21,
	0x18, 0x98, 0x9f, 0x2d, 0x93, 0xf5, 0xc5, 0x6e, 0x92, 0xdf, 0x1d, 0xc5, 0xe7, 0xc1, 0x93, 0x7c,
	0x3e, 0x5a, 0xdb, 0xa9, 0x3a, 0xff, 0xec, 0x08, 0xb6, 0xb9, 0x5a, 0x21, 0x5b, 0xdd, 0x74, 0x04,
	0xec, 0xe1, 0xdb, 0x02, 0xeb, 0xba, 0xac, 0xd8, 0x19, 0x39, 0x2a, 0xea, 0x7e, 0x86, 0xaf, 0x7b,
	0xeb, 0xda, 0xe6, 0x50, 0xf5, 0x1f, 0xce, 0xfe, 0xa9, 0x99, 0xa8, 0x1b, 0x51, 0x37, 0x16, 0xb3,
	0xde, 0xae, 0x64, 0xa7, 0xc7, 0xbf, 0x00, 0x00, 0x00, 0xff, 0xff, 0x89, 0x76, 0x3a, 0x03, 0x43,
	0x01, 0x00, 0x00,
}

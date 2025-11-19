// Copyright 2024 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package etcdserverpb

import (
	"github.com/gogo/protobuf/proto"
)

// ErrorDetail provides structured error context for debugging.
// This message is attached to gRPC status details when errors occur.
// Note: This struct should be regenerated from rpc.proto using genproto.sh
// when proto tooling is available.
type ErrorDetail struct {
	// RequestId is a unique identifier for the request that caused the error.
	RequestId string `protobuf:"bytes,1,opt,name=request_id,json=requestId,proto3" json:"request_id,omitempty"`
	// MemberId is the ID of the member that processed the request.
	MemberId uint64 `protobuf:"varint,2,opt,name=member_id,json=memberId,proto3" json:"member_id,omitempty"`
	// Operation is the type of operation that was attempted (e.g., "Put", "Range").
	Operation string `protobuf:"bytes,3,opt,name=operation,proto3" json:"operation,omitempty"`
	// Key is the key involved in the operation (truncated to 50 chars for security).
	Key string `protobuf:"bytes,4,opt,name=key,proto3" json:"key,omitempty"`
	// DurationMs is how long the operation took before failing.
	DurationMs int64 `protobuf:"varint,5,opt,name=duration_ms,json=durationMs,proto3" json:"duration_ms,omitempty"`
	// LeaderId is the current leader ID at the time of error.
	LeaderId uint64 `protobuf:"varint,6,opt,name=leader_id,json=leaderId,proto3" json:"leader_id,omitempty"`
	// Cause is a machine-readable error classification.
	Cause string `protobuf:"bytes,7,opt,name=cause,proto3" json:"cause,omitempty"`
	// RetryAfterMs suggests how long the client should wait before retrying.
	RetryAfterMs         int64    `protobuf:"varint,8,opt,name=retry_after_ms,json=retryAfterMs,proto3" json:"retry_after_ms,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *ErrorDetail) Reset()         { *m = ErrorDetail{} }
func (m *ErrorDetail) String() string { return proto.CompactTextString(m) }
func (*ErrorDetail) ProtoMessage()    {}

func (m *ErrorDetail) GetRequestId() string {
	if m != nil {
		return m.RequestId
	}
	return ""
}

func (m *ErrorDetail) GetMemberId() uint64 {
	if m != nil {
		return m.MemberId
	}
	return 0
}

func (m *ErrorDetail) GetOperation() string {
	if m != nil {
		return m.Operation
	}
	return ""
}

func (m *ErrorDetail) GetKey() string {
	if m != nil {
		return m.Key
	}
	return ""
}

func (m *ErrorDetail) GetDurationMs() int64 {
	if m != nil {
		return m.DurationMs
	}
	return 0
}

func (m *ErrorDetail) GetLeaderId() uint64 {
	if m != nil {
		return m.LeaderId
	}
	return 0
}

func (m *ErrorDetail) GetCause() string {
	if m != nil {
		return m.Cause
	}
	return ""
}

func (m *ErrorDetail) GetRetryAfterMs() int64 {
	if m != nil {
		return m.RetryAfterMs
	}
	return 0
}

// Marshal implements proto.Marshaler.
func (m *ErrorDetail) Marshal() ([]byte, error) {
	return proto.Marshal(m)
}

// Unmarshal implements proto.Unmarshaler.
func (m *ErrorDetail) Unmarshal(data []byte) error {
	return proto.Unmarshal(data, m)
}

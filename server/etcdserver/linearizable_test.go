// Copyright 2015 The etcd Authors
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

package etcdserver

import (
	"testing"

	"go.etcd.io/etcd/server/v3/etcdserver/errors"
	"go.etcd.io/raft/v3"
)

func TestUint64ToBigEndianBytes(t *testing.T) {
	tests := []struct {
		input    uint64
		expected []byte
	}{
		{0, []byte{0, 0, 0, 0, 0, 0, 0, 0}},
		{1, []byte{0, 0, 0, 0, 0, 0, 0, 1}},
		{256, []byte{0, 0, 0, 0, 0, 0, 1, 0}},
		{0xFFFFFFFFFFFFFFFF, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}},
	}

	for _, tt := range tests {
		result := uint64ToBigEndianBytes(tt.input)
		if !bytesEqual(result, tt.expected) {
			t.Errorf("uint64ToBigEndianBytes(%d) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestBytesEqual(t *testing.T) {
	tests := []struct {
		a        []byte
		b        []byte
		expected bool
	}{
		{[]byte{}, []byte{}, true},
		{[]byte{1}, []byte{1}, true},
		{[]byte{1, 2, 3}, []byte{1, 2, 3}, true},
		{[]byte{1}, []byte{2}, false},
		{[]byte{1, 2}, []byte{1}, false},
		{nil, nil, true},
		{nil, []byte{}, true},
	}

	for i, tt := range tests {
		result := bytesEqual(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("test %d: bytesEqual(%v, %v) = %v, want %v", i, tt.a, tt.b, result, tt.expected)
		}
	}
}

func TestIsStopped(t *testing.T) {
	tests := []struct {
		err      error
		expected bool
	}{
		{nil, false},
		{raft.ErrStopped, true},
		{errors.ErrStopped, true},
		{errors.ErrTimeout, false},
		{errors.ErrLeaderChanged, false},
	}

	for i, tt := range tests {
		result := isStopped(tt.err)
		if result != tt.expected {
			t.Errorf("test %d: isStopped(%v) = %v, want %v", i, tt.err, result, tt.expected)
		}
	}
}

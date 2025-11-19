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

	"go.etcd.io/raft/v3/raftpb"
)

func TestShouldSnapshotToDisk(t *testing.T) {
	tests := []struct {
		name              string
		forceDiskSnapshot bool
		appliedi          uint64
		diskSnapshotIndex uint64
		snapshotCount     uint64
		expected          bool
	}{
		{
			name:              "force snapshot with different indices",
			forceDiskSnapshot: true,
			appliedi:          100,
			diskSnapshotIndex: 50,
			snapshotCount:     10000,
			expected:          true,
		},
		{
			name:              "force snapshot with same indices",
			forceDiskSnapshot: true,
			appliedi:          100,
			diskSnapshotIndex: 100,
			snapshotCount:     10000,
			expected:          false,
		},
		{
			name:              "no force but exceeded snapshot count",
			forceDiskSnapshot: false,
			appliedi:          10100,
			diskSnapshotIndex: 0,
			snapshotCount:     10000,
			expected:          true,
		},
		{
			name:              "no force and below snapshot count",
			forceDiskSnapshot: false,
			appliedi:          5000,
			diskSnapshotIndex: 0,
			snapshotCount:     10000,
			expected:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal EtcdServer for testing
			s := &EtcdServer{
				forceDiskSnapshot: tt.forceDiskSnapshot,
			}
			s.Cfg.SnapshotCount = tt.snapshotCount

			ep := &etcdProgress{
				appliedi:          tt.appliedi,
				diskSnapshotIndex: tt.diskSnapshotIndex,
			}

			result := s.shouldSnapshotToDisk(ep)
			if result != tt.expected {
				t.Errorf("shouldSnapshotToDisk() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestShouldSnapshotToMemory(t *testing.T) {
	tests := []struct {
		name                string
		appliedi            uint64
		memorySnapshotIndex uint64
		expected            bool
	}{
		{
			name:                "should snapshot to memory",
			appliedi:            200,
			memorySnapshotIndex: 50,
			expected:            true,
		},
		{
			name:                "should not snapshot to memory - below threshold",
			appliedi:            100,
			memorySnapshotIndex: 50,
			expected:            false,
		},
		{
			name:                "should not snapshot to memory - at threshold",
			appliedi:            150,
			memorySnapshotIndex: 50,
			expected:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &EtcdServer{}

			ep := &etcdProgress{
				appliedi:            tt.appliedi,
				memorySnapshotIndex: tt.memorySnapshotIndex,
			}

			result := s.shouldSnapshotToMemory(ep)
			if result != tt.expected {
				t.Errorf("shouldSnapshotToMemory() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestVerifySnapshotIndex(t *testing.T) {
	// Test that verifySnapshotIndex doesn't panic when indices match
	snapshot := raftpb.Snapshot{
		Metadata: raftpb.SnapshotMetadata{
			Index: 100,
		},
	}

	// This should not panic
	verifySnapshotIndex(snapshot, 100)
}

func TestVerifyConsistentIndexIsLatest(t *testing.T) {
	// Test that verifyConsistentIndexIsLatest doesn't panic when consistent index >= snapshot index
	snapshot := raftpb.Snapshot{
		Metadata: raftpb.SnapshotMetadata{
			Index: 100,
		},
	}

	// These should not panic
	verifyConsistentIndexIsLatest(snapshot, 100)
	verifyConsistentIndexIsLatest(snapshot, 200)
}

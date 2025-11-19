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
	"context"

	"go.etcd.io/raft/v3/raftpb"
)

// RaftProposer defines the interface for proposing Raft operations.
// This interface allows for easier testing and decoupling of components.
type RaftProposer interface {
	// Propose proposes data to be appended to the Raft log.
	Propose(ctx context.Context, data []byte) error
	// ReadIndex requests a read index from Raft.
	ReadIndex(ctx context.Context, rctx []byte) error
}

// RequestProcessor defines the interface for processing internal Raft requests.
// This interface is used to decouple request processing from the main server logic.
type RequestProcessor interface {
	// Process handles an internal Raft request and returns the result.
	Process(ctx context.Context, req InternalRaftRequest) (*Result, error)
}

// SnapshotCreator defines the interface for snapshot operations.
// This interface allows for better testing and modularity of snapshot functionality.
type SnapshotCreator interface {
	// TriggerSnapshot forces a snapshot to be taken.
	TriggerSnapshot()
	// CreateSnapshot creates a new snapshot and returns it.
	CreateSnapshot() (*raftpb.Snapshot, error)
}

// LinearizableReader defines the interface for linearizable read operations.
// This interface abstracts the linearizable read mechanism for testing.
type LinearizableReader interface {
	// LinearizableReadNotify notifies the linearizable read loop and waits
	// for the read to be ready.
	LinearizableReadNotify(ctx context.Context) error
}

// InternalRaftRequest represents an internal request to be processed through Raft.
// This is a placeholder type - the actual type is defined in the protobuf package.
type InternalRaftRequest interface{}

// Result represents the result of processing a Raft request.
// This is a placeholder type - the actual type is defined in the apply package.
type Result interface{}

// SnapshotServer defines the interface for server snapshot operations.
// This interface is used by various components that need to trigger snapshots.
type SnapshotServer interface {
	// ForceSnapshot forces a snapshot to be taken.
	ForceSnapshot()
}

// Ensure EtcdServer implements the key interfaces.
// These compile-time checks ensure that EtcdServer properly implements
// the interfaces defined in this file.
var (
	_ LinearizableReader = (*EtcdServer)(nil)
	_ SnapshotServer     = (*EtcdServer)(nil)
)

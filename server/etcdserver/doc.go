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

// Package etcdserver defines how etcd servers interact and store their states.
//
// # Package Structure
//
// The etcdserver package is organized into several modules for better maintainability:
//
//   - server.go: Main EtcdServer struct and lifecycle coordination
//   - raft.go: Raft node wrapper and message processing
//   - v3_server.go: V3 API request handling
//   - linearizable.go: Linearizable read handling and coordination
//   - snapshot_mgr.go: Snapshot creation, application, and compaction
//   - interfaces.go: Interface definitions for module boundaries
//   - metrics.go: Prometheus metrics
//
// This modular structure improves testability and reduces cognitive load
// when working with the codebase.
package etcdserver

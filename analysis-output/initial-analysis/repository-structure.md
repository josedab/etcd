# etcd Repository Structure

**Commit:** `d6bc3229d81384aed0fc03a3ddd3dccfef092048`

---

## Directory Overview

```
etcd/
├── api/                    # Protocol Buffers and API definitions
├── cache/                  # Watch cache implementation
├── client/                 # Official Go client libraries
├── contrib/                # Community contributions and examples
├── Documentation/          # User and developer documentation
├── etcdctl/                # Command-line control tool
├── etcdutl/                # Offline utility tool
├── hack/                   # Development utilities and scripts
├── pkg/                    # Shared utility packages
├── scripts/                # Build, test, and release scripts
├── server/                 # Core etcd server implementation
├── tests/                  # Comprehensive test suite
└── tools/                  # Benchmarking and analysis tools
```

---

## Detailed Directory Descriptions

### `/api/` - Protocol Definitions

API protocol buffer definitions and generated code.

```
api/
├── authpb/           # Authentication structures
├── etcdserverpb/     # RPC request/response types (KV, Watch, Lease, etc.)
├── membershippb/     # Cluster membership types
├── mvccpb/           # Key-value with revision metadata
├── v3rpc/            # v3 RPC error types and utilities
│   └── rpctypes/     # RPC type definitions
├── version/          # API version package
└── versionpb/        # Version protocol buffers
```

**Key Files:**
- `etcdserverpb/rpc.proto` - Main gRPC service definitions
- `mvccpb/kv.proto` - KeyValue message definition

---

### `/client/` - Client Libraries

Official Go client for interacting with etcd.

```
client/
├── pkg/v3/           # Client utilities (shared with pkg/)
└── v3/               # V3 Client API
    ├── clientv3util/ # Client utilities
    ├── concurrency/  # Distributed primitives (locks, elections, mutexes)
    ├── credentials/  # TLS/auth credentials
    ├── experimental/ # Experimental features
    ├── internal/     # Internal resolver and endpoint management
    ├── kubernetes/   # Kubernetes-specific integrations
    ├── leasing/      # Leasing utilities
    ├── mirror/       # KV synchronization
    ├── mock/         # Mock client for testing
    ├── namespace/    # Key namespace filtering
    ├── naming/       # Service discovery
    ├── ordering/     # Ordering guarantees
    ├── snapshot/     # Snapshot utilities
    └── yaml/         # YAML configuration support
```

**Key Files:**
- `v3/client.go` - Main client interface
- `v3/kv.go` - KV operations (Put, Get, Delete, Txn)
- `v3/watch.go` - Watch stream implementation
- `v3/lease.go` - Lease management
- `v3/concurrency/mutex.go` - Distributed mutex

---

### `/server/` - Server Implementation

Core etcd server code - this is the heart of the project.

```
server/
├── auth/             # Authentication and authorization
├── config/           # Configuration parsing and validation
├── embed/            # Embeddable server API
├── etcdmain/         # Binary entry point
├── etcdserver/       # Core server logic
│   ├── api/          # Server-internal APIs
│   │   ├── etcdhttp/     # HTTP v2 API (legacy)
│   │   ├── membership/   # Cluster membership management
│   │   ├── rafthttp/     # Raft peer communication
│   │   ├── snap/         # Snapshot handling
│   │   ├── v2store/      # V2 store (legacy)
│   │   ├── v2stats/      # V2 statistics
│   │   ├── v3alarm/      # Quota and alarm system
│   │   ├── v3client/     # Internal v3 client
│   │   ├── v3compactor/  # Revision compaction
│   │   ├── v3discovery/  # Service discovery
│   │   ├── v3election/   # Leader election API
│   │   ├── v3lock/       # Distributed lock API
│   │   └── v3rpc/        # V3 gRPC handlers
│   ├── apply/        # Command application logic
│   ├── cindex/       # Commit index tracking
│   ├── errors/       # Server-specific errors
│   ├── txn/          # Transaction processing
│   └── version/      # Version information
├── features/         # Feature gates
├── lease/            # Lease lifecycle management
├── mock/             # Mock implementations for testing
├── proxy/            # Proxy implementations
│   ├── grpcproxy/    # gRPC protocol-aware proxy
│   └── tcpproxy/     # TCP passthrough proxy
├── storage/          # Storage layer
│   ├── backend/      # BoltDB wrapper
│   ├── datadir/      # Data directory management
│   ├── mvcc/         # Multi-Version Concurrency Control
│   ├── schema/       # Database schema
│   └── wal/          # Write-Ahead Log
└── verify/           # Data verification tools
```

**Key Files:**
- `etcdserver/server.go` - Main server implementation (~2000 lines)
- `etcdserver/v3_server.go` - V3 API implementation
- `etcdserver/raft.go` - Raft consensus integration
- `storage/mvcc/kvstore.go` - Versioned key-value store
- `storage/wal/wal.go` - Write-ahead log implementation
- `storage/backend/backend.go` - BoltDB abstraction

---

### `/tests/` - Test Suite

Comprehensive testing infrastructure.

```
tests/
├── integration/      # In-process cluster tests
│   ├── clientv3/     # Client library tests
│   ├── embed/        # Embedded server tests
│   ├── proxy/        # Proxy tests
│   ├── snapshot/     # Snapshot tests
│   └── v2store/      # V2 store tests
├── e2e/              # End-to-end binary tests
├── robustness/       # Chaos and fault injection tests
│   ├── model/        # State machine models
│   ├── scenarios/    # Test scenarios
│   ├── failpoint/    # Failure injection
│   └── validate/     # Result validation
├── antithesis/       # Property-based testing
├── framework/        # Test framework utilities
├── common/           # Common test utilities
└── fixtures/         # Test data
```

**Key Files:**
- `integration/v3_grpc_test.go` - Comprehensive gRPC tests (~56KB)
- `integration/cluster_test.go` - Multi-member cluster tests
- `e2e/ctl_v3_kv_test.go` - CLI end-to-end tests

---

### `/pkg/` - Utility Packages

Reusable utilities (designed to be standalone).

```
pkg/
├── adt/              # Abstract data types
├── cobrautl/         # Cobra CLI utilities
├── contention/       # Contention tracking
├── cpuutil/          # CPU utilities
├── crc/              # CRC checksums
├── debugutil/        # Debug utilities
├── expect/           # Expect testing
├── featuregate/      # Feature gates
├── flags/            # Flag management
├── grpctesting/      # gRPC testing utilities
├── httputil/         # HTTP utilities
├── idutil/           # ID generation
├── ioutil/           # I/O utilities
├── netutil/          # Network utilities
├── notify/           # Notification mechanisms
├── osutil/           # OS utilities
├── pbutil/           # Protocol buffer utilities
├── proxy/            # Proxy utilities
├── report/           # Reporting utilities
├── runtime/          # Runtime utilities
├── schedule/         # Scheduling utilities
└── stringutil/       # String utilities
```

---

### `/etcdctl/` - CLI Tool

Command-line interface for etcd operations.

```
etcdctl/
├── main.go           # Entry point
└── v3/               # V3 CLI implementation
    └── ctlv3/        # Command handlers
```

---

### `/etcdutl/` - Utility Tool

Offline database operations (defragmentation, snapshots).

```
etcdutl/
├── main.go           # Entry point
└── [utility commands]
```

---

### `/tools/` - Development Tools

Benchmarking and analysis utilities.

```
tools/
├── benchmark/        # Performance benchmarking
├── etcd-dump-db/     # Database dumping
├── etcd-dump-logs/   # WAL log dumping
├── etcd-dump-metrics/# Metrics dumping
├── local-tester/     # Local testing tool
├── proto-annotations/# Protocol buffer annotations
├── rw-heatmaps/      # Read/write heatmap generator
├── testgrid-analysis/# Test grid analysis
└── mod/              # Module manipulation
```

---

### `/hack/` - Development Utilities

Scripts for development and deployment.

```
hack/
├── benchmark/            # Benchmark scripts
├── insta-discovery/      # Instance discovery
├── kubernetes-deploy/    # Kubernetes deployment
├── patch/                # Patch utilities
└── tls-setup/            # TLS setup scripts
```

---

### `/scripts/` - Build & Test Scripts

Core build and test automation.

```
scripts/
├── build.sh              # Main build script
├── build_lib.sh          # Build library functions
├── build_tools.sh        # Build development tools
├── test.sh               # Test runner
├── test_lib.sh           # Test utilities
├── genproto.sh           # Protocol buffer generation
├── release.sh            # Release script
├── fuzzing.sh            # Fuzz testing
└── [verification scripts]
```

---

### `/.github/` - GitHub Configuration

CI/CD and GitHub automation.

```
.github/
└── workflows/
    ├── tests.yaml            # Main test workflow
    ├── codeql-analysis.yml   # Security scanning
    ├── antithesis-test.yml   # Property-based testing
    └── [other workflows]
```

---

## Module Structure

etcd uses Go workspaces with multiple modules:

| Module | Path | Description |
|--------|------|-------------|
| Root | `go.mod` | Workspace coordinator |
| API | `api/go.mod` | Protocol definitions |
| Client | `client/v3/go.mod` | Client library |
| Client Pkg | `client/pkg/go.mod` | Client utilities |
| Server | `server/go.mod` | Server implementation |
| Pkg | `pkg/go.mod` | Shared utilities |
| etcdctl | `etcdctl/go.mod` | CLI tool |
| etcdutl | `etcdutl/go.mod` | Utility tool |
| Cache | `cache/go.mod` | Watch cache |
| Tests | `tests/go.mod` | Test suite |

---

## Entry Points

| Binary | Entry Point | Purpose |
|--------|-------------|---------|
| etcd | `server/main.go` | etcd server |
| etcdctl | `etcdctl/main.go` | CLI tool |
| etcdutl | `etcdutl/main.go` | Utility tool |

---

## Configuration Files

| File | Purpose |
|------|---------|
| `Makefile` | Build automation |
| `go.work` | Go workspace configuration |
| `Dockerfile` | Container image |
| `Procfile` | Local cluster setup |
| `codecov.yml` | Code coverage config |
| `etcd.conf.yml.sample` | Example configuration |

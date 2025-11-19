# etcd Codebase Analysis - Quick Start Guide

**Read this first for a rapid understanding of the etcd codebase.**

**Analysis Commit:** `d6bc3229d81384aed0fc03a3ddd3dccfef092048`

---

## What is etcd?

etcd is a **distributed, reliable key-value store** that uses the Raft consensus algorithm. It's the backbone of Kubernetes, storing all cluster state and configuration.

### Why It Matters
- Every Kubernetes cluster runs etcd
- Millions of production deployments worldwide
- Critical infrastructure that must never lose data

---

## 5-Minute Architecture Overview

```
Client Request → gRPC API → Raft Consensus → Apply to State Machine → Storage
```

### Key Components

1. **Client Library** (`client/v3/`)
   - What you use to talk to etcd
   - Provides KV, Watch, Lease, Auth APIs

2. **Server** (`server/etcdserver/`)
   - Handles requests, coordinates consensus
   - Main file: `server.go` (the heart of etcd)

3. **Storage** (`server/storage/`)
   - **MVCC**: Multi-version concurrency control for watches
   - **WAL**: Write-ahead log for durability
   - **Backend**: BoltDB wrapper for persistence

4. **Raft** (`go.etcd.io/raft/v3`)
   - Consensus algorithm implementation
   - Ensures all nodes agree on state

---

## Critical Paths to Understand

### 1. Write Path (Put a key)

```go
// Client call
client.Put(ctx, "foo", "bar")

// Server flow:
// 1. v3rpc/kv.go:Put() → validates request
// 2. etcdserver/v3_server.go:Put() → proposes to Raft
// 3. Raft replicates to majority
// 4. apply/apply.go → applies to state machine
// 5. storage/mvcc/kvstore.go:Put() → stores in MVCC
// 6. storage/backend/batch_tx.go → persists to BoltDB
```

### 2. Read Path (Get a key)

```go
// Client call
client.Get(ctx, "foo")

// Server flow:
// 1. v3rpc/kv.go:Range() → validates request
// 2. etcdserver/v3_server.go:Range()
//    → linearizable: wait for read index
//    → serializable: read directly
// 3. storage/mvcc/kvstore.go:Range() → retrieves from MVCC
```

### 3. Watch Path (Subscribe to changes)

```go
// Client call
watchChan := client.Watch(ctx, "foo")

// Server flow:
// 1. v3rpc/watch.go → creates watch stream
// 2. storage/mvcc/watchable_store.go → registers watcher
// 3. On changes: events pushed to watch channel
```

---

## Key Files to Read First

| Priority | File | Purpose |
|----------|------|---------|
| 1 | `server/etcdserver/server.go` | Main server logic |
| 2 | `server/etcdserver/v3_server.go` | V3 API implementation |
| 3 | `server/storage/mvcc/kvstore.go` | KV store with versioning |
| 4 | `client/v3/client.go` | Client connection management |
| 5 | `server/etcdserver/raft.go` | Raft integration |

---

## Quick Terminology

| Term | Meaning |
|------|---------|
| **Revision** | Global logical clock, increments on every change |
| **ModRevision** | Revision when a key was last modified |
| **CreateRevision** | Revision when a key was created |
| **Lease** | TTL-based expiration for keys |
| **Compaction** | Removing old revisions to reclaim space |
| **WAL** | Write-Ahead Log for crash recovery |
| **MVCC** | Multi-Version Concurrency Control |

---

## Building and Testing

```bash
# Build etcd server
make build

# Run unit tests
make test-unit

# Run integration tests
make test-integration

# Run all tests
make test

# Build and run local cluster
goreman start
```

---

## Project Structure at a Glance

```
etcd/
├── api/           # Protocol buffer definitions
├── client/v3/     # Go client library
├── server/        # Server implementation
│   ├── etcdserver/    # Core server logic
│   ├── storage/       # MVCC, WAL, Backend
│   ├── auth/          # Authentication
│   └── lease/         # Lease management
├── etcdctl/       # CLI tool
├── pkg/           # Shared utilities
└── tests/         # Integration & e2e tests
```

---

## Common Operations

### Start a development cluster

```bash
# Using goreman (from Procfile)
goreman start

# Manual single node
./bin/etcd

# Three node cluster
./bin/etcd --name node1 --initial-cluster node1=http://localhost:2380,...
```

### Use etcdctl

```bash
# Put a key
etcdctl put foo bar

# Get a key
etcdctl get foo

# Watch for changes
etcdctl watch foo

# List all keys
etcdctl get "" --prefix
```

---

## What Makes etcd Special

### 1. Strong Consistency
Every read returns the most recent write (linearizable).

### 2. Watch Mechanism
Subscribe to key changes with revision-based guarantees.

### 3. Transactions
Multi-key atomic operations with conditions.

```go
// If foo == "bar", set baz = "qux"
txn := client.Txn(ctx).
    If(clientv3.Compare(clientv3.Value("foo"), "=", "bar")).
    Then(clientv3.OpPut("baz", "qux")).
    Else(clientv3.OpGet("foo"))
```

### 4. Leases
Automatic key expiration with keep-alive.

```go
lease, _ := client.Grant(ctx, 60) // 60 second TTL
client.Put(ctx, "foo", "bar", clientv3.WithLease(lease.ID))
```

---

## Next Steps

1. **Deep dive**: Read `blog-series/01-architecture-overview.md`
2. **Storage internals**: Read `blog-series/02-deep-dive-storage.md`
3. **Improvement ideas**: Check `rfcs/00-prioritization-matrix.md`

---

## Quick Reference URLs

- GitHub: https://github.com/etcd-io/etcd
- Documentation: https://etcd.io/docs
- This analysis commit: https://github.com/etcd-io/etcd/tree/d6bc3229d81384aed0fc03a3ddd3dccfef092048

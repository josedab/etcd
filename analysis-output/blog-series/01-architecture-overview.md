# Understanding etcd: Architecture and Core Concepts

*Part 1 of the etcd Deep Dive Series*

**Analysis Commit:** [`d6bc3229d81384aed0fc03a3ddd3dccfef092048`](https://github.com/etcd-io/etcd/tree/d6bc3229d81384aed0fc03a3ddd3dccfef092048)

---

## What You'll Learn

- Why etcd exists and what problems it solves
- The architectural patterns and trade-offs
- Core abstractions that power the system
- How to navigate the codebase

---

## Introduction

If you've used Kubernetes, you've used etcd—whether you knew it or not. Every pod, deployment, and service configuration lives in etcd. It's the source of truth that makes Kubernetes work.

But what is etcd, exactly? And why does a distributed key-value store need to be so sophisticated?

Let's find out.

---

## What is etcd?

etcd (pronounced "et-see-dee") is a distributed, reliable key-value store. The name comes from the Unix `/etc` directory (for configuration) plus "d" for distributed.

It provides three key guarantees:

1. **Strong consistency** - Every read returns the most recent write
2. **High availability** - Tolerates minority node failures
3. **Durability** - Data survives restarts and crashes

These guarantees make etcd ideal for storing configuration and coordination data in distributed systems.

---

## The Problem Domain

Distributed systems face a fundamental challenge: **how do multiple nodes agree on shared state?**

Consider these scenarios:
- Which node is the current leader?
- What's the latest configuration?
- Who owns this distributed lock?

Without consensus, you get:
- Split-brain scenarios (multiple leaders)
- Lost updates (stale reads)
- Deadlocks (abandoned locks)

etcd solves this with the **Raft consensus algorithm**, ensuring all nodes agree on every change.

---

## Architecture Overview

Here's how etcd is structured:

```
┌──────────────────────────────────────────────────┐
│                  Client (gRPC)                   │
└────────────────────────┬─────────────────────────┘
                         │
                         ▼
┌──────────────────────────────────────────────────┐
│              V3 RPC Layer                        │
│  (server/etcdserver/api/v3rpc/)                  │
│  - Request validation                            │
│  - Auth checking                                 │
│  - Response formatting                           │
└────────────────────────┬─────────────────────────┘
                         │
                         ▼
┌──────────────────────────────────────────────────┐
│              Server Core                         │
│  (server/etcdserver/)                            │
│  - Request routing                               │
│  - Raft integration                              │
│  - State machine                                 │
└──────────┬─────────────┬─────────────┬───────────┘
           │             │             │
           ▼             ▼             ▼
┌──────────────┐ ┌─────────────┐ ┌─────────────┐
│    Raft      │ │   Storage   │ │   Auth      │
│ Consensus    │ │    Layer    │ │   Lease     │
│ (external)   │ │   (MVCC)    │ │   Alarm     │
└──────────────┘ └─────────────┘ └─────────────┘
                        │
                        ▼
              ┌─────────────────┐
              │    BoltDB       │
              │   (Persistent)  │
              └─────────────────┘
```

---

## The CAP Theorem Trade-off

The CAP theorem states that a distributed system can only provide two of three guarantees:
- **C**onsistency
- **A**vailability
- **P**artition tolerance

etcd is a **CP system**—it chooses consistency over availability during network partitions.

**Why?** Because incorrect data in a coordination system is worse than unavailable data.

Imagine if Kubernetes received stale data from etcd:
- Pods scheduled on failed nodes
- Services routing to wrong endpoints
- RBAC rules not enforced

The consequences of inconsistency are severe. etcd makes the right trade-off for its problem domain.

### The Cost

During a network partition, a minority partition becomes unavailable for writes (and linearizable reads). This is acceptable because:

1. Partitions are rare in well-managed networks
2. Minority partitions shouldn't make progress anyway
3. Consistency violations are much harder to recover from

---

## Core Abstractions

Let's explore the key concepts that make etcd work.

### 1. Revisions

Every mutation in etcd increments a global **revision counter**. This creates a total ordering of all operations.

```go
// From server/storage/mvcc/kvstore.go
type store struct {
    // ...
    currentRev int64      // Current global revision
    compactMainRev int64  // Revision before which history is compacted
}
```

The revision appears in every response:

```go
// Example: Put response
resp, _ := client.Put(ctx, "foo", "bar")
fmt.Println(resp.Header.Revision) // e.g., 42
```

**Why revisions matter:**
- Enable point-in-time reads
- Power the watch mechanism
- Detect concurrent modifications

### 2. MVCC (Multi-Version Concurrency Control)

etcd keeps multiple versions of each key, indexed by revision. This enables:

- Historical queries: "What was `foo` at revision 100?"
- Watch events: "What changed since revision 50?"
- Compaction: "Delete history before revision 1000"

```go
// Get a key at a specific revision
resp, _ := client.Get(ctx, "foo", clientv3.WithRev(100))
```

The storage layer ([`server/storage/mvcc/kvstore.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/storage/mvcc/kvstore.go)) manages this complexity.

### 3. Leases

A **lease** is a time-to-live (TTL) for keys. When a lease expires, all attached keys are automatically deleted.

```go
// Grant a lease with 60-second TTL
lease, _ := client.Grant(ctx, 60)

// Attach key to lease
client.Put(ctx, "session/node1", "alive", clientv3.WithLease(lease.ID))

// Keep alive to prevent expiration
keepAlive, _ := client.KeepAlive(ctx, lease.ID)
```

**Use cases:**
- Service registration (delete when service dies)
- Distributed locks (auto-release on failure)
- Session management

The lease implementation lives in [`server/lease/lessor.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/lease/lessor.go).

### 4. Watches

**Watches** subscribe to key changes, receiving events when keys are created, modified, or deleted.

```go
watchChan := client.Watch(ctx, "config/", clientv3.WithPrefix())

for event := range watchChan {
    for _, ev := range event.Events {
        fmt.Printf("%s %s = %s\n", ev.Type, ev.Kv.Key, ev.Kv.Value)
    }
}
```

The watch system is implemented in:
- Client: [`client/v3/watch.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/client/v3/watch.go)
- Server: [`server/storage/mvcc/watchable_store.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/storage/mvcc/watchable_store.go)

**Key feature:** Watches are revision-based. You can start watching from a historical revision:

```go
// Resume watching from where we left off
client.Watch(ctx, "config/", clientv3.WithRev(lastSeenRevision+1))
```

This enables reliable delivery even if your client disconnects.

---

## The Write Path

When you write a key, here's what happens:

```
Client: Put("foo", "bar")
         │
         ▼
1. V3 RPC: Validate request
         │
         ▼
2. Server: Propose to Raft
         │
         ▼
3. Raft: Replicate to majority
         │
         ▼
4. Server: Apply to state machine
         │
         ▼
5. MVCC: Store with new revision
         │
         ▼
6. Backend: Persist to BoltDB
         │
         ▼
7. Response to client
```

Let's trace through the code.

### Step 1: RPC Handler

The gRPC handler in [`server/etcdserver/api/v3rpc/kv.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/etcdserver/api/v3rpc/kv.go):

```go
func (s *kvServer) Put(ctx context.Context, r *pb.PutRequest) (*pb.PutResponse, error) {
    // Validate request
    if err := checkPutRequest(r); err != nil {
        return nil, err
    }

    // Forward to server core
    resp, err := s.kv.Put(ctx, r)
    // ...
}
```

### Step 2-3: Raft Proposal

The server proposes the operation to Raft in [`server/etcdserver/v3_server.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/etcdserver/v3_server.go):

```go
func (s *EtcdServer) Put(ctx context.Context, r *pb.PutRequest) (*pb.PutResponse, error) {
    // Propose to Raft and wait for commit
    resp, err := s.raftRequest(ctx, pb.InternalRaftRequest{Put: r})
    // ...
}
```

The request is serialized, replicated to a majority of nodes, and committed.

### Step 4-5: Apply to State Machine

Once committed, the applier executes the operation ([`server/etcdserver/apply/apply.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/etcdserver/apply/apply.go)):

```go
func (a *uberApplier) Apply(req *pb.InternalRaftRequest, ...) *Result {
    switch {
    case req.Put != nil:
        return a.applyPut(req.Put, ...)
    // ...
    }
}
```

### Step 6: Persistence

The MVCC store writes to BoltDB ([`server/storage/mvcc/kvstore.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/storage/mvcc/kvstore.go)):

```go
func (s *store) Put(key, value []byte, lease lease.LeaseID) int64 {
    // Increment revision
    rev := s.currentRev + 1

    // Create versioned key-value
    kv := mvccpb.KeyValue{
        Key:            key,
        Value:          value,
        CreateRevision: rev,
        ModRevision:    rev,
        Version:        1,
        Lease:          int64(lease),
    }

    // Persist
    s.kvindex.Put(key, rev)
    s.tx.UnsafeSeqPut(bucketName, key, kv)

    return rev
}
```

---

## The Read Path

Reads can be **linearizable** or **serializable**.

### Linearizable Reads (Default)

Guarantees you see the most recent committed write:

```go
resp, _ := client.Get(ctx, "foo")
```

Process:
1. Request goes to any node
2. Node asks leader: "What's the current commit index?"
3. Wait until local state catches up
4. Return data

This adds latency but guarantees freshness.

### Serializable Reads

May return stale data but is faster:

```go
resp, _ := client.Get(ctx, "foo", clientv3.WithSerializable())
```

Useful when:
- Slight staleness is acceptable
- You need lower latency
- You're doing read-heavy operations

The implementation is in [`server/etcdserver/v3_server.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/etcdserver/v3_server.go):

```go
func (s *EtcdServer) Range(ctx context.Context, r *pb.RangeRequest) (*pb.RangeResponse, error) {
    if !r.Serializable {
        // Wait for read index from leader
        if err := s.linearizableReadNotify(ctx); err != nil {
            return nil, err
        }
    }

    // Read from local store
    return s.applyV3.Range(ctx, r)
}
```

---

## Navigating the Codebase

Here's how to find your way around:

### Entry Points

| If you want to understand... | Start here |
|------------------------------|------------|
| Server startup | `server/etcdmain/main.go` |
| API handling | `server/etcdserver/api/v3rpc/` |
| Core server logic | `server/etcdserver/server.go` |
| Storage | `server/storage/mvcc/kvstore.go` |
| Client library | `client/v3/client.go` |

### Key Interfaces

The codebase uses interfaces extensively for testability:

```go
// From server/etcdserver/v3_server.go
type RaftKV interface {
    Range(ctx context.Context, r *pb.RangeRequest) (*pb.RangeResponse, error)
    Put(ctx context.Context, r *pb.PutRequest) (*pb.PutResponse, error)
    DeleteRange(ctx context.Context, r *pb.DeleteRangeRequest) (*pb.DeleteRangeResponse, error)
    Txn(ctx context.Context, r *pb.TxnRequest) (*pb.TxnResponse, error)
    Compact(ctx context.Context, r *pb.CompactionRequest) (*pb.CompactionResponse, error)
}
```

### Package Structure

```
server/
├── etcdserver/      # Core server (start here!)
│   ├── api/         # Protocol handlers
│   └── apply/       # State machine application
├── storage/         # Persistence layer
│   ├── mvcc/        # Multi-version store
│   ├── wal/         # Write-ahead log
│   └── backend/     # BoltDB wrapper
├── auth/            # Authentication
└── lease/           # Lease management
```

---

## Why These Trade-offs?

Let's examine why etcd makes the choices it does.

### Raft over Paxos

**Choice:** Raft consensus algorithm
**Alternative:** Paxos (more established)

**Reasoning:** Raft was designed for understandability. The [Raft paper](https://raft.github.io/raft.pdf) explicitly states this goal. For a project like etcd that needs to be maintained by many contributors, this clarity is valuable.

**Trade-off:** Raft's single-leader model can be a bottleneck, but it simplifies reasoning about the system.

### BoltDB for Storage

**Choice:** BoltDB (B+tree)
**Alternative:** LSM-tree (RocksDB, LevelDB)

**Reasoning:** BoltDB provides:
- Pure Go (no CGo)
- Simple, battle-tested design
- Good read performance
- ACID transactions

**Trade-off:** Write amplification is higher than LSM trees, but etcd's workload is read-heavy, so this is acceptable.

### gRPC over HTTP/REST

**Choice:** gRPC with Protocol Buffers
**Alternative:** HTTP/REST with JSON

**Reasoning:**
- Strong typing prevents many bugs
- Bidirectional streaming for watches
- Better performance (binary protocol)
- Generated clients in multiple languages

**Trade-off:** Less approachable for debugging (can't just use curl), but `etcdctl` and gRPC-gateway mitigate this.

---

## Key Takeaways

1. **etcd is a CP system** - It prioritizes consistency over availability, which is the right choice for coordination data.

2. **Revisions are fundamental** - The global revision counter enables watches, historical queries, and conflict detection.

3. **MVCC enables watches** - By keeping key history, etcd can send change events efficiently.

4. **Raft ensures consensus** - Every write is agreed upon by a majority of nodes before being considered committed.

5. **The write path goes through Raft** - No shortcuts. Every mutation is proposed, replicated, and then applied.

6. **Reads can trade consistency for performance** - Use serializable reads when appropriate.

---

## Next in the Series

In [Part 2: Deep Dive - The Storage Layer](02-deep-dive-storage.md), we'll explore:
- How MVCC stores multiple versions
- The BoltDB backend implementation
- Write-ahead logging for durability
- Compaction strategies

---

## Explore the Code

Clone and checkout:

```bash
git clone https://github.com/etcd-io/etcd.git
cd etcd
git checkout d6bc3229d81384aed0fc03a3ddd3dccfef092048
```

Start exploring:
- [`server/etcdserver/server.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/etcdserver/server.go) - The heart of etcd
- [`server/storage/mvcc/kvstore.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/storage/mvcc/kvstore.go) - Where data lives

---

*Next: [Part 2 - Deep Dive: The Storage Layer](02-deep-dive-storage.md)*

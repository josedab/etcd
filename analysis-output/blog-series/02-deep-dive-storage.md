# Deep Dive: The Storage Layer

*Part 2 of the etcd Deep Dive Series*

**Analysis Commit:** [`d6bc3229d81384aed0fc03a3ddd3dccfef092048`](https://github.com/etcd-io/etcd/tree/d6bc3229d81384aed0fc03a3ddd3dccfef092048)

---

## What You'll Learn

- How MVCC stores multiple versions of keys
- The BoltDB backend architecture
- Write-ahead logging for durability
- How compaction reclaims space
- The actual write and read paths

---

## Introduction

In the previous post, we saw that etcd uses MVCC (Multi-Version Concurrency Control) to store data. But what does that actually mean? How does etcd manage to keep multiple versions of every key while still being performant?

Let's dive into the storage layer and find out.

---

## Storage Architecture

The storage layer has three main components:

```
┌────────────────────────────────────────────┐
│              MVCC Store                    │
│  (server/storage/mvcc/)                    │
│  - Key versioning                          │
│  - Watch notifications                     │
│  - Index management                        │
└──────────────────┬─────────────────────────┘
                   │
         ┌─────────┴─────────┐
         │                   │
         ▼                   ▼
┌─────────────────┐  ┌─────────────────┐
│    Backend      │  │      WAL        │
│   (BoltDB)      │  │  (Write-Ahead)  │
│                 │  │                 │
│  - B+tree KV    │  │  - Log entries  │
│  - Transactions │  │  - Crash safety │
│  - Snapshots    │  │  - Recovery     │
└─────────────────┘  └─────────────────┘
```

Let's explore each component.

---

## MVCC: Multi-Version Concurrency Control

The core insight of MVCC is simple: **don't overwrite data, append new versions**.

### The Store Structure

From [`server/storage/mvcc/kvstore.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/storage/mvcc/kvstore.go):

```go
type store struct {
    ReadView
    WriteView

    cfg StoreConfig

    mu sync.RWMutex

    b       backend.Backend
    kvindex index

    le lease.Lessor

    revMu sync.RWMutex
    currentRev int64
    compactMainRev int64

    fifoSched schedule.Scheduler

    stopc chan struct{}
    lg    *zap.Logger
}
```

Key fields:
- `b`: The BoltDB backend
- `kvindex`: In-memory index mapping keys to revisions
- `currentRev`: Global revision counter
- `compactMainRev`: Oldest revision that hasn't been compacted

### How Keys Are Stored

Each key-value pair includes revision metadata:

```go
// From api/mvccpb/kv.pb.go
type KeyValue struct {
    Key            []byte
    CreateRevision int64  // Revision when created
    ModRevision    int64  // Revision when last modified
    Version        int64  // Number of modifications
    Value          []byte
    Lease          int64  // Associated lease ID
}
```

When you put a key multiple times:

```go
client.Put(ctx, "foo", "v1")  // CreateRev=10, ModRev=10, Version=1
client.Put(ctx, "foo", "v2")  // CreateRev=10, ModRev=11, Version=2
client.Put(ctx, "foo", "v3")  // CreateRev=10, ModRev=12, Version=3
```

All three versions exist in storage until compacted.

### The Key Index

The in-memory index maps keys to their revisions:

```go
// From server/storage/mvcc/index.go
type treeIndex struct {
    sync.RWMutex
    tree *btree.BTreeG[*keyIndex]
    lg   *zap.Logger
}

type keyIndex struct {
    key         []byte
    modified    revision  // Latest modification
    generations []generation
}

type generation struct {
    ver     int64       // Version number
    created revision    // When created
    revs    []revision  // All revisions
}
```

This B-tree index enables fast key lookups without scanning the entire database.

### Put Implementation

Here's how a Put actually works ([`server/storage/mvcc/kvstore_txn.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/storage/mvcc/kvstore_txn.go)):

```go
func (tw *storeTxnWrite) put(key, value []byte, leaseID lease.LeaseID) {
    rev := tw.beginRev + 1
    c := rev

    // Check if key exists
    _, created, ver, err := tw.s.kvindex.Get(key, rev)
    if err == nil {
        c = created.main
        ver = ver + 1
    }

    // Create internal key with revision
    ibytes := newRevBytes()
    idxRev := revision{main: rev, sub: int64(tw.changes)}
    revToBytes(idxRev, ibytes)

    // Store key-value
    kv := mvccpb.KeyValue{
        Key:            key,
        Value:          value,
        CreateRevision: c,
        ModRevision:    rev,
        Version:        ver,
        Lease:          int64(leaseID),
    }

    d, err := kv.Marshal()
    tw.tx.UnsafeSeqPut(schema.Key, ibytes, d)

    // Update index
    tw.s.kvindex.Put(key, idxRev)
    tw.changes++
}
```

The key insight: the BoltDB key is the **revision**, not the user key. This allows efficient sequential writes.

### Range Queries

Range queries use the index to find relevant revisions, then fetch from BoltDB:

```go
func (tr *storeTxnRead) rangeKeys(key, end []byte, limit, rev int64) (*RangeResult, error) {
    // Get revisions from index
    revpairs, total := tr.s.kvindex.Range(key, end, rev)

    if limit > 0 && total > limit {
        // Apply limit
    }

    // Fetch key-values from backend
    kvs := make([]mvccpb.KeyValue, len(revpairs))
    for i, revpair := range revpairs {
        revBytes := newRevBytes()
        revToBytes(revpair.revision, revBytes)

        _, vs := tr.tx.UnsafeRange(schema.Key, revBytes, nil, 0)
        kvs[i].Unmarshal(vs[0])
    }

    return &RangeResult{KVs: kvs, Count: total}, nil
}
```

---

## BoltDB Backend

etcd uses BoltDB (maintained as [bbolt](https://github.com/etcd-io/bbolt)) for persistent storage.

### Why BoltDB?

- **Pure Go**: No CGo dependencies
- **B+tree**: Good read performance
- **ACID transactions**: Atomic updates
- **Memory-mapped**: Fast sequential reads

### Backend Architecture

From [`server/storage/backend/backend.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/storage/backend/backend.go):

```go
type Backend interface {
    ReadTx() ReadTx
    BatchTx() BatchTx
    ConcurrentReadTx() ReadTx

    Snapshot() Snapshot
    Hash(ignores func(bucketName, keyName []byte) bool) (uint32, error)

    Size() int64
    SizeInUse() int64
    OpenReadTxN() int64

    Defrag() error
    ForceCommit()
    Close() error
}
```

### Buckets

Data is organized into buckets (like tables):

```go
// From server/storage/schema/bucket.go
var (
    Key    = schema.Bucket{Name: []byte("key")}
    Meta   = schema.Bucket{Name: []byte("meta")}
    Lease  = schema.Bucket{Name: []byte("lease")}
    Alarm  = schema.Bucket{Name: []byte("alarm")}
    Auth   = schema.Bucket{Name: []byte("auth")}
    // ...
)
```

### Batch Transactions

Writes are batched for performance ([`server/storage/backend/batch_tx.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/storage/backend/batch_tx.go)):

```go
type batchTxBuffered struct {
    batchTx
    buf txWriteBuffer
}

// Writes go to buffer first
func (t *batchTxBuffered) UnsafeSeqPut(bucket Bucket, key []byte, value []byte) {
    t.buf.putSeq(bucket, key, value)
}

// Commit flushes buffer to BoltDB
func (t *batchTxBuffered) commit(stop bool) {
    // Flush buffered writes
    t.buf.writeback(&t.batchTx)

    // Commit BoltDB transaction
    t.batchTx.commit(stop)
}
```

Batching reduces fsync overhead by grouping multiple operations.

### Read Transactions

Reads use separate transactions for isolation:

```go
type readTx struct {
    buf     txReadBuffer
    txMu    *sync.RWMutex
    tx      *bolt.Tx
    buckets map[BucketID]*bolt.Bucket
}
```

The `txReadBuffer` caches recent reads to avoid repeated BoltDB lookups.

---

## Write-Ahead Log (WAL)

Before any Raft entry is committed, it's written to the WAL. This ensures durability even if etcd crashes.

### WAL Structure

From [`server/storage/wal/wal.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/storage/wal/wal.go):

```go
type WAL struct {
    lg *zap.Logger

    dir string // The directory for storing wal files

    dirFile *os.File // File descriptor for the directory

    metadata []byte           // Metadata recorded at head
    state    raftpb.HardState // Raft hard state

    start     walpb.Snapshot // Snapshot to start reading
    decoder   *decoder       // Read from existing logs
    readClose func() error

    mu      sync.Mutex
    enti    uint64   // Index of last entry saved
    encoder *encoder // Encode records

    locks []*fileutil.LockedFile // Wal files
    fp    *filePipeline          // File pipeline for allocation
}
```

### WAL Records

The WAL stores several record types:

```go
// From server/storage/wal/walpb/record.proto
message Record {
    int64 type = 1;
    uint32 crc = 2;
    bytes data = 3;
}

// Record types
const (
    metadataType int64 = iota + 1
    entryType
    stateType
    crcType
    snapshotType
)
```

### Write Path

When Raft commits an entry:

```go
func (w *WAL) Save(st raftpb.HardState, ents []raftpb.Entry) error {
    w.mu.Lock()
    defer w.mu.Unlock()

    // Save Raft state
    if !raft.IsEmptyHardState(st) {
        w.state = st
        err := w.saveState(&st)
    }

    // Save entries
    for i := range ents {
        err := w.saveEntry(&ents[i])
    }

    // Fsync to ensure durability
    err := w.sync()

    return nil
}
```

The critical `w.sync()` call forces data to disk.

### Segment Files

WAL is split into segment files (default 64MB each):

```
member/wal/
├── 0000000000000000-0000000000000000.wal
├── 0000000000000001-0000000000010001.wal
└── 0000000000000002-0000000000020001.wal
```

Filename format: `{seq}-{index}.wal`

### Recovery

On startup, WAL replays all entries:

```go
func (w *WAL) ReadAll() (metadata []byte, state raftpb.HardState, ents []raftpb.Entry, err error) {
    w.mu.Lock()
    defer w.mu.Unlock()

    rec := &walpb.Record{}

    for err = w.decoder.decode(rec); err == nil; err = w.decoder.decode(rec) {
        switch rec.Type {
        case entryType:
            var e raftpb.Entry
            e.Unmarshal(rec.Data)
            ents = append(ents, e)
        case stateType:
            state.Unmarshal(rec.Data)
        // ...
        }
    }

    return metadata, state, ents, nil
}
```

---

## Compaction

MVCC stores all versions, but disk space is finite. Compaction removes old versions.

### How Compaction Works

```go
// From server/storage/mvcc/kvstore.go
func (s *store) Compact(trace *traceutil.Trace, rev int64) (<-chan struct{}, error) {
    s.revMu.Lock()
    if rev <= s.compactMainRev {
        // Already compacted
        s.revMu.Unlock()
        return nil, ErrCompacted
    }

    s.compactMainRev = rev
    s.revMu.Unlock()

    // Schedule compaction
    return s.scheduleCompaction(rev)
}
```

The actual compaction:

```go
func (s *store) scheduleCompaction(compactMainRev int64) (<-chan struct{}, error) {
    ch := make(chan struct{})

    s.fifoSched.Schedule(func() {
        defer close(ch)

        // Get keys to delete from index
        keep := s.kvindex.Compact(compactMainRev)

        // Delete from backend
        tx := s.b.BatchTx()
        tx.LockOutsideApply()
        // Delete old revisions not in keep set
        tx.Unlock()
    })

    return ch, nil
}
```

### Compaction Strategies

etcd supports two automatic compaction modes:

**Periodic (time-based):**
```bash
etcd --auto-compaction-mode=periodic --auto-compaction-retention=1h
```

**Revision (retain N revisions):**
```bash
etcd --auto-compaction-mode=revision --auto-compaction-retention=10000
```

Implementation in [`server/etcdserver/api/v3compactor/`](https://github.com/etcd-io/etcd/tree/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/etcdserver/api/v3compactor).

### Defragmentation

Compaction marks space as free, but doesn't return it to the filesystem. Defragmentation rewrites the database file:

```go
// From server/storage/backend/backend.go
func (b *backend) Defrag() error {
    // Create new database file
    // Copy live data
    // Swap files
}
```

Run during maintenance windows (causes brief unavailability).

---

## The Complete Write Path

Let's trace a Put from client to disk:

```
1. Client: Put("foo", "bar")
   │
   ▼
2. gRPC → Server: Validate request
   │
   ▼
3. Server → Raft: Propose entry
   │
   ▼
4. Raft → WAL: Write entry
   │        │
   │        ├─ Encode entry
   │        ├─ Write to file
   │        └─ Fsync ← DURABILITY POINT
   │
   ▼
5. Raft → Peers: Replicate entry
   │
   ▼
6. Raft → Server: Entry committed
   │
   ▼
7. Server → Apply: Execute operation
   │
   ▼
8. Apply → MVCC: Store key-value
   │        │
   │        ├─ Increment revision
   │        ├─ Update index
   │        └─ Write to buffer
   │
   ▼
9. MVCC → Backend: Batch commit
   │        │
   │        ├─ Flush buffer
   │        └─ Commit BoltDB tx
   │
   ▼
10. Response to client
```

### Critical Points

- **Step 4 (WAL fsync)**: This is the durability guarantee. Even if etcd crashes before step 9, the operation will be replayed from WAL.

- **Step 9 (Batch commit)**: BoltDB commits are batched for performance. Default batch interval is 100ms.

---

## Watch Integration

The MVCC store also powers the watch system.

### Watchable Store

From [`server/storage/mvcc/watchable_store.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/storage/mvcc/watchable_store.go):

```go
type watchableStore struct {
    *store

    mu sync.RWMutex

    unsynced watcherGroup  // Watchers catching up
    synced   watcherGroup  // Watchers up-to-date

    stopc chan struct{}
    wg    sync.WaitGroup
}
```

### Event Generation

When data changes, events are sent to watchers:

```go
func (s *watchableStore) notify(rev int64, evs []mvccpb.Event) {
    // Find watchers interested in these keys
    for w := range s.synced.watcherSetByKey[key] {
        // Send event
        select {
        case w.ch <- WatchResponse{Events: evs}:
        default:
            // Move to unsynced if blocked
            s.unsynced.add(w)
        }
    }
}
```

### Efficiency

Watches use MVCC revisions to:
- Start from historical point
- Never miss events
- Resume after disconnection

---

## Performance Characteristics

### Memory Usage

- **Index**: All keys held in memory (B-tree)
- **Read buffer**: Recent reads cached
- **Write buffer**: Batched writes accumulated

Estimate: ~100 bytes per key for index entry.

### Disk I/O

- **WAL**: Sequential writes (fast)
- **BoltDB**: Random reads, sequential writes
- **Fsync**: Dominated by WAL syncs

### Bottlenecks

1. **WAL fsync**: Required for durability, ~1ms per sync
2. **BoltDB lock**: Single writer at a time
3. **Index memory**: Grows with key count

---

## Key Takeaways

1. **MVCC enables features** - Multiple versions allow watches, historical queries, and consistent reads without blocking writes.

2. **The revision is the key** - In BoltDB, data is stored by revision, not user key. This enables efficient sequential writes.

3. **WAL provides durability** - Operations are durable once written to WAL, even before BoltDB commit.

4. **Batching improves throughput** - Writes are batched to reduce fsync overhead.

5. **Compaction is essential** - Without compaction, disk usage grows unbounded.

6. **The index must fit in memory** - This is a scalability constraint.

---

## Next in the Series

In [Part 3: Patterns and Practices](03-patterns-practices.md), we'll explore:
- Interface-driven design
- Concurrency patterns
- Error handling strategies
- The applier pattern

---

## Explore the Code

Key files to study:
- [`server/storage/mvcc/kvstore.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/storage/mvcc/kvstore.go) - MVCC implementation
- [`server/storage/backend/backend.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/storage/backend/backend.go) - BoltDB wrapper
- [`server/storage/wal/wal.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/storage/wal/wal.go) - Write-ahead log
- [`server/storage/mvcc/watchable_store.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/storage/mvcc/watchable_store.go) - Watch integration

---

*Next: [Part 3 - Patterns and Practices](03-patterns-practices.md)*

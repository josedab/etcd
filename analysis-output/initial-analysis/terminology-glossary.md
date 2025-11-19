# etcd Terminology Glossary

**Commit:** `d6bc3229d81384aed0fc03a3ddd3dccfef092048`

---

## Core Concepts

### Revision
A 64-bit integer representing the global logical clock in etcd. It's a monotonically increasing counter that increments on every write operation (put, delete, transaction).

- **Main Revision**: Global sequence number
- **Sub Revision**: For operations within the same transaction

**Usage:** `client.Get(ctx, "key", clientv3.WithRev(revision))`

### ModRevision
The revision when a key was last modified. Used to detect changes and implement optimistic locking.

### CreateRevision
The revision when a key was first created. Remains constant through modifications until the key is deleted and recreated.

### Version
The number of modifications to a specific key since it was created. Starts at 1 when created, increments on each update.

---

## Storage Terms

### MVCC (Multi-Version Concurrency Control)
The storage model that maintains multiple versions of each key. Enables:
- Historical queries by revision
- Watch events without blocking writes
- Consistent point-in-time reads

**Location:** `/home/user/etcd/server/storage/mvcc/`

### WAL (Write-Ahead Log)
A sequential log where all operations are written before being applied to the key-value store. Provides durability and crash recovery.

**Location:** `/home/user/etcd/server/storage/wal/`

### Backend
The persistent storage layer using BoltDB (BBolt). Stores key-value data in a B+tree structure.

**Location:** `/home/user/etcd/server/storage/backend/`

### Compaction
The process of removing old key revisions to reclaim disk space. Only the latest revision of each key is kept after compaction.

**Types:**
- Periodic (time-based)
- Revision (explicit revision number)

### Snapshot
A point-in-time copy of the entire etcd state. Used for:
- Cluster recovery
- New member bootstrap
- Backup

---

## Consensus Terms

### Raft
The distributed consensus algorithm etcd uses to maintain consistency across cluster nodes. Ensures all nodes agree on the same state.

### Leader
The single node that handles all write operations and coordinates log replication to followers.

### Follower
A node that replicates entries from the leader. Can serve linearizable reads (after leader confirmation) or serializable reads.

### Candidate
A node that's running for leader election. Occurs when the current leader fails or during initial cluster startup.

### Term
A monotonically increasing number representing a leader's reign. Incremented at each election.

### Log Entry
A single operation (put, delete) that's replicated through Raft. Contains term number and index.

### Commit Index
The highest log entry known to be replicated on a majority of servers.

### Apply Index
The highest log entry that has been applied to the state machine.

---

## Cluster Terms

### Member
A single etcd server instance in a cluster. Each member has:
- **ID**: Unique identifier
- **Name**: Human-readable name
- **Peer URLs**: For Raft communication
- **Client URLs**: For client connections

### Peer
Another member in the cluster. Members communicate with peers via Raft.

### Quorum
The majority of cluster members needed to make progress. For n members, quorum = (n/2) + 1.

| Cluster Size | Quorum | Fault Tolerance |
|-------------|--------|-----------------|
| 3 | 2 | 1 failure |
| 5 | 3 | 2 failures |
| 7 | 4 | 3 failures |

### Learner
A non-voting member that receives log replication but doesn't participate in quorum. Used to add members safely without risking availability.

---

## Client Terms

### Lease
A time-to-live (TTL) mechanism for keys. When a lease expires, all associated keys are automatically deleted.

```go
lease, _ := client.Grant(ctx, 60)  // 60 second TTL
client.Put(ctx, "key", "value", clientv3.WithLease(lease.ID))
```

### Keep-Alive
Periodic renewal of a lease to prevent expiration.

### Watch
A subscription to key changes. Returns a stream of events when keys matching a pattern are modified.

```go
watchChan := client.Watch(ctx, "key")
for event := range watchChan {
    // Handle event
}
```

### WatchEvent
A notification about a key change:
- **PUT**: Key was created or updated
- **DELETE**: Key was deleted

### Prefix
A key pattern matching all keys starting with a given string.

```go
client.Get(ctx, "prefix/", clientv3.WithPrefix())
```

### Range
A query over a set of keys, specified by start key and optional end key.

---

## Transaction Terms

### Txn (Transaction)
An atomic operation that can conditionally execute multiple operations.

Structure:
- **If**: Conditions to check
- **Then**: Operations if conditions are true
- **Else**: Operations if conditions are false

```go
client.Txn(ctx).
    If(clientv3.Compare(clientv3.Value("key"), "=", "value")).
    Then(clientv3.OpPut("key2", "value2")).
    Else(clientv3.OpGet("key"))
```

### Compare
A condition in a transaction comparing a key's:
- Value
- Create revision
- Mod revision
- Version

### Op (Operation)
A single operation in a transaction: Get, Put, Delete, or Txn.

---

## API Terms

### Linearizable Read
A read that's guaranteed to return the most recent committed write. Requires leader confirmation.

### Serializable Read
A read that may return stale data but is faster (no leader round-trip). Useful for read-heavy workloads where slight staleness is acceptable.

### RangeRequest
A request to read one or more keys, optionally with:
- Revision (point-in-time read)
- Limit (max results)
- Sort order
- Keys only (exclude values)

### CompactionRequest
A request to remove key history before a specific revision.

---

## Observability Terms

### Alarm
A system alert indicating a problem:
- **NOSPACE**: Storage quota exceeded
- **CORRUPT**: Data corruption detected

### Defragment
Reclaiming disk space by rewriting the BoltDB file. Should be done during maintenance windows.

### Hash
A consistency check comparing the hash of all keys across cluster members.

---

## Network Terms

### Client URL
The endpoint for client connections (default: `http://localhost:2379`).

### Peer URL
The endpoint for Raft peer communication (default: `http://localhost:2380`).

### Advertise URL
The externally-accessible URL advertised to other members or clients.

### gRPC Proxy
A proxy that can load balance client requests across etcd members.

---

## Security Terms

### RBAC (Role-Based Access Control)
Permission system where:
- **Users** are assigned to **Roles**
- **Roles** have **Permissions** on key ranges

### Auth Token
A JWT or simple token for authenticated requests.

### TLS
Transport Layer Security for encrypted connections:
- **Client TLS**: Between clients and servers
- **Peer TLS**: Between cluster members
- **Mutual TLS**: Both sides present certificates

---

## Configuration Terms

### Discovery
Methods for cluster formation:
- **Static**: Explicit member list
- **etcd Discovery**: Using existing etcd cluster
- **DNS Discovery**: Using SRV records

### Initial Cluster State
- **new**: Fresh cluster formation
- **existing**: Joining existing cluster

### Data Dir
Directory storing etcd data (WAL, snapshots, backend database).

### Quota
Maximum database size allowed. Operations are rejected when quota is exceeded.

---

## Internal Terms

### Apply
The process of executing committed Raft log entries on the state machine.

### Propose
Submitting an operation to the Raft leader for consensus.

### Index
An in-memory B-tree mapping keys to their revisions in the backend.

### Key Index
Per-key history tracking all revisions of a specific key.

### Batch Transaction
Accumulated write operations committed together to improve performance.

---

## Abbreviations

| Abbreviation | Full Form |
|-------------|-----------|
| KV | Key-Value |
| TTL | Time-To-Live |
| WAL | Write-Ahead Log |
| MVCC | Multi-Version Concurrency Control |
| RBAC | Role-Based Access Control |
| TLS | Transport Layer Security |
| gRPC | Google Remote Procedure Call |
| API | Application Programming Interface |
| CLI | Command-Line Interface |
| E2E | End-to-End |

---

## See Also

- [etcd documentation](https://etcd.io/docs)
- [Raft paper](https://raft.github.io/raft.pdf)
- [Protocol buffer definitions](/home/user/etcd/api/)

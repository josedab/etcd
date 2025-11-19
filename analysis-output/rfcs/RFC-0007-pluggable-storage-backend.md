# RFC-0007: Pluggable Storage Backend Interface

**Status:** Draft
**Author:** Claude Code Analysis
**Created:** November 19, 2025
**Category:** Long-term

---

## Summary

Define a clean interface for storage backends to enable alternative implementations beyond BoltDB, allowing optimization for specific workloads while maintaining etcd's consistency guarantees.

---

## Motivation

### Problem

BoltDB is etcd's only storage backend:

- **Write-optimized**: Poor for write-heavy workloads
- **Single file**: Scalability limits
- **Memory-mapped**: Large datasets stress memory
- **Defrag requirement**: Operational overhead

### Use Cases for Alternatives

1. **Write-heavy**: LSM-tree backends (RocksDB, Pebble)
2. **Large datasets**: Sharded storage
3. **Testing**: In-memory backend for speed
4. **Specialized**: Time-series optimizations

### Expected Benefits

- Better performance for specific workloads
- Reduced operational overhead
- Innovation without forking etcd
- Cleaner architecture (explicit interface)

---

## Detailed Design

### Backend Interface

Define clean abstraction:

```go
// server/storage/backend/interface.go
type Backend interface {
    // Transactions
    ReadTx() ReadTx
    BatchTx() BatchTx
    ConcurrentReadTx() ReadTx

    // Operations
    ForceCommit()
    Close() error

    // Maintenance
    Snapshot() Snapshot
    Defrag() error

    // Metrics
    Size() int64
    SizeInUse() int64
    OpenReadTxN() int64

    // Integrity
    Hash(ignores func(bucketName, keyName []byte) bool) (uint32, error)
    HashByRev(rev int64) (uint32, error)
}

type ReadTx interface {
    Lock()
    Unlock()
    RLock()
    RUnlock()

    UnsafeRange(bucket Bucket, key, endKey []byte, limit int64) ([][]byte, [][]byte)
    UnsafeForEach(bucket Bucket, visitor func(k, v []byte) error) error
}

type BatchTx interface {
    ReadTx
    UnsafeSeqPut(bucket Bucket, key []byte, value []byte)
    UnsafePut(bucket Bucket, key []byte, value []byte)
    UnsafeDelete(bucket Bucket, key []byte)
    Commit()
    CommitAndStop()
}

type Snapshot interface {
    Size() int64
    WriteTo(w io.Writer) (int64, error)
    Close() error
}
```

### Backend Factory

Register backends at startup:

```go
// server/storage/backend/factory.go
type BackendFactory func(cfg BackendConfig) (Backend, error)

var factories = map[string]BackendFactory{
    "bolt":   NewBoltBackend,
    "memory": NewMemoryBackend,
}

func Register(name string, factory BackendFactory) {
    factories[name] = factory
}

func Create(name string, cfg BackendConfig) (Backend, error) {
    factory, ok := factories[name]
    if !ok {
        return nil, fmt.Errorf("unknown backend: %s", name)
    }
    return factory(cfg)
}
```

### Configuration

Add backend selection:

```go
// server/config/config.go
type ServerConfig struct {
    // ...
    BackendType string
    BackendConfig map[string]string
}
```

Command-line:

```bash
etcd --backend-type=bolt --backend-config="path=/data/etcd.db"
etcd --backend-type=pebble --backend-config="path=/data,cache=1GB"
```

### BoltDB Implementation

Refactor existing code:

```go
// server/storage/backend/bolt.go
type boltBackend struct {
    db *bolt.DB
    // ... existing fields
}

func NewBoltBackend(cfg BackendConfig) (Backend, error) {
    db, err := bolt.Open(cfg.Path, 0600, &bolt.Options{
        Timeout:      cfg.Timeout,
        FreelistType: bolt.FreelistMapType,
    })
    if err != nil {
        return nil, err
    }
    return &boltBackend{db: db}, nil
}
```

### Memory Backend (for testing)

```go
// server/storage/backend/memory.go
type memoryBackend struct {
    mu      sync.RWMutex
    buckets map[string]map[string][]byte
}

func NewMemoryBackend(cfg BackendConfig) (Backend, error) {
    return &memoryBackend{
        buckets: make(map[string]map[string][]byte),
    }, nil
}

func (m *memoryBackend) ReadTx() ReadTx {
    return &memoryReadTx{b: m}
}
```

### Pebble Backend (example)

```go
// contrib/pebble-backend/backend.go
type pebbleBackend struct {
    db *pebble.DB
}

func NewPebbleBackend(cfg backend.BackendConfig) (backend.Backend, error) {
    db, err := pebble.Open(cfg.Path, &pebble.Options{
        Cache: pebble.NewCache(cfg.CacheSize),
    })
    return &pebbleBackend{db: db}, err
}
```

---

## Implementation Plan

### Phase 1: Define Interface (Weeks 1-2)
- [ ] Extract interface from current BoltDB code
- [ ] Document interface contract
- [ ] Create interface tests

### Phase 2: Refactor BoltDB (Weeks 3-4)
- [ ] Move current code to bolt.go
- [ ] Implement interface
- [ ] Verify no regressions

### Phase 3: Factory and Config (Weeks 5-6)
- [ ] Implement factory pattern
- [ ] Add configuration support
- [ ] Update startup code

### Phase 4: Memory Backend (Weeks 7-8)
- [ ] Implement in-memory backend
- [ ] Use in tests
- [ ] Benchmark speedup

### Phase 5: Documentation (Weeks 9-10)
- [ ] Interface documentation
- [ ] Contributor guide for new backends
- [ ] Performance guidelines

### Future Phases (separate RFCs)
- Pebble backend implementation
- Tiered storage
- Cloud-native backends

---

## Backwards Compatibility

**Fully backward compatible:**

- Default backend remains BoltDB
- Existing configurations work unchanged
- Data format unchanged (backend-specific)

### Data Migration

Each backend has its own format. Migration requires:

```bash
# Export from bolt
etcdutl snapshot save snap.db

# Import to new backend
etcd --backend-type=pebble migrate --from=snap.db
```

---

## Alternatives Considered

### Alternative 1: RocksDB as default

**Pros:**
- Better write performance
- Proven at scale

**Cons:**
- CGo dependency
- More complex operations

**Decision:** Keep BoltDB default, allow alternatives.

### Alternative 2: Abstract at MVCC level

**Pros:**
- Higher-level abstraction

**Cons:**
- MVCC is etcd-specific logic
- Backend should be lower level

**Decision:** Backend level is correct abstraction.

---

## Open Questions

1. **Interface stability**: How to version the interface?
   - Proposed: Semantic versioning, deprecation warnings

2. **Bucket operations**: Should interface include bucket creation?
   - Proposed: Yes, backends need to manage schema

3. **Plugin loading**: Dynamic loading or compile-time?
   - Proposed: Compile-time initially, plugins later

---

## Success Criteria

- [ ] Interface defined and documented
- [ ] BoltDB refactored to implement interface
- [ ] Memory backend for fast tests
- [ ] No performance regression for BoltDB
- [ ] One external backend contributed

---

## Effort Estimation

**Total: 8-12 weeks**

| Phase | Effort |
|-------|--------|
| Interface design | 2 weeks |
| BoltDB refactor | 2 weeks |
| Factory/config | 2 weeks |
| Memory backend | 2 weeks |
| Documentation | 2 weeks |
| Community feedback | 2 weeks |

---

## Stakeholder Approvals

- [ ] etcd maintainers
- [ ] Storage SIG
- [ ] Community review

---

## Rollback Strategy

1. Each phase is independent
2. Interface can coexist with direct BoltDB usage
3. Feature flag to disable alternative backends

---

## References

- [CockroachDB storage interface](https://github.com/cockroachdb/cockroach/tree/master/pkg/storage)
- [Pebble - Go LSM database](https://github.com/cockroachdb/pebble)
- Current backend: [`server/storage/backend/`](https://github.com/etcd-io/etcd/tree/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/storage/backend)

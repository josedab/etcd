# RFC-0009: In-Memory Key Index Redesign for Scalability

**Status:** Draft
**Author:** Claude Code Analysis
**Created:** November 19, 2025
**Category:** Long-term (P0 - Critical)

---

## Summary

Redesign the in-memory key index to support millions of keys by implementing a hybrid approach with hot keys in memory and cold keys on disk, enabling etcd to scale beyond current memory limitations.

---

## Motivation

### Problem

etcd's MVCC implementation stores **all key metadata in RAM** using a B-tree structure. This creates a hard scalability limit based on available memory.

**Current Architecture:**
```go
// From server/storage/mvcc/index.go
type treeIndex struct {
    sync.RWMutex
    tree *btree.BTreeG[*keyIndex]  // ALL keys in memory
    lg   *zap.Logger
}

// From server/storage/mvcc/key_index.go
type keyIndex struct {
    key         []byte              // Key stored in RAM
    modified    Revision            // In RAM
    generations []generation        // ALL generations in RAM
}
```

### Current Limitations

**Memory Usage:**
- ~100 bytes per key for index entry
- 1 million keys ≈ 100 MB (just index)
- 10 million keys ≈ 1 GB (just index)
- Plus value data in BoltDB
- Plus watch state, Raft buffers, etc.

**Real-World Impact:**
- Kubernetes clusters: 100K-500K keys typical
- Large deployments: OOM kills at 1M+ keys
- Multi-tenant scenarios: Cannot isolate by keyspace
- GC pressure: Large heap → long GC pauses

### Evidence from Codebase

```go
// From server/storage/mvcc/key_index.go:30-40
type keyIndex struct {
    key         []byte
    modified    revision  // Latest modification
    generations []generation
}

type generation struct {
    ver     int64
    created revision
    revs    []revision  // All revisions for this generation
}
```

No pagination, no eviction, no memory budgeting exists.

### Expected Benefits

- **10× scalability**: Support 10M+ keys in 8GB RAM
- **Predictable memory**: Memory usage bounded by budget
- **Better multi-tenancy**: Isolate memory per keyspace
- **Reduced GC pressure**: Smaller heap → faster GC

---

## Detailed Design

### 1. Hybrid Index Architecture

```
┌─────────────────────────────────────────┐
│         Hot Key Cache (In-Memory)       │
│         LRU, 500MB budget               │
│         - Recently accessed             │
│         - Frequently accessed           │
│         - Active watch keys             │
└────────────┬────────────────────────────┘
             │
             ▼
┌─────────────────────────────────────────┐
│       Bloom Filter (In-Memory)          │
│       - Negative lookup optimization    │
│       - 10MB for 10M keys               │
└────────────┬────────────────────────────┘
             │
             ▼
┌─────────────────────────────────────────┐
│    Disk-Based Index (BoltDB Bucket)     │
│    - All key metadata                   │
│    - Paged access                       │
│    - Compressed                         │
└─────────────────────────────────────────┘
```

### 2. Index Entry Structure

**In-Memory (Hot):**
```go
type hotKeyIndex struct {
    key         []byte
    modified    revision
    latestGen   *generation  // Only latest generation in memory
    diskRef     uint64       // Pointer to full history on disk
    accessTime  time.Time    // For LRU eviction
    accessCount uint32       // For frequency tracking
}
```

**On-Disk (Cold):**
```go
type diskKeyIndex struct {
    key         []byte
    generations []generation  // Full history
    metadata    indexMetadata
}

type indexMetadata struct {
    created     time.Time
    accessed    time.Time
    watchCount  uint32
    size        uint32
}
```

### 3. Access Patterns

**Read (Get/Range):**
```go
func (ti *tieredIndex) Get(key []byte, rev int64) (*keyIndex, error) {
    // 1. Check hot cache
    if ki := ti.hotCache.Get(key); ki != nil {
        ti.recordHit(key)
        return ki, nil
    }

    // 2. Check bloom filter (negative lookup)
    if !ti.bloom.MayContain(key) {
        return nil, ErrKeyNotFound
    }

    // 3. Load from disk
    diskIndex, err := ti.loadFromDisk(key)
    if err != nil {
        return nil, err
    }

    // 4. Promote to hot cache
    ti.hotCache.Add(key, diskIndex.toHotIndex())

    return diskIndex, nil
}
```

**Write (Put/Delete):**
```go
func (ti *tieredIndex) Put(key []byte, rev revision) {
    // 1. Update hot cache if present
    if ki := ti.hotCache.Get(key); ki != nil {
        ki.modified = rev
        ki.latestGen.revs = append(ki.latestGen.revs, rev)
    }

    // 2. Update disk index (async batched)
    ti.diskQueue.Add(keyUpdate{key: key, rev: rev})

    // 3. Update bloom filter
    ti.bloom.Add(key)
}
```

### 4. Cache Management

**LRU Eviction:**
```go
type hotCache struct {
    mu          sync.RWMutex
    cache       map[string]*hotKeyIndex
    lru         *lruList
    maxSize     int64
    currentSize int64
}

func (hc *hotCache) evict() {
    for hc.currentSize > hc.maxSize {
        victim := hc.lru.removeTail()
        delete(hc.cache, string(victim.key))
        hc.currentSize -= victim.memSize()
    }
}
```

**Promotion Criteria:**
```go
func (ti *tieredIndex) shouldPromote(key []byte, meta *indexMetadata) bool {
    // Promote if:
    // 1. High access frequency
    if meta.accessCount > ti.promotionThreshold {
        return true
    }

    // 2. Active watches
    if meta.watchCount > 0 {
        return true
    }

    // 3. Recently accessed
    if time.Since(meta.accessed) < ti.recentWindow {
        return true
    }

    return false
}
```

### 5. Bloom Filter

```go
type bloomFilter struct {
    bits   []byte
    hashes int
    size   int
}

// 10M keys, 1% false positive rate = ~10MB
func newBloomFilter(expectedKeys int) *bloomFilter {
    size := optimalSize(expectedKeys, 0.01)  // 1% FP rate
    hashes := optimalHashes(size, expectedKeys)

    return &bloomFilter{
        bits:   make([]byte, size/8),
        hashes: hashes,
        size:   size,
    }
}
```

### 6. Disk Index Organization

```go
// Use dedicated BoltDB bucket
var indexBucket = []byte("__index__")

// Key format: key -> protobuf(diskKeyIndex)
func (ti *tieredIndex) loadFromDisk(key []byte) (*diskKeyIndex, error) {
    var idx diskKeyIndex

    err := ti.backend.BatchTx().UnsafeRange(
        indexBucket,
        key,
        nil,
        1,
    )

    if err != nil {
        return nil, err
    }

    // Deserialize
    return &idx, proto.Unmarshal(data, &idx)
}
```

### 7. Configuration

```go
type TieredIndexConfig struct {
    // Hot cache size in bytes
    HotCacheSize int64  // Default: 500MB

    // Promotion threshold (accesses)
    PromotionThreshold uint32  // Default: 10

    // Recent access window
    RecentWindow time.Duration  // Default: 5 minutes

    // Bloom filter parameters
    BloomFPRate float64  // Default: 0.01 (1%)
    BloomSize   int      // Auto-calculated

    // Disk write batching
    DiskBatchSize     int           // Default: 1000
    DiskBatchInterval time.Duration // Default: 100ms
}
```

---

## Implementation Plan

### Phase 1: Foundation (Weeks 1-3)
- [ ] Design index bucket schema
- [ ] Implement bloom filter
- [ ] Create hot cache with LRU
- [ ] Unit tests

### Phase 2: Integration (Weeks 4-6)
- [ ] Integrate with existing MVCC store
- [ ] Implement tiered lookup
- [ ] Add promotion/demotion logic
- [ ] Migration from old index

### Phase 3: Optimization (Weeks 7-9)
- [ ] Tune cache hit rates
- [ ] Optimize disk access patterns
- [ ] Add background compaction
- [ ] Performance benchmarks

### Phase 4: Production (Weeks 10-12)
- [ ] Load testing (10M keys)
- [ ] Memory profiling
- [ ] Failure mode testing
- [ ] Documentation

---

## Backwards Compatibility

**Migration Strategy:**

```go
func migrateToTieredIndex(oldIndex *treeIndex, config TieredIndexConfig) (*tieredIndex, error) {
    newIndex := newTieredIndex(config)

    // 1. Build bloom filter from all keys
    oldIndex.tree.Ascend(func(ki *keyIndex) bool {
        newIndex.bloom.Add(ki.key)
        return true
    })

    // 2. Promote hot keys (recent, watched)
    oldIndex.tree.Ascend(func(ki *keyIndex) bool {
        if shouldPromoteOnMigration(ki) {
            newIndex.hotCache.Add(ki.key, ki.toHotIndex())
        }
        return true
    })

    // 3. Write cold keys to disk
    batch := newIndex.backend.BatchTx()
    oldIndex.tree.Ascend(func(ki *keyIndex) bool {
        if !newIndex.hotCache.Contains(ki.key) {
            diskIndex := ki.toDiskIndex()
            batch.UnsafePut(indexBucket, ki.key, diskIndex.marshal())
        }
        return true
    })
    batch.Commit()

    return newIndex, nil
}
```

**Compatibility:**
- Reads work immediately after migration
- Writes update both structures during transition
- Rollback: rebuild in-memory from disk

---

## Performance Analysis

### Memory Savings

**Before:**
```
1M keys × 100 bytes = 100 MB (all in RAM)
10M keys × 100 bytes = 1 GB (all in RAM)
```

**After (with 500MB cache):**
```
Hot: 500 MB cache (top 5M keys)
Bloom: 10 MB (10M keys, 1% FP)
Disk: BoltDB (compressed)
Total RAM: 510 MB vs 1 GB (49% savings)
```

### Access Patterns

**Typical Kubernetes Workload:**
- 90% of accesses hit 10% of keys (hot keys)
- Watch-heavy on service/pod keys
- Occasional full scans (list operations)

**Expected Performance:**
- Cache hit rate: >90% (promoted watches)
- Bloom filter savings: 99% on non-existent keys
- Disk access: <10% of operations
- Latency impact: +1-2ms for cache miss

---

## Alternatives Considered

### Alternative 1: Pagination Only

**Approach:** Keep all keys but paginate access.

**Pros:**
- Simpler implementation
- No cache complexity

**Cons:**
- Still limited by total key count
- Doesn't solve memory problem

**Decision:** Insufficient for 10M+ keys.

### Alternative 2: External Index (Redis, etc.)

**Approach:** Store index in separate system.

**Pros:**
- Unlimited scalability

**Cons:**
- Adds dependency
- Network overhead
- Consistency complexity

**Decision:** Too complex, contradicts etcd philosophy.

### Alternative 3: Shard Keys by Range

**Approach:** Multiple etcd clusters, route by key prefix.

**Pros:**
- Linear scalability

**Cons:**
- Operational complexity
- Cross-shard transactions impossible
- Client-side sharding logic

**Decision:** Better as user-level pattern, not core feature.

---

## Open Questions

1. **Cache size tuning:** How to auto-tune based on workload?
   - Proposed: Adaptive sizing based on hit rate
   - Start with 500MB default, adjust by 10% if hit rate <80%

2. **Bloom filter rebuild:** When to rebuild after many deletes?
   - Proposed: Rebuild when false positive rate >5%
   - Or every compaction cycle

3. **Disk index compaction:** When to compact disk index?
   - Proposed: Piggyback on MVCC compaction
   - Remove deleted keys, compress generations

---

## Success Criteria

- [ ] Support 10M keys in 8GB RAM
- [ ] Memory usage <1GB for index
- [ ] Cache hit rate >90% for typical workloads
- [ ] Latency increase <5ms at P99 for cache misses
- [ ] No regression for <100K key deployments

---

## Effort Estimation

**Total: 10-12 weeks**

| Phase | Effort |
|-------|--------|
| Design and prototyping | 2 weeks |
| Core implementation | 3 weeks |
| Integration and migration | 3 weeks |
| Testing and optimization | 2 weeks |
| Documentation | 1 week |
| Buffer | 1 week |

**Team:** 2 senior engineers

---

## Stakeholder Approvals

- [ ] etcd maintainers
- [ ] Storage SIG
- [ ] Large deployment operators (Kubernetes, etc.)
- [ ] Community RFC review

---

## Rollback Strategy

1. **Feature flag:** `--experimental-tiered-index`
2. **Gradual rollout:**
   - Alpha: opt-in testing
   - Beta: default with rollback option
   - GA: remove old implementation
3. **Migration:**
   - Export to snapshot
   - Restore with new index
   - Verify consistency

---

## References

- [RocksDB Tiered Storage](https://github.com/facebook/rocksdb/wiki/RocksDB-Bloom-Filter)
- [LevelDB Table Cache](https://github.com/google/leveldb/blob/main/doc/impl.md)
- [Bloom Filter Calculator](https://hur.st/bloomfilter/)
- Current implementation: [`server/storage/mvcc/index.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/storage/mvcc/index.go)

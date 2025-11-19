# Performance Analysis and Optimization Opportunities

*Part 5 of the etcd Deep Dive Series*

**Analysis Commit:** [`d6bc3229d81384aed0fc03a3ddd3dccfef092048`](https://github.com/etcd-io/etcd/tree/d6bc3229d81384aed0fc03a3ddd3dccfef092048)

---

## What You'll Learn

- etcd's performance characteristics
- Key bottlenecks and their causes
- Tuning parameters for your workload
- Compaction and defragmentation strategies
- Scaling considerations
- Future optimization opportunities

---

## Introduction

etcd prioritizes consistency over raw performance. But that doesn't mean we can't make it fast. Understanding the performance characteristics helps you deploy etcd optimally and identify when something's wrong.

Let's explore how etcd performs and where we can tune it.

---

## Performance Characteristics

### Typical Benchmarks

For a 3-node cluster on modern hardware:

| Operation | Throughput | P99 Latency |
|-----------|------------|-------------|
| Put (256B value) | ~10K ops/sec | ~50ms |
| Get (256B value) | ~20K ops/sec | ~10ms |
| Sequential Get | ~50K ops/sec | ~5ms |
| Watch events | ~100K events/sec | - |

These numbers vary significantly based on:
- Hardware (disk IOPS, network latency)
- Data size (key length, value size)
- Cluster size (3 vs 5 nodes)
- Request patterns (sequential vs random)

### Why Writes Are Slower

Writes go through:
1. **gRPC deserialization** (~0.1ms)
2. **Raft proposal** (~0.1ms)
3. **WAL fsync** (~1-5ms)  ← Primary bottleneck
4. **Replication** (network RTT)
5. **Commit wait** (~1-5ms)
6. **Apply** (~0.1ms)
7. **BoltDB commit** (~1-5ms)

The **fsync** calls are the dominant factor. Each write must be durably stored before acknowledgment.

### Why Reads Are Faster

Linearizable reads need:
1. Leader confirmation (network RTT)
2. Local read from MVCC store

Serializable reads bypass leader confirmation entirely.

---

## Identifying Bottlenecks

### Disk I/O

The most common bottleneck. Check with:

```bash
# WAL fsync duration
etcd_disk_wal_fsync_duration_seconds

# Backend commit duration
etcd_disk_backend_commit_duration_seconds
```

**Symptoms:**
- High fsync latency (>10ms)
- Proposal queue backing up
- Leader timeouts

**Solutions:**
- Use SSD (preferably NVMe)
- Separate WAL and data directories
- Tune batch interval

### Network

Check peer communication:

```bash
# Round-trip time to peers
etcd_network_peer_round_trip_time_seconds

# Bytes sent/received
etcd_network_peer_sent_bytes_total
```

**Symptoms:**
- High RTT (>10ms within datacenter)
- Frequent leader elections
- Heartbeat timeouts

**Solutions:**
- Co-locate cluster nodes
- Check network configuration
- Adjust heartbeat interval

### Memory

```bash
# Go memory stats
go_memstats_alloc_bytes
go_memstats_heap_inuse_bytes
```

**Symptoms:**
- High GC pause times
- OOM kills
- Slow compaction

**Solutions:**
- Increase memory
- Reduce key count
- More aggressive compaction

---

## Tuning Parameters

### Heartbeat and Election Timeouts

```bash
etcd --heartbeat-interval=100 --election-timeout=1000
```

- **Heartbeat interval**: How often leader sends heartbeats (default: 100ms)
- **Election timeout**: Time before follower starts election (default: 1000ms)

**Rules:**
- Election timeout ≥ 10 × heartbeat interval
- For low-latency networks: heartbeat=50ms, election=500ms
- For WAN: heartbeat=500ms, election=5000ms

### Batch Size

From [`server/storage/backend/backend.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/storage/backend/backend.go):

```go
defaultBatchLimit    = 10000
defaultBatchInterval = 100 * time.Millisecond
```

Writes are batched for efficiency:
- Higher interval = better throughput, worse latency
- Lower interval = better latency, worse throughput

Configure via:
```bash
etcd --backend-batch-interval=50ms --backend-batch-limit=5000
```

### Snapshot Count

```bash
etcd --snapshot-count=10000
```

How many entries before taking a snapshot. Lower values:
- Faster recovery (less WAL to replay)
- More frequent snapshots (disk I/O)

### Quota

```bash
etcd --quota-backend-bytes=8589934592  # 8GB
```

Maximum database size. When exceeded, etcd rejects writes. Default is 2GB.

---

## Compaction Strategies

### Why Compaction Matters

Without compaction:
- Disk usage grows unbounded
- Key index consumes more memory
- Scans become slower

### Automatic Compaction

**Periodic (time-based):**
```bash
etcd --auto-compaction-mode=periodic --auto-compaction-retention=1h
```
Compacts revisions older than 1 hour.

**Revision-based:**
```bash
etcd --auto-compaction-mode=revision --auto-compaction-retention=10000
```
Keeps only 10,000 revisions.

### Manual Compaction

```bash
# Get current revision
rev=$(etcdctl endpoint status --write-out=json | jq '.[0].Status.header.revision')

# Compact
etcdctl compact $rev
```

### Defragmentation

Compaction marks space as free but doesn't return it. Defragmentation reclaims it:

```bash
etcdctl defrag --endpoints=http://localhost:2379
```

**Warning:** Defrag blocks the member briefly. Run during maintenance windows.

### Best Practice

1. Enable automatic compaction (periodic 1h or revision 10000)
2. Schedule regular defrag (weekly during low traffic)
3. Monitor `etcd_mvcc_db_total_size_in_bytes` vs `etcd_mvcc_db_total_size_in_use_in_bytes`

---

## Watch System Performance

Watches can handle high event volumes but have considerations.

### How Watches Work

From [`server/storage/mvcc/watchable_store.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/storage/mvcc/watchable_store.go):

```go
type watchableStore struct {
    *store

    mu sync.RWMutex

    unsynced watcherGroup  // Watchers catching up
    synced   watcherGroup  // Up-to-date watchers
}
```

Watchers are categorized:
- **Synced**: Caught up, waiting for new events
- **Unsynced**: Behind, need to catch up from history

### Event Batching

Events are sent in batches:

```go
chanBufLen = 128  // Buffer per watch channel
```

If a watcher can't keep up, it becomes a "victim" and events are dropped (requiring resync).

### Optimization Opportunities

1. **Watch batching**: Current implementation sends events as they occur. Batching could reduce overhead.

2. **Watch cache**: The new `cache/` module improves watch performance for common patterns.

3. **Bloom filters**: Could reduce unnecessary event delivery.

---

## Scaling Considerations

### Vertical Scaling

etcd benefits from:
- **Fast disk**: NVMe SSD is ideal
- **Low-latency network**: <1ms RTT between nodes
- **More memory**: For larger key indexes

### Horizontal Scaling (Reads)

Add learners (non-voting members) for read scaling:

```bash
etcdctl member add learner-1 --learner --peer-urls=http://...
```

Learners:
- Receive all data
- Can serve serializable reads
- Don't participate in quorum

### Limitations

etcd has inherent scaling limits:
- **Single leader writes**: All writes go through leader
- **Index in memory**: Key count limited by RAM
- **BoltDB single-file**: One B+tree for all data

For larger datasets, consider:
- Sharding (multiple etcd clusters)
- Different storage (if consistency requirements allow)

---

## Benchmark Tools

### Built-in Benchmark

```bash
# Build benchmark tool
make tools

# Put benchmark
./tools/benchmark/benchmark put \
    --endpoints=localhost:2379 \
    --clients=100 \
    --conns=100 \
    --total=100000

# Range benchmark
./tools/benchmark/benchmark range key \
    --endpoints=localhost:2379 \
    --clients=100 \
    --total=100000
```

### Prometheus Metrics

Key metrics to monitor:

```promql
# Write latency
histogram_quantile(0.99,
  rate(etcd_disk_wal_fsync_duration_seconds_bucket[5m]))

# Read latency
histogram_quantile(0.99,
  rate(grpc_server_handling_seconds_bucket{grpc_method="Range"}[5m]))

# Proposals per second
rate(etcd_server_proposals_committed_total[5m])

# Active watches
etcd_debugging_mvcc_slow_watcher_total
```

---

## Performance Anti-Patterns

### 1. Large Values

```go
// BAD: 1MB value
cli.Put(ctx, "key", hugeValue)

// BETTER: Store reference, data elsewhere
cli.Put(ctx, "key", "s3://bucket/key")
```

etcd is optimized for small values (<1MB). Use external storage for large blobs.

### 2. Too Many Keys

Each key needs ~100 bytes in the in-memory index. With 10M keys:
- 1GB just for index
- Slower scans and compaction

### 3. Hot Keys

```go
// BAD: All clients watching same key
cli.Watch(ctx, "leader")

// BETTER: Use range or shard watches
cli.Watch(ctx, fmt.Sprintf("leader/%d", clientID % 10))
```

### 4. Unbounded Watches

```go
// BAD: Watch everything
cli.Watch(ctx, "", clientv3.WithPrefix())

// BETTER: Scope to what you need
cli.Watch(ctx, "/my-service/", clientv3.WithPrefix())
```

### 5. No Timeouts

```go
// BAD
cli.Put(context.Background(), "key", "value")

// GOOD
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
cli.Put(ctx, "key", "value")
```

---

## Future Optimization Opportunities

Based on codebase analysis, here are potential improvements:

### 1. Adaptive Batch Sizing

Currently fixed at 100ms. Could dynamically adjust based on load:
- High load: Larger batches
- Low load: Immediate commits

### 2. Request Hedging

Client could send to multiple endpoints simultaneously:
- First response wins
- Reduces tail latency

### 3. Better Watch Coalescing

Multiple watchers on same key could share event delivery.

### 4. MVCC Index Sharding

The single B-tree index is a bottleneck. Sharding by key prefix could help.

### 5. Pluggable Storage Backend

BoltDB is good but not optimal for all workloads. A pluggable interface would allow:
- LSM-tree for write-heavy
- Memory-only for testing

---

## Production Checklist

### Hardware
- [ ] SSD storage (NVMe preferred)
- [ ] Dedicated disk for WAL
- [ ] Low-latency network between nodes
- [ ] Sufficient memory for key index

### Configuration
- [ ] Appropriate heartbeat/election timeouts
- [ ] Quota set appropriately
- [ ] Automatic compaction enabled
- [ ] Snapshot count tuned

### Monitoring
- [ ] Prometheus metrics collected
- [ ] Alerts for leader changes
- [ ] Alerts for high disk latency
- [ ] Disk space monitoring

### Operations
- [ ] Regular defragmentation scheduled
- [ ] Backup strategy in place
- [ ] Runbook for common issues

---

## Key Takeaways

1. **Disk is the bottleneck** - Fast storage is the single most impactful upgrade.

2. **Writes are inherently slow** - Durability requires fsync. This is a feature, not a bug.

3. **Tune for your workload** - Defaults are safe but not optimal.

4. **Compaction is essential** - Without it, performance degrades over time.

5. **Monitor actively** - etcd exposes excellent metrics; use them.

6. **Know the limits** - etcd isn't a general-purpose database.

---

## Series Conclusion

Over these five posts, we've explored:
1. etcd's architecture and trade-offs
2. The storage layer internals
3. Design patterns that make it maintainable
4. How to integrate and extend it
5. Performance characteristics and tuning

etcd is a beautifully engineered system that makes appropriate trade-offs for its problem domain. It prioritizes correctness and durability, which is exactly what you want for coordination data.

I hope this series helps you understand not just how to use etcd, but why it works the way it does.

---

## Explore the Code

Performance-related files:
- [`tools/benchmark/`](https://github.com/etcd-io/etcd/tree/d6bc3229d81384aed0fc03a3ddd3dccfef092048/tools/benchmark) - Benchmark tool
- [`server/etcdserver/metrics.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/etcdserver/metrics.go) - Server metrics
- [`server/storage/backend/backend.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/storage/backend/backend.go) - Backend batching
- [`server/etcdserver/api/v3compactor/`](https://github.com/etcd-io/etcd/tree/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/etcdserver/api/v3compactor) - Compaction

---

## Thank You

Thanks for reading this series! If you found it valuable, consider:
- Contributing to etcd
- Sharing with colleagues
- Providing feedback

Happy distributed systems engineering!

---

*This concludes the etcd Deep Dive Series.*

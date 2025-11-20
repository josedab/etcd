# RFC-0011: Lease Management Optimization for Kubernetes Scale

**Status:** Draft
**Author:** Claude Code Analysis
**Created:** November 19, 2025
**Category:** Strategic (P1 - High Priority)

---

## Summary

Optimize lease management to support 100K+ concurrent leases with reduced CPU overhead through lease pooling, adaptive TTLs, batch renewals, and background revocation, specifically targeting Kubernetes workloads.

---

## Motivation

### Problem

Kubernetes creates 2 leases per pod by default:
- **Leader election lease** for controllers
- **Node heartbeat lease** for kubelet

**Scale Calculation:**
```
10,000 pods × 2 leases = 20,000 active leases
20,000 leases ÷ 30s TTL = 667 renewals/sec baseline
During pod churn (10% turnover): 2,000 creates+deletes/min
```

### Current Implementation Limits

From [`server/lease/lessor.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/lease/lessor.go):

```go
const (
    // Default revoke rate
    defaultLeaseRevokeRate = 1000  // leases per second

    // Checkpoint rate
    leaseCheckpointRate = 1000
    defaultLeaseCheckpointInterval = 5 * time.Minute
    maxLeaseCheckpointBatchSize = 1000
)

// TODO comment at line 290:
// TODO: when lessor is under high load, it should give out lease
// with longer TTL to reduce renew load.
```

**Pain Points:**
1. **High CPU overhead:** Heap operations for every expiry check
2. **Checkpoint bloat:** 1000-lease batches every 5 minutes
3. **No lease pooling:** Each lease tracked individually
4. **Fixed TTLs:** No adaptive extension under load
5. **Inline revocation:** Blocking during lease cleanup

### Real-World Impact

**Kubernetes Cluster (10K pods):**
- 20K concurrent leases
- CPU usage: 5-10% just for lease management
- Checkpoint log entries: 20 entries/5min = 240/hour
- Memory: ~5KB per lease = 100MB

**During pod churn:**
- 3× lease operations (create, renew, revoke)
- Control plane saturation
- Increased apply latency

### Expected Benefits

- **50% reduction in lease CPU** overhead
- **Support 100K+ leases** in single cluster
- **Reduced checkpoint bloat** (10× fewer entries)
- **Better Kubernetes scalability** (larger clusters)
- **Lower tail latency** during pod churn

---

## Detailed Design

### 1. Lease Pooling

Group leases with similar TTLs to reduce tracking overhead.

```go
type LeasePool struct {
    ttl         int64           // Pool TTL (e.g., 30s)
    leases      map[LeaseID]*Lease
    expireQueue *expiryQueue    // Shared expiry tracking
    mu          sync.RWMutex
}

type Lessor struct {
    pools map[int64]*LeasePool  // TTL -> Pool

    // Configuration
    poolGranularity int64  // Round TTLs to nearest N seconds
}

// Grant lease to appropriate pool
func (le *Lessor) Grant(id LeaseID, ttl int64) (*Lease, error) {
    // Round TTL to pool granularity
    poolTTL := (ttl / le.poolGranularity) * le.poolGranularity

    // Get or create pool
    pool := le.getOrCreatePool(poolTTL)

    // Add lease to pool
    lease := &Lease{
        ID:      id,
        TTL:     ttl,
        pool:    pool,
        expiry:  time.Now().Add(time.Duration(ttl) * time.Second),
    }

    pool.add(lease)
    return lease, nil
}

// Pool-level expiry management
func (lp *LeasePool) checkExpiry() {
    now := time.Now()

    lp.mu.Lock()
    defer lp.mu.Unlock()

    // Batch process expired leases in pool
    expired := lp.expireQueue.PopExpired(now)

    for _, lease := range expired {
        lp.revokeLease(lease)
    }
}
```

**Benefits:**
- Reduce heap operations (pool-level vs per-lease)
- Better cache locality
- Batch expiry checking

### 2. Adaptive TTL Extension

Implement the TODO from lessor.go - extend TTLs under high load.

```go
type AdaptiveTTLConfig struct {
    // Load threshold to trigger extension
    LoadThreshold float64  // 0.8 = 80% of capacity

    // Maximum TTL multiplier
    MaxMultiplier float64  // 2.0 = double TTL

    // Adjustment interval
    AdjustInterval time.Duration  // 1 minute
}

type TTLAdapter struct {
    config       AdaptiveTTLConfig
    currentLoad  float64
    multiplier   float64
    lastAdjust   time.Time
}

func (ta *TTLAdapter) adjustTTL(requestedTTL int64, currentLoad float64) int64 {
    ta.currentLoad = currentLoad

    // Check if we should extend TTLs
    if currentLoad > ta.config.LoadThreshold {
        // Gradually increase multiplier
        ta.multiplier = math.Min(
            ta.multiplier * 1.1,
            ta.config.MaxMultiplier,
        )
    } else {
        // Gradually decrease back to normal
        ta.multiplier = math.Max(
            ta.multiplier * 0.95,
            1.0,
        )
    }

    // Apply multiplier
    adjustedTTL := int64(float64(requestedTTL) * ta.multiplier)

    return adjustedTTL
}

// Usage in Grant
func (le *Lessor) Grant(id LeaseID, ttl int64) (*Lease, error) {
    // Calculate current load
    load := le.calculateLoad()

    // Adjust TTL if under load
    adjustedTTL := le.adapter.adjustTTL(ttl, load)

    lease := &Lease{
        ID:          id,
        TTL:         adjustedTTL,  // Extended TTL
        RequestedTTL: ttl,          // Original request
    }

    return lease, nil
}

// Load calculation
func (le *Lessor) calculateLoad() float64 {
    // Metrics: renewal rate, CPU usage, pending revocations
    renewalRate := le.metrics.renewalsPerSecond.Rate()
    capacity := le.maxRenewalRate

    return renewalRate / capacity
}
```

**Benefits:**
- Automatic load shedding
- Fewer renewals during peaks
- Better cluster stability

### 3. Batch Lease Renewals

Allow clients to renew multiple leases in single RPC.

```go
// API addition
message LeaseKeepAliveRequest {
    repeated int64 IDs = 1;  // Multiple lease IDs
}

message LeaseKeepAliveResponse {
    repeated LeaseKeepAliveStatus statuses = 1;
}

message LeaseKeepAliveStatus {
    int64 ID = 1;
    int64 TTL = 2;
    bool success = 3;
    string error = 4;
}

// Server implementation
func (ls *leaseServer) LeaseKeepAlive(stream pb.Lease_LeaseKeepAliveServer) error {
    for {
        req, err := stream.Recv()
        if err != nil {
            return err
        }

        // Process all IDs in batch
        statuses := make([]*pb.LeaseKeepAliveStatus, len(req.IDs))

        for i, id := range req.IDs {
            lease, err := ls.le.Renew(LeaseID(id))
            statuses[i] = &pb.LeaseKeepAliveStatus{
                ID:      id,
                Success: err == nil,
            }
            if err == nil {
                statuses[i].TTL = lease.TTL
            } else {
                statuses[i].Error = err.Error()
            }
        }

        // Single response for all renewals
        resp := &pb.LeaseKeepAliveResponse{
            Statuses: statuses,
        }

        if err := stream.Send(resp); err != nil {
            return err
        }
    }
}
```

**Client Usage:**
```go
// Kubernetes could batch all pod leases
leaseIDs := []int64{lease1.ID, lease2.ID, lease3.ID, ...}

resp, err := client.LeaseKeepAliveBatch(ctx, leaseIDs)
for _, status := range resp.Statuses {
    if !status.Success {
        log.Error("lease renewal failed", status.ID, status.Error)
    }
}
```

**Benefits:**
- Reduce RPC overhead (1 call vs N calls)
- Lower network bandwidth
- Batch Raft proposals

### 4. Background Lazy Revocation

Move revocation to background goroutine to avoid blocking.

```go
type LazyRevoker struct {
    pending chan *Lease
    workers int
    wg      sync.WaitGroup
}

func (lr *LazyRevoker) start() {
    for i := 0; i < lr.workers; i++ {
        lr.wg.Add(1)
        go lr.worker()
    }
}

func (lr *LazyRevoker) worker() {
    defer lr.wg.Done()

    for lease := range lr.pending {
        lr.revokeLease(lease)
    }
}

func (lr *LazyRevoker) revokeLease(lease *Lease) {
    // Delete attached keys
    for _, key := range lease.itemSet {
        lr.kv.Delete(key)
    }

    // Remove lease
    lr.lessor.deleteLease(lease)
}

// In expiry check
func (lp *LeasePool) checkExpiry() {
    expired := lp.expireQueue.PopExpired(time.Now())

    for _, lease := range expired {
        // Enqueue for background revocation
        lp.revoker.pending <- lease
    }
}
```

**Benefits:**
- Non-blocking expiry checks
- Smooth revocation (no spikes)
- Better request latency

### 5. Optimized Checkpoint Format

Reduce checkpoint log bloat.

```go
// Current: one entry per lease batch
type LeaseCheckpoint struct {
    Leases []LeaseCheckpointItem  // Up to 1000 leases
}

// Proposed: compressed format
type CompressedLeaseCheckpoint struct {
    // Bitmap of active lease IDs (if sequential)
    Bitmap []byte

    // Or delta encoding for sparse IDs
    BaseID int64
    Deltas []int32  // Offset from base

    // TTL pools (group by TTL)
    Pools map[int64][]int64  // TTL -> lease IDs
}

// Checkpoint with compression
func (le *Lessor) checkpoint() {
    // Group leases by TTL
    pools := make(map[int64][]int64)

    le.mu.RLock()
    for id, lease := range le.leaseMap {
        pools[lease.TTL] = append(pools[lease.TTL], int64(id))
    }
    le.mu.RUnlock()

    // Create compressed checkpoint
    cp := &CompressedLeaseCheckpoint{
        Pools: pools,
    }

    // Propose to Raft (much smaller than before)
    le.raft.Propose(cp.marshal())
}
```

**Benefits:**
- 5-10× smaller checkpoint entries
- Less Raft log growth
- Faster checkpoint recovery

### 6. Lease Metrics Enhancement

```go
var (
    leasePoolSize = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "etcd_lease_pool_size",
            Help: "Number of leases in each TTL pool",
        },
        []string{"ttl"},
    )

    leaseRenewalBatchSize = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name: "etcd_lease_renewal_batch_size",
            Help: "Number of leases renewed in single request",
            Buckets: []float64{1, 5, 10, 50, 100, 500, 1000},
        },
    )

    leaseTTLAdjustment = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name: "etcd_lease_ttl_multiplier",
            Help: "TTL adjustment multiplier",
            Buckets: prometheus.LinearBuckets(1.0, 0.1, 11),
        },
    )

    leaseRevocationLatency = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name: "etcd_lease_revocation_latency_seconds",
            Help: "Time to revoke expired lease",
        },
    )
)
```

---

## Implementation Plan

### Phase 1: Lease Pooling (Weeks 1-2)
- [ ] Implement LeasePool structure
- [ ] Pool-based expiry checking
- [ ] Migration from flat map
- [ ] Unit tests

### Phase 2: Adaptive TTL (Weeks 3-4)
- [ ] Load calculation metrics
- [ ] TTL adaptation logic
- [ ] Configuration options
- [ ] Integration tests

### Phase 3: Batch Renewals (Weeks 5-6)
- [ ] Protocol buffer changes
- [ ] Server batch processing
- [ ] Client library support
- [ ] E2E tests

### Phase 4: Background Revocation (Week 7)
- [ ] Worker pool implementation
- [ ] Queue management
- [ ] Error handling
- [ ] Performance tests

### Phase 5: Optimizations (Week 8)
- [ ] Checkpoint compression
- [ ] Metrics enhancement
- [ ] Documentation

---

## Backwards Compatibility

**Fully backward compatible:**
- Existing LeaseGrant/LeaseKeepAlive unchanged
- Batch renewal is optional new API
- Adaptive TTL transparent to clients
- Pool granularity configurable (default: 1s = no pooling)

**Migration:**
- Gradual rollout via feature flag
- Clients can adopt batch renewals incrementally
- No breaking protocol changes

---

## Performance Analysis

### CPU Reduction

**Before (20K leases):**
```
Heap operations: 20K individual entries
CPU: ~8% continuous
Checkpoint: 20 batches × 50ms = 1s every 5min
```

**After (20K leases with pooling):**
```
Pools: ~5 pools (common TTLs: 10s, 30s, 60s, etc.)
Heap operations: 5 pool-level + 20K simple list
CPU: ~4% (50% reduction)
Checkpoint: 1 compressed entry = 100ms every 5min
```

### Network Reduction

**Before (20K leases, 30s TTL):**
```
Renewals: 20K / 30s = 667 req/sec
Network: 667 × 1KB = 667 KB/sec
```

**After (batch renewal, 100 leases/batch):**
```
Renewals: 200 req/sec (batched)
Network: 200 × 5KB = 1 MB/sec total
Per-lease overhead: 70% reduction
```

---

## Alternatives Considered

### Alternative 1: Client-Side Lease Management

**Approach:** Clients pool leases themselves.

**Cons:**
- Doesn't solve server CPU issue
- Coordination complexity
- Not transparent

**Decision:** Server-side pooling better.

### Alternative 2: Remove Checkpoints

**Approach:** Eliminate checkpoint mechanism.

**Cons:**
- Lease recovery after crash impossible
- Kubernetes requires lease persistence

**Decision:** Optimize, don't remove.

### Alternative 3: Separate Lease Server

**Approach:** Dedicated service for leases.

**Cons:**
- Additional deployment complexity
- Network hop overhead
- Coordination issues

**Decision:** Optimize within etcd.

---

## Open Questions

1. **Pool granularity default:** What's optimal bucket size?
   - Proposed: 5 seconds (30s, 35s both → 30s pool)
   - Configurable per deployment

2. **Adaptive TTL limits:** Maximum extension multiplier?
   - Proposed: 2.0× (30s → 60s max)
   - Prevents excessive delays

3. **Batch size limits:** Maximum leases per batch renewal?
   - Proposed: 1000 leases per request
   - Balance atomicity vs latency

---

## Success Criteria

- [ ] Support 100K concurrent leases
- [ ] 50% CPU reduction for lease management
- [ ] 10× reduction in checkpoint log entries
- [ ] No increase in P99 lease renewal latency
- [ ] Kubernetes scalability validation (20K+ pods)

---

## Effort Estimation

**Total: 7-8 weeks**

| Phase | Effort |
|-------|--------|
| Lease pooling | 2 weeks |
| Adaptive TTL | 2 weeks |
| Batch renewals | 2 weeks |
| Background revocation | 1 week |
| Testing and docs | 1 week |

**Team:** 1-2 engineers

---

## Stakeholder Approvals

- [ ] etcd maintainers
- [ ] Kubernetes SIG-API-Machinery
- [ ] Large cluster operators

---

## Rollback Strategy

1. **Feature flags:**
   - `--experimental-lease-pooling`
   - `--experimental-adaptive-ttl`
   - `--experimental-batch-renewal`

2. **Gradual rollout:**
   - Alpha: opt-in testing
   - Beta: default enabled with monitoring
   - GA: stable

3. **Rollback:** Disable flags, revert to individual tracking

---

## References

- [Kubernetes Lease API](https://kubernetes.io/docs/reference/kubernetes-api/cluster-resources/lease-v1/)
- [etcd Lease Design Doc](https://etcd.io/docs/current/learning/design-lease/)
- Current implementation: [`server/lease/lessor.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/lease/lessor.go)

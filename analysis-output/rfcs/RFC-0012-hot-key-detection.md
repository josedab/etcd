# RFC-0012: Hot Key Detection and Mitigation

**Status:** Draft
**Author:** Claude Code Analysis
**Created:** November 19, 2025
**Category:** Strategic (P1 - High Priority)

---

## Summary

Implement hot key detection, monitoring, and mitigation to prevent single keys from saturating the cluster, with automatic rate limiting, watch coalescing, and operational dashboards.

---

## Motivation

### Problem

etcd currently has **no visibility** into per-key operation rates. All metrics are cluster-wide aggregates.

**Evidence from codebase:**
```go
// From server/storage/mvcc/metrics.go
rangeCounter = prometheus.NewCounter(...)    // Total only, no per-key
putCounter = prometheus.NewCounter(...)      // Total only, no per-key
// No per-key metrics exist anywhere
```

### Real-World Hot Key Scenarios

1. **Kubernetes ConfigMap Hot Keys:**
   - Single ConfigMap watched by 10K+ pods
   - Results in 10K watch streams on same key
   - Server CPU saturated sending duplicates

2. **Leader Election Keys:**
   - Thundering herd on leader key updates
   - 100s of clients competing for same lock
   - Watch storm on every change

3. **Global Counters:**
   - Distributed apps using etcd for counters
   - Single key receiving 1000s writes/sec
   - Becomes cluster bottleneck

4. **Service Discovery:**
   - Popular service key watched by all consumers
   - Every update broadcasts to all watchers

### Current Limitations

**No Detection:**
- Cannot identify which keys are hot
- No metrics for per-key operation rate
- No alerts when key becomes hot

**No Mitigation:**
- No rate limiting per key
- Duplicate watch events not coalesced
- No client-side caching hints

**No Visibility:**
- Operators debug by guessing
- No dashboard showing top keys
- Log analysis required for investigation

### Expected Benefits

- **Prevent cluster saturation** from single hot key
- **Operational visibility** into key access patterns
- **Automatic mitigation** of common hot key issues
- **Better SLAs** for mixed workloads (hot + cold keys)

---

## Detailed Design

### 1. Hot Key Tracking Architecture

```
┌──────────────────────────────────────────┐
│         Request Path                     │
│                                          │
│  ┌────────────────────────────────────┐  │
│  │  Hot Key Tracker (Middleware)      │  │
│  │  - Count Sketch (probabilistic)    │  │
│  │  - Rolling window (last 60s)       │  │
│  │  - Top-K tracking                  │  │
│  └───────────┬────────────────────────┘  │
│              │                           │
│              ├─ Put/Get/Delete          │
│              ├─ Watch                    │
│              └─ Txn                      │
│                                          │
│  ┌────────────────────────────────────┐  │
│  │  Hot Key Dashboard                 │  │
│  │  - Top keys by operation           │  │
│  │  - Watch count per key             │  │
│  │  - Rate limit status               │  │
│  └────────────────────────────────────┘  │
└──────────────────────────────────────────┘
```

### 2. Probabilistic Counting (Count-Min Sketch)

Use space-efficient algorithm for tracking millions of keys.

```go
// Count-Min Sketch for hot key detection
type CountMinSketch struct {
    width  int      // Number of counters per hash
    depth  int      // Number of hash functions
    counts [][]int64
    hashes []hash.Hash64
}

func NewCountMinSketch(width, depth int) *CountMinSketch {
    cms := &CountMinSketch{
        width:  width,
        depth:  depth,
        counts: make([][]int64, depth),
    }

    for i := 0; i < depth; i++ {
        cms.counts[i] = make([]int64, width)
        cms.hashes = append(cms.hashes, fnv.New64a())
    }

    return cms
}

func (cms *CountMinSketch) Increment(key []byte) {
    for i := 0; i < cms.depth; i++ {
        cms.hashes[i].Reset()
        cms.hashes[i].Write(key)
        hash := cms.hashes[i].Sum64()
        pos := hash % uint64(cms.width)
        atomic.AddInt64(&cms.counts[i][pos], 1)
    }
}

func (cms *CountMinSketch) Estimate(key []byte) int64 {
    min := int64(math.MaxInt64)

    for i := 0; i < cms.depth; i++ {
        cms.hashes[i].Reset()
        cms.hashes[i].Write(key)
        hash := cms.hashes[i].Sum64()
        pos := hash % uint64(cms.width)
        count := atomic.LoadInt64(&cms.counts[i][pos])
        if count < min {
            min = count
        }
    }

    return min
}
```

### 3. Hot Key Tracker

```go
type HotKeyTracker struct {
    // Count-Min Sketch for all keys
    sketch *CountMinSketch

    // Top-K heap for hot keys
    topK      *TopKHeap
    threshold int64  // Ops/sec to be considered "hot"

    // Rolling window (last 60 seconds)
    windows [60]*CountMinSketch
    current int

    // Watch tracking
    watchCounts map[string]int32  // key -> watcher count

    mu sync.RWMutex
}

func (hkt *HotKeyTracker) RecordOperation(key []byte, opType OpType) {
    // Increment count in current window
    hkt.sketch.Increment(key)

    // Update top-K if necessary
    count := hkt.sketch.Estimate(key)
    if count > hkt.threshold {
        hkt.topK.Insert(string(key), count)
    }
}

func (hkt *HotKeyTracker) RecordWatch(key []byte) {
    hkt.mu.Lock()
    defer hkt.mu.Unlock()

    keyStr := string(key)
    hkt.watchCounts[keyStr]++

    // Alert if watch count exceeds threshold
    if hkt.watchCounts[keyStr] > hkt.watchThreshold {
        hkt.alertHotWatch(keyStr, hkt.watchCounts[keyStr])
    }
}

// Rotate windows every second
func (hkt *HotKeyTracker) rotateWindow() {
    hkt.mu.Lock()
    defer hkt.mu.Unlock()

    hkt.current = (hkt.current + 1) % 60
    hkt.windows[hkt.current] = NewCountMinSketch(10000, 5)
    hkt.sketch = hkt.windows[hkt.current]
}

// Get operations per second for key
func (hkt *HotKeyTracker) GetOpsPerSecond(key []byte) float64 {
    hkt.mu.RLock()
    defer hkt.mu.RUnlock()

    // Sum across all 60-second windows
    total := int64(0)
    for _, window := range hkt.windows {
        if window != nil {
            total += window.Estimate(key)
        }
    }

    return float64(total) / 60.0
}
```

### 4. Top-K Heap

```go
type TopKHeap struct {
    k     int
    items []*HotKeyItem
    index map[string]int  // key -> heap position
    mu    sync.RWMutex
}

type HotKeyItem struct {
    Key   string
    Count int64
    Index int
}

func (h *TopKHeap) Insert(key string, count int64) {
    h.mu.Lock()
    defer h.mu.Unlock()

    if pos, exists := h.index[key]; exists {
        // Update existing
        h.items[pos].Count = count
        heap.Fix(h, pos)
    } else if len(h.items) < h.k {
        // Add new item
        item := &HotKeyItem{Key: key, Count: count}
        heap.Push(h, item)
    } else if count > h.items[0].Count {
        // Replace minimum if new count is higher
        h.items[0].Key = key
        h.items[0].Count = count
        heap.Fix(h, 0)
    }
}

func (h *TopKHeap) GetTopK() []*HotKeyItem {
    h.mu.RLock()
    defer h.mu.RUnlock()

    result := make([]*HotKeyItem, len(h.items))
    copy(result, h.items)

    // Sort descending
    sort.Slice(result, func(i, j int) bool {
        return result[i].Count > result[j].Count
    })

    return result
}
```

### 5. Integration with MVCC Operations

```go
// Wrap existing operations
type instrumentedStore struct {
    *store
    tracker *HotKeyTracker
}

func (s *instrumentedStore) Put(key, value []byte, lease LeaseID) int64 {
    // Track hot key
    s.tracker.RecordOperation(key, OpTypePut)

    // Check rate limit
    if s.tracker.IsRateLimited(key) {
        return ErrRateLimitExceeded
    }

    // Proceed with actual Put
    return s.store.Put(key, value, lease)
}

func (s *instrumentedStore) Range(key, end []byte, ...) (*RangeResult, error) {
    // Track hot key
    s.tracker.RecordOperation(key, OpTypeRange)

    return s.store.Range(key, end, ...)
}
```

### 6. Rate Limiting

```go
type RateLimiter struct {
    limits map[string]*rate.Limiter
    mu     sync.RWMutex

    // Default rate limit for hot keys
    defaultRate  rate.Limit  // 1000 ops/sec
    defaultBurst int         // 100 burst
}

func (rl *RateLimiter) CheckLimit(key []byte) error {
    keyStr := string(key)

    rl.mu.RLock()
    limiter, exists := rl.limits[keyStr]
    rl.mu.RUnlock()

    if !exists {
        // Not rate limited
        return nil
    }

    if !limiter.Allow() {
        return ErrRateLimitExceeded
    }

    return nil
}

// Automatically rate limit hot keys
func (hkt *HotKeyTracker) ApplyRateLimit(key string, opsPerSec float64) {
    if opsPerSec > float64(hkt.threshold) {
        // Create rate limiter for this key
        limiter := rate.NewLimiter(
            rate.Limit(hkt.maxOpsPerKey),
            hkt.burstSize,
        )

        hkt.rateLimiter.AddLimit(key, limiter)
        hkt.alertRateLimitApplied(key, opsPerSec)
    }
}
```

### 7. Watch Coalescing for Hot Keys

```go
// Coalesce multiple watchers on same key
type WatchCoalescer struct {
    groups map[string]*WatchGroup
    mu     sync.RWMutex
}

type WatchGroup struct {
    key      string
    watchers []*watcher
    eventCh  chan mvccpb.Event
}

func (wc *WatchCoalescer) AddWatcher(w *watcher) {
    wc.mu.Lock()
    defer wc.mu.Unlock()

    key := string(w.key)
    group, exists := wc.groups[key]

    if !exists {
        group = &WatchGroup{
            key:     key,
            eventCh: make(chan mvccpb.Event, 1000),
        }
        wc.groups[key] = group
        go group.broadcast()
    }

    group.watchers = append(group.watchers, w)
}

func (wg *WatchGroup) broadcast() {
    for event := range wg.eventCh {
        // Send same event to all watchers in group
        for _, w := range wg.watchers {
            select {
            case w.ch <- event:
            default:
                // Watcher slow, move to victim
            }
        }
    }
}
```

### 8. Metrics and Monitoring

```go
var (
    hotKeyOperations = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "etcd_hotkey_operations_total",
            Help: "Operations on hot keys",
        },
        []string{"key", "operation"},
    )

    hotKeyWatchers = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "etcd_hotkey_watchers",
            Help: "Number of watchers on hot keys",
        },
        []string{"key"},
    )

    rateLimitedOperations = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "etcd_ratelimited_operations_total",
            Help: "Operations rate limited",
        },
        []string{"key"},
    )
)
```

### 9. Dashboard and API

```go
// HTTP endpoint for hot key dashboard
func (s *EtcdServer) handleHotKeys(w http.ResponseWriter, r *http.Request) {
    topKeys := s.hotKeyTracker.GetTopK()

    response := HotKeyDashboard{
        TopKeys: make([]HotKeyInfo, len(topKeys)),
    }

    for i, item := range topKeys {
        response.TopKeys[i] = HotKeyInfo{
            Key:         item.Key,
            OpsPerSec:   s.hotKeyTracker.GetOpsPerSecond([]byte(item.Key)),
            Watchers:    s.hotKeyTracker.GetWatchCount(item.Key),
            RateLimited: s.hotKeyTracker.IsRateLimited([]byte(item.Key)),
        }
    }

    json.NewEncoder(w).Encode(response)
}

// CLI command
// etcdctl hotkeys list --top=10
```

---

## Implementation Plan

### Phase 1: Detection (Weeks 1-2)
- [ ] Implement Count-Min Sketch
- [ ] Add Top-K heap
- [ ] Rolling window tracking
- [ ] Unit tests

### Phase 2: Integration (Weeks 3-4)
- [ ] Integrate with MVCC operations
- [ ] Watch count tracking
- [ ] Metrics exposure
- [ ] Integration tests

### Phase 3: Mitigation (Weeks 5-6)
- [ ] Rate limiting logic
- [ ] Watch coalescing
- [ ] Automatic detection thresholds
- [ ] Performance tests

### Phase 4: Dashboard (Weeks 7-8)
- [ ] HTTP API
- [ ] CLI commands
- [ ] Alerting integration
- [ ] Documentation

---

## Backwards Compatibility

**Fully backward compatible:**
- Detection is read-only (no behavior changes)
- Rate limiting disabled by default
- All new features opt-in via configuration

**No Breaking Changes:**
- No protocol changes
- No API changes
- Metrics are additive

---

## Configuration

```go
type HotKeyConfig struct {
    // Enable hot key detection
    Enabled bool

    // Operations per second threshold
    Threshold int64  // Default: 1000

    // Top-K to track
    TopK int  // Default: 100

    // Watch count threshold
    WatchThreshold int32  // Default: 1000

    // Rate limiting
    RateLimitEnabled bool
    MaxOpsPerKey     int64  // Default: 10000
    BurstSize        int    // Default: 1000

    // Watch coalescing
    CoalesceWatches bool
}
```

```bash
# Command line
etcd \
    --hot-key-detection=true \
    --hot-key-threshold=1000 \
    --hot-key-rate-limit=true
```

---

## Performance Impact

### Memory Overhead

**Count-Min Sketch:**
```
Width: 10,000
Depth: 5
Size per window: 10,000 × 5 × 8 bytes = 400 KB
60 windows: 400 KB × 60 = 24 MB
```

**Top-K Heap:**
```
K = 100
Size: 100 × (key + count + pointer) ≈ 10 KB
```

**Total: ~25 MB**

### CPU Overhead

- Count increment: ~200ns (hash + atomic add)
- Per-operation: negligible (<0.1%)

---

## Alternatives Considered

### Alternative 1: Exact Counting

**Approach:** Track every key exactly.

**Cons:**
- Memory grows with key count (unbounded)
- Hash map overhead

**Decision:** Probabilistic counting sufficient and bounded.

### Alternative 2: Sampling

**Approach:** Sample 1% of operations.

**Cons:**
- Miss bursts
- Less accurate for infrequent hot keys

**Decision:** Count-Min Sketch better accuracy.

---

## Success Criteria

- [ ] Detect hot keys with >95% accuracy
- [ ] Memory overhead <50 MB
- [ ] CPU overhead <1%
- [ ] Rate limiting prevents cluster saturation
- [ ] Dashboard shows top keys in real-time

---

## Effort Estimation

**Total: 7-8 weeks**

| Phase | Effort |
|-------|--------|
| Detection | 2 weeks |
| Integration | 2 weeks |
| Mitigation | 2 weeks |
| Dashboard | 2 weeks |

**Team:** 1-2 engineers

---

## Stakeholder Approvals

- [ ] etcd maintainers
- [ ] Kubernetes SIG (for ConfigMap hot keys)
- [ ] Large deployment operators

---

## Rollback Strategy

1. **Feature flag:** `--hot-key-detection=false`
2. **Gradual rollout:**
   - Alpha: Detection only
   - Beta: Mitigation enabled
   - GA: Default on

---

## References

- [Count-Min Sketch Paper](http://dimacs.rutgers.edu/~graham/pubs/papers/cm-full.pdf)
- [Frequency Estimation Algorithms](https://highlyscalable.wordpress.com/2012/05/01/probabilistic-structures-web-analytics-data-mining/)
- [Google Zanzibar Hot Spot Detection](https://research.google/pubs/pub48190/)

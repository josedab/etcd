# RFC-0004: Adaptive Watch Batching

**Status:** Draft
**Author:** Claude Code Analysis
**Created:** November 19, 2025
**Category:** Strategic

---

## Summary

Implement adaptive batching for watch events to reduce CPU overhead and improve throughput during high-frequency updates, while maintaining low latency during quiet periods.

---

## Motivation

### Problem

The current watch implementation sends events individually:

```go
// Current: send immediately
for event := range events {
    watcher.send(event)  // gRPC overhead per event
}
```

This causes:
1. High CPU usage during bursts (many small gRPC messages)
2. Network overhead (headers per message)
3. Client-side processing overhead

### Observed Behavior

During bulk operations:
- 10K puts → 10K individual watch events
- High CPU on both server and client
- Network saturation with small messages

### Expected Benefits

- 30% reduction in CPU usage during bursts
- Better network utilization (fewer packets)
- Maintained low latency during normal operation

---

## Detailed Design

### 1. Batching Configuration

Add configuration in `watchableStore`:

```go
type watchConfig struct {
    // BatchWaitTime is the maximum time to wait for batch accumulation.
    // Default: 10ms
    BatchWaitTime time.Duration

    // BatchSizeThreshold triggers immediate send when reached.
    // Default: 100 events
    BatchSizeThreshold int

    // AdaptiveEnabled enables dynamic batch sizing.
    // Default: true
    AdaptiveEnabled bool
}
```

### 2. Batch Accumulator

Implement batching in watch stream:

```go
type watchBatcher struct {
    events    []mvccpb.Event
    timer     *time.Timer
    threshold int
    waitTime  time.Duration

    mu sync.Mutex
}

func (b *watchBatcher) add(event mvccpb.Event) bool {
    b.mu.Lock()
    defer b.mu.Unlock()

    b.events = append(b.events, event)

    // Immediate send if threshold reached
    if len(b.events) >= b.threshold {
        return true // trigger send
    }

    // Start timer on first event
    if len(b.events) == 1 {
        b.timer.Reset(b.waitTime)
    }

    return false
}

func (b *watchBatcher) flush() []mvccpb.Event {
    b.mu.Lock()
    defer b.mu.Unlock()

    events := b.events
    b.events = make([]mvccpb.Event, 0, b.threshold)
    b.timer.Stop()

    return events
}
```

### 3. Adaptive Sizing

Dynamically adjust batch parameters based on load:

```go
type adaptiveBatcher struct {
    *watchBatcher

    // Metrics for adaptation
    eventRate    float64
    lastAdjust   time.Time
    adjustPeriod time.Duration
}

func (a *adaptiveBatcher) adapt() {
    if time.Since(a.lastAdjust) < a.adjustPeriod {
        return
    }

    // Calculate events per second
    rate := a.calculateRate()

    // Adjust parameters
    switch {
    case rate > 10000:
        // High load: longer wait, larger batches
        a.waitTime = 50 * time.Millisecond
        a.threshold = 500
    case rate > 1000:
        // Medium load
        a.waitTime = 20 * time.Millisecond
        a.threshold = 200
    default:
        // Low load: minimal batching
        a.waitTime = 5 * time.Millisecond
        a.threshold = 50
    }

    a.lastAdjust = time.Now()
}
```

### 4. Integration with Watch Stream

Modify `serverWatchStream` in `server/etcdserver/api/v3rpc/watch.go`:

```go
func (sws *serverWatchStream) sendLoop() {
    batcher := newAdaptiveBatcher(sws.cfg)

    for {
        select {
        case event := <-sws.eventCh:
            if batcher.add(event) {
                sws.sendBatch(batcher.flush())
            }

        case <-batcher.timer.C:
            if events := batcher.flush(); len(events) > 0 {
                sws.sendBatch(events)
            }

        case <-sws.closec:
            return
        }
    }
}

func (sws *serverWatchStream) sendBatch(events []mvccpb.Event) {
    resp := &pb.WatchResponse{
        Header: sws.newResponseHeader(),
        Events: events,
    }
    sws.gRPCStream.Send(resp)
}
```

### 5. Metrics

Add Prometheus metrics:

```go
var (
    watchBatchSize = prometheus.NewHistogram(prometheus.HistogramOpts{
        Namespace: "etcd",
        Subsystem: "watch",
        Name:      "batch_size",
        Help:      "Size of watch event batches sent.",
        Buckets:   []float64{1, 10, 50, 100, 500, 1000},
    })

    watchBatchLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
        Namespace: "etcd",
        Subsystem: "watch",
        Name:      "batch_latency_seconds",
        Help:      "Time events wait in batch before sending.",
        Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1},
    })
)
```

---

## Example Behavior

### Low Traffic (< 100 events/sec)

```
Event 1 arrives at T=0ms
  → Timer starts (5ms wait)
  → No more events
  → T=5ms: flush [Event 1]
```

Latency: 5ms (minimal batching)

### High Traffic (> 10K events/sec)

```
Events 1-100 arrive at T=0-5ms
  → Batch accumulates
  → Threshold reached
  → T=5ms: flush [Events 1-100]

Events 101-200 arrive at T=5-10ms
  → New batch accumulates
  → Threshold reached
  → T=10ms: flush [Events 101-200]
```

Batches of 100, every ~5ms, much lower overhead.

---

## Implementation Plan

### Phase 1: Basic Batching (Week 1)
- [ ] Implement watchBatcher
- [ ] Add configuration
- [ ] Unit tests

### Phase 2: Integration (Week 2)
- [ ] Integrate with watch stream
- [ ] Backward compatibility check
- [ ] Integration tests

### Phase 3: Adaptive Logic (Week 3)
- [ ] Implement adaptive sizing
- [ ] Add metrics
- [ ] Load testing

---

## Backwards Compatibility

**Backward compatible with considerations:**

- Wire format unchanged (events array already supported)
- Clients receive events in batches instead of singles
- Maximum latency increase: `BatchWaitTime` (default 10ms)

### Client Impact

Clients already handle batched events:

```go
for resp := range watchChan {
    for _, event := range resp.Events {  // Already iterates array
        handleEvent(event)
    }
}
```

---

## Alternatives Considered

### Alternative 1: Client-side batching

**Pros:**
- No server changes
- Client control

**Cons:**
- Doesn't reduce server CPU
- Doesn't reduce network overhead

**Decision:** Server-side batching addresses root cause.

### Alternative 2: Fixed batch size only

**Pros:**
- Simpler implementation
- Predictable behavior

**Cons:**
- Poor for mixed workloads
- Either too slow or too inefficient

**Decision:** Adaptive provides best of both.

---

## Open Questions

1. **Default wait time**: What's optimal for most workloads?
   - Proposed: 10ms (tune based on feedback)

2. **Per-watcher vs global batching**: Should each watcher batch independently?
   - Proposed: Per-watcher for isolation

3. **Disable option**: Should clients be able to disable batching?
   - Proposed: Server-side config only initially

---

## Success Criteria

- [ ] 30% reduction in CPU during bulk watch events
- [ ] No increase in P50 latency
- [ ] <20ms P99 latency during batching
- [ ] Metrics available for tuning

---

## Effort Estimation

**Total: 2-3 weeks**

| Task | Effort |
|------|--------|
| Basic batching | 3 days |
| Integration | 3 days |
| Adaptive logic | 3 days |
| Testing & tuning | 3 days |
| Documentation | 1 day |

---

## Stakeholder Approvals

- [ ] etcd maintainers
- [ ] Watch system contributors

---

## Rollback Strategy

1. Set `BatchWaitTime: 0` to disable batching
2. Revert PR if issues discovered
3. No data migration needed

---

## References

- [Nagle's Algorithm](https://en.wikipedia.org/wiki/Nagle%27s_algorithm)
- [TCP_NODELAY vs batching trade-offs](https://brooker.co.za/blog/2024/05/09/nagle.html)
- Watch implementation: [`server/storage/mvcc/watchable_store.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/storage/mvcc/watchable_store.go)

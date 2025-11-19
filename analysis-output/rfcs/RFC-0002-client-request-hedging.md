# RFC-0002: Client Request Hedging

**Status:** Draft
**Author:** Claude Code Analysis
**Created:** November 19, 2025
**Category:** Quick Win

---

## Summary

Implement request hedging in the etcd client to reduce tail latency by sending duplicate requests to multiple endpoints after a timeout threshold.

---

## Motivation

### Problem

The etcd client currently sends requests to a single endpoint. If that endpoint is slow (due to load, GC, network issues), the entire request experiences high latency—even if other cluster members could respond faster.

### Current Behavior

```go
// Current: sequential retry on failure
endpoint1 → timeout → endpoint2 → timeout → endpoint3
// Total time: timeout * 3
```

### Desired Behavior

```go
// Hedged: parallel after threshold
endpoint1 ─────────┐
         (50ms)    ├→ first response wins
endpoint2 ─────────┘
// Total time: min(response1, response2)
```

### Expected Benefits

- 30-50% reduction in P99 latency
- Better utilization of healthy cluster members
- Improved user experience during partial failures

---

## Detailed Design

### 1. Add Hedging Configuration

Extend `clientv3.Config`:

```go
type Config struct {
    // ... existing fields ...

    // HedgingDelay is the time to wait before sending a hedged request.
    // Zero disables hedging. Default: 0 (disabled).
    HedgingDelay time.Duration

    // HedgingMaxRequests is the maximum number of hedged requests to send.
    // Default: 2 (original + 1 hedge).
    HedgingMaxRequests int
}
```

### 2. Implement Hedging Interceptor

Add gRPC interceptor in `client/v3/retry_interceptor.go`:

```go
func (c *Client) hedgingInterceptor() grpc.UnaryClientInterceptor {
    return func(ctx context.Context, method string, req, reply interface{},
        cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {

        if c.cfg.HedgingDelay <= 0 {
            return invoker(ctx, method, req, reply, cc, opts...)
        }

        return c.hedgedInvoke(ctx, method, req, reply, cc, invoker, opts)
    }
}

func (c *Client) hedgedInvoke(ctx context.Context, method string, req, reply interface{},
    cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts []grpc.CallOption) error {

    resultCh := make(chan error, c.cfg.HedgingMaxRequests)
    ctx, cancel := context.WithCancel(ctx)
    defer cancel()

    // Send initial request
    go func() {
        resultCh <- invoker(ctx, method, req, reply, cc, opts...)
    }()

    // Set up hedging timer
    hedgeTimer := time.NewTimer(c.cfg.HedgingDelay)
    defer hedgeTimer.Stop()

    hedgeCount := 1
    for {
        select {
        case err := <-resultCh:
            if err == nil || !isRetryable(err) {
                return err
            }
            // Continue waiting for other responses
            hedgeCount--
            if hedgeCount == 0 {
                return err
            }

        case <-hedgeTimer.C:
            if hedgeCount < c.cfg.HedgingMaxRequests {
                hedgeCount++
                go func() {
                    // Send to different endpoint
                    resultCh <- invoker(ctx, method, req, reply, cc, opts...)
                }()
                // Reset timer for next hedge
                hedgeTimer.Reset(c.cfg.HedgingDelay)
            }

        case <-ctx.Done():
            return ctx.Err()
        }
    }
}

func isRetryable(err error) bool {
    switch err {
    case rpctypes.ErrTimeout, rpctypes.ErrNoLeader:
        return true
    default:
        return status.Code(err) == codes.Unavailable
    }
}
```

### 3. Endpoint Selection for Hedging

Ensure hedged requests go to different endpoints:

```go
// In resolver or balancer
func (r *Resolver) pickHedgeEndpoint(exclude string) string {
    r.mu.RLock()
    defer r.mu.RUnlock()

    for _, ep := range r.endpoints {
        if ep != exclude {
            return ep
        }
    }
    return exclude // fallback
}
```

### 4. Read-Only Operations Only

Hedging should only apply to idempotent operations:

```go
var hedgeableMethods = map[string]bool{
    "/etcdserverpb.KV/Range":      true,
    "/etcdserverpb.Lease/LeaseTimeToLive": true,
    "/etcdserverpb.Cluster/MemberList":    true,
    "/etcdserverpb.Maintenance/Status":    true,
    // NOT: Put, Delete, Txn, LeaseGrant, etc.
}

func isHedgeable(method string) bool {
    return hedgeableMethods[method]
}
```

---

## Example Usage

### Enable Hedging

```go
cli, err := clientv3.New(clientv3.Config{
    Endpoints:          []string{"localhost:2379", "localhost:22379", "localhost:32379"},
    DialTimeout:        5 * time.Second,
    HedgingDelay:       50 * time.Millisecond,
    HedgingMaxRequests: 2,
})
```

### Behavior

```
Request: Get("foo")

T=0ms:   Send to endpoint1
T=50ms:  No response yet, send to endpoint2
T=75ms:  endpoint2 responds → return result, cancel endpoint1
```

---

## Implementation Plan

### Phase 1: Core Implementation (Days 1-3)
- [ ] Add configuration fields
- [ ] Implement hedging interceptor
- [ ] Unit tests for hedging logic

### Phase 2: Integration (Days 4-5)
- [ ] Integrate with existing retry logic
- [ ] Add to client builder
- [ ] Integration tests

### Phase 3: Validation (Days 5-7)
- [ ] Benchmark: measure latency improvement
- [ ] Chaos testing: partial cluster failures
- [ ] Documentation

---

## Backwards Compatibility

**Fully backward compatible:**
- Hedging is disabled by default (`HedgingDelay: 0`)
- Existing clients work unchanged
- No server-side changes required

### Migration Path

Users can opt-in by setting `HedgingDelay`:

```go
// Before (no change needed)
cli, _ := clientv3.New(clientv3.Config{
    Endpoints: endpoints,
})

// After (opt-in to hedging)
cli, _ := clientv3.New(clientv3.Config{
    Endpoints:    endpoints,
    HedgingDelay: 50 * time.Millisecond,
})
```

---

## Alternatives Considered

### Alternative 1: gRPC Built-in Hedging

gRPC has hedging support via service config.

**Pros:**
- Standard implementation
- Well-tested

**Cons:**
- Less control over etcd-specific logic
- Harder to integrate with etcd's endpoint management

**Decision:** Custom implementation for better integration.

### Alternative 2: Speculative Execution

Send all requests in parallel immediately.

**Pros:**
- Lowest possible latency

**Cons:**
- 3x server load
- Wasteful for normal cases

**Decision:** Delayed hedging balances latency and load.

---

## Open Questions

1. **Default delay value**: What's the optimal hedging delay?
   - Proposed: 50ms default (configurable)
   - Needs benchmarking

2. **Watch streams**: Should we hedge watch creation?
   - Proposed: No, watches are stateful

3. **Metrics**: What metrics to expose?
   - Proposed: `etcd_client_hedged_requests_total`

---

## Success Criteria

- [ ] P99 latency reduced by 30%+ for Get operations
- [ ] No increase in server error rate
- [ ] Less than 10% increase in total request volume
- [ ] Feature documented and tested

---

## Effort Estimation

**Total: 5-7 dev-days**

| Task | Effort |
|------|--------|
| Core implementation | 2 days |
| Tests (unit + integration) | 2 days |
| Benchmarking | 1 day |
| Documentation | 0.5 days |
| Code review | 0.5 days |

---

## Stakeholder Approvals

- [ ] Client library maintainers
- [ ] API SIG (for config changes)

---

## Rollback Strategy

1. Users disable by setting `HedgingDelay: 0`
2. If critical issues: revert PR
3. No server-side changes to roll back

---

## References

- [gRPC Hedging](https://grpc.io/docs/guides/request-hedging/)
- [The Tail at Scale (Dean & Barroso)](https://research.google/pubs/pub40801/)
- [Hedged Requests in Go](https://pkg.go.dev/github.com/cristalhq/hedgedhttp)

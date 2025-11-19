# Patterns and Practices in etcd

*Part 3 of the etcd Deep Dive Series*

**Analysis Commit:** [`d6bc3229d81384aed0fc03a3ddd3dccfef092048`](https://github.com/etcd-io/etcd/tree/d6bc3229d81384aed0fc03a3ddd3dccfef092048)

---

## What You'll Learn

- Design patterns that make etcd maintainable
- Concurrency patterns for safe concurrent access
- Error handling strategies
- The applier pattern for state machines
- Configuration and graceful shutdown

---

## Introduction

etcd is a mature codebase with ~107,000 lines of Go source code. How do you maintain something this large while keeping it correct and performant?

The answer lies in consistent patterns. Let's explore the design decisions that make etcd work.

---

## Interface-Driven Design

etcd uses interfaces extensively. This isn't just for testing—it's how the system achieves modularity.

### The Server Interface

From [`server/etcdserver/server.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/etcdserver/server.go):

```go
type Server interface {
    AddMember(ctx context.Context, memb membership.Member) ([]*membership.Member, error)
    RemoveMember(ctx context.Context, id uint64) ([]*membership.Member, error)
    UpdateMember(ctx context.Context, updateMemb membership.Member) ([]*membership.Member, error)
    PromoteMember(ctx context.Context, id uint64) ([]*membership.Member, error)

    ClusterVersion() *semver.Version
    StorageVersion() *semver.Version
    Cluster() api.Cluster
    Alarms() []*pb.AlarmMember

    LeaderChangedNotify() <-chan struct{}
}
```

### Why Interfaces?

1. **Testability**: Mock implementations for unit tests
2. **Modularity**: Components can be developed independently
3. **Documentation**: Interfaces define contracts clearly

### Example: Backend Interface

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

This abstraction allows:
- Unit tests with in-memory backend
- Potential future storage backends
- Clear responsibility boundaries

### Fake Implementations

The `server/mock` directory provides test doubles:

```go
// From server/lease/lessor.go
type FakeLessor struct{}

func (fl *FakeLessor) Grant(id LeaseID, ttl int64) (*Lease, error) {
    return &Lease{}, nil
}

// Usage in tests
store := NewStore(zaptest.NewLogger(t), backend, &lease.FakeLessor{}, cfg)
```

---

## Concurrency Patterns

etcd handles thousands of concurrent operations. Here's how it stays correct.

### Reader-Writer Mutex Pattern

The most common pattern—allow many readers or one writer:

```go
// From server/storage/mvcc/kvstore.go
type store struct {
    mu sync.RWMutex
    // ...
}

// Read operations
func (s *store) Range(key, end []byte, limit, rev int64) (kvs []mvccpb.KeyValue, err error) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    // Read data
}

// Write operations
func (s *store) Put(key, value []byte, lease lease.LeaseID) int64 {
    s.mu.Lock()
    defer s.mu.Unlock()
    // Write data
}
```

### Atomic Operations for Hot Paths

For frequently accessed state, atomics avoid lock contention:

```go
// From server/etcdserver/server.go
type EtcdServer struct {
    inflightSnapshots atomic.Int64
    appliedIndex      atomic.Uint64
    committedIndex    atomic.Uint64
    term              atomic.Uint64
    lead              atomic.Uint64
    // ...
}

// Usage
func (s *EtcdServer) Lead() uint64 {
    return s.lead.Load()
}

func (s *EtcdServer) setLead(lead uint64) {
    s.lead.Store(lead)
}
```

### Channel-Based Communication

Goroutines communicate via channels, not shared memory:

```go
// From server/etcdserver/server.go
type EtcdServer struct {
    readych   chan struct{}      // Server is ready
    stop      chan struct{}      // Shutdown signal
    stopping  chan struct{}      // Stopping in progress
    done      chan struct{}      // Shutdown complete
    readwaitc chan struct{}      // Read wait notification
    errorc    chan error         // Error reporting
    // ...
}
```

### Wait Pattern for Request Correlation

The wait pattern correlates requests with responses:

```go
// From pkg/wait/wait.go
type Wait interface {
    Register(id uint64) <-chan any
    Trigger(id uint64, x any)
    IsRegistered(id uint64) bool
}

// Implementation
type list struct {
    l sync.RWMutex
    m map[uint64]chan any
}

func (w *list) Register(id uint64) <-chan any {
    w.l.Lock()
    defer w.l.Unlock()
    ch := make(chan any, 1)
    w.m[id] = ch
    return ch
}

func (w *list) Trigger(id uint64, x any) {
    w.l.Lock()
    ch, ok := w.m[id]
    delete(w.m, id)
    w.l.Unlock()
    if ok {
        ch <- x
        close(ch)
    }
}
```

**Usage**: When proposing to Raft:
1. Register a wait with request ID
2. Send proposal
3. Wait on channel for response
4. Applier triggers the wait when complete

### Context-Based Cancellation

All operations accept context for cancellation:

```go
func (s *EtcdServer) Put(ctx context.Context, r *pb.PutRequest) (*pb.PutResponse, error) {
    result, err := s.processInternalRaftRequestOnce(ctx, pb.InternalRaftRequest{Put: r})
    if err != nil {
        return nil, err
    }
    return result.resp.(*pb.PutResponse), result.err
}
```

This enables:
- Client timeouts
- Request cancellation
- Graceful shutdown

---

## Error Handling Patterns

etcd has a sophisticated error handling strategy.

### Structured Error Types

From [`server/etcdserver/errors/errors.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/etcdserver/errors/errors.go):

```go
var (
    ErrUnknownMethod            = errors.New("etcdserver: unknown method")
    ErrStopped                  = errors.New("etcdserver: server stopped")
    ErrCanceled                 = errors.New("etcdserver: request cancelled")
    ErrTimeout                  = errors.New("etcdserver: request timed out")
    ErrTimeoutDueToLeaderFail   = errors.New("etcdserver: request timed out, possibly due to previous leader failure")
    ErrTimeoutDueToConnectionLost = errors.New("etcdserver: request timed out, possibly due to connection lost")
    ErrTimeoutWaitAppliedIndex  = errors.New("etcdserver: request timed out, waiting for the applied index took too long")
    ErrNoLeader                 = errors.New("etcdserver: no leader")
    ErrNotLeader                = errors.New("etcdserver: not leader")
    ErrLeaderChanged            = errors.New("etcdserver: leader changed")
    ErrRequestTooLarge          = errors.New("etcdserver: request is too large")
    ErrNoSpace                  = errors.New("etcdserver: no space")
    ErrTooManyRequests          = errors.New("etcdserver: too many requests")
    ErrUnhealthy                = errors.New("etcdserver: unhealthy cluster")
    ErrCorrupt                  = errors.New("etcdserver: corrupt cluster")
    ErrBadLeaderTransferee      = errors.New("etcdserver: bad leader transferee")
)
```

### Sentinel Errors with Context

```go
type DiscoveryError struct {
    Op  string
    Err error
}

func (e DiscoveryError) Error() string {
    return fmt.Sprintf("failed to %s discovery cluster (%v)", e.Op, e.Err)
}
```

### gRPC Status Mapping

Internal errors map to gRPC status codes:

```go
// From server/etcdserver/api/v3rpc/util.go
func togRPCError(err error) error {
    switch err {
    case errors.ErrNoSpace:
        return rpctypes.ErrGRPCNoSpace
    case errors.ErrNoLeader:
        return rpctypes.ErrGRPCNoLeader
    case errors.ErrTimeout:
        return rpctypes.ErrGRPCTimeout
    // ...
    }
}
```

### Error Wrapping

Errors include context as they propagate:

```go
if err := s.waitAppliedIndex(); err != nil {
    return nil, fmt.Errorf("waiting for applied index: %w", err)
}
```

---

## The Applier Pattern

The applier is a key architectural pattern that separates consensus from execution.

### Why Separate Consensus from Execution?

1. **Consistency**: All nodes must apply operations in the same order
2. **Determinism**: Same input must produce same output on all nodes
3. **Recovery**: Operations can be replayed from Raft log

### Applier Interface

From [`server/etcdserver/apply/apply.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/etcdserver/apply/apply.go):

```go
type applierV3 interface {
    Apply(r *pb.InternalRaftRequest, shouldApplyV3 membership.ShouldApplyV3) *Result

    Put(p *pb.PutRequest) (*pb.PutResponse, *traceutil.Trace, error)
    Range(r *pb.RangeRequest) (*pb.RangeResponse, *traceutil.Trace, error)
    DeleteRange(r *pb.DeleteRangeRequest) (*pb.DeleteRangeResponse, *traceutil.Trace, error)
    Txn(rt *pb.TxnRequest) (*pb.TxnResponse, *traceutil.Trace, error)
    Compaction(c *pb.CompactionRequest) (*pb.CompactionResponse, <-chan struct{}, *traceutil.Trace, error)

    LeaseGrant(lc *pb.LeaseGrantRequest) (*pb.LeaseGrantResponse, error)
    LeaseRevoke(lc *pb.LeaseRevokeRequest) (*pb.LeaseRevokeResponse, error)
    // Auth operations...
}
```

### The Uber Applier

The uber applier wraps appliers with cross-cutting concerns:

```go
type uberApplier struct {
    lg *zap.Logger
    a  applierV3
    // metrics, auth, quota, etc.
}

func (a *uberApplier) Apply(r *pb.InternalRaftRequest, shouldApplyV3 membership.ShouldApplyV3) *Result {
    // Pre-apply checks (auth, quota)
    // Apply the operation
    // Post-apply (metrics, triggers)
}
```

### Layered Appliers

Appliers can be composed:

```go
// Base applier
base := newApplierV3(kv, lessor, auth, ...)

// Add capped applier (respects quota)
capped := newApplierV3Capped(base)

// Add auth applier
authed := newAuthApplierV3(capped, auth, lessor)

// Add quota applier
quotaed := newApplierV3Quota(authed, quota)
```

### Determinism Requirements

Appliers must be deterministic. No:
- Random values
- Current time (use Raft-provided time)
- External I/O

```go
// BAD - non-deterministic
func (a *applier) Put(r *pb.PutRequest) *pb.PutResponse {
    r.Value = append(r.Value, time.Now().String()...) // Different on each node!
}

// GOOD - deterministic
func (a *applier) Put(r *pb.PutRequest) *pb.PutResponse {
    // Use revision (same on all nodes) as timestamp
    rev := a.s.KV().Rev()
}
```

---

## Configuration Management

etcd has a sophisticated configuration system.

### Configuration Priority

From [`server/config/config.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/config/config.go):

```
Config File > Environment Variables > Command-line Flags > Defaults
```

### Configuration Structure

```go
type ServerConfig struct {
    Name                 string
    ClientURLs           []url.URL
    PeerURLs             []url.URL

    DataDir              string
    WALDir               string

    TickMs               uint
    ElectionMs           uint

    InitialPeerURLsMap   types.URLsMap
    InitialClusterToken  string

    AutoCompactionMode   string
    AutoCompactionRetention string

    // ... many more
}
```

### Environment Variable Mapping

```go
// From server/embed/config.go
func ConfigFromFile(path string) (*Config, error) {
    // Load from file
    // Override with environment variables
    // ETCD_NAME -> --name
    // ETCD_DATA_DIR -> --data-dir
    // etc.
}
```

### Validation

Configuration is validated at startup:

```go
func (cfg *ServerConfig) Validate() error {
    if cfg.TickMs == 0 {
        return errors.New("tick-ms must be greater than 0")
    }
    if cfg.ElectionMs == 0 {
        return errors.New("election-ms must be greater than 0")
    }
    if cfg.ElectionMs <= cfg.TickMs {
        return errors.New("election-ms must be greater than tick-ms")
    }
    // ... more validations
}
```

---

## Graceful Shutdown

etcd shuts down cleanly, ensuring no data loss.

### Shutdown Sequence

From [`server/etcdserver/server.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/etcdserver/server.go):

```go
func (s *EtcdServer) Stop() {
    // Signal stop
    select {
    case s.stop <- struct{}{}:
    case <-s.done:
        return
    }
    // Wait for completion
    <-s.done
}

func (s *EtcdServer) run() {
    // ... normal operation ...

    defer func() {
        // Final cleanup
        s.r.Stop()
        s.compactor.Stop()
        s.KV().Close()
        // Signal done
        close(s.done)
    }()

    for {
        select {
        case <-s.stop:
            return
        // ... handle other channels ...
        }
    }
}
```

### WaitGroup for Goroutines

Track all goroutines for clean shutdown:

```go
type EtcdServer struct {
    wgMu sync.RWMutex
    wg   sync.WaitGroup
    // ...
}

// Start a goroutine
func (s *EtcdServer) GoAttach(f func()) {
    s.wgMu.RLock()
    defer s.wgMu.RUnlock()

    select {
    case <-s.stopping:
        return
    default:
    }

    s.wg.Add(1)
    go func() {
        defer s.wg.Done()
        f()
    }()
}

// Wait for all goroutines
func (s *EtcdServer) waitForShutdown() {
    s.wgMu.Lock()
    close(s.stopping)
    s.wgMu.Unlock()
    s.wg.Wait()
}
```

---

## Observability Patterns

etcd is built for production observability.

### Structured Logging

Using Uber's Zap logger:

```go
// From throughout the codebase
s.lg.Info("starting etcd server",
    zap.String("local-member-id", s.id.String()),
    zap.String("data-dir", s.cfg.DataDir),
)

s.lg.Warn("failed to send watch response",
    zap.Error(err),
    zap.Int64("watch-id", w.id),
)
```

### Metrics

Prometheus metrics are defined alongside code:

```go
// From server/etcdserver/metrics.go
var (
    proposalsCommitted = prometheus.NewCounter(prometheus.CounterOpts{
        Namespace: "etcd",
        Subsystem: "server",
        Name:      "proposals_committed_total",
        Help:      "The total number of consensus proposals committed.",
    })

    proposalsFailed = prometheus.NewCounter(prometheus.CounterOpts{
        Namespace: "etcd",
        Subsystem: "server",
        Name:      "proposals_failed_total",
        Help:      "The total number of failed proposals.",
    })
)

func init() {
    prometheus.MustRegister(proposalsCommitted)
    prometheus.MustRegister(proposalsFailed)
}
```

### Tracing

OpenTelemetry integration:

```go
// From server/etcdserver/v3_server.go
func (s *EtcdServer) Range(ctx context.Context, r *pb.RangeRequest) (*pb.RangeResponse, error) {
    ctx, span := tracing.Start(ctx, "range",
        trace.WithAttributes(
            attribute.String("range_begin", string(r.GetKey())),
            attribute.String("range_end", string(r.GetRangeEnd())),
        ),
    )
    defer span.End()
    // ... operation ...
}
```

---

## Testing Patterns

etcd has a comprehensive testing strategy.

### Table-Driven Tests

```go
func TestPut(t *testing.T) {
    tests := []struct {
        name    string
        key     string
        value   string
        wantErr bool
    }{
        {"simple", "foo", "bar", false},
        {"empty key", "", "bar", true},
        {"large value", "foo", strings.Repeat("x", 1024*1024), false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            _, err := kv.Put(context.Background(), tt.key, tt.value)
            if (err != nil) != tt.wantErr {
                t.Errorf("Put() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

### Leak Detection

Detect goroutine leaks:

```go
// From tests
func TestMain(m *testing.M) {
    testutil.RegisterLeakDetection()
    os.Exit(m.Run())
}
```

### Integration Test Framework

```go
// From tests/integration
func TestV3Auth(t *testing.T) {
    integration.BeforeTest(t)

    clus := integration.NewCluster(t, &integration.ClusterConfig{
        Size:      1,
        AuthToken: "simple",
    })
    defer clus.Terminate(t)

    // Test code using real cluster
}
```

---

## Key Takeaways

1. **Interfaces enable testing** - Every major component has an interface, enabling mocks and fakes.

2. **Patterns reduce bugs** - Consistent patterns (RWMutex, channels, atomics) make concurrency predictable.

3. **Errors have meaning** - Structured errors help clients respond appropriately.

4. **Appliers separate concerns** - Consensus and execution are cleanly separated.

5. **Graceful shutdown is non-trivial** - Track all goroutines and close them cleanly.

6. **Observability is built-in** - Metrics, logging, and tracing are first-class citizens.

---

## Next in the Series

In [Part 4: Extending and Integrating](04-extending-integrating.md), we'll explore:
- Client library architecture
- Distributed primitives (locks, elections)
- Watch patterns
- Transaction usage
- Retry strategies

---

## Explore the Code

Key files for patterns:
- [`pkg/wait/wait.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/pkg/wait/wait.go) - Wait pattern
- [`server/etcdserver/errors/errors.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/etcdserver/errors/errors.go) - Error types
- [`server/etcdserver/apply/apply.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/etcdserver/apply/apply.go) - Applier pattern
- [`server/etcdserver/server.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/etcdserver/server.go) - Shutdown and goroutine management

---

*Next: [Part 4 - Extending and Integrating](04-extending-integrating.md)*

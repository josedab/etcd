# RFC-0003: Server Package Decomposition

**Status:** Draft
**Author:** Claude Code Analysis
**Created:** November 19, 2025
**Category:** Strategic

---

## Summary

Decompose the monolithic `server/etcdserver/server.go` (~75KB) into smaller, domain-specific modules to improve maintainability, testability, and developer onboarding.

---

## Motivation

### Problem

The main server file has grown to approximately 2000+ lines:

```bash
$ wc -l server/etcdserver/server.go
2147 server/etcdserver/server.go
```

This creates several issues:

1. **Cognitive load**: Hard to understand the full file
2. **Merge conflicts**: Multiple developers touching same file
3. **Testing difficulty**: Large surface area to test
4. **Onboarding time**: New contributors struggle to navigate

### Current Structure

The file contains multiple concerns:
- Server lifecycle (start, stop)
- Raft integration
- Request processing
- Cluster membership
- Leader election
- Metrics and monitoring
- Snapshot management
- Linearizable read handling

### Expected Benefits

- 50% reduction in largest file size
- Faster code reviews (smaller PRs)
- Easier testing (isolated concerns)
- Reduced merge conflicts
- Faster developer onboarding

---

## Detailed Design

### Proposed Module Structure

```
server/etcdserver/
├── server.go           # Reduced: lifecycle, coordination (~500 lines)
├── raft_node.go        # Raft integration
├── request_handler.go  # Request routing and processing
├── membership.go       # Cluster membership management
├── linearizable.go     # Linearizable read handling
├── snapshot_mgr.go     # Snapshot management
├── metrics.go          # Metrics (already separate)
├── apply/              # Already extracted
└── internal/
    ├── lifecycle/      # Start/stop/ready coordination
    └── leadership/     # Leader election and transfer
```

### Module Breakdown

#### 1. server.go (Reduced Core)

```go
// server.go - Main coordinator
type EtcdServer struct {
    cfg       ServerConfig
    lg        *zap.Logger

    // Embedded modules
    *raftNode
    *requestHandler
    *membershipMgr
    *snapshotMgr

    // Coordination
    readych   chan struct{}
    stop      chan struct{}
    done      chan struct{}
}

func NewServer(cfg ServerConfig) (*EtcdServer, error) {
    s := &EtcdServer{cfg: cfg}
    s.raftNode = newRaftNode(cfg, s)
    s.requestHandler = newRequestHandler(cfg, s)
    s.membershipMgr = newMembershipMgr(cfg, s)
    s.snapshotMgr = newSnapshotMgr(cfg, s)
    return s, nil
}

func (s *EtcdServer) Start() error { /* coordinate startup */ }
func (s *EtcdServer) Stop()        { /* coordinate shutdown */ }
```

#### 2. raft_node.go

```go
// raft_node.go - Raft integration
type raftNode struct {
    lg *zap.Logger

    node        raft.Node
    raftStorage *raft.MemoryStorage
    storage     Storage
    transport   rafthttp.Transporter

    // Channels
    applyc     chan toApply
    readStateC chan raft.ReadState
    msgSnapC   chan raftpb.Message

    // State
    lead   atomic.Uint64
    term   atomic.Uint64
}

func (r *raftNode) propose(ctx context.Context, data []byte) error { ... }
func (r *raftNode) processMessages() { ... }
func (r *raftNode) applyEntries(ents []raftpb.Entry) { ... }
```

#### 3. request_handler.go

```go
// request_handler.go - Request processing
type requestHandler struct {
    lg      *zap.Logger
    server  *EtcdServer
    applier apply.Applier
    wait    wait.Wait
}

func (h *requestHandler) processInternalRaftRequest(
    ctx context.Context, r pb.InternalRaftRequest) (*Result, error) { ... }

func (h *requestHandler) processRangeRequest(
    ctx context.Context, r *pb.RangeRequest) (*pb.RangeResponse, error) { ... }
```

#### 4. linearizable.go

```go
// linearizable.go - Linearizable read handling
type linearizableReader struct {
    lg     *zap.Logger
    server *EtcdServer

    readMu      sync.RWMutex
    readwaitc   chan struct{}
    readNotifier *notifier
}

func (l *linearizableReader) linearizableReadNotify(ctx context.Context) error { ... }
func (l *linearizableReader) readIndexLoop() { ... }
```

#### 5. snapshot_mgr.go

```go
// snapshot_mgr.go - Snapshot management
type snapshotMgr struct {
    lg      *zap.Logger
    server  *EtcdServer
    storage Storage

    inflightSnapshots atomic.Int64
}

func (s *snapshotMgr) triggerSnapshot() { ... }
func (s *snapshotMgr) applySnapshot(snap raftpb.Snapshot) error { ... }
func (s *snapshotMgr) createSnapshot() (*raftpb.Snapshot, error) { ... }
```

### Interface Definitions

Define clear interfaces between modules:

```go
// interfaces.go
type RaftProposer interface {
    Propose(ctx context.Context, data []byte) error
    ReadIndex(ctx context.Context) error
}

type RequestProcessor interface {
    Process(ctx context.Context, req pb.InternalRaftRequest) (*Result, error)
}

type SnapshotCreator interface {
    TriggerSnapshot()
    CreateSnapshot() (*raftpb.Snapshot, error)
}
```

---

## Implementation Plan

### Phase 1: Extract Low-Risk Modules (Week 1)
- [ ] Extract linearizable read handling
- [ ] Extract snapshot management
- [ ] Add integration tests for extracted modules

### Phase 2: Extract Core Modules (Week 2)
- [ ] Extract Raft node wrapper
- [ ] Extract request handler
- [ ] Update internal references

### Phase 3: Refine Interfaces (Week 3)
- [ ] Define clean interfaces between modules
- [ ] Reduce coupling
- [ ] Add module-level tests

### Phase 4: Documentation and Cleanup (Week 4)
- [ ] Update architecture documentation
- [ ] Add package-level docs
- [ ] Code review and refinement

---

## Backwards Compatibility

**Fully backward compatible:**
- No API changes
- No behavioral changes
- Internal refactoring only

### Verification

- All existing tests must pass
- Benchmark results should be unchanged
- No changes to configuration or command-line flags

---

## Alternatives Considered

### Alternative 1: Leave as-is

**Pros:**
- No risk of regression
- No effort required

**Cons:**
- Technical debt continues to accumulate
- Maintainability worsens over time

**Decision:** Short-term safety isn't worth long-term cost.

### Alternative 2: Complete rewrite

**Pros:**
- Clean slate design
- Optimal architecture

**Cons:**
- High risk of regressions
- Months of effort
- Blocks other development

**Decision:** Incremental refactoring is safer.

### Alternative 3: Use internal packages only

**Pros:**
- Clear visibility boundaries

**Cons:**
- Too many small packages
- Import path complexity

**Decision:** Keep related code in same package, separate files.

---

## Open Questions

1. **Module boundaries**: Are the proposed boundaries correct?
   - Proposed: Review with team after Phase 1

2. **Interface granularity**: How much interface abstraction?
   - Proposed: Minimal, only where needed for testing

3. **Shared state**: How to handle shared state between modules?
   - Proposed: Pass via constructor, avoid globals

---

## Success Criteria

- [ ] `server.go` reduced to <600 lines
- [ ] No single file >800 lines (excluding generated)
- [ ] All existing tests pass
- [ ] No performance regression (benchmark)
- [ ] Each module independently testable

---

## Effort Estimation

**Total: 3-4 weeks**

| Phase | Effort |
|-------|--------|
| Phase 1: Low-risk extraction | 1 week |
| Phase 2: Core modules | 1 week |
| Phase 3: Interface refinement | 0.5 weeks |
| Phase 4: Documentation | 0.5 weeks |
| Buffer for issues | 1 week |

---

## Stakeholder Approvals

- [ ] etcd maintainers
- [ ] Regular contributors (RFC review)

---

## Rollback Strategy

Each phase can be reverted independently:
1. Revert specific PR
2. Previous structure restored
3. No user-facing impact

---

## Testing Strategy

### Unit Tests
- Each module should have independent tests
- Mock interfaces for isolation

### Integration Tests
- Existing integration tests must pass
- Add tests for module interactions

### Benchmarks
- Compare before/after performance
- Ensure no regression

---

## References

- [Refactoring: Improving the Design of Existing Code (Fowler)](https://martinfowler.com/books/refactoring.html)
- [Working Effectively with Legacy Code (Feathers)](https://www.oreilly.com/library/view/working-effectively-with/0131177052/)
- Current server.go: [`server/etcdserver/server.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/etcdserver/server.go)

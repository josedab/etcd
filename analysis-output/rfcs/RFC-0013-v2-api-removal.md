# RFC-0013: Complete V2 API Removal

**Status:** Draft
**Author:** Claude Code Analysis
**Created:** November 19, 2025
**Category:** Strategic (P1 - Technical Debt)

---

## Summary

Complete the removal of the deprecated V2 API from etcd codebase, reducing maintenance burden, security surface area, test complexity, and binary size.

---

## Motivation

### Problem

The V2 API has been deprecated since etcd v3.5 but remains in the codebase:

**Evidence:**
```bash
# V2-related files still present
$ find server/etcdserver/api/v2* -type f | wc -l
21 files

$ wc -l server/etcdserver/api/v2store/*.go
  107,342 total (107KB of legacy code)
```

**V2 Deprecation Framework Exists but Incomplete:**
```go
// From server/config/v2_deprecation.go:24-28
const (
    V2Depr0NotYet                 = "not-yet"
    V2Depr1WriteOnly              = "write-only"
    V2Depr1WriteOnlyDrop          = "write-only-drop-data"
    V2Depr2Gone                   = "gone"  // Not implemented!
)
```

### Remaining V2 Components

1. **V2 Store:** `server/etcdserver/api/v2store/` (21 files)
2. **V2 HTTP API:** `server/etcdserver/api/etcdhttp/`
3. **V2 Stats:** `server/etcdserver/api/v2stats/`
4. **V2 Errors:** `server/etcdserver/api/v2error/`
5. **V2 Configuration:** Support in config parsing
6. **V2 Tests:** Integration and unit tests

### Costs of Keeping V2

1. **Maintenance Burden:**
   - Bug fixes for deprecated code
   - Security patches for old attack surface
   - Documentation maintenance

2. **Test Complexity:**
   - Dual test paths (V2 and V3)
   - Increased CI time
   - More failure modes

3. **Binary Size:**
   - ~5-10% bloat from unused V2 code
   - Longer build times

4. **Security Risk:**
   - Older code = more vulnerabilities
   - V2 HTTP uses different auth model
   - Attack surface for no user benefit

5. **Developer Confusion:**
   - New contributors see V2 code
   - Unclear which API to use
   - Documentation split between versions

### Expected Benefits

- **Remove ~3,000+ lines** of legacy code
- **Reduce binary size** by 5-10%
- **Simplify testing** (single API path)
- **Reduce security surface** area
- **Faster builds** and CI
- **Cleaner codebase** for new contributors

---

## Detailed Design

### 1. Migration Timeline

```
v3.6 (Current): V2 deprecated, write-only mode default
v3.7 (Next):    V2 removal announced, migration guide published
v3.8 (Target):  V2 completely removed from codebase
```

### 2. Components to Remove

#### Phase 1: V2 Store

```bash
# Files to remove
server/etcdserver/api/v2store/
├── doc.go
├── event.go
├── event_history.go
├── event_queue.go
├── metrics.go
├── node.go
├── node_extern.go
├── stats.go
├── store.go
├── store_test.go
├── ttl_key_heap.go
├── watcher.go
├── watcher_hub.go
└── ... (21 files total)
```

**Lines of Code:** ~3,000

**Dependencies:**
- No longer used by V3 API
- Only accessed via deprecated V2 HTTP

#### Phase 2: V2 HTTP API

```bash
# Files to remove/modify
server/etcdserver/api/etcdhttp/
├── client.go          # V2 handlers
├── client_auth.go     # V2 auth
├── metrics.go         # V2 metrics
└── peer.go            # Keep (used by V3)
```

**Modification Strategy:**
- Remove V2-specific handlers
- Keep peer communication (used by Raft)
- Keep metrics endpoint

#### Phase 3: V2 Stats

```bash
# Files to remove
server/etcdserver/api/v2stats/
├── leader.go
├── server.go
└── stats.go
```

Replace with V3 metrics (already available in Prometheus).

#### Phase 4: V2 Configuration

```go
// Remove from server/config/config.go
type ServerConfig struct {
    // REMOVE: V2 specific fields
    // V2Deprecation V2DeprecationEnum
    // EnableV2      bool

    // Keep V3 fields
    // ...
}
```

#### Phase 5: V2 Tests

```bash
# Remove V2 test files
tests/integration/v2store/*
tests/e2e/v2*
server/etcdserver/api/v2store/*_test.go
```

### 3. Removed Interfaces

```go
// These will be removed:

// V2 Store interface
type Store interface {
    Get(nodePath string, recursive, sorted bool) (*Event, error)
    Set(nodePath string, dir bool, value string, expireOpts TTLOptionSet) (*Event, error)
    Update(nodePath string, newValue string, expireOpts TTLOptionSet) (*Event, error)
    Create(nodePath string, dir bool, value string, ...) (*Event, error)
    Delete(nodePath string, dir, recursive bool) (*Event, error)
    // ... more methods
}

// V2 HTTP handlers
func (h *httpAPI) Get(w http.ResponseWriter, r *http.Request)
func (h *httpAPI) Put(w http.ResponseWriter, r *http.Request)
func (h *httpAPI) Post(w http.ResponseWriter, r *http.Request)
func (h *httpAPI) Delete(w http.ResponseWriter, r *http.Request)
```

### 4. Migration Guide

**For Remaining V2 Users:**

```markdown
# V2 to V3 Migration Guide

## Key Differences

| V2 | V3 |
|----|-----|
| HTTP API | gRPC API (with HTTP gateway) |
| Directory structure | Flat key-value with prefix patterns |
| TTL (per key) | Lease (shared across keys) |
| Atomic Compare-And-Swap | Transaction with conditions |
| In-order keys | Sorted keys |

## Migration Examples

### Get Key
V2: `curl http://localhost:2379/v2/keys/foo`
V3: `etcdctl get foo`

### Set Key with TTL
V2: `curl http://localhost:2379/v2/keys/foo -XPUT -d value=bar -d ttl=60`
V3:
```bash
# Grant lease
LEASE=$(etcdctl lease grant 60 | awk '{print $2}')
# Put with lease
etcdctl put foo bar --lease=$LEASE
```

### Watch Key
V2: `curl http://localhost:2379/v2/keys/foo?wait=true`
V3: `etcdctl watch foo`

### Directory (Prefix)
V2: `curl http://localhost:2379/v2/keys/dir?dir=true`
V3: `etcdctl get /dir/ --prefix`
```

### 5. Deprecation Warnings

Before removal, enhance warnings:

```go
// In V2 HTTP handler
func (h *httpAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // Add deprecation header
    w.Header().Set("X-Etcd-Deprecation", "v2 API is deprecated and will be removed in v3.8")
    w.Header().Set("X-Etcd-Migration-Guide", "https://etcd.io/docs/v3.8/migration/v2-to-v3")

    // Log deprecation warning
    h.lg.Warn("V2 API accessed",
        zap.String("endpoint", r.URL.Path),
        zap.String("client", r.RemoteAddr),
    )

    // Increment deprecation metric
    v2DeprecationCounter.Inc()

    // Proceed with handler
    // ...
}
```

### 6. Feature Flag for Safety

```go
// Allow disabling V2 early for testing
type Config struct {
    // Default: false (V2 disabled)
    EnableV2API bool
}

// In server startup
if !cfg.EnableV2API {
    lg.Info("V2 API disabled (will be removed in v3.8)")
    // Skip V2 handler registration
    return
}
```

---

## Implementation Plan

### Phase 1: Preparation (v3.7 - Weeks 1-2)
- [ ] Publish migration guide
- [ ] Add deprecation warnings to V2 API
- [ ] Metrics for V2 usage tracking
- [ ] Email campaigns to announce removal

### Phase 2: Deprecation Period (v3.7 - 6 months)
- [ ] Monitor V2 usage via metrics
- [ ] Support users migrating to V3
- [ ] Address migration blockers

### Phase 3: Removal (v3.8 - Weeks 1-4)
- [ ] Remove V2 store code
- [ ] Remove V2 HTTP handlers
- [ ] Remove V2 stats
- [ ] Remove V2 tests
- [ ] Update documentation
- [ ] Update examples

### Phase 4: Cleanup (v3.8 - Week 5)
- [ ] Remove V2 dependencies
- [ ] Binary size verification
- [ ] Performance regression testing
- [ ] Release notes

---

## Backwards Compatibility

**Breaking Change:** Yes, this is intentionally breaking.

### Migration Path

1. **v3.6 (Current):**
   - V2 write-only by default
   - Fully functional V3

2. **v3.7 (Next release):**
   - V2 disabled by default (`--enable-v2=false`)
   - Can be enabled with flag (opt-in)
   - Migration guide published
   - 6-month migration period

3. **v3.8 (Target):**
   - V2 completely removed
   - No opt-in available

### For Users Still on V2

**Options:**

1. **Migrate to V3** (recommended)
   - Follow migration guide
   - Use etcdctl v3
   - Update application code

2. **Stay on v3.6** (temporary)
   - Security patches only
   - Not recommended for new deployments

3. **Fork etcd** (not recommended)
   - Maintain V2 yourself
   - Miss out on etcd improvements

---

## Risk Assessment

### High Risks

1. **User Adoption Lag:**
   - Some users may still use V2
   - **Mitigation:** 6-month deprecation period, active communication

2. **Undiscovered V2 Dependencies:**
   - Tools/libraries relying on V2
   - **Mitigation:** Survey ecosystem, provide migration help

### Medium Risks

3. **Migration Complexity:**
   - Some V2 features hard to map to V3
   - **Mitigation:** Comprehensive migration guide

### Low Risks

4. **Regression Introduction:**
   - Removal might break V3
   - **Mitigation:** Extensive testing, gradual removal

---

## Alternatives Considered

### Alternative 1: Keep V2 Forever

**Cons:**
- Permanent maintenance burden
- Security risk
- Code bloat

**Decision:** Not sustainable.

### Alternative 2: V2 Compatibility Layer

**Approach:** Translate V2 calls to V3.

**Cons:**
- Still need to maintain translation
- Performance overhead
- Complexity

**Decision:** Doesn't solve technical debt.

### Alternative 3: Extract V2 to Plugin

**Approach:** Make V2 an optional plugin.

**Cons:**
- Plugin interface complexity
- Still need to maintain V2 code
- Binary still bloated if loaded

**Decision:** Doesn't reduce burden enough.

---

## Success Criteria

- [ ] V2 code completely removed from main branch
- [ ] Binary size reduced by 5-10%
- [ ] Test suite runs 10% faster
- [ ] No V2 API endpoints accessible
- [ ] Migration guide used successfully by 10+ organizations
- [ ] Zero critical issues from removal

---

## Effort Estimation

**Total: 4-5 weeks**

| Phase | Effort |
|-------|--------|
| Migration guide | 1 week |
| Code removal | 2 weeks |
| Testing and validation | 1 week |
| Documentation updates | 0.5 weeks |
| Buffer | 0.5 weeks |

**Team:** 1-2 engineers

---

## Stakeholder Approvals

- [ ] etcd maintainers
- [ ] Major users (Kubernetes, CoreOS, etc.)
- [ ] Community RFC discussion
- [ ] User survey on V2 usage

---

## Communication Plan

### 6 Months Before Removal (v3.7 release)

1. **Blog Post:** "V2 API Removal Timeline"
2. **Mailing List:** Announcement to etcd-dev
3. **Social Media:** Twitter, Reddit posts
4. **Documentation:** Large banner on V2 docs

### 3 Months Before

1. **Reminder Blog Post**
2. **User Survey:** Who's still on V2?
3. **Office Hours:** Help users migrate

### 1 Month Before

1. **Final Warning**
2. **Migration success stories**
3. **Release candidate with V2 removed**

### At Removal (v3.8 release)

1. **Release Notes:** Clear breaking change notice
2. **Migration guide linked prominently**
3. **Support channels for migration issues**

---

## Rollback Strategy

**No rollback planned** - this is intentional removal.

If critical issues discovered:
1. Emergency patch for v3.8.1
2. Restore minimal V2 read-only mode
3. Extend deprecation period
4. Fix migration blockers

---

## Metrics to Track

```go
var (
    v2APIAccess = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "etcd_v2_api_access_total",
            Help: "V2 API access count (pre-removal)",
        },
        []string{"endpoint"},
    )

    v2UsersActive = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "etcd_v2_active_users",
            Help: "Unique IPs accessing V2 API",
        },
    )
)
```

Track for 6 months before removal to understand impact.

---

## References

- [V2 Deprecation Announcement](https://etcd.io/blog/2021/v2-deprecation/)
- [V3 API Overview](https://etcd.io/docs/current/learning/api/)
- [Python 2 EOL](https://www.python.org/doc/sunset-python-2/) - Similar deprecation case study
- Current V2 code: [`server/etcdserver/api/v2store/`](https://github.com/etcd-io/etcd/tree/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/etcdserver/api/v2store)

# RFC-0006: Protocol Buffer Library Migration

**Status:** Draft
**Author:** Claude Code Analysis
**Created:** November 19, 2025
**Category:** Long-term

---

## Summary

Migrate from the deprecated gogo/protobuf library to the official google.golang.org/protobuf library to ensure continued security patches and maintenance.

---

## Motivation

### Problem

etcd currently uses gogo/protobuf v1.3.2:

```go
// Current usage throughout codebase
import "github.com/gogo/protobuf/proto"
```

**Issues:**

1. **Deprecated**: gogo/protobuf is in maintenance mode, no new features
2. **Security risk**: Won't receive security patches
3. **Ecosystem drift**: New tools target official protobuf
4. **Dual libraries**: Both gogo and official in use, causing confusion

### Impact of Inaction

- Potential unpatched vulnerabilities
- Increasing technical debt
- Harder integration with modern Go ecosystem
- Confusion for contributors

### Expected Benefits

- Continued security patches
- Better tooling compatibility
- Simplified dependency tree
- Cleaner codebase (single protobuf library)

---

## Detailed Design

### Migration Strategy

**Phased approach** to minimize risk:

1. Audit all gogo usage
2. Update code generation
3. Migrate internal usage
4. Update wire format (if needed)
5. Remove gogo dependency

### Phase 1: Audit

Identify all gogo/protobuf usage:

```bash
# Find imports
grep -r "gogo/protobuf" --include="*.go" .

# Find generated files
find . -name "*.pb.go" -exec grep -l "gogo" {} \;
```

**Expected locations:**
- `api/*/` - Generated protocol buffers
- `server/storage/wal/walpb/`
- `server/lease/leasepb/`
- Internal marshal/unmarshal calls

### Phase 2: Update Generation

Modify protoc generation:

**Before:**
```bash
protoc --gogo_out=plugins=grpc:. *.proto
```

**After:**
```bash
protoc --go_out=. --go-grpc_out=. *.proto
```

Update `scripts/genproto.sh`:

```bash
# Install tools
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Generate
protoc \
  --go_out=. --go_opt=paths=source_relative \
  --go-grpc_out=. --go-grpc_opt=paths=source_relative \
  *.proto
```

### Phase 3: Update .proto Files

Remove gogo-specific options:

```protobuf
// Before
import "github.com/gogo/protobuf/gogoproto/gogo.proto";

message KeyValue {
    option (gogoproto.goproto_stringer) = false;
    // ...
}

// After
message KeyValue {
    // Standard protobuf, no gogo options
}
```

### Phase 4: Update Import Paths

```go
// Before
import "github.com/gogo/protobuf/proto"

// After
import "google.golang.org/protobuf/proto"
```

### Phase 5: Handle API Differences

gogo and official have API differences:

```go
// Before (gogo)
size := proto.Size(msg)
data, _ := proto.Marshal(msg)

// After (official)
size := proto.Size(msg)
data, _ := proto.Marshal(msg)  // Same API, different import

// Before (gogo extensions)
proto.Merge(dst, src)

// After (official)
proto.Merge(dst, src)  // Same
```

**Significant differences:**

```go
// Before (gogo)
import "github.com/gogo/protobuf/types"
ts := types.TimestampNow()

// After (official)
import "google.golang.org/protobuf/types/known/timestamppb"
ts := timestamppb.Now()
```

### Phase 6: Wire Compatibility

Ensure binary compatibility:

```go
// Test wire compatibility
func TestWireCompatibility(t *testing.T) {
    // Marshal with old code
    oldData, _ := oldproto.Marshal(oldMsg)

    // Unmarshal with new code
    newMsg := &NewType{}
    err := newproto.Unmarshal(oldData, newMsg)
    require.NoError(t, err)

    // Verify fields
    assert.Equal(t, oldMsg.Field, newMsg.Field)
}
```

---

## Implementation Plan

### Phase 1: Preparation (Weeks 1-2)
- [ ] Complete audit of gogo usage
- [ ] Document all affected files
- [ ] Set up compatibility tests

### Phase 2: Generation Update (Weeks 3-4)
- [ ] Update genproto.sh
- [ ] Regenerate all .pb.go files
- [ ] Fix compilation errors

### Phase 3: Internal Migration (Weeks 5-6)
- [ ] Update marshal/unmarshal calls
- [ ] Update type references
- [ ] Fix test failures

### Phase 4: Testing (Weeks 7-8)
- [ ] Wire compatibility tests
- [ ] Integration tests
- [ ] Performance benchmarks

### Phase 5: Cleanup (Week 9)
- [ ] Remove gogo dependency
- [ ] Update documentation
- [ ] Final review

---

## Backwards Compatibility

### Wire Format

**Must maintain binary compatibility:**

- Existing data in WAL must remain readable
- Existing snapshots must remain loadable
- Client-server communication must work across versions

### Compatibility Testing

```go
func TestWALCompatibility(t *testing.T) {
    // Load WAL written with old library
    wal, err := Open("testdata/v3.5-wal")
    require.NoError(t, err)

    // Read and verify
    entries, err := wal.ReadAll()
    require.NoError(t, err)
    require.NotEmpty(t, entries)
}
```

### Rolling Upgrade

Support mixed clusters during upgrade:
- Old server ↔ New server: Must work
- Old client ↔ New server: Must work
- New client ↔ Old server: Must work

---

## Alternatives Considered

### Alternative 1: Keep gogo/protobuf

**Pros:**
- No migration effort
- No risk of regression

**Cons:**
- Security risk increases over time
- Technical debt accumulates

**Decision:** Risk of inaction exceeds migration cost.

### Alternative 2: vtprotobuf

**Pros:**
- Better performance than both
- Active development

**Cons:**
- Another migration later
- Less ecosystem support

**Decision:** Official library is safer choice.

### Alternative 3: Big-bang migration

**Pros:**
- Faster completion

**Cons:**
- High risk
- Blocks other development

**Decision:** Phased approach reduces risk.

---

## Open Questions

1. **Performance impact**: Is official protobuf slower?
   - Proposed: Benchmark and accept if <5% regression

2. **Custom options**: What happens to gogo options?
   - Proposed: Remove most, they were optimizations

3. **Timeline**: How long for full migration?
   - Proposed: 6-8 weeks with dedicated effort

---

## Success Criteria

- [ ] Zero gogo/protobuf imports
- [ ] All tests passing
- [ ] Binary compatibility verified
- [ ] No performance regression >5%
- [ ] Rolling upgrade tested

---

## Effort Estimation

**Total: 6-8 weeks**

| Phase | Effort |
|-------|--------|
| Audit and preparation | 1 week |
| Generation update | 1 week |
| Internal migration | 2 weeks |
| Testing | 2 weeks |
| Cleanup and docs | 1 week |
| Buffer | 1 week |

---

## Risk Assessment

### High Risks

1. **Wire incompatibility**: Could cause data loss
   - Mitigation: Extensive compatibility testing

2. **Performance regression**: Could affect production
   - Mitigation: Benchmark before merge

3. **Subtle bugs**: Proto behavior differences
   - Mitigation: Thorough code review

---

## Stakeholder Approvals

- [ ] etcd maintainers
- [ ] API SIG
- [ ] Community review (RFC discussion)

---

## Rollback Strategy

### Pre-merge
- Don't merge until all tests pass
- Keep gogo version in branch for comparison

### Post-merge
If critical issues:
1. Revert merge commit
2. Re-add gogo dependency
3. Regenerate with gogo

---

## References

- [gogo/protobuf deprecation](https://github.com/gogo/protobuf/issues/691)
- [Official protobuf Go API](https://pkg.go.dev/google.golang.org/protobuf)
- [Migration guide](https://go.dev/blog/protobuf-apiv2)
- [Wire compatibility](https://developers.google.com/protocol-buffers/docs/encoding)

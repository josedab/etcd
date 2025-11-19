# RFC-0005: Structured Error Context Propagation

**Status:** Draft
**Author:** Claude Code Analysis
**Created:** November 19, 2025
**Category:** Strategic

---

## Summary

Enhance etcd's error handling to include structured context (request ID, operation type, timing) that propagates from server to client, improving debuggability and observability.

---

## Motivation

### Problem

Current errors lack context:

```go
// Server returns
return nil, errors.ErrTimeout

// Client receives
error: etcdserver: request timed out
```

Missing information:
- Which request timed out?
- How long did it take?
- Which member was contacted?
- What operation was attempted?

### Debugging Challenges

1. **Log correlation**: Can't match client error to server logs
2. **Root cause**: Unclear if timeout was network, disk, or consensus
3. **Metrics gap**: Can't attribute errors to specific operations

### Expected Benefits

- Faster incident resolution (hours → minutes)
- Better error metrics (by operation, member, cause)
- Improved client retry logic (context-aware)

---

## Detailed Design

### 1. Error Context Structure

Define structured error context:

```go
// api/v3rpc/rpctypes/error.go
type ErrorContext struct {
    RequestID   string        `json:"request_id,omitempty"`
    MemberID    uint64        `json:"member_id,omitempty"`
    Operation   string        `json:"operation,omitempty"`
    Key         string        `json:"key,omitempty"`
    Duration    time.Duration `json:"duration,omitempty"`
    LeaderID    uint64        `json:"leader_id,omitempty"`
    Cause       string        `json:"cause,omitempty"`
    RetryAfter  time.Duration `json:"retry_after,omitempty"`
}
```

### 2. gRPC Metadata Propagation

Use gRPC metadata for context:

```go
// Server side
func (s *kvServer) Put(ctx context.Context, r *pb.PutRequest) (*pb.PutResponse, error) {
    start := time.Now()
    requestID := generateRequestID()

    resp, err := s.kv.Put(ctx, r)
    if err != nil {
        errCtx := &rpctypes.ErrorContext{
            RequestID: requestID,
            MemberID:  uint64(s.member.ID()),
            Operation: "Put",
            Key:       string(r.Key),
            Duration:  time.Since(start),
            LeaderID:  s.leader.ID(),
            Cause:     classifyError(err),
        }

        // Attach context to gRPC error
        return nil, rpctypes.ErrorWithContext(err, errCtx)
    }

    return resp, nil
}
```

### 3. Error Classification

Classify errors by cause:

```go
func classifyError(err error) string {
    switch err {
    case errors.ErrTimeout:
        return "timeout"
    case errors.ErrNoLeader:
        return "no_leader"
    case errors.ErrNoSpace:
        return "quota_exceeded"
    case errors.ErrCorrupt:
        return "corruption"
    default:
        if strings.Contains(err.Error(), "connection") {
            return "connection_lost"
        }
        return "unknown"
    }
}
```

### 4. gRPC Status Details

Attach context via gRPC status details:

```go
func ErrorWithContext(err error, ctx *ErrorContext) error {
    st := status.New(errorToCode(err), err.Error())

    // Serialize context to proto
    detail := &pb.ErrorDetail{
        RequestId:  ctx.RequestID,
        MemberId:   ctx.MemberID,
        Operation:  ctx.Operation,
        DurationMs: int64(ctx.Duration / time.Millisecond),
        LeaderId:   ctx.LeaderID,
        Cause:      ctx.Cause,
    }

    st, _ = st.WithDetails(detail)
    return st.Err()
}
```

### 5. Client-Side Extraction

Extract context in client:

```go
// client/v3/error.go
func ExtractErrorContext(err error) (*ErrorContext, bool) {
    st := status.Convert(err)
    for _, detail := range st.Details() {
        if errDetail, ok := detail.(*pb.ErrorDetail); ok {
            return &ErrorContext{
                RequestID:  errDetail.RequestId,
                MemberID:   errDetail.MemberId,
                Operation:  errDetail.Operation,
                Duration:   time.Duration(errDetail.DurationMs) * time.Millisecond,
                LeaderID:   errDetail.LeaderId,
                Cause:      errDetail.Cause,
            }, true
        }
    }
    return nil, false
}

// Usage
_, err := cli.Put(ctx, "key", "value")
if err != nil {
    if ctx, ok := clientv3.ExtractErrorContext(err); ok {
        log.Printf("Request %s to member %d failed after %v: %s",
            ctx.RequestID, ctx.MemberID, ctx.Duration, ctx.Cause)
    }
}
```

### 6. Protocol Buffer Definition

Add to API:

```protobuf
// api/etcdserverpb/error.proto
message ErrorDetail {
    string request_id = 1;
    uint64 member_id = 2;
    string operation = 3;
    string key = 4;
    int64 duration_ms = 5;
    uint64 leader_id = 6;
    string cause = 7;
    int64 retry_after_ms = 8;
}
```

---

## Example Usage

### Before

```go
_, err := cli.Put(ctx, "key", "value")
if err != nil {
    log.Printf("put failed: %v", err)
    // Output: put failed: etcdserver: request timed out
}
```

### After

```go
_, err := cli.Put(ctx, "key", "value")
if err != nil {
    if ectx, ok := clientv3.ExtractErrorContext(err); ok {
        log.Printf("Request %s to member %d failed: %v (cause: %s, duration: %v)",
            ectx.RequestID, ectx.MemberID, err, ectx.Cause, ectx.Duration)
        // Output: Request abc123 to member 1 failed: etcdserver: request timed out
        //         (cause: disk_slow, duration: 5.2s)
    }
}
```

---

## Implementation Plan

### Phase 1: Protocol and Types (Week 1)
- [ ] Define ErrorDetail proto
- [ ] Add ErrorContext struct
- [ ] Generate code

### Phase 2: Server Integration (Week 1-2)
- [ ] Add context to KV operations
- [ ] Add context to other RPCs
- [ ] Error classification logic

### Phase 3: Client Support (Week 2)
- [ ] Context extraction helpers
- [ ] Documentation
- [ ] Examples

---

## Backwards Compatibility

**Fully backward compatible:**

- Old clients ignore unknown status details
- Old servers don't send details (clients handle gracefully)
- Error messages unchanged

```go
// Client handles missing context gracefully
if ectx, ok := clientv3.ExtractErrorContext(err); ok {
    // Use context
} else {
    // Fall back to basic error
}
```

---

## Alternatives Considered

### Alternative 1: Custom error types

```go
type EtcdError struct {
    Err     error
    Context ErrorContext
}
```

**Cons:**
- Doesn't survive gRPC boundary well
- Type assertion issues

**Decision:** Use gRPC status details for proper propagation.

### Alternative 2: Error string encoding

```go
return fmt.Errorf("timeout [req=%s member=%d]: %w", reqID, memberID, err)
```

**Cons:**
- Parsing is fragile
- Not machine-readable

**Decision:** Structured proto is more reliable.

---

## Open Questions

1. **Request ID format**: UUID vs shorter format?
   - Proposed: 16-char hex (shorter, still unique enough)

2. **Key in context**: Security concern for sensitive keys?
   - Proposed: Truncate to first 50 chars, omit in auth errors

3. **Retry hints**: Should server suggest retry timing?
   - Proposed: Yes, for quota and rate-limit errors

---

## Success Criteria

- [ ] 90% of errors include context
- [ ] Request ID enables log correlation
- [ ] Error classification covers common cases
- [ ] Client helpers documented

---

## Effort Estimation

**Total: 2 weeks**

| Task | Effort |
|------|--------|
| Protocol definition | 1 day |
| Server integration | 3 days |
| Client support | 2 days |
| Testing | 2 days |
| Documentation | 1 day |

---

## Stakeholder Approvals

- [ ] etcd maintainers
- [ ] API SIG (proto changes)

---

## Rollback Strategy

1. Server: Don't attach details (no code path)
2. Client: Ignore details (already handles)
3. No breaking changes, can disable per-operation

---

## References

- [gRPC Status with Details](https://grpc.io/docs/guides/status-codes/)
- [Error Handling Best Practices](https://cloud.google.com/apis/design/errors)
- Current errors: [`server/etcdserver/errors/errors.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/etcdserver/errors/errors.go)

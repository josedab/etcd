# Structured Error Context

This document describes the structured error context feature introduced in etcd v3.6.

## Overview

When errors occur in etcd, they now include structured context that provides debugging information such as:

- Request ID for log correlation
- Member ID that processed the request
- Operation type (Put, Range, etc.)
- Duration before failure
- Leader ID at time of error
- Error classification (cause)
- Retry suggestions

## Client Usage

### Extracting Error Context

```go
import (
    "log"
    clientv3 "go.etcd.io/etcd/client/v3"
)

cli, _ := clientv3.New(clientv3.Config{...})
_, err := cli.Put(ctx, "key", "value")
if err != nil {
    if ectx := clientv3.ExtractErrorContext(err); ectx != nil {
        log.Printf("Request %s to member %d failed after %v: %s",
            ectx.RequestID, ectx.MemberID, ectx.Duration, ectx.Cause)
    }
}
```

### Checking for Retry

```go
if shouldRetry, wait := clientv3.ShouldRetry(err); shouldRetry {
    time.Sleep(wait)
    // Retry operation
}
```

### Using the Bool Pattern

```go
if ectx, ok := clientv3.ErrorContextFromError(err); ok {
    // Use context
} else {
    // Fall back to basic error handling
}
```

## Error Causes

The `Cause` field contains a machine-readable error classification:

| Cause | Description |
|-------|-------------|
| `timeout` | Operation timed out |
| `no_leader` | No cluster leader |
| `not_leader` | Request sent to non-leader |
| `quota_exceeded` | Database space exceeded |
| `corruption` | Cluster corruption detected |
| `connection_lost` | Network connection lost |
| `leader_changed` | Leader changed during operation |
| `compacted` | Required revision compacted |
| `auth_failed` | Authentication failed |
| `permission_denied` | Permission denied |
| `invalid_request` | Invalid request parameters |
| `server_stopped` | Server is stopped |
| `unknown` | Unknown error |

## Backward Compatibility

This feature is fully backward compatible:

- Old clients ignore the additional status details
- Old servers don't send details (clients handle gracefully)
- Error messages remain unchanged

Clients should always check if context is present:

```go
if ectx := clientv3.ExtractErrorContext(err); ectx != nil {
    // Use context
} else {
    // Handle as before
}
```

## Server-Side Details

The error context is attached to gRPC errors as status details using the `ErrorDetail` protocol buffer message. This is automatically handled by the v3rpc layer for KV operations.

### Operations with Context

- Put
- Range (Get)
- DeleteRange
- Txn
- Compact

## Best Practices

1. **Always check for context presence** - Not all errors will have context
2. **Use request ID for log correlation** - Search server logs using the request ID
3. **Respect retry suggestions** - The server provides informed retry timing
4. **Log context on failures** - Greatly improves debugging
5. **Don't rely on key for sensitive operations** - Keys in auth errors are omitted

## Implementation Notes

- Request IDs are 16-character hex strings (8 random bytes)
- Keys are truncated to 50 characters for security
- Keys in auth-related errors are completely omitted
- Duration is measured from request start to error return

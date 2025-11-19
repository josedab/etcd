# Extending and Integrating etcd

*Part 4 of the etcd Deep Dive Series*

**Analysis Commit:** [`d6bc3229d81384aed0fc03a3ddd3dccfef092048`](https://github.com/etcd-io/etcd/tree/d6bc3229d81384aed0fc03a3ddd3dccfef092048)

---

## What You'll Learn

- Client library architecture and usage
- Distributed primitives (locks, elections, semaphores)
- Watch patterns for reliable event handling
- Transactions for atomic operations
- Retry strategies and error handling

---

## Introduction

Understanding etcd internals is valuable, but most of us interact with etcd through its client library. Let's explore how to use etcd effectively—from basic operations to sophisticated distributed coordination patterns.

---

## Client Library Architecture

The Go client library lives in [`client/v3/`](https://github.com/etcd-io/etcd/tree/d6bc3229d81384aed0fc03a3ddd3dccfef092048/client/v3).

### Client Structure

From [`client/v3/client.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/client/v3/client.go):

```go
type Client struct {
    Cluster       // Cluster management
    KV            // Key-value operations
    Lease         // Lease management
    Watcher       // Watch streams
    Auth          // Authentication
    Maintenance   // Maintenance operations

    conn *grpc.ClientConn

    cfg      Config
    creds    grpcCredentialsProvider
    resolver *resolver.EtcdManualResolver
    mu       *sync.RWMutex

    ctx       context.Context
    cancel    context.CancelFunc
    Username  string
    Password  string
    authTokenBundle credentials.PerRPCCredentials

    callOpts []grpc.CallOption
    lgMu     *sync.RWMutex
    lg       *zap.Logger
}
```

### Creating a Client

```go
import clientv3 "go.etcd.io/etcd/client/v3"

cli, err := clientv3.New(clientv3.Config{
    Endpoints:   []string{"localhost:2379"},
    DialTimeout: 5 * time.Second,
    Username:    "root",          // Optional
    Password:    "password",      // Optional
    TLS:         tlsConfig,       // Optional
})
if err != nil {
    log.Fatal(err)
}
defer cli.Close()
```

### Key Configuration Options

```go
type Config struct {
    Endpoints            []string              // etcd server addresses
    AutoSyncInterval     time.Duration         // Sync member list periodically
    DialTimeout          time.Duration         // Timeout for connection
    DialKeepAliveTime    time.Duration         // Keepalive interval
    DialKeepAliveTimeout time.Duration         // Keepalive timeout
    MaxCallSendMsgSize   int                   // Max gRPC send size
    MaxCallRecvMsgSize   int                   // Max gRPC receive size
    TLS                  *tls.Config           // TLS configuration
    Username             string                // Authentication
    Password             string
    RejectOldCluster     bool                  // Reject old cluster versions
    DialOptions          []grpc.DialOption     // Custom gRPC options
    Context              context.Context       // Base context
    Logger               *zap.Logger           // Custom logger
    LogConfig            *zap.Config           // Logger config
    PermitWithoutStream  bool                  // Allow keepalive without stream
}
```

---

## Basic Operations

### Put and Get

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

// Put
_, err := cli.Put(ctx, "key", "value")
if err != nil {
    log.Fatal(err)
}

// Get
resp, err := cli.Get(ctx, "key")
if err != nil {
    log.Fatal(err)
}

for _, kv := range resp.Kvs {
    fmt.Printf("%s = %s\n", kv.Key, kv.Value)
}
```

### Get Options

```go
// Get with prefix (all keys starting with "prefix/")
resp, _ := cli.Get(ctx, "prefix/", clientv3.WithPrefix())

// Get with range (keys from "a" to "z")
resp, _ := cli.Get(ctx, "a", clientv3.WithRange("z"))

// Get with limit
resp, _ := cli.Get(ctx, "prefix/", clientv3.WithPrefix(), clientv3.WithLimit(10))

// Get keys only (no values)
resp, _ := cli.Get(ctx, "prefix/", clientv3.WithPrefix(), clientv3.WithKeysOnly())

// Get at specific revision
resp, _ := cli.Get(ctx, "key", clientv3.WithRev(100))

// Serializable read (may return stale data, but faster)
resp, _ := cli.Get(ctx, "key", clientv3.WithSerializable())

// Sort results
resp, _ := cli.Get(ctx, "prefix/",
    clientv3.WithPrefix(),
    clientv3.WithSort(clientv3.SortByKey, clientv3.SortAscend))
```

### Delete

```go
// Delete single key
resp, err := cli.Delete(ctx, "key")
fmt.Println("Deleted:", resp.Deleted)

// Delete with prefix
resp, err := cli.Delete(ctx, "prefix/", clientv3.WithPrefix())

// Delete and return previous value
resp, err := cli.Delete(ctx, "key", clientv3.WithPrevKV())
if len(resp.PrevKvs) > 0 {
    fmt.Println("Previous value:", string(resp.PrevKvs[0].Value))
}
```

---

## Leases

Leases provide TTL-based key expiration.

### Basic Lease Usage

```go
// Grant a lease with 10-second TTL
lease, err := cli.Grant(ctx, 10)
if err != nil {
    log.Fatal(err)
}

// Put a key with the lease
_, err = cli.Put(ctx, "ephemeral-key", "value", clientv3.WithLease(lease.ID))

// Key will be deleted after 10 seconds unless renewed
```

### Keep-Alive

```go
// Automatic keep-alive
lease, _ := cli.Grant(ctx, 10)

// This returns a channel that must be read to keep the lease alive
keepAliveChan, err := cli.KeepAlive(ctx, lease.ID)
if err != nil {
    log.Fatal(err)
}

// Read keep-alive responses (or they'll block)
go func() {
    for ka := range keepAliveChan {
        if ka == nil {
            // Lease expired or revoked
            return
        }
        fmt.Println("TTL renewed:", ka.TTL)
    }
}()

// Put with lease
cli.Put(ctx, "service/node1", "alive", clientv3.WithLease(lease.ID))
```

### Single Keep-Alive

```go
// Single renewal (for precise control)
resp, err := cli.KeepAliveOnce(ctx, lease.ID)
if err != nil {
    log.Fatal(err)
}
fmt.Println("TTL:", resp.TTL)
```

### Revoke Lease

```go
// Explicitly revoke (deletes all attached keys)
_, err := cli.Revoke(ctx, lease.ID)
```

---

## Watches

Watches subscribe to key changes.

### Basic Watch

```go
watchChan := cli.Watch(ctx, "key")

for watchResp := range watchChan {
    for _, event := range watchResp.Events {
        switch event.Type {
        case clientv3.EventTypePut:
            fmt.Printf("PUT %s = %s\n", event.Kv.Key, event.Kv.Value)
        case clientv3.EventTypeDelete:
            fmt.Printf("DELETE %s\n", event.Kv.Key)
        }
    }
}
```

### Watch Options

```go
// Watch with prefix
watchChan := cli.Watch(ctx, "prefix/", clientv3.WithPrefix())

// Watch starting from revision (for resuming)
watchChan := cli.Watch(ctx, "key", clientv3.WithRev(lastRevision+1))

// Watch with previous key-value (for deletes)
watchChan := cli.Watch(ctx, "key", clientv3.WithPrevKV())

// Watch for progress notifications
watchChan := cli.Watch(ctx, "key", clientv3.WithProgressNotify())

// Watch with filter (only puts or only deletes)
watchChan := cli.Watch(ctx, "key", clientv3.WithFilterPut())    // Only deletes
watchChan := cli.Watch(ctx, "key", clientv3.WithFilterDelete()) // Only puts
```

### Reliable Watch Pattern

For production, handle disconnections:

```go
func watchWithRetry(cli *clientv3.Client, key string) {
    var revision int64

    for {
        var opts []clientv3.OpOption
        opts = append(opts, clientv3.WithPrefix())
        if revision > 0 {
            opts = append(opts, clientv3.WithRev(revision))
        }

        watchChan := cli.Watch(context.Background(), key, opts...)

        for watchResp := range watchChan {
            if watchResp.Canceled {
                // Watch was canceled, check error
                if watchResp.Err() != nil {
                    log.Println("watch error:", watchResp.Err())
                }
                break
            }

            if watchResp.IsProgressNotify() {
                // Update revision even without events
                revision = watchResp.Header.Revision
                continue
            }

            for _, event := range watchResp.Events {
                // Process event
                revision = event.Kv.ModRevision
            }
        }

        // Reconnect after brief delay
        time.Sleep(time.Second)
    }
}
```

---

## Transactions

Transactions provide atomic multi-key operations with conditions.

### Transaction Structure

```go
txn := cli.Txn(ctx).
    If(/* conditions */).
    Then(/* operations if conditions true */).
    Else(/* operations if conditions false */)

resp, err := txn.Commit()
```

### Compare Operations

```go
// Compare value
clientv3.Compare(clientv3.Value("key"), "=", "expected")
clientv3.Compare(clientv3.Value("key"), "!=", "value")
clientv3.Compare(clientv3.Value("key"), ">", "value")

// Compare version (modification count)
clientv3.Compare(clientv3.Version("key"), "=", 0)  // Key doesn't exist
clientv3.Compare(clientv3.Version("key"), ">", 0)  // Key exists

// Compare mod revision
clientv3.Compare(clientv3.ModRevision("key"), "=", revision)

// Compare create revision
clientv3.Compare(clientv3.CreateRevision("key"), "=", 0)  // Key doesn't exist
```

### Operations

```go
clientv3.OpPut("key", "value")
clientv3.OpGet("key")
clientv3.OpDelete("key")
clientv3.OpTxn(/* nested transaction */)
```

### Example: Compare-and-Swap

```go
// Only set if value equals expected
resp, err := cli.Txn(ctx).
    If(clientv3.Compare(clientv3.Value("counter"), "=", "5")).
    Then(clientv3.OpPut("counter", "6")).
    Else(clientv3.OpGet("counter")).
    Commit()

if resp.Succeeded {
    fmt.Println("Counter updated to 6")
} else {
    // Get current value from else clause
    kv := resp.Responses[0].GetResponseRange().Kvs[0]
    fmt.Println("Current value:", string(kv.Value))
}
```

### Example: Create If Not Exists

```go
resp, err := cli.Txn(ctx).
    If(clientv3.Compare(clientv3.CreateRevision("lock"), "=", 0)).
    Then(clientv3.OpPut("lock", "holder")).
    Else(clientv3.OpGet("lock")).
    Commit()

if resp.Succeeded {
    fmt.Println("Lock acquired")
} else {
    kv := resp.Responses[0].GetResponseRange().Kvs[0]
    fmt.Println("Lock held by:", string(kv.Value))
}
```

---

## Distributed Primitives

The [`client/v3/concurrency`](https://github.com/etcd-io/etcd/tree/d6bc3229d81384aed0fc03a3ddd3dccfef092048/client/v3/concurrency) package provides higher-level coordination primitives.

### Session

A session combines a client with a lease:

```go
import "go.etcd.io/etcd/client/v3/concurrency"

session, err := concurrency.NewSession(cli, concurrency.WithTTL(10))
if err != nil {
    log.Fatal(err)
}
defer session.Close()

// Session provides a lease ID
fmt.Println("Lease ID:", session.Lease())
```

### Mutex (Distributed Lock)

```go
mutex := concurrency.NewMutex(session, "/locks/my-lock")

// Acquire lock
if err := mutex.Lock(ctx); err != nil {
    log.Fatal(err)
}
defer mutex.Unlock(ctx)

// Critical section
doWork()
```

### TryLock (Non-blocking)

```go
mutex := concurrency.NewMutex(session, "/locks/my-lock")

if err := mutex.TryLock(ctx); err != nil {
    if err == concurrency.ErrLocked {
        fmt.Println("Lock is held by someone else")
        return
    }
    log.Fatal(err)
}
defer mutex.Unlock(ctx)

doWork()
```

### How Mutex Works

From [`client/v3/concurrency/mutex.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/client/v3/concurrency/mutex.go):

```go
func (m *Mutex) Lock(ctx context.Context) error {
    // Create a unique key under the lock prefix
    m.myKey = fmt.Sprintf("%s%x", m.pfx, m.s.Lease())

    // Put key with lease (creates if not exists)
    resp, err := m.s.Client().Txn(ctx).
        If(v3.Compare(v3.CreateRevision(m.myKey), "=", 0)).
        Then(v3.OpPut(m.myKey, "", v3.WithLease(m.s.Lease()))).
        Else(v3.OpGet(m.myKey)).
        Commit()

    // Wait until our key is first (by create revision)
    err = waitDeletes(ctx, m.s.Client(), m.pfx, m.myRev-1)
    return err
}
```

Key insight: The lock uses **create revision ordering**. The first key (by creation) wins.

### Leader Election

```go
election := concurrency.NewElection(session, "/election/my-service")

// Campaign to become leader
if err := election.Campaign(ctx, "node-1"); err != nil {
    log.Fatal(err)
}

fmt.Println("I am the leader!")

// Do leader work...

// Voluntarily step down
election.Resign(ctx)
```

### Observing Leader

```go
// Watch for leader changes
observeChan := election.Observe(ctx)
for resp := range observeChan {
    fmt.Println("Current leader:", string(resp.Kvs[0].Value))
}
```

### Semaphore

Control concurrent access to a resource:

```go
// Not in standard library, but easy to implement with transactions
func acquireSemaphore(cli *clientv3.Client, name string, limit int) error {
    ctx := context.Background()

    for {
        // Count current holders
        resp, _ := cli.Get(ctx, name+"/", clientv3.WithPrefix(), clientv3.WithCountOnly())

        if resp.Count < int64(limit) {
            // Try to acquire
            lease, _ := cli.Grant(ctx, 30)
            key := fmt.Sprintf("%s/%d", name, lease.ID)

            txnResp, _ := cli.Txn(ctx).
                If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).
                Then(clientv3.OpPut(key, "", clientv3.WithLease(lease.ID))).
                Commit()

            if txnResp.Succeeded {
                return nil
            }
        }

        // Wait and retry
        time.Sleep(100 * time.Millisecond)
    }
}
```

---

## Error Handling

### Common Errors

```go
import "go.etcd.io/etcd/api/v3/v3rpc/rpctypes"

resp, err := cli.Get(ctx, "key")
if err != nil {
    switch err {
    case context.Canceled:
        log.Println("Context canceled")
    case context.DeadlineExceeded:
        log.Println("Timeout")
    case rpctypes.ErrEmptyKey:
        log.Println("Empty key")
    case rpctypes.ErrKeyNotFound:
        log.Println("Key not found")
    case rpctypes.ErrCompacted:
        log.Println("Revision compacted, use newer revision")
    case rpctypes.ErrFutureRev:
        log.Println("Revision in the future")
    case rpctypes.ErrNoSpace:
        log.Println("No space left, run compaction and defrag")
    default:
        log.Println("Unknown error:", err)
    }
}
```

### Retry Strategy

```go
func putWithRetry(cli *clientv3.Client, key, value string) error {
    var lastErr error

    for i := 0; i < 3; i++ {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        _, err := cli.Put(ctx, key, value)
        cancel()

        if err == nil {
            return nil
        }

        lastErr = err

        // Check if retryable
        switch err {
        case context.DeadlineExceeded, rpctypes.ErrTimeout:
            // Retryable
            time.Sleep(time.Duration(i+1) * time.Second)
            continue
        case rpctypes.ErrNoSpace, rpctypes.ErrCorrupt:
            // Not retryable
            return err
        }
    }

    return lastErr
}
```

---

## Kubernetes Integration

Kubernetes uses etcd extensively. Here's how they integrate:

### API Server Storage

Kubernetes stores all resources in etcd:

```
/registry/pods/{namespace}/{name}
/registry/services/{namespace}/{name}
/registry/deployments/{namespace}/{name}
```

### Watch-Based Controllers

Controllers use watches to react to changes:

```go
// Simplified controller pattern
watchChan := cli.Watch(ctx, "/registry/pods/", clientv3.WithPrefix())

for resp := range watchChan {
    for _, event := range resp.Events {
        switch event.Type {
        case clientv3.EventTypePut:
            handlePodUpdate(event.Kv)
        case clientv3.EventTypeDelete:
            handlePodDelete(event.Kv)
        }
    }
}
```

### Leader Election for HA

Multiple API server instances use etcd for leader election:

```go
// Kubernetes uses client-go's leader election, but it's similar to:
election := concurrency.NewElection(session, "/kube-scheduler/leader")
election.Campaign(ctx, hostname)
```

---

## Best Practices

### 1. Always Use Context with Timeout

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
cli.Put(ctx, "key", "value")
```

### 2. Close Resources

```go
cli, _ := clientv3.New(config)
defer cli.Close()

session, _ := concurrency.NewSession(cli)
defer session.Close()
```

### 3. Handle Watch Compaction

```go
watchChan := cli.Watch(ctx, "key", clientv3.WithRev(oldRevision))
for resp := range watchChan {
    if resp.CompactRevision > 0 {
        // Our revision was compacted, do full sync
        doFullSync()
        break
    }
}
```

### 4. Use Leases for Ephemeral Data

```go
// Service registration
lease, _ := cli.Grant(ctx, 30)
cli.Put(ctx, "/services/my-app/node1", "10.0.0.1", clientv3.WithLease(lease.ID))
keepAliveChan, _ := cli.KeepAlive(ctx, lease.ID)
go func() {
    for range keepAliveChan {
        // Lease renewed
    }
}()
```

### 5. Batch Operations

```go
// Use transactions for multiple operations
cli.Txn(ctx).
    Then(
        clientv3.OpPut("key1", "value1"),
        clientv3.OpPut("key2", "value2"),
        clientv3.OpPut("key3", "value3"),
    ).Commit()
```

---

## Key Takeaways

1. **Client is straightforward** - Basic operations (Put, Get, Delete) are simple and intuitive.

2. **Leases automate cleanup** - Use leases for ephemeral data like service registration.

3. **Watches enable reactive systems** - But handle reconnection and compaction properly.

4. **Transactions are powerful** - Compare-and-swap enables safe coordination.

5. **Primitives simplify coordination** - Use Mutex and Election instead of rolling your own.

6. **Errors need specific handling** - Different errors require different responses.

---

## Next in the Series

In [Part 5: Performance Analysis and Optimization](05-performance-analysis.md), we'll explore:
- Performance characteristics
- Bottlenecks and tuning
- Compaction strategies
- Scaling considerations

---

## Explore the Code

Key files:
- [`client/v3/client.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/client/v3/client.go) - Client implementation
- [`client/v3/concurrency/mutex.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/client/v3/concurrency/mutex.go) - Distributed lock
- [`client/v3/concurrency/election.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/client/v3/concurrency/election.go) - Leader election
- [`client/v3/watch.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/client/v3/watch.go) - Watch implementation

---

*Next: [Part 5 - Performance Analysis and Optimization](05-performance-analysis.md)*

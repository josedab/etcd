# etcd Pluggable Storage Backend

This package provides a pluggable storage backend interface for etcd, enabling alternative storage implementations beyond the default BoltDB.

## Overview

The backend package defines clean interfaces that abstract the storage layer, allowing different storage engines to be used interchangeably while maintaining etcd's consistency guarantees.

## Architecture

### Core Interfaces

- **Backend**: Main interface for storage operations including transactions, snapshots, and maintenance
- **ReadTx**: Read-only transaction interface
- **BatchTx**: Write transaction interface with batching support
- **Snapshot**: Point-in-time backup interface

### Available Backends

1. **BoltDB (default)**: The original embedded key-value store, optimized for read-heavy workloads
2. **Memory**: In-memory storage for testing, provides fast operations without disk I/O

## Usage

### Creating a Backend

```go
import "go.etcd.io/etcd/server/v3/storage/backend"

// Create default BoltDB backend
cfg := backend.DefaultBackendConfig(logger)
cfg.Path = "/path/to/db"
b, err := backend.Create(backend.BackendTypeBolt, cfg)

// Create memory backend for testing
memCfg := backend.DefaultBackendConfig(logger)
memBackend, err := backend.Create(backend.BackendTypeMemory, memCfg)
```

### Using the Factory

```go
// Check available backends
types := backend.Available()

// Check if a backend is registered
if backend.IsRegistered(backend.BackendTypeBolt) {
    // Backend is available
}
```

### Basic Operations

```go
// Write operations
tx := b.BatchTx()
tx.Lock()
tx.UnsafeCreateBucket(myBucket)
tx.UnsafePut(myBucket, []byte("key"), []byte("value"))
tx.Unlock()

// Read operations
rtx := b.ReadTx()
rtx.RLock()
keys, vals := rtx.UnsafeRange(myBucket, []byte("key"), nil, 0)
rtx.RUnlock()

// Force commit
b.ForceCommit()

// Close
b.Close()
```

## Implementing a Custom Backend

To implement a new storage backend:

1. Create a new file (e.g., `mybackend.go`) that implements the `Backend` interface
2. Implement `ReadTx` and `BatchTx` interfaces for transaction support
3. Implement the `Snapshot` interface for backup support
4. Register your backend in `init()`:

```go
func init() {
    backend.Register("mybackend", func(cfg backend.BackendConfig) (backend.Backend, error) {
        return NewMyBackend(cfg)
    })
}
```

### Interface Requirements

Your backend must implement:

- **Transaction semantics**: Proper isolation between read and write transactions
- **Consistency**: Data should be durable after commit
- **Thread safety**: All operations must be safe for concurrent access
- **Bucket support**: Logical grouping of key-value pairs

### Testing

Use the interface tests to verify your implementation:

```go
go test -v ./server/storage/backend -run TestBackend
```

## Configuration

### BackendConfig Options

| Field | Description | Default |
|-------|-------------|---------|
| Type | Backend type (bolt, memory) | bolt |
| Path | Database file path | required |
| BatchInterval | Max time before commit | 100ms |
| BatchLimit | Max operations before commit | 10000 |
| MmapSize | Memory map size | 10GB |
| UnsafeNoFsync | Disable fsync (unsafe) | false |
| Mlock | Lock memory to prevent swapping | false |
| Timeout | File lock timeout | 0 (wait forever) |

## Performance Considerations

### BoltDB Backend
- Optimized for read-heavy workloads
- Uses memory-mapped files
- Requires periodic defragmentation

### Memory Backend
- Extremely fast for testing
- No persistence
- Limited by available RAM

## Migration

When switching between backends:

```bash
# Export data from current backend
etcdutl snapshot save snapshot.db

# Start with new backend type
etcd --backend-type=memory  # for testing
```

## Future Backends

The pluggable interface enables:
- LSM-tree backends (RocksDB, Pebble) for write-heavy workloads
- Cloud-native backends for distributed storage
- Specialized backends for time-series or other workloads

## Contributing

1. Implement the Backend interface
2. Add comprehensive tests
3. Document performance characteristics
4. Submit PR with benchmarks

## References

- [RFC-0007: Pluggable Storage Backend](../../../analysis-output/rfcs/RFC-0007-pluggable-storage-backend.md)
- [BoltDB Documentation](https://github.com/etcd-io/bbolt)
- [etcd Storage Overview](https://etcd.io/docs/latest/learning/data_model/)

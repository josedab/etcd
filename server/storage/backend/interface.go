// Copyright 2015 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package backend defines the storage backend interface for etcd.
// This interface enables alternative storage implementations beyond BoltDB,
// allowing optimization for specific workloads while maintaining etcd's
// consistency guarantees.
package backend

import (
	"io"
)

// BucketID is a unique identifier for a bucket within the backend.
// The ID must not be persisted and can be used as a lightweight identifier
// in in-memory maps.
type BucketID int

// Bucket represents a logical grouping of key-value pairs within the backend.
// Buckets provide namespace isolation for different types of data.
type Bucket interface {
	// ID returns a unique identifier of a bucket.
	// The id must NOT be persisted and can be used as lightweight identifier
	// in the in-memory maps.
	ID() BucketID
	// Name returns the bucket name as bytes.
	Name() []byte
	// String implements Stringer (human readable name).
	String() string
	// IsSafeRangeBucket is a hack to avoid inadvertently reading duplicate keys;
	// overwrites on a bucket should only fetch with limit=1, but safeRangeBucket
	// is known to never overwrite any key so range is safe.
	IsSafeRangeBucket() bool
}

// Backend represents a storage backend that provides transactional
// key-value storage capabilities. Implementations must be thread-safe.
//
// The Backend interface is designed to be storage-agnostic, allowing
// different underlying storage engines (BoltDB, Pebble, in-memory, etc.)
// to be used interchangeably.
type Backend interface {
	// ReadTx returns a read transaction.
	// It is replaced by ConcurrentReadTx in the main data path, see #10523.
	ReadTx() ReadTx
	// BatchTx returns a batch transaction for write operations.
	// Write operations are buffered and committed periodically.
	BatchTx() BatchTx
	// ConcurrentReadTx returns a non-blocking read transaction.
	// This is the preferred method for read operations in the main data path.
	ConcurrentReadTx() ReadTx

	// Snapshot returns a snapshot of the backend for backup purposes.
	Snapshot() Snapshot
	// Hash computes a hash of all data in the backend.
	// The ignores function can be used to skip certain keys.
	Hash(ignores func(bucketName, keyName []byte) bool) (uint32, error)
	// Size returns the current size of the backend physically allocated.
	// The backend can hold DB space that is not utilized at the moment,
	// since it can conduct pre-allocation or spare unused space for recycling.
	// Use SizeInUse() instead for the actual DB size.
	Size() int64
	// SizeInUse returns the current size of the backend logically in use.
	// Since the backend can manage free space in a non-byte unit such as
	// number of pages, the returned value can be not exactly accurate in bytes.
	SizeInUse() int64
	// OpenReadTxN returns the number of currently open read transactions in the backend.
	OpenReadTxN() int64
	// Defrag defragments the backend to reclaim unused space.
	Defrag() error
	// ForceCommit forces the current batching tx to commit.
	ForceCommit()
	// Close closes the backend, releasing all resources.
	Close() error

	// SetTxPostLockInsideApplyHook sets a hook that is called after locking the tx
	// during apply operations.
	SetTxPostLockInsideApplyHook(func())
}

// Snapshot represents a point-in-time view of the backend data.
// Snapshots are used for backup and replication purposes.
type Snapshot interface {
	// Size returns the size of the snapshot in bytes.
	Size() int64
	// WriteTo writes the snapshot to the given writer.
	WriteTo(w io.Writer) (n int64, err error)
	// Close closes the snapshot and releases associated resources.
	Close() error
}

// ReadTx represents a read-only transaction.
// Read transactions provide a consistent view of the data.
type ReadTx interface {
	// RLock acquires a read lock on the transaction.
	RLock()
	// RUnlock releases a read lock on the transaction.
	RUnlock()
	UnsafeReader
}

// UnsafeReader provides read operations that must be called while holding
// appropriate locks.
type UnsafeReader interface {
	// UnsafeRange retrieves key-value pairs within the given range.
	// If endKey is nil, only the key matching 'key' is returned.
	// If limit <= 0, all matching keys are returned.
	UnsafeRange(bucket Bucket, key, endKey []byte, limit int64) (keys [][]byte, vals [][]byte)
	// UnsafeForEach iterates over all key-value pairs in the bucket.
	UnsafeForEach(bucket Bucket, visitor func(k, v []byte) error) error
}

// BatchTx represents a write transaction that batches multiple operations.
// Operations are buffered and committed periodically or when the batch limit is reached.
type BatchTx interface {
	// Lock acquires a write lock on the transaction.
	Lock()
	// Unlock releases a write lock on the transaction.
	Unlock()
	// Commit commits a previous tx and begins a new writable one.
	Commit()
	// CommitAndStop commits the previous tx and does not create a new one.
	CommitAndStop()
	// LockInsideApply acquires a lock within an apply operation.
	LockInsideApply()
	// LockOutsideApply acquires a lock outside an apply operation.
	LockOutsideApply()
	UnsafeReadWriter
}

// UnsafeReadWriter combines read and write operations.
type UnsafeReadWriter interface {
	UnsafeReader
	UnsafeWriter
}

// UnsafeWriter provides write operations that must be called while holding
// appropriate locks.
type UnsafeWriter interface {
	// UnsafeCreateBucket creates a bucket if it doesn't exist.
	UnsafeCreateBucket(bucket Bucket)
	// UnsafeDeleteBucket deletes a bucket.
	UnsafeDeleteBucket(bucket Bucket)
	// UnsafePut stores a key-value pair in the bucket.
	UnsafePut(bucket Bucket, key []byte, value []byte)
	// UnsafeSeqPut stores a key-value pair optimized for sequential access.
	UnsafeSeqPut(bucket Bucket, key []byte, value []byte)
	// UnsafeDelete removes a key from the bucket.
	UnsafeDelete(bucket Bucket, key []byte)
}

// Hooks allow adding additional logic executed during transaction lifetime.
type Hooks interface {
	// OnPreCommitUnsafe is executed before Commit of transactions.
	// The given transaction is already locked.
	OnPreCommitUnsafe(tx UnsafeReadWriter)
}

// HookFunc is a function type for transaction hooks.
type HookFunc func(tx UnsafeReadWriter)

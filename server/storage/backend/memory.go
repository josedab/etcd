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

package backend

import (
	"bytes"
	"hash/crc32"
	"io"
	"sort"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"
)

// memoryBackend implements Backend interface using in-memory storage.
// This is useful for testing purposes as it provides fast operations
// without disk I/O overhead.
type memoryBackend struct {
	mu sync.RWMutex

	// buckets stores data organized by bucket name
	buckets map[string]map[string][]byte

	// size tracking
	size      int64
	sizeInUse int64

	// transaction support
	readTx  *memoryReadTx
	batchTx *memoryBatchTx

	// commit tracking
	commits     int64
	openReadTxN int64

	// hooks
	hooks                     Hooks
	txPostLockInsideApplyHook func()

	lg *zap.Logger
}

// NewMemoryBackend creates a new in-memory backend.
func NewMemoryBackend(cfg BackendConfig) (Backend, error) {
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}

	b := &memoryBackend{
		buckets: make(map[string]map[string][]byte),
		lg:      cfg.Logger,
		hooks:   cfg.Hooks,
	}

	b.readTx = &memoryReadTx{backend: b}
	b.batchTx = &memoryBatchTx{
		backend: b,
		pending: make(map[string]map[string][]byte),
	}

	return b, nil
}

func (b *memoryBackend) ReadTx() ReadTx {
	return b.readTx
}

func (b *memoryBackend) BatchTx() BatchTx {
	return b.batchTx
}

func (b *memoryBackend) ConcurrentReadTx() ReadTx {
	atomic.AddInt64(&b.openReadTxN, 1)
	return &memoryConcurrentReadTx{
		backend: b,
	}
}

func (b *memoryBackend) Snapshot() Snapshot {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Create a deep copy of data for the snapshot
	data := make(map[string]map[string][]byte)
	var size int64
	for bucket, kvs := range b.buckets {
		data[bucket] = make(map[string][]byte)
		for k, v := range kvs {
			valueCopy := make([]byte, len(v))
			copy(valueCopy, v)
			data[bucket][k] = valueCopy
			size += int64(len(k) + len(v))
		}
	}

	return &memorySnapshot{
		data: data,
		size: size,
	}
}

func (b *memoryBackend) Hash(ignores func(bucketName, keyName []byte) bool) (uint32, error) {
	h := crc32.New(crc32.MakeTable(crc32.Castagnoli))

	b.mu.RLock()
	defer b.mu.RUnlock()

	// Sort bucket names for deterministic hash
	bucketNames := make([]string, 0, len(b.buckets))
	for name := range b.buckets {
		bucketNames = append(bucketNames, name)
	}
	sort.Strings(bucketNames)

	for _, bucketName := range bucketNames {
		bucket := b.buckets[bucketName]
		h.Write([]byte(bucketName))

		// Sort keys for deterministic hash
		keys := make([]string, 0, len(bucket))
		for k := range bucket {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			if ignores != nil && !ignores([]byte(bucketName), []byte(k)) {
				h.Write([]byte(k))
				h.Write(bucket[k])
			}
		}
	}

	return h.Sum32(), nil
}

func (b *memoryBackend) Size() int64 {
	return atomic.LoadInt64(&b.size)
}

func (b *memoryBackend) SizeInUse() int64 {
	return atomic.LoadInt64(&b.sizeInUse)
}

func (b *memoryBackend) OpenReadTxN() int64 {
	return atomic.LoadInt64(&b.openReadTxN)
}

func (b *memoryBackend) Defrag() error {
	// No defragmentation needed for in-memory storage
	return nil
}

func (b *memoryBackend) ForceCommit() {
	b.batchTx.Commit()
}

func (b *memoryBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buckets = nil
	return nil
}

func (b *memoryBackend) SetTxPostLockInsideApplyHook(hook func()) {
	b.batchTx.Lock()
	defer b.batchTx.Unlock()
	b.txPostLockInsideApplyHook = hook
}

func (b *memoryBackend) updateSize() {
	var size int64
	for _, bucket := range b.buckets {
		for k, v := range bucket {
			size += int64(len(k) + len(v))
		}
	}
	atomic.StoreInt64(&b.size, size)
	atomic.StoreInt64(&b.sizeInUse, size)
}

// memoryReadTx implements ReadTx for the memory backend
type memoryReadTx struct {
	mu      sync.RWMutex
	backend *memoryBackend
}

func (tx *memoryReadTx) RLock() {
	tx.mu.RLock()
	tx.backend.mu.RLock()
}

func (tx *memoryReadTx) RUnlock() {
	tx.backend.mu.RUnlock()
	tx.mu.RUnlock()
}

func (tx *memoryReadTx) UnsafeRange(bucket Bucket, key, endKey []byte, limit int64) ([][]byte, [][]byte) {
	return tx.backend.unsafeRange(bucket, key, endKey, limit)
}

func (tx *memoryReadTx) UnsafeForEach(bucket Bucket, visitor func(k, v []byte) error) error {
	return tx.backend.unsafeForEach(bucket, visitor)
}

// memoryConcurrentReadTx implements a concurrent read transaction
type memoryConcurrentReadTx struct {
	backend *memoryBackend
}

func (tx *memoryConcurrentReadTx) RLock() {}

func (tx *memoryConcurrentReadTx) RUnlock() {
	atomic.AddInt64(&tx.backend.openReadTxN, -1)
}

func (tx *memoryConcurrentReadTx) UnsafeRange(bucket Bucket, key, endKey []byte, limit int64) ([][]byte, [][]byte) {
	tx.backend.mu.RLock()
	defer tx.backend.mu.RUnlock()
	return tx.backend.unsafeRange(bucket, key, endKey, limit)
}

func (tx *memoryConcurrentReadTx) UnsafeForEach(bucket Bucket, visitor func(k, v []byte) error) error {
	tx.backend.mu.RLock()
	defer tx.backend.mu.RUnlock()
	return tx.backend.unsafeForEach(bucket, visitor)
}

// memoryBatchTx implements BatchTx for the memory backend
type memoryBatchTx struct {
	mu      sync.Mutex
	backend *memoryBackend
	pending map[string]map[string][]byte
}

func (tx *memoryBatchTx) Lock() {
	tx.mu.Lock()
}

func (tx *memoryBatchTx) Unlock() {
	tx.mu.Unlock()
}

func (tx *memoryBatchTx) LockInsideApply() {
	tx.mu.Lock()
	if tx.backend.txPostLockInsideApplyHook != nil {
		tx.backend.txPostLockInsideApplyHook()
	}
}

func (tx *memoryBatchTx) LockOutsideApply() {
	tx.mu.Lock()
}

func (tx *memoryBatchTx) Commit() {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.commit()
}

func (tx *memoryBatchTx) CommitAndStop() {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.commit()
}

func (tx *memoryBatchTx) commit() {
	if tx.backend.hooks != nil {
		tx.backend.hooks.OnPreCommitUnsafe(tx)
	}

	tx.backend.mu.Lock()
	defer tx.backend.mu.Unlock()

	// Apply pending changes
	for bucket, kvs := range tx.pending {
		if _, ok := tx.backend.buckets[bucket]; !ok {
			tx.backend.buckets[bucket] = make(map[string][]byte)
		}
		for k, v := range kvs {
			if v == nil {
				delete(tx.backend.buckets[bucket], k)
			} else {
				tx.backend.buckets[bucket][k] = v
			}
		}
	}

	// Clear pending changes
	tx.pending = make(map[string]map[string][]byte)

	// Update size
	tx.backend.updateSize()

	atomic.AddInt64(&tx.backend.commits, 1)
}

func (tx *memoryBatchTx) UnsafeCreateBucket(bucket Bucket) {
	tx.backend.mu.Lock()
	defer tx.backend.mu.Unlock()

	bucketName := string(bucket.Name())
	if _, ok := tx.backend.buckets[bucketName]; !ok {
		tx.backend.buckets[bucketName] = make(map[string][]byte)
	}
}

func (tx *memoryBatchTx) UnsafeDeleteBucket(bucket Bucket) {
	tx.backend.mu.Lock()
	defer tx.backend.mu.Unlock()

	delete(tx.backend.buckets, string(bucket.Name()))
}

func (tx *memoryBatchTx) UnsafePut(bucket Bucket, key []byte, value []byte) {
	bucketName := string(bucket.Name())
	if _, ok := tx.pending[bucketName]; !ok {
		tx.pending[bucketName] = make(map[string][]byte)
	}
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	tx.pending[bucketName][string(key)] = valueCopy

	// Also apply to main storage immediately for read-your-writes
	tx.backend.mu.Lock()
	if _, ok := tx.backend.buckets[bucketName]; !ok {
		tx.backend.buckets[bucketName] = make(map[string][]byte)
	}
	tx.backend.buckets[bucketName][string(key)] = valueCopy
	tx.backend.mu.Unlock()
}

func (tx *memoryBatchTx) UnsafeSeqPut(bucket Bucket, key []byte, value []byte) {
	tx.UnsafePut(bucket, key, value)
}

func (tx *memoryBatchTx) UnsafeDelete(bucket Bucket, key []byte) {
	bucketName := string(bucket.Name())
	if _, ok := tx.pending[bucketName]; !ok {
		tx.pending[bucketName] = make(map[string][]byte)
	}
	tx.pending[bucketName][string(key)] = nil // nil marks deletion

	// Also apply to main storage immediately
	tx.backend.mu.Lock()
	if b, ok := tx.backend.buckets[bucketName]; ok {
		delete(b, string(key))
	}
	tx.backend.mu.Unlock()
}

func (tx *memoryBatchTx) UnsafeRange(bucket Bucket, key, endKey []byte, limit int64) ([][]byte, [][]byte) {
	tx.backend.mu.RLock()
	defer tx.backend.mu.RUnlock()
	return tx.backend.unsafeRange(bucket, key, endKey, limit)
}

func (tx *memoryBatchTx) UnsafeForEach(bucket Bucket, visitor func(k, v []byte) error) error {
	tx.backend.mu.RLock()
	defer tx.backend.mu.RUnlock()
	return tx.backend.unsafeForEach(bucket, visitor)
}

// unsafeRange performs range query without locking (caller must hold lock)
func (b *memoryBackend) unsafeRange(bucket Bucket, key, endKey []byte, limit int64) ([][]byte, [][]byte) {
	bucketName := string(bucket.Name())
	data, ok := b.buckets[bucketName]
	if !ok {
		return nil, nil
	}

	if limit <= 0 {
		limit = int64(len(data))
	}

	var keys [][]byte
	var vals [][]byte

	// Sort keys for range query
	sortedKeys := make([]string, 0, len(data))
	for k := range data {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	for _, k := range sortedKeys {
		keyBytes := []byte(k)
		if bytes.Compare(keyBytes, key) < 0 {
			continue
		}
		if endKey != nil && bytes.Compare(keyBytes, endKey) >= 0 {
			break
		}
		if endKey == nil && !bytes.Equal(keyBytes, key) {
			break
		}

		keys = append(keys, keyBytes)
		vals = append(vals, data[k])

		if int64(len(keys)) >= limit {
			break
		}
	}

	return keys, vals
}

// unsafeForEach iterates over all keys in bucket without locking
func (b *memoryBackend) unsafeForEach(bucket Bucket, visitor func(k, v []byte) error) error {
	bucketName := string(bucket.Name())
	data, ok := b.buckets[bucketName]
	if !ok {
		return nil
	}

	// Sort keys for deterministic iteration
	sortedKeys := make([]string, 0, len(data))
	for k := range data {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	for _, k := range sortedKeys {
		if err := visitor([]byte(k), data[k]); err != nil {
			return err
		}
	}

	return nil
}

// memorySnapshot implements Snapshot for the memory backend
type memorySnapshot struct {
	data   map[string]map[string][]byte
	size   int64
	closed bool
}

func (s *memorySnapshot) Size() int64 {
	return s.size
}

func (s *memorySnapshot) WriteTo(w io.Writer) (int64, error) {
	// Simple serialization: write bucket names and key-value pairs
	var written int64

	// Sort bucket names for deterministic output
	bucketNames := make([]string, 0, len(s.data))
	for name := range s.data {
		bucketNames = append(bucketNames, name)
	}
	sort.Strings(bucketNames)

	for _, bucketName := range bucketNames {
		bucket := s.data[bucketName]
		// Write bucket name
		n, err := w.Write([]byte(bucketName))
		if err != nil {
			return written, err
		}
		written += int64(n)

		// Sort keys
		keys := make([]string, 0, len(bucket))
		for k := range bucket {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		// Write key-value pairs
		for _, k := range keys {
			n, err = w.Write([]byte(k))
			if err != nil {
				return written, err
			}
			written += int64(n)

			n, err = w.Write(bucket[k])
			if err != nil {
				return written, err
			}
			written += int64(n)
		}
	}

	return written, nil
}

func (s *memorySnapshot) Close() error {
	s.data = nil
	s.closed = true
	return nil
}

func init() {
	// Register the memory backend
	Register(BackendTypeMemory, func(cfg BackendConfig) (Backend, error) {
		return NewMemoryBackend(cfg)
	})
}

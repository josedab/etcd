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
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

// testBucket is a simple bucket implementation for testing
type testBucket struct {
	id   BucketID
	name []byte
	safe bool
}

func (b testBucket) ID() BucketID            { return b.id }
func (b testBucket) Name() []byte            { return b.name }
func (b testBucket) String() string          { return string(b.name) }
func (b testBucket) IsSafeRangeBucket() bool { return b.safe }

var (
	testBucketKey  = testBucket{id: 1, name: []byte("key"), safe: true}
	testBucketMeta = testBucket{id: 2, name: []byte("meta"), safe: false}
)

// backendTestSuite contains tests that all backends must pass
type backendTestSuite struct {
	name    string
	backend Backend
	cleanup func()
}

// getTestBackends returns all backend implementations for testing
func getTestBackends(t *testing.T) []backendTestSuite {
	suites := []backendTestSuite{}

	// BoltDB backend
	tmpDir, err := os.MkdirTemp("", "etcd-backend-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	dbPath := filepath.Join(tmpDir, "db")
	cfg := DefaultBackendConfig(zap.NewNop())
	cfg.Path = dbPath
	boltBackend, err := Create(BackendTypeBolt, cfg)
	if err != nil {
		t.Fatalf("failed to create bolt backend: %v", err)
	}
	suites = append(suites, backendTestSuite{
		name:    "bolt",
		backend: boltBackend,
		cleanup: func() {
			boltBackend.Close()
			os.RemoveAll(tmpDir)
		},
	})

	// Memory backend
	memCfg := DefaultBackendConfig(zap.NewNop())
	memBackend, err := Create(BackendTypeMemory, memCfg)
	if err != nil {
		t.Fatalf("failed to create memory backend: %v", err)
	}
	suites = append(suites, backendTestSuite{
		name:    "memory",
		backend: memBackend,
		cleanup: func() {
			memBackend.Close()
		},
	})

	return suites
}

func TestBackendFactory(t *testing.T) {
	// Test that both backend types are registered
	types := Available()
	hasBot, hasMem := false, false
	for _, bt := range types {
		if bt == BackendTypeBolt {
			hasBot = true
		}
		if bt == BackendTypeMemory {
			hasMem = true
		}
	}
	if !hasBot {
		t.Error("bolt backend not registered")
	}
	if !hasMem {
		t.Error("memory backend not registered")
	}

	// Test IsRegistered
	if !IsRegistered(BackendTypeBolt) {
		t.Error("bolt backend should be registered")
	}
	if !IsRegistered(BackendTypeMemory) {
		t.Error("memory backend should be registered")
	}
	if IsRegistered("nonexistent") {
		t.Error("nonexistent backend should not be registered")
	}

	// Test creating unknown backend
	_, err := Create("nonexistent", BackendConfig{})
	if err == nil {
		t.Error("expected error creating nonexistent backend")
	}
}

func TestBackendPutGet(t *testing.T) {
	for _, suite := range getTestBackends(t) {
		t.Run(suite.name, func(t *testing.T) {
			defer suite.cleanup()

			// Create bucket and put data
			tx := suite.backend.BatchTx()
			tx.Lock()
			tx.UnsafeCreateBucket(testBucketKey)
			tx.UnsafePut(testBucketKey, []byte("key1"), []byte("value1"))
			tx.UnsafePut(testBucketKey, []byte("key2"), []byte("value2"))
			tx.Unlock()

			suite.backend.ForceCommit()

			// Read data back
			rtx := suite.backend.ReadTx()
			rtx.RLock()
			keys, vals := rtx.UnsafeRange(testBucketKey, []byte("key1"), nil, 0)
			rtx.RUnlock()

			if len(keys) != 1 {
				t.Fatalf("expected 1 key, got %d", len(keys))
			}
			if !bytes.Equal(keys[0], []byte("key1")) {
				t.Errorf("expected key1, got %s", keys[0])
			}
			if !bytes.Equal(vals[0], []byte("value1")) {
				t.Errorf("expected value1, got %s", vals[0])
			}
		})
	}
}

func TestBackendDelete(t *testing.T) {
	for _, suite := range getTestBackends(t) {
		t.Run(suite.name, func(t *testing.T) {
			defer suite.cleanup()

			// Create bucket and put data
			tx := suite.backend.BatchTx()
			tx.Lock()
			tx.UnsafeCreateBucket(testBucketKey)
			tx.UnsafePut(testBucketKey, []byte("key1"), []byte("value1"))
			tx.Unlock()

			suite.backend.ForceCommit()

			// Delete data
			tx.Lock()
			tx.UnsafeDelete(testBucketKey, []byte("key1"))
			tx.Unlock()

			suite.backend.ForceCommit()

			// Verify deletion
			rtx := suite.backend.ReadTx()
			rtx.RLock()
			keys, _ := rtx.UnsafeRange(testBucketKey, []byte("key1"), nil, 0)
			rtx.RUnlock()

			if len(keys) != 0 {
				t.Errorf("expected 0 keys after deletion, got %d", len(keys))
			}
		})
	}
}

func TestBackendRange(t *testing.T) {
	for _, suite := range getTestBackends(t) {
		t.Run(suite.name, func(t *testing.T) {
			defer suite.cleanup()

			// Create bucket and put data
			tx := suite.backend.BatchTx()
			tx.Lock()
			tx.UnsafeCreateBucket(testBucketKey)
			tx.UnsafePut(testBucketKey, []byte("key1"), []byte("value1"))
			tx.UnsafePut(testBucketKey, []byte("key2"), []byte("value2"))
			tx.UnsafePut(testBucketKey, []byte("key3"), []byte("value3"))
			tx.Unlock()

			suite.backend.ForceCommit()

			// Range query
			rtx := suite.backend.ReadTx()
			rtx.RLock()
			keys, vals := rtx.UnsafeRange(testBucketKey, []byte("key1"), []byte("key3"), 0)
			rtx.RUnlock()

			if len(keys) != 2 {
				t.Fatalf("expected 2 keys, got %d", len(keys))
			}
			if !bytes.Equal(keys[0], []byte("key1")) {
				t.Errorf("expected key1, got %s", keys[0])
			}
			if !bytes.Equal(keys[1], []byte("key2")) {
				t.Errorf("expected key2, got %s", keys[1])
			}
			if !bytes.Equal(vals[0], []byte("value1")) {
				t.Errorf("expected value1, got %s", vals[0])
			}
			if !bytes.Equal(vals[1], []byte("value2")) {
				t.Errorf("expected value2, got %s", vals[1])
			}
		})
	}
}

func TestBackendForEach(t *testing.T) {
	for _, suite := range getTestBackends(t) {
		t.Run(suite.name, func(t *testing.T) {
			defer suite.cleanup()

			// Create bucket and put data
			tx := suite.backend.BatchTx()
			tx.Lock()
			tx.UnsafeCreateBucket(testBucketKey)
			tx.UnsafePut(testBucketKey, []byte("key1"), []byte("value1"))
			tx.UnsafePut(testBucketKey, []byte("key2"), []byte("value2"))
			tx.Unlock()

			suite.backend.ForceCommit()

			// ForEach query
			rtx := suite.backend.ReadTx()
			rtx.RLock()
			count := 0
			err := rtx.UnsafeForEach(testBucketKey, func(k, v []byte) error {
				count++
				return nil
			})
			rtx.RUnlock()

			if err != nil {
				t.Fatalf("ForEach failed: %v", err)
			}
			if count != 2 {
				t.Errorf("expected 2 keys in ForEach, got %d", count)
			}
		})
	}
}

func TestBackendSnapshot(t *testing.T) {
	for _, suite := range getTestBackends(t) {
		t.Run(suite.name, func(t *testing.T) {
			defer suite.cleanup()

			// Create bucket and put data
			tx := suite.backend.BatchTx()
			tx.Lock()
			tx.UnsafeCreateBucket(testBucketKey)
			tx.UnsafePut(testBucketKey, []byte("key1"), []byte("value1"))
			tx.Unlock()

			suite.backend.ForceCommit()

			// Take snapshot
			snap := suite.backend.Snapshot()
			if snap == nil {
				t.Fatal("snapshot is nil")
			}

			// Write snapshot to buffer
			var buf bytes.Buffer
			n, err := snap.WriteTo(&buf)
			if err != nil {
				t.Fatalf("WriteTo failed: %v", err)
			}
			if n == 0 {
				t.Error("snapshot wrote 0 bytes")
			}

			if err := snap.Close(); err != nil {
				t.Errorf("Close failed: %v", err)
			}
		})
	}
}

func TestBackendHash(t *testing.T) {
	for _, suite := range getTestBackends(t) {
		t.Run(suite.name, func(t *testing.T) {
			defer suite.cleanup()

			// Create bucket and put data
			tx := suite.backend.BatchTx()
			tx.Lock()
			tx.UnsafeCreateBucket(testBucketKey)
			tx.UnsafePut(testBucketKey, []byte("key1"), []byte("value1"))
			tx.Unlock()

			suite.backend.ForceCommit()

			// Compute hash
			hash1, err := suite.backend.Hash(nil)
			if err != nil {
				t.Fatalf("Hash failed: %v", err)
			}

			// Hash should be consistent
			hash2, err := suite.backend.Hash(nil)
			if err != nil {
				t.Fatalf("Hash failed: %v", err)
			}
			if hash1 != hash2 {
				t.Errorf("hash not consistent: %d != %d", hash1, hash2)
			}
		})
	}
}

func TestBackendConcurrentReadTx(t *testing.T) {
	for _, suite := range getTestBackends(t) {
		t.Run(suite.name, func(t *testing.T) {
			defer suite.cleanup()

			// Create bucket and put data
			tx := suite.backend.BatchTx()
			tx.Lock()
			tx.UnsafeCreateBucket(testBucketKey)
			tx.UnsafePut(testBucketKey, []byte("key1"), []byte("value1"))
			tx.Unlock()

			suite.backend.ForceCommit()

			// Get concurrent read tx
			crtx := suite.backend.ConcurrentReadTx()
			if crtx == nil {
				t.Fatal("ConcurrentReadTx returned nil")
			}

			// Read data
			crtx.RLock()
			keys, vals := crtx.UnsafeRange(testBucketKey, []byte("key1"), nil, 0)
			crtx.RUnlock()

			if len(keys) != 1 {
				t.Fatalf("expected 1 key, got %d", len(keys))
			}
			if !bytes.Equal(vals[0], []byte("value1")) {
				t.Errorf("expected value1, got %s", vals[0])
			}
		})
	}
}

func TestBackendSize(t *testing.T) {
	for _, suite := range getTestBackends(t) {
		t.Run(suite.name, func(t *testing.T) {
			defer suite.cleanup()

			// Initial size should be >= 0
			if suite.backend.Size() < 0 {
				t.Error("Size should be >= 0")
			}
			if suite.backend.SizeInUse() < 0 {
				t.Error("SizeInUse should be >= 0")
			}
		})
	}
}

func TestBackendDefrag(t *testing.T) {
	for _, suite := range getTestBackends(t) {
		t.Run(suite.name, func(t *testing.T) {
			defer suite.cleanup()

			// Defrag should not error
			if err := suite.backend.Defrag(); err != nil {
				t.Errorf("Defrag failed: %v", err)
			}
		})
	}
}

func TestBackendMultipleBuckets(t *testing.T) {
	for _, suite := range getTestBackends(t) {
		t.Run(suite.name, func(t *testing.T) {
			defer suite.cleanup()

			// Create multiple buckets
			tx := suite.backend.BatchTx()
			tx.Lock()
			tx.UnsafeCreateBucket(testBucketKey)
			tx.UnsafeCreateBucket(testBucketMeta)
			tx.UnsafePut(testBucketKey, []byte("key1"), []byte("value1"))
			tx.UnsafePut(testBucketMeta, []byte("meta1"), []byte("metavalue1"))
			tx.Unlock()

			suite.backend.ForceCommit()

			// Read from both buckets
			rtx := suite.backend.ReadTx()
			rtx.RLock()
			keys1, vals1 := rtx.UnsafeRange(testBucketKey, []byte("key1"), nil, 0)
			keys2, vals2 := rtx.UnsafeRange(testBucketMeta, []byte("meta1"), nil, 0)
			rtx.RUnlock()

			if len(keys1) != 1 || !bytes.Equal(vals1[0], []byte("value1")) {
				t.Error("failed to read from testBucketKey")
			}
			if len(keys2) != 1 || !bytes.Equal(vals2[0], []byte("metavalue1")) {
				t.Error("failed to read from testBucketMeta")
			}
		})
	}
}

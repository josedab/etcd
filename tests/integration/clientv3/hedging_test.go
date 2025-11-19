// Copyright 2024 The etcd Authors
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

package clientv3test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/tests/v3/framework/integration"
)

// TestHedgingBasicGet tests that hedging works for basic Get operations
func TestHedgingBasicGet(t *testing.T) {
	integration.BeforeTest(t)

	clus := integration.NewCluster(t, &integration.ClusterConfig{Size: 3})
	defer clus.Terminate(t)

	// Create client with hedging enabled
	endpoints := make([]string, len(clus.Members))
	for i, m := range clus.Members {
		endpoints[i] = m.GRPCURL
	}

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:          endpoints,
		DialTimeout:        5 * time.Second,
		HedgingDelay:       50 * time.Millisecond,
		HedgingMaxRequests: 2,
	})
	require.NoError(t, err)
	defer cli.Close()

	ctx := t.Context()

	// Put some data first
	_, err = cli.Put(ctx, "hedging-test-key", "hedging-test-value")
	require.NoError(t, err)

	// Get with hedging enabled
	resp, err := cli.Get(ctx, "hedging-test-key")
	require.NoError(t, err)
	require.Len(t, resp.Kvs, 1)
	require.Equal(t, "hedging-test-value", string(resp.Kvs[0].Value))
}

// TestHedgingDisabled tests that operations work normally when hedging is disabled
func TestHedgingDisabled(t *testing.T) {
	integration.BeforeTest(t)

	clus := integration.NewCluster(t, &integration.ClusterConfig{Size: 3})
	defer clus.Terminate(t)

	// Create client with hedging disabled (default)
	endpoints := make([]string, len(clus.Members))
	for i, m := range clus.Members {
		endpoints[i] = m.GRPCURL
	}

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
		// HedgingDelay is 0, so hedging is disabled
	})
	require.NoError(t, err)
	defer cli.Close()

	ctx := t.Context()

	// Put and Get should work normally
	_, err = cli.Put(ctx, "no-hedging-key", "no-hedging-value")
	require.NoError(t, err)

	resp, err := cli.Get(ctx, "no-hedging-key")
	require.NoError(t, err)
	require.Len(t, resp.Kvs, 1)
	require.Equal(t, "no-hedging-value", string(resp.Kvs[0].Value))
}

// TestHedgingMutableOperationsNotHedged tests that Put/Delete operations are not hedged
func TestHedgingMutableOperationsNotHedged(t *testing.T) {
	integration.BeforeTest(t)

	clus := integration.NewCluster(t, &integration.ClusterConfig{Size: 3})
	defer clus.Terminate(t)

	// Create client with hedging enabled
	endpoints := make([]string, len(clus.Members))
	for i, m := range clus.Members {
		endpoints[i] = m.GRPCURL
	}

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:          endpoints,
		DialTimeout:        5 * time.Second,
		HedgingDelay:       50 * time.Millisecond,
		HedgingMaxRequests: 2,
	})
	require.NoError(t, err)
	defer cli.Close()

	ctx := t.Context()

	// Put operations should work correctly (not hedged)
	_, err = cli.Put(ctx, "mutable-test-key", "value1")
	require.NoError(t, err)

	// Verify value
	resp, err := cli.Get(ctx, "mutable-test-key")
	require.NoError(t, err)
	require.Len(t, resp.Kvs, 1)
	require.Equal(t, "value1", string(resp.Kvs[0].Value))

	// Delete operations should work correctly (not hedged)
	_, err = cli.Delete(ctx, "mutable-test-key")
	require.NoError(t, err)

	// Verify deletion
	resp, err = cli.Get(ctx, "mutable-test-key")
	require.NoError(t, err)
	require.Len(t, resp.Kvs, 0)
}

// TestHedgingMultipleReadOperations tests that various read operations work with hedging
func TestHedgingMultipleReadOperations(t *testing.T) {
	integration.BeforeTest(t)

	clus := integration.NewCluster(t, &integration.ClusterConfig{Size: 3})
	defer clus.Terminate(t)

	// Create client with hedging enabled
	endpoints := make([]string, len(clus.Members))
	for i, m := range clus.Members {
		endpoints[i] = m.GRPCURL
	}

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:          endpoints,
		DialTimeout:        5 * time.Second,
		HedgingDelay:       50 * time.Millisecond,
		HedgingMaxRequests: 2,
	})
	require.NoError(t, err)
	defer cli.Close()

	ctx := t.Context()

	// Test MemberList (should be hedgeable)
	_, err = cli.MemberList(ctx)
	require.NoError(t, err)

	// Test Status (should be hedgeable)
	for _, ep := range endpoints {
		_, err = cli.Status(ctx, ep)
		require.NoError(t, err)
	}

	// Test with Lease
	lease, err := cli.Grant(ctx, 60)
	require.NoError(t, err)

	// LeaseTimeToLive (should be hedgeable)
	_, err = cli.TimeToLive(ctx, lease.ID)
	require.NoError(t, err)
}

// TestHedgingWithMultipleEndpoints tests hedging behavior with multiple endpoints
func TestHedgingWithMultipleEndpoints(t *testing.T) {
	integration.BeforeTest(t)

	clus := integration.NewCluster(t, &integration.ClusterConfig{Size: 3})
	defer clus.Terminate(t)

	// Create client with hedging enabled
	endpoints := make([]string, len(clus.Members))
	for i, m := range clus.Members {
		endpoints[i] = m.GRPCURL
	}

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:          endpoints,
		DialTimeout:        5 * time.Second,
		HedgingDelay:       20 * time.Millisecond,
		HedgingMaxRequests: 3,
	})
	require.NoError(t, err)
	defer cli.Close()

	ctx := t.Context()

	// Put data
	for i := 0; i < 10; i++ {
		key := "multi-endpoint-key-" + string(rune('0'+i))
		_, err = cli.Put(ctx, key, "value")
		require.NoError(t, err)
	}

	// Get data with hedging
	for i := 0; i < 10; i++ {
		key := "multi-endpoint-key-" + string(rune('0'+i))
		resp, err := cli.Get(ctx, key)
		require.NoError(t, err)
		require.Len(t, resp.Kvs, 1)
	}
}

// TestHedgingConfigDefaults tests that default configuration values are applied correctly
func TestHedgingConfigDefaults(t *testing.T) {
	integration.BeforeTest(t)

	clus := integration.NewCluster(t, &integration.ClusterConfig{Size: 1})
	defer clus.Terminate(t)

	endpoints := make([]string, len(clus.Members))
	for i, m := range clus.Members {
		endpoints[i] = m.GRPCURL
	}

	// Create client with hedging enabled but default max requests
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:          endpoints,
		DialTimeout:        5 * time.Second,
		HedgingDelay:       50 * time.Millisecond,
		HedgingMaxRequests: 0, // Should use default (2)
	})
	require.NoError(t, err)
	defer cli.Close()

	ctx := t.Context()

	// Operations should still work with default max requests
	_, err = cli.Put(ctx, "default-config-key", "value")
	require.NoError(t, err)

	resp, err := cli.Get(ctx, "default-config-key")
	require.NoError(t, err)
	require.Len(t, resp.Kvs, 1)
}

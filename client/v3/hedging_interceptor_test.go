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

package clientv3

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIsHedgeable(t *testing.T) {
	tests := []struct {
		name   string
		method string
		want   bool
	}{
		{
			name:   "Range is hedgeable",
			method: "/etcdserverpb.KV/Range",
			want:   true,
		},
		{
			name:   "LeaseTimeToLive is hedgeable",
			method: "/etcdserverpb.Lease/LeaseTimeToLive",
			want:   true,
		},
		{
			name:   "MemberList is hedgeable",
			method: "/etcdserverpb.Cluster/MemberList",
			want:   true,
		},
		{
			name:   "Status is hedgeable",
			method: "/etcdserverpb.Maintenance/Status",
			want:   true,
		},
		{
			name:   "UserList is hedgeable",
			method: "/etcdserverpb.Auth/UserList",
			want:   true,
		},
		{
			name:   "Put is not hedgeable",
			method: "/etcdserverpb.KV/Put",
			want:   false,
		},
		{
			name:   "DeleteRange is not hedgeable",
			method: "/etcdserverpb.KV/DeleteRange",
			want:   false,
		},
		{
			name:   "Txn is not hedgeable",
			method: "/etcdserverpb.KV/Txn",
			want:   false,
		},
		{
			name:   "LeaseGrant is not hedgeable",
			method: "/etcdserverpb.Lease/LeaseGrant",
			want:   false,
		},
		{
			name:   "LeaseRevoke is not hedgeable",
			method: "/etcdserverpb.Lease/LeaseRevoke",
			want:   false,
		},
		{
			name:   "MemberAdd is not hedgeable",
			method: "/etcdserverpb.Cluster/MemberAdd",
			want:   false,
		},
		{
			name:   "Unknown method is not hedgeable",
			method: "/unknown/Method",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isHedgeable(tt.method); got != tt.want {
				t.Errorf("isHedgeable(%s) = %v, want %v", tt.method, got, tt.want)
			}
		})
	}
}

func TestIsHedgingRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error is not retryable",
			err:  nil,
			want: false,
		},
		{
			name: "Unavailable is retryable",
			err:  status.Error(codes.Unavailable, "unavailable"),
			want: true,
		},
		{
			name: "DeadlineExceeded is retryable",
			err:  status.Error(codes.DeadlineExceeded, "timeout"),
			want: true,
		},
		{
			name: "ResourceExhausted is retryable",
			err:  status.Error(codes.ResourceExhausted, "overloaded"),
			want: true,
		},
		{
			name: "NotFound is not retryable",
			err:  status.Error(codes.NotFound, "not found"),
			want: false,
		},
		{
			name: "InvalidArgument is not retryable",
			err:  status.Error(codes.InvalidArgument, "invalid"),
			want: false,
		},
		{
			name: "PermissionDenied is not retryable",
			err:  status.Error(codes.PermissionDenied, "denied"),
			want: false,
		},
		{
			name: "Internal is not retryable",
			err:  status.Error(codes.Internal, "internal"),
			want: false,
		},
		{
			name: "Non-gRPC error is not retryable",
			err:  errors.New("some error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isHedgingRetryable(tt.err); got != tt.want {
				t.Errorf("isHedgingRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChainUnaryInterceptors(t *testing.T) {
	t.Run("empty chain", func(t *testing.T) {
		interceptor := chainUnaryInterceptors()
		called := false
		invoker := func(ctx context.Context, method string, req, reply interface{},
			cc *grpc.ClientConn, opts ...grpc.CallOption) error {
			called = true
			return nil
		}

		err := interceptor(context.Background(), "/test", nil, nil, nil, invoker, nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !called {
			t.Error("invoker was not called")
		}
	})

	t.Run("single interceptor", func(t *testing.T) {
		var interceptorCalled bool
		interceptor1 := func(ctx context.Context, method string, req, reply interface{},
			cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			interceptorCalled = true
			return invoker(ctx, method, req, reply, cc, opts...)
		}

		chain := chainUnaryInterceptors(interceptor1)
		invokerCalled := false
		invoker := func(ctx context.Context, method string, req, reply interface{},
			cc *grpc.ClientConn, opts ...grpc.CallOption) error {
			invokerCalled = true
			return nil
		}

		err := chain(context.Background(), "/test", nil, nil, nil, invoker, nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !interceptorCalled {
			t.Error("interceptor was not called")
		}
		if !invokerCalled {
			t.Error("invoker was not called")
		}
	})

	t.Run("multiple interceptors execute in order", func(t *testing.T) {
		var order []int
		var mu sync.Mutex

		makeInterceptor := func(id int) grpc.UnaryClientInterceptor {
			return func(ctx context.Context, method string, req, reply interface{},
				cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
				mu.Lock()
				order = append(order, id)
				mu.Unlock()
				return invoker(ctx, method, req, reply, cc, opts...)
			}
		}

		chain := chainUnaryInterceptors(makeInterceptor(1), makeInterceptor(2), makeInterceptor(3))
		invoker := func(ctx context.Context, method string, req, reply interface{},
			cc *grpc.ClientConn, opts ...grpc.CallOption) error {
			mu.Lock()
			order = append(order, 0)
			mu.Unlock()
			return nil
		}

		err := chain(context.Background(), "/test", nil, nil, nil, invoker, nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		expected := []int{1, 2, 3, 0}
		if len(order) != len(expected) {
			t.Errorf("expected %d calls, got %d", len(expected), len(order))
		}
		for i, v := range expected {
			if order[i] != v {
				t.Errorf("call order[%d] = %d, want %d", i, order[i], v)
			}
		}
	})
}

func TestHedgingInterceptor_Disabled(t *testing.T) {
	lg, _ := zap.NewDevelopment()
	c := &Client{
		lg:   lg,
		lgMu: new(sync.RWMutex),
		cfg: Config{
			HedgingDelay: 0, // Disabled
		},
	}

	interceptor := c.hedgingInterceptor()
	invokerCalled := false
	invoker := func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		invokerCalled = true
		return nil
	}

	err := interceptor(context.Background(), "/etcdserverpb.KV/Range", nil, nil, nil, invoker, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !invokerCalled {
		t.Error("invoker should be called directly when hedging is disabled")
	}
}

func TestHedgingInterceptor_NonHedgeableMethod(t *testing.T) {
	lg, _ := zap.NewDevelopment()
	c := &Client{
		lg:   lg,
		lgMu: new(sync.RWMutex),
		cfg: Config{
			HedgingDelay:       50 * time.Millisecond,
			HedgingMaxRequests: 2,
		},
	}

	interceptor := c.hedgingInterceptor()
	invokerCalled := false
	invoker := func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		invokerCalled = true
		return nil
	}

	// Put is not hedgeable
	err := interceptor(context.Background(), "/etcdserverpb.KV/Put", nil, nil, nil, invoker, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !invokerCalled {
		t.Error("invoker should be called directly for non-hedgeable method")
	}
}

func TestHedgingInterceptor_ImmediateSuccess(t *testing.T) {
	lg, _ := zap.NewDevelopment()
	c := &Client{
		lg:   lg,
		lgMu: new(sync.RWMutex),
		cfg: Config{
			HedgingDelay:       50 * time.Millisecond,
			HedgingMaxRequests: 2,
		},
	}

	interceptor := c.hedgingInterceptor()
	var callCount int32
	invoker := func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		atomic.AddInt32(&callCount, 1)
		return nil
	}

	err := interceptor(context.Background(), "/etcdserverpb.KV/Range", nil, nil, nil, invoker, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Only one call should be made since first succeeded immediately
	if count := atomic.LoadInt32(&callCount); count != 1 {
		t.Errorf("expected 1 call, got %d", count)
	}
}

func TestHedgingInterceptor_HedgedRequestOnDelay(t *testing.T) {
	lg, _ := zap.NewDevelopment()
	c := &Client{
		lg:   lg,
		lgMu: new(sync.RWMutex),
		cfg: Config{
			HedgingDelay:       10 * time.Millisecond,
			HedgingMaxRequests: 3,
		},
	}

	interceptor := c.hedgingInterceptor()
	var callCount int32
	var firstCallDone int32

	invoker := func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		count := atomic.AddInt32(&callCount, 1)
		if count == 1 {
			// First call is slow
			time.Sleep(50 * time.Millisecond)
			atomic.StoreInt32(&firstCallDone, 1)
		}
		return nil
	}

	err := interceptor(context.Background(), "/etcdserverpb.KV/Range", nil, nil, nil, invoker, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// At least 2 calls should be made (initial + hedge)
	if count := atomic.LoadInt32(&callCount); count < 2 {
		t.Errorf("expected at least 2 calls, got %d", count)
	}
}

func TestHedgingInterceptor_NonRetryableError(t *testing.T) {
	lg, _ := zap.NewDevelopment()
	c := &Client{
		lg:   lg,
		lgMu: new(sync.RWMutex),
		cfg: Config{
			HedgingDelay:       10 * time.Millisecond,
			HedgingMaxRequests: 3,
		},
	}

	interceptor := c.hedgingInterceptor()
	expectedErr := status.Error(codes.NotFound, "not found")

	invoker := func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		return expectedErr
	}

	err := interceptor(context.Background(), "/etcdserverpb.KV/Range", nil, nil, nil, invoker, nil)
	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestHedgingInterceptor_ContextCancellation(t *testing.T) {
	lg, _ := zap.NewDevelopment()
	c := &Client{
		lg:   lg,
		lgMu: new(sync.RWMutex),
		cfg: Config{
			HedgingDelay:       10 * time.Millisecond,
			HedgingMaxRequests: 3,
		},
	}

	interceptor := c.hedgingInterceptor()

	invoker := func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		// Block until context is cancelled
		<-ctx.Done()
		return ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := interceptor(ctx, "/etcdserverpb.KV/Range", nil, nil, nil, invoker, nil)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestHedgingInterceptor_DefaultMaxRequests(t *testing.T) {
	lg, _ := zap.NewDevelopment()
	c := &Client{
		lg:   lg,
		lgMu: new(sync.RWMutex),
		cfg: Config{
			HedgingDelay:       5 * time.Millisecond,
			HedgingMaxRequests: 0, // Should use default (2)
		},
	}

	interceptor := c.hedgingInterceptor()
	var callCount int32

	invoker := func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		atomic.AddInt32(&callCount, 1)
		// Return error to trigger hedging
		return status.Error(codes.Unavailable, "unavailable")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_ = interceptor(ctx, "/etcdserverpb.KV/Range", nil, nil, nil, invoker, nil)

	// Should make at most 2 calls (default max)
	if count := atomic.LoadInt32(&callCount); count > defaultHedgingMaxRequests {
		t.Errorf("expected at most %d calls, got %d", defaultHedgingMaxRequests, count)
	}
}

func TestHedgingInterceptor_AllRequestsFail(t *testing.T) {
	lg, _ := zap.NewDevelopment()
	c := &Client{
		lg:   lg,
		lgMu: new(sync.RWMutex),
		cfg: Config{
			HedgingDelay:       5 * time.Millisecond,
			HedgingMaxRequests: 2,
		},
	}

	interceptor := c.hedgingInterceptor()
	expectedErr := status.Error(codes.Unavailable, "all endpoints unavailable")

	invoker := func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		return expectedErr
	}

	err := interceptor(context.Background(), "/etcdserverpb.KV/Range", nil, nil, nil, invoker, nil)
	if err == nil {
		t.Error("expected error when all requests fail")
	}
	if status.Code(err) != codes.Unavailable {
		t.Errorf("expected Unavailable error, got %v", err)
	}
}

func TestHedgingInterceptor_SecondRequestSucceeds(t *testing.T) {
	lg, _ := zap.NewDevelopment()
	c := &Client{
		lg:   lg,
		lgMu: new(sync.RWMutex),
		cfg: Config{
			HedgingDelay:       10 * time.Millisecond,
			HedgingMaxRequests: 2,
		},
	}

	interceptor := c.hedgingInterceptor()
	var callCount int32

	invoker := func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		count := atomic.AddInt32(&callCount, 1)
		if count == 1 {
			// First call fails
			return status.Error(codes.Unavailable, "unavailable")
		}
		// Second call succeeds
		return nil
	}

	err := interceptor(context.Background(), "/etcdserverpb.KV/Range", nil, nil, nil, invoker, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if count := atomic.LoadInt32(&callCount); count != 2 {
		t.Errorf("expected 2 calls, got %d", count)
	}
}

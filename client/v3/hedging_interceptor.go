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
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Default hedging configuration values
const (
	// defaultHedgingMaxRequests is the default maximum number of concurrent hedged requests.
	defaultHedgingMaxRequests = 2
)

// hedgeableMethods defines which gRPC methods are safe for hedging.
// Only idempotent read-only operations should be hedged.
var hedgeableMethods = map[string]bool{
	"/etcdserverpb.KV/Range":                true,
	"/etcdserverpb.Lease/LeaseTimeToLive":   true,
	"/etcdserverpb.Lease/LeaseLeases":       true,
	"/etcdserverpb.Cluster/MemberList":      true,
	"/etcdserverpb.Maintenance/Status":      true,
	"/etcdserverpb.Maintenance/Alarm":       true,
	"/etcdserverpb.Maintenance/Hash":        true,
	"/etcdserverpb.Maintenance/HashKV":      true,
	"/etcdserverpb.Auth/UserList":           true,
	"/etcdserverpb.Auth/UserGet":            true,
	"/etcdserverpb.Auth/RoleGet":            true,
	"/etcdserverpb.Auth/RoleList":           true,
	// NOT hedgeable: Put, Delete, Txn, LeaseGrant, LeaseRevoke, etc.
	// These are mutable operations that should not be duplicated.
}

// isHedgeable returns true if the given gRPC method is safe to hedge.
func isHedgeable(method string) bool {
	return hedgeableMethods[method]
}

// hedgingResult holds the result from a hedged request.
type hedgingResult struct {
	err error
}

// hedgingInterceptor returns a gRPC unary client interceptor that implements request hedging.
// Hedging sends duplicate requests to reduce tail latency by racing responses from multiple endpoints.
func (c *Client) hedgingInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {

		// Skip hedging if disabled or method is not hedgeable
		if c.cfg.HedgingDelay <= 0 || !isHedgeable(method) {
			return invoker(ctx, method, req, reply, cc, opts...)
		}

		return c.hedgedInvoke(ctx, method, req, reply, cc, invoker, opts)
	}
}

// hedgedInvoke executes a gRPC call with hedging support.
// It sends the initial request immediately, then sends additional hedged requests
// after HedgingDelay intervals until a successful response is received or max requests are sent.
func (c *Client) hedgedInvoke(ctx context.Context, method string, req, reply interface{},
	cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts []grpc.CallOption) error {

	maxRequests := c.cfg.HedgingMaxRequests
	if maxRequests <= 0 {
		maxRequests = defaultHedgingMaxRequests
	}

	// Create a cancellable context for all hedged requests
	hedgeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	resultCh := make(chan hedgingResult, maxRequests)

	// Track the number of active requests
	var activeRequests int
	var mu sync.Mutex

	// Use a WaitGroup to ensure all goroutines complete before returning
	var wg sync.WaitGroup

	// Helper to send a request
	sendRequest := func(requestNum int) {
		wg.Add(1)
		go func() {
			defer wg.Done()

			c.lg.Debug(
				"sending hedged request",
				zap.String("method", method),
				zap.Int("requestNum", requestNum),
			)

			// Create a copy of reply for this request to avoid race conditions
			// Note: We use the same reply object since we'll cancel other requests
			// once we get a successful response
			err := invoker(hedgeCtx, method, req, reply, cc, opts...)

			select {
			case resultCh <- hedgingResult{err: err}:
			case <-hedgeCtx.Done():
				// Context cancelled, result not needed
			}
		}()
	}

	// Send initial request
	mu.Lock()
	activeRequests = 1
	mu.Unlock()
	sendRequest(1)

	// Set up hedging timer
	hedgeTimer := time.NewTimer(c.cfg.HedgingDelay)
	defer hedgeTimer.Stop()

	var lastErr error
	for {
		select {
		case result := <-resultCh:
			mu.Lock()
			activeRequests--
			remaining := activeRequests
			mu.Unlock()

			if result.err == nil {
				// Success! Cancel other requests and return
				c.lg.Debug(
					"hedged request succeeded",
					zap.String("method", method),
				)
				cancel()
				// Wait for other goroutines to finish
				wg.Wait()
				return nil
			}

			lastErr = result.err

			// Check if this is a non-retryable error
			if !isHedgingRetryable(result.err) {
				c.lg.Debug(
					"hedged request failed with non-retryable error",
					zap.String("method", method),
					zap.Error(result.err),
				)
				cancel()
				wg.Wait()
				return result.err
			}

			c.lg.Debug(
				"hedged request failed",
				zap.String("method", method),
				zap.Int("remaining", remaining),
				zap.Error(result.err),
			)

			// If no more active requests and we've hit the max, return the error
			if remaining == 0 {
				mu.Lock()
				currentActive := activeRequests
				mu.Unlock()
				if currentActive == 0 {
					wg.Wait()
					return lastErr
				}
			}

		case <-hedgeTimer.C:
			mu.Lock()
			currentActive := activeRequests
			mu.Unlock()

			if currentActive < maxRequests {
				mu.Lock()
				activeRequests++
				requestNum := activeRequests
				mu.Unlock()

				sendRequest(requestNum)

				// Reset timer for next hedge if we haven't hit max
				mu.Lock()
				if activeRequests < maxRequests {
					hedgeTimer.Reset(c.cfg.HedgingDelay)
				}
				mu.Unlock()
			}

		case <-ctx.Done():
			cancel()
			wg.Wait()
			return ctx.Err()
		}
	}
}

// isHedgingRetryable returns true if the error should be considered retryable for hedging purposes.
// We only retry on transient errors that might succeed on a different endpoint.
func isHedgingRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Check gRPC status code
	st, ok := status.FromError(err)
	if !ok {
		return false
	}

	switch st.Code() {
	case codes.Unavailable:
		// Endpoint is unavailable, another endpoint might be available
		return true
	case codes.DeadlineExceeded:
		// Request timed out, might succeed on another endpoint
		return true
	case codes.ResourceExhausted:
		// Server is overloaded, another endpoint might not be
		return true
	default:
		// Other errors are likely not going to succeed on retry
		return false
	}
}

// chainUnaryInterceptors chains multiple unary client interceptors into a single interceptor.
// The first interceptor will be the outermost, and the last will be closest to the actual call.
func chainUnaryInterceptors(interceptors ...grpc.UnaryClientInterceptor) grpc.UnaryClientInterceptor {
	n := len(interceptors)
	if n == 0 {
		return func(ctx context.Context, method string, req, reply interface{},
			cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
	}
	if n == 1 {
		return interceptors[0]
	}

	return func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		// Build the chain from inside out
		chainedInvoker := invoker
		for i := n - 1; i >= 0; i-- {
			// Capture the current index and invoker
			interceptor := interceptors[i]
			currentInvoker := chainedInvoker
			chainedInvoker = func(ctx context.Context, method string, req, reply interface{},
				cc *grpc.ClientConn, _ grpc.UnaryInvoker, opts ...grpc.CallOption) error {
				return interceptor(ctx, method, req, reply, cc, currentInvoker, opts...)
			}
		}
		return chainedInvoker(ctx, method, req, reply, cc, invoker, opts...)
	}
}

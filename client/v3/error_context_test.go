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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
)

func TestExtractErrorContext(t *testing.T) {
	// Create an error with context
	originalErr := rpctypes.ErrGRPCTimeout
	ctx := &rpctypes.ErrorContext{
		RequestID:  "test-request-id1",
		MemberID:   12345,
		Operation:  "Put",
		Key:        "my-key",
		Duration:   500 * time.Millisecond,
		LeaderID:   67890,
		Cause:      rpctypes.CauseTimeout,
		RetryAfter: 5 * time.Second,
	}

	wrappedErr := rpctypes.ErrorWithContext(originalErr, ctx)

	// Extract using client helper
	extractedCtx := ExtractErrorContext(wrappedErr)
	assert.NotNil(t, extractedCtx)
	assert.Equal(t, "test-request-id1", extractedCtx.RequestID)
	assert.Equal(t, uint64(12345), extractedCtx.MemberID)
	assert.Equal(t, "Put", extractedCtx.Operation)
}

func TestHasErrorContext(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "error with context",
			err:      rpctypes.ErrorWithContext(rpctypes.ErrGRPCTimeout, &rpctypes.ErrorContext{RequestID: "test"}),
			expected: true,
		},
		{
			name:     "error without context",
			err:      rpctypes.ErrGRPCTimeout,
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasErrorContext(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestErrorContextFromError(t *testing.T) {
	// With context
	ctx := &rpctypes.ErrorContext{
		RequestID: "test-id",
		Operation: "Get",
	}
	wrappedErr := rpctypes.ErrorWithContext(rpctypes.ErrGRPCTimeout, ctx)

	extractedCtx, ok := ErrorContextFromError(wrappedErr)
	assert.True(t, ok)
	assert.NotNil(t, extractedCtx)
	assert.Equal(t, "test-id", extractedCtx.RequestID)

	// Without context
	extractedCtx, ok = ErrorContextFromError(rpctypes.ErrGRPCEmptyKey)
	assert.False(t, ok)
	assert.Nil(t, extractedCtx)
}

func TestShouldRetry(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedRetry  bool
		expectedMinWait time.Duration
	}{
		{
			name: "timeout with retry suggestion",
			err: rpctypes.ErrorWithContext(
				rpctypes.ErrGRPCTimeout,
				&rpctypes.ErrorContext{
					Cause:      rpctypes.CauseTimeout,
					RetryAfter: 5 * time.Second,
				},
			),
			expectedRetry:  true,
			expectedMinWait: 5 * time.Second,
		},
		{
			name: "no leader - retryable cause",
			err: rpctypes.ErrorWithContext(
				rpctypes.ErrGRPCNoLeader,
				&rpctypes.ErrorContext{
					Cause: rpctypes.CauseNoLeader,
				},
			),
			expectedRetry:  true,
			expectedMinWait: 0,
		},
		{
			name: "connection lost - retryable cause",
			err: rpctypes.ErrorWithContext(
				rpctypes.ErrGRPCTimeout,
				&rpctypes.ErrorContext{
					Cause: rpctypes.CauseConnectionLost,
				},
			),
			expectedRetry:  true,
			expectedMinWait: 0,
		},
		{
			name: "invalid request - not retryable",
			err: rpctypes.ErrorWithContext(
				rpctypes.ErrGRPCEmptyKey,
				&rpctypes.ErrorContext{
					Cause: rpctypes.CauseInvalidRequest,
				},
			),
			expectedRetry:  false,
			expectedMinWait: 0,
		},
		{
			name:           "error without context",
			err:            rpctypes.ErrGRPCTimeout,
			expectedRetry:  false,
			expectedMinWait: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shouldRetry, wait := ShouldRetry(tt.err)
			assert.Equal(t, tt.expectedRetry, shouldRetry)
			assert.Equal(t, tt.expectedMinWait, wait)
		})
	}
}

func TestErrorContextIntegrationScenario(t *testing.T) {
	// Simulate a realistic error handling scenario
	ctx := &rpctypes.ErrorContext{
		RequestID:  rpctypes.GenerateRequestID(),
		MemberID:   1,
		Operation:  "Put",
		Key:        "user/profile/12345",
		Duration:   2500 * time.Millisecond,
		LeaderID:   2,
		Cause:      rpctypes.CauseTimeout,
		RetryAfter: 5 * time.Second,
	}

	err := rpctypes.ErrorWithContext(rpctypes.ErrGRPCTimeout, ctx)

	// Simulate client error handling
	if ectx, ok := ErrorContextFromError(err); ok {
		// Verify we can access all context information
		assert.Equal(t, 16, len(ectx.RequestID))
		assert.Equal(t, uint64(1), ectx.MemberID)
		assert.Equal(t, "Put", ectx.Operation)
		assert.Equal(t, "user/profile/12345", ectx.Key)
		assert.Equal(t, 2500*time.Millisecond, ectx.Duration)
		assert.Equal(t, uint64(2), ectx.LeaderID)
		assert.Equal(t, rpctypes.CauseTimeout, ectx.Cause)
		assert.Equal(t, 5*time.Second, ectx.RetryAfter)

		// Check retry logic
		shouldRetry, wait := ShouldRetry(err)
		assert.True(t, shouldRetry)
		assert.Equal(t, 5*time.Second, wait)
	} else {
		t.Fatal("Expected to extract error context")
	}
}

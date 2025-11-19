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

package rpctypes

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/status"
)

func TestGenerateRequestID(t *testing.T) {
	id1 := GenerateRequestID()
	id2 := GenerateRequestID()

	// Should be 16 characters (8 bytes as hex)
	assert.Equal(t, 16, len(id1), "Request ID should be 16 characters")
	assert.Equal(t, 16, len(id2), "Request ID should be 16 characters")

	// Should be unique
	assert.NotEqual(t, id1, id2, "Request IDs should be unique")
}

func TestTruncateKey(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		maxLen      int
		isAuthError bool
		expected    string
	}{
		{
			name:     "short key",
			key:      "mykey",
			maxLen:   50,
			expected: "mykey",
		},
		{
			name:     "exactly max length",
			key:      "12345678901234567890123456789012345678901234567890",
			maxLen:   50,
			expected: "12345678901234567890123456789012345678901234567890",
		},
		{
			name:     "longer than max",
			key:      "123456789012345678901234567890123456789012345678901234567890",
			maxLen:   50,
			expected: "12345678901234567890123456789012345678901234567890...",
		},
		{
			name:        "auth error omits key",
			key:         "sensitive-key",
			maxLen:      50,
			isAuthError: true,
			expected:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TruncateKey(tt.key, tt.maxLen, tt.isAuthError)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "timeout error",
			err:      ErrGRPCTimeout,
			expected: CauseTimeout,
		},
		{
			name:     "no leader",
			err:      ErrGRPCNoLeader,
			expected: CauseNoLeader,
		},
		{
			name:     "not leader",
			err:      ErrGRPCNotLeader,
			expected: CauseNotLeader,
		},
		{
			name:     "quota exceeded",
			err:      ErrGRPCNoSpace,
			expected: CauseQuotaExceeded,
		},
		{
			name:     "corruption",
			err:      ErrGRPCCorrupt,
			expected: CauseCorruption,
		},
		{
			name:     "leader changed",
			err:      ErrGRPCLeaderChanged,
			expected: CauseLeaderChanged,
		},
		{
			name:     "compacted",
			err:      ErrGRPCCompacted,
			expected: CauseCompacted,
		},
		{
			name:     "auth failed",
			err:      ErrGRPCAuthFailed,
			expected: CauseAuthFailed,
		},
		{
			name:     "permission denied",
			err:      ErrGRPCPermissionDenied,
			expected: CausePermissionDenied,
		},
		{
			name:     "invalid request - empty key",
			err:      ErrGRPCEmptyKey,
			expected: CauseInvalidRequest,
		},
		{
			name:     "stopped",
			err:      ErrGRPCStopped,
			expected: CauseStopped,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "auth failed",
			err:      ErrGRPCAuthFailed,
			expected: true,
		},
		{
			name:     "invalid auth token",
			err:      ErrGRPCInvalidAuthToken,
			expected: true,
		},
		{
			name:     "permission denied",
			err:      ErrGRPCPermissionDenied,
			expected: true,
		},
		{
			name:     "timeout - not auth",
			err:      ErrGRPCTimeout,
			expected: false,
		},
		{
			name:     "empty key - not auth",
			err:      ErrGRPCEmptyKey,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsAuthError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetRetryAfter(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected time.Duration
	}{
		{
			name:     "quota exceeded",
			err:      ErrGRPCNoSpace,
			expected: 30 * time.Second,
		},
		{
			name:     "rate limited",
			err:      ErrGRPCTooManyRequests,
			expected: 1 * time.Second,
		},
		{
			name:     "no leader",
			err:      ErrGRPCNoLeader,
			expected: 2 * time.Second,
		},
		{
			name:     "timeout",
			err:      ErrGRPCTimeout,
			expected: 5 * time.Second,
		},
		{
			name:     "empty key - no retry",
			err:      ErrGRPCEmptyKey,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetRetryAfter(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestErrorWithContextAndExtract(t *testing.T) {
	// Create an error with context
	originalErr := ErrGRPCTimeout
	ctx := &ErrorContext{
		RequestID:  "abc123def456ghij",
		MemberID:   12345,
		Operation:  "Put",
		Key:        "test-key",
		Duration:   500 * time.Millisecond,
		LeaderID:   67890,
		Cause:      CauseTimeout,
		RetryAfter: 5 * time.Second,
	}

	wrappedErr := ErrorWithContext(originalErr, ctx)

	// Verify it's still a gRPC error
	st := status.Convert(wrappedErr)
	assert.NotNil(t, st)

	// Extract context back
	extractedCtx := ExtractErrorContext(wrappedErr)
	assert.NotNil(t, extractedCtx, "Should extract context from error")

	// Verify all fields
	assert.Equal(t, ctx.RequestID, extractedCtx.RequestID)
	assert.Equal(t, ctx.MemberID, extractedCtx.MemberID)
	assert.Equal(t, ctx.Operation, extractedCtx.Operation)
	assert.Equal(t, ctx.Key, extractedCtx.Key)
	assert.Equal(t, ctx.Duration, extractedCtx.Duration)
	assert.Equal(t, ctx.LeaderID, extractedCtx.LeaderID)
	assert.Equal(t, ctx.Cause, extractedCtx.Cause)
	assert.Equal(t, ctx.RetryAfter, extractedCtx.RetryAfter)
}

func TestErrorWithContextNilCases(t *testing.T) {
	// Nil error should return nil
	result := ErrorWithContext(nil, &ErrorContext{})
	assert.Nil(t, result)

	// Nil context should return original error
	originalErr := ErrGRPCTimeout
	result = ErrorWithContext(originalErr, nil)
	assert.Equal(t, originalErr, result)
}

func TestExtractErrorContextNoContext(t *testing.T) {
	// Plain error without context
	err := ErrGRPCTimeout

	extractedCtx := ExtractErrorContext(err)
	assert.Nil(t, extractedCtx, "Should return nil for error without context")
}

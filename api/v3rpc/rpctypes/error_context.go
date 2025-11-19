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
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "go.etcd.io/etcd/api/v3/etcdserverpb"
)

// ErrorContext holds structured error context for debugging.
type ErrorContext struct {
	// RequestID is a unique identifier for the request.
	RequestID string
	// MemberID is the ID of the member that processed the request.
	MemberID uint64
	// Operation is the type of operation (e.g., "Put", "Range").
	Operation string
	// Key is the key involved in the operation (truncated for security).
	Key string
	// Duration is how long the operation took before failing.
	Duration time.Duration
	// LeaderID is the current leader ID at the time of error.
	LeaderID uint64
	// Cause is a machine-readable error classification.
	Cause string
	// RetryAfter suggests how long the client should wait before retrying.
	RetryAfter time.Duration
}

// Error cause classifications
const (
	CauseTimeout         = "timeout"
	CauseNoLeader        = "no_leader"
	CauseNotLeader       = "not_leader"
	CauseQuotaExceeded   = "quota_exceeded"
	CauseCorruption      = "corruption"
	CauseConnectionLost  = "connection_lost"
	CauseLeaderChanged   = "leader_changed"
	CauseCompacted       = "compacted"
	CauseAuthFailed      = "auth_failed"
	CausePermissionDenied = "permission_denied"
	CauseInvalidRequest  = "invalid_request"
	CauseStopped         = "server_stopped"
	CauseUnknown         = "unknown"
)

// GenerateRequestID creates a unique 16-character hex request ID.
func GenerateRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if random fails
		return hex.EncodeToString([]byte(time.Now().String())[:8])
	}
	return hex.EncodeToString(b)
}

// TruncateKey truncates a key to maxLen characters for security.
// Keys in auth-related errors are completely omitted.
func TruncateKey(key string, maxLen int, isAuthError bool) string {
	if isAuthError {
		return ""
	}
	if len(key) <= maxLen {
		return key
	}
	return key[:maxLen] + "..."
}

// ClassifyError returns a machine-readable error classification.
func ClassifyError(err error) string {
	if err == nil {
		return ""
	}

	// Check for known gRPC errors
	switch err {
	case ErrGRPCTimeout, ErrGRPCTimeoutDueToLeaderFail, ErrGRPCTimeoutDueToConnectionLost, ErrGRPCTimeoutWaitAppliedIndex:
		return CauseTimeout
	case ErrGRPCNoLeader:
		return CauseNoLeader
	case ErrGRPCNotLeader:
		return CauseNotLeader
	case ErrGRPCNoSpace:
		return CauseQuotaExceeded
	case ErrGRPCCorrupt:
		return CauseCorruption
	case ErrGRPCLeaderChanged:
		return CauseLeaderChanged
	case ErrGRPCCompacted:
		return CauseCompacted
	case ErrGRPCStopped:
		return CauseStopped
	case ErrGRPCAuthFailed, ErrGRPCInvalidAuthToken, ErrGRPCAuthNotEnabled:
		return CauseAuthFailed
	case ErrGRPCPermissionDenied:
		return CausePermissionDenied
	case ErrGRPCEmptyKey, ErrGRPCKeyNotFound, ErrGRPCValueProvided, ErrGRPCLeaseProvided,
		ErrGRPCTooManyOps, ErrGRPCDuplicateKey, ErrGRPCInvalidSortOption:
		return CauseInvalidRequest
	}

	// Check error message for connection issues
	errStr := err.Error()
	if strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "transport") ||
		strings.Contains(errStr, "unavailable") {
		return CauseConnectionLost
	}

	return CauseUnknown
}

// IsAuthError checks if an error is related to authentication.
func IsAuthError(err error) bool {
	switch err {
	case ErrGRPCAuthFailed, ErrGRPCInvalidAuthToken, ErrGRPCAuthNotEnabled,
		ErrGRPCPermissionDenied, ErrGRPCRootUserNotExist, ErrGRPCRootRoleNotExist,
		ErrGRPCUserAlreadyExist, ErrGRPCUserEmpty, ErrGRPCUserNotFound,
		ErrGRPCRoleAlreadyExist, ErrGRPCRoleNotFound, ErrGRPCRoleEmpty,
		ErrGRPCRoleNotGranted, ErrGRPCPermissionNotGranted:
		return true
	}
	return false
}

// GetRetryAfter returns a suggested retry duration based on the error.
func GetRetryAfter(err error) time.Duration {
	switch err {
	case ErrGRPCNoSpace:
		// Quota exceeded - suggest waiting longer
		return 30 * time.Second
	case ErrGRPCTooManyRequests:
		// Rate limited - suggest short wait
		return 1 * time.Second
	case ErrGRPCNoLeader, ErrGRPCLeaderChanged:
		// Leader issues - wait for election
		return 2 * time.Second
	case ErrGRPCTimeout, ErrGRPCTimeoutDueToLeaderFail:
		// Timeout - backoff
		return 5 * time.Second
	}
	return 0
}

// errorToCode converts an error to a gRPC code.
func errorToCode(err error) codes.Code {
	if st, ok := status.FromError(err); ok {
		return st.Code()
	}
	return codes.Unknown
}

// ErrorWithContext wraps an error with structured context as gRPC status details.
func ErrorWithContext(err error, ctx *ErrorContext) error {
	if err == nil || ctx == nil {
		return err
	}

	// Get the original status
	st := status.Convert(err)

	// Create ErrorDetail proto
	detail := &pb.ErrorDetail{
		RequestId:    ctx.RequestID,
		MemberId:     ctx.MemberID,
		Operation:    ctx.Operation,
		Key:          ctx.Key,
		DurationMs:   int64(ctx.Duration / time.Millisecond),
		LeaderId:     ctx.LeaderID,
		Cause:        ctx.Cause,
		RetryAfterMs: int64(ctx.RetryAfter / time.Millisecond),
	}

	// Attach detail to status
	newSt, attachErr := st.WithDetails(detail)
	if attachErr != nil {
		// If we can't attach details, return original error
		return err
	}

	return newSt.Err()
}

// ExtractErrorContext extracts ErrorContext from a gRPC error.
// Returns nil if no context is found.
func ExtractErrorContext(err error) *ErrorContext {
	st := status.Convert(err)
	for _, detail := range st.Details() {
		if errDetail, ok := detail.(*pb.ErrorDetail); ok {
			return &ErrorContext{
				RequestID:  errDetail.GetRequestId(),
				MemberID:   errDetail.GetMemberId(),
				Operation:  errDetail.GetOperation(),
				Key:        errDetail.GetKey(),
				Duration:   time.Duration(errDetail.GetDurationMs()) * time.Millisecond,
				LeaderID:   errDetail.GetLeaderId(),
				Cause:      errDetail.GetCause(),
				RetryAfter: time.Duration(errDetail.GetRetryAfterMs()) * time.Millisecond,
			}
		}
	}
	return nil
}

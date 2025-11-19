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
	"time"

	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
)

// ErrorContext holds structured error context for debugging.
// This is an alias of rpctypes.ErrorContext for client convenience.
type ErrorContext = rpctypes.ErrorContext

// ExtractErrorContext extracts structured error context from a gRPC error.
// Returns nil if no context is found in the error.
//
// Example usage:
//
//	_, err := cli.Put(ctx, "key", "value")
//	if err != nil {
//	    if ectx := clientv3.ExtractErrorContext(err); ectx != nil {
//	        log.Printf("Request %s to member %d failed after %v: %s",
//	            ectx.RequestID, ectx.MemberID, ectx.Duration, ectx.Cause)
//	    }
//	}
func ExtractErrorContext(err error) *ErrorContext {
	return rpctypes.ExtractErrorContext(err)
}

// HasErrorContext checks if an error contains structured context.
func HasErrorContext(err error) bool {
	return rpctypes.ExtractErrorContext(err) != nil
}

// ErrorContextFromError extracts error context and returns it along with a boolean
// indicating whether context was found. This is useful for conditional handling.
//
// Example usage:
//
//	if ectx, ok := clientv3.ErrorContextFromError(err); ok {
//	    // Use context
//	} else {
//	    // Fall back to basic error handling
//	}
func ErrorContextFromError(err error) (*ErrorContext, bool) {
	ctx := rpctypes.ExtractErrorContext(err)
	return ctx, ctx != nil
}

// ShouldRetry returns true if the error suggests the client should retry,
// and provides the suggested wait duration.
func ShouldRetry(err error) (bool, time.Duration) {
	ctx := rpctypes.ExtractErrorContext(err)
	if ctx == nil {
		return false, 0
	}

	// Check if retry is suggested
	if ctx.RetryAfter > 0 {
		return true, ctx.RetryAfter
	}

	// Certain error causes are generally retryable
	switch ctx.Cause {
	case rpctypes.CauseTimeout, rpctypes.CauseNoLeader, rpctypes.CauseLeaderChanged, rpctypes.CauseConnectionLost:
		return true, 0
	}

	return false, 0
}

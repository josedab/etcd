// Copyright 2025 The etcd Authors
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

package v3rpc

import (
	"sync"
	"time"

	pb "go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/api/v3/mvccpb"
)

// WatchBatchConfig holds configuration for watch event batching.
type WatchBatchConfig struct {
	// BatchWaitTime is the maximum time to wait for batch accumulation.
	// Default: 10ms
	BatchWaitTime time.Duration

	// BatchSizeThreshold triggers immediate send when reached.
	// Default: 100 events
	BatchSizeThreshold int

	// AdaptiveEnabled enables dynamic batch sizing.
	// Default: true
	AdaptiveEnabled bool
}

// DefaultWatchBatchConfig returns the default configuration for watch batching.
func DefaultWatchBatchConfig() WatchBatchConfig {
	return WatchBatchConfig{
		BatchWaitTime:      10 * time.Millisecond,
		BatchSizeThreshold: 100,
		AdaptiveEnabled:    true,
	}
}

// watchBatcher accumulates watch events and flushes them in batches.
type watchBatcher struct {
	events    []*pb.WatchResponse
	timer     *time.Timer
	threshold int
	waitTime  time.Duration
	timerC    <-chan time.Time

	mu sync.Mutex
}

// newWatchBatcher creates a new watchBatcher with the given configuration.
func newWatchBatcher(cfg WatchBatchConfig) *watchBatcher {
	timer := time.NewTimer(cfg.BatchWaitTime)
	timer.Stop() // Stop initially, will be reset on first event

	return &watchBatcher{
		events:    make([]*pb.WatchResponse, 0, cfg.BatchSizeThreshold),
		timer:     timer,
		threshold: cfg.BatchSizeThreshold,
		waitTime:  cfg.BatchWaitTime,
		timerC:    timer.C,
	}
}

// add adds a watch response to the batch. Returns true if the batch should be sent
// (threshold reached).
func (b *watchBatcher) add(wr *pb.WatchResponse) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.events = append(b.events, wr)

	// Immediate send if threshold reached
	if b.countEvents() >= b.threshold {
		return true
	}

	// Start timer on first event
	if len(b.events) == 1 {
		b.timer.Reset(b.waitTime)
	}

	return false
}

// countEvents returns the total number of events in the batch.
func (b *watchBatcher) countEvents() int {
	total := 0
	for _, wr := range b.events {
		total += len(wr.Events)
	}
	return total
}

// flush returns all accumulated responses and resets the batcher.
func (b *watchBatcher) flush() []*pb.WatchResponse {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.events) == 0 {
		return nil
	}

	events := b.events
	b.events = make([]*pb.WatchResponse, 0, b.threshold)
	b.timer.Stop()

	return events
}

// timerChan returns the timer channel for select operations.
func (b *watchBatcher) timerChan() <-chan time.Time {
	return b.timerC
}

// stop stops the batcher's timer.
func (b *watchBatcher) stop() {
	b.timer.Stop()
}

// adaptiveBatcher extends watchBatcher with dynamic parameter adjustment.
type adaptiveBatcher struct {
	*watchBatcher

	// Metrics for adaptation
	eventCount   int64
	lastAdjust   time.Time
	adjustPeriod time.Duration

	// Event tracking for rate calculation
	eventTimes []time.Time
	maxSamples int

	mu sync.Mutex
}

// newAdaptiveBatcher creates a new adaptive batcher with the given configuration.
func newAdaptiveBatcher(cfg WatchBatchConfig) *adaptiveBatcher {
	return &adaptiveBatcher{
		watchBatcher: newWatchBatcher(cfg),
		lastAdjust:   time.Now(),
		adjustPeriod: 1 * time.Second,
		eventTimes:   make([]time.Time, 0, 1000),
		maxSamples:   1000,
	}
}

// add adds a watch response and adapts batching parameters if needed.
func (a *adaptiveBatcher) add(wr *pb.WatchResponse) bool {
	a.mu.Lock()

	// Record event time for rate calculation
	now := time.Now()
	a.eventTimes = append(a.eventTimes, now)

	// Keep only recent samples
	if len(a.eventTimes) > a.maxSamples {
		a.eventTimes = a.eventTimes[len(a.eventTimes)-a.maxSamples:]
	}

	a.eventCount += int64(len(wr.Events))

	// Adapt parameters periodically
	if now.Sub(a.lastAdjust) >= a.adjustPeriod {
		a.adapt()
		a.lastAdjust = now
	}

	a.mu.Unlock()

	return a.watchBatcher.add(wr)
}

// adapt adjusts batching parameters based on current event rate.
func (a *adaptiveBatcher) adapt() {
	rate := a.calculateRate()

	// Record the current batch parameters for metrics
	var newWaitTime time.Duration
	var newThreshold int

	switch {
	case rate > 10000:
		// High load: longer wait, larger batches
		newWaitTime = 50 * time.Millisecond
		newThreshold = 500
	case rate > 1000:
		// Medium load
		newWaitTime = 20 * time.Millisecond
		newThreshold = 200
	default:
		// Low load: minimal batching
		newWaitTime = 5 * time.Millisecond
		newThreshold = 50
	}

	// Update batcher parameters
	a.watchBatcher.mu.Lock()
	a.watchBatcher.waitTime = newWaitTime
	a.watchBatcher.threshold = newThreshold
	a.watchBatcher.mu.Unlock()
}

// calculateRate calculates the current event rate per second.
func (a *adaptiveBatcher) calculateRate() float64 {
	if len(a.eventTimes) < 2 {
		return 0
	}

	// Calculate rate from recent events
	duration := a.eventTimes[len(a.eventTimes)-1].Sub(a.eventTimes[0])
	if duration <= 0 {
		return 0
	}

	return float64(len(a.eventTimes)) / duration.Seconds()
}

// mergeWatchResponses combines multiple watch responses for the same watch ID
// into optimally batched responses. Returns responses that can be sent individually.
func mergeWatchResponses(responses []*pb.WatchResponse) []*pb.WatchResponse {
	if len(responses) == 0 {
		return nil
	}

	if len(responses) == 1 {
		return responses
	}

	// Group responses by watch ID
	byWatchID := make(map[int64][]*pb.WatchResponse)
	var order []int64

	for _, wr := range responses {
		if _, exists := byWatchID[wr.WatchId]; !exists {
			order = append(order, wr.WatchId)
		}
		byWatchID[wr.WatchId] = append(byWatchID[wr.WatchId], wr)
	}

	// Merge events for each watch ID
	result := make([]*pb.WatchResponse, 0, len(order))
	for _, watchID := range order {
		wrs := byWatchID[watchID]
		if len(wrs) == 1 {
			result = append(result, wrs[0])
			continue
		}

		// Merge multiple responses for same watch ID
		merged := &pb.WatchResponse{
			Header:  wrs[len(wrs)-1].Header, // Use latest header
			WatchId: watchID,
			Events:  make([]*mvccpb.Event, 0),
		}

		for _, wr := range wrs {
			merged.Events = append(merged.Events, wr.Events...)
			// Preserve cancellation/compaction info
			if wr.Canceled {
				merged.Canceled = true
				merged.CancelReason = wr.CancelReason
			}
			if wr.CompactRevision != 0 {
				merged.CompactRevision = wr.CompactRevision
			}
		}

		result = append(result, merged)
	}

	return result
}

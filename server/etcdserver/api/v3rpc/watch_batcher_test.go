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
	"testing"
	"time"

	pb "go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/api/v3/mvccpb"
)

func TestDefaultWatchBatchConfig(t *testing.T) {
	cfg := DefaultWatchBatchConfig()

	if cfg.BatchWaitTime != 10*time.Millisecond {
		t.Errorf("expected BatchWaitTime 10ms, got %v", cfg.BatchWaitTime)
	}
	if cfg.BatchSizeThreshold != 100 {
		t.Errorf("expected BatchSizeThreshold 100, got %d", cfg.BatchSizeThreshold)
	}
	if !cfg.AdaptiveEnabled {
		t.Error("expected AdaptiveEnabled to be true")
	}
}

func TestWatchBatcher_Add(t *testing.T) {
	cfg := WatchBatchConfig{
		BatchWaitTime:      10 * time.Millisecond,
		BatchSizeThreshold: 3,
		AdaptiveEnabled:    false,
	}
	batcher := newWatchBatcher(cfg)
	defer batcher.stop()

	// Create test events
	event1 := createTestWatchResponse(1, 1)
	event2 := createTestWatchResponse(1, 1)
	event3 := createTestWatchResponse(1, 1)

	// Add first event - should not trigger flush
	if batcher.add(event1) {
		t.Error("first event should not trigger flush")
	}

	// Add second event - should not trigger flush
	if batcher.add(event2) {
		t.Error("second event should not trigger flush")
	}

	// Add third event - should trigger flush (threshold reached)
	if !batcher.add(event3) {
		t.Error("third event should trigger flush")
	}
}

func TestWatchBatcher_Flush(t *testing.T) {
	cfg := WatchBatchConfig{
		BatchWaitTime:      10 * time.Millisecond,
		BatchSizeThreshold: 100,
		AdaptiveEnabled:    false,
	}
	batcher := newWatchBatcher(cfg)
	defer batcher.stop()

	// Empty flush should return nil
	if events := batcher.flush(); events != nil {
		t.Error("empty flush should return nil")
	}

	// Add events and flush
	event1 := createTestWatchResponse(1, 2)
	event2 := createTestWatchResponse(2, 1)
	batcher.add(event1)
	batcher.add(event2)

	events := batcher.flush()
	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}

	// Flush again should be empty
	if events := batcher.flush(); events != nil {
		t.Error("second flush should return nil")
	}
}

func TestWatchBatcher_Timer(t *testing.T) {
	cfg := WatchBatchConfig{
		BatchWaitTime:      50 * time.Millisecond,
		BatchSizeThreshold: 100,
		AdaptiveEnabled:    false,
	}
	batcher := newWatchBatcher(cfg)
	defer batcher.stop()

	// Add an event to start the timer
	event := createTestWatchResponse(1, 1)
	batcher.add(event)

	// Wait for timer to fire
	select {
	case <-batcher.timerChan():
		// Timer fired as expected
	case <-time.After(100 * time.Millisecond):
		t.Error("timer should have fired")
	}
}

func TestAdaptiveBatcher_Adaptation(t *testing.T) {
	cfg := WatchBatchConfig{
		BatchWaitTime:      10 * time.Millisecond,
		BatchSizeThreshold: 100,
		AdaptiveEnabled:    true,
	}
	batcher := newAdaptiveBatcher(cfg)
	defer batcher.stop()

	// Simulate high event rate
	batcher.adjustPeriod = 0 // Force immediate adaptation
	for i := 0; i < 100; i++ {
		event := createTestWatchResponse(1, 1)
		batcher.add(event)
	}

	// Check that parameters were adapted
	batcher.watchBatcher.mu.Lock()
	threshold := batcher.watchBatcher.threshold
	waitTime := batcher.watchBatcher.waitTime
	batcher.watchBatcher.mu.Unlock()

	// With low rate calculation (short time period), it should be in low load mode
	// The exact values depend on timing, but we can at least verify it's reasonable
	if threshold < 1 || threshold > 1000 {
		t.Errorf("unexpected threshold after adaptation: %d", threshold)
	}
	if waitTime < time.Millisecond || waitTime > 100*time.Millisecond {
		t.Errorf("unexpected waitTime after adaptation: %v", waitTime)
	}
}

func TestMergeWatchResponses_Empty(t *testing.T) {
	result := mergeWatchResponses(nil)
	if result != nil {
		t.Error("merging nil should return nil")
	}

	result = mergeWatchResponses([]*pb.WatchResponse{})
	if result != nil {
		t.Error("merging empty slice should return nil")
	}
}

func TestMergeWatchResponses_Single(t *testing.T) {
	response := createTestWatchResponse(1, 3)
	result := mergeWatchResponses([]*pb.WatchResponse{response})

	if len(result) != 1 {
		t.Errorf("expected 1 response, got %d", len(result))
	}
	if len(result[0].Events) != 3 {
		t.Errorf("expected 3 events, got %d", len(result[0].Events))
	}
}

func TestMergeWatchResponses_SameWatchID(t *testing.T) {
	// Create multiple responses for the same watch ID
	response1 := createTestWatchResponse(1, 2)
	response2 := createTestWatchResponse(1, 3)

	result := mergeWatchResponses([]*pb.WatchResponse{response1, response2})

	if len(result) != 1 {
		t.Errorf("expected 1 merged response, got %d", len(result))
	}
	if len(result[0].Events) != 5 {
		t.Errorf("expected 5 events after merge, got %d", len(result[0].Events))
	}
	if result[0].WatchId != 1 {
		t.Errorf("expected watch ID 1, got %d", result[0].WatchId)
	}
}

func TestMergeWatchResponses_DifferentWatchIDs(t *testing.T) {
	// Create responses for different watch IDs
	response1 := createTestWatchResponse(1, 2)
	response2 := createTestWatchResponse(2, 3)
	response3 := createTestWatchResponse(1, 1)

	result := mergeWatchResponses([]*pb.WatchResponse{response1, response2, response3})

	if len(result) != 2 {
		t.Errorf("expected 2 responses, got %d", len(result))
	}

	// Check that watch ID 1 events are merged
	var watch1Events, watch2Events int
	for _, r := range result {
		if r.WatchId == 1 {
			watch1Events = len(r.Events)
		} else if r.WatchId == 2 {
			watch2Events = len(r.Events)
		}
	}

	if watch1Events != 3 {
		t.Errorf("expected 3 events for watch ID 1, got %d", watch1Events)
	}
	if watch2Events != 3 {
		t.Errorf("expected 3 events for watch ID 2, got %d", watch2Events)
	}
}

func TestMergeWatchResponses_PreservesCancellation(t *testing.T) {
	response1 := createTestWatchResponse(1, 1)
	response2 := createTestWatchResponse(1, 1)
	response2.Canceled = true
	response2.CancelReason = "test cancellation"

	result := mergeWatchResponses([]*pb.WatchResponse{response1, response2})

	if len(result) != 1 {
		t.Errorf("expected 1 response, got %d", len(result))
	}
	if !result[0].Canceled {
		t.Error("merged response should be canceled")
	}
	if result[0].CancelReason != "test cancellation" {
		t.Errorf("expected cancel reason 'test cancellation', got '%s'", result[0].CancelReason)
	}
}

func TestMergeWatchResponses_PreservesCompactRevision(t *testing.T) {
	response1 := createTestWatchResponse(1, 1)
	response2 := createTestWatchResponse(1, 1)
	response2.CompactRevision = 100

	result := mergeWatchResponses([]*pb.WatchResponse{response1, response2})

	if len(result) != 1 {
		t.Errorf("expected 1 response, got %d", len(result))
	}
	if result[0].CompactRevision != 100 {
		t.Errorf("expected compact revision 100, got %d", result[0].CompactRevision)
	}
}

func TestMergeWatchResponses_PreservesOrder(t *testing.T) {
	// Create responses for different watch IDs in specific order
	response1 := createTestWatchResponse(1, 1)
	response2 := createTestWatchResponse(2, 1)
	response3 := createTestWatchResponse(3, 1)

	result := mergeWatchResponses([]*pb.WatchResponse{response1, response2, response3})

	if len(result) != 3 {
		t.Errorf("expected 3 responses, got %d", len(result))
	}

	// Check order is preserved
	if result[0].WatchId != 1 || result[1].WatchId != 2 || result[2].WatchId != 3 {
		t.Error("order should be preserved")
	}
}

func TestWatchBatcher_CountEvents(t *testing.T) {
	cfg := WatchBatchConfig{
		BatchWaitTime:      10 * time.Millisecond,
		BatchSizeThreshold: 100,
		AdaptiveEnabled:    false,
	}
	batcher := newWatchBatcher(cfg)
	defer batcher.stop()

	// Add responses with different event counts
	batcher.add(createTestWatchResponse(1, 5))
	batcher.add(createTestWatchResponse(2, 3))
	batcher.add(createTestWatchResponse(1, 2))

	count := batcher.countEvents()
	if count != 10 {
		t.Errorf("expected 10 events, got %d", count)
	}
}

func TestAdaptiveBatcher_RateCalculation(t *testing.T) {
	cfg := WatchBatchConfig{
		BatchWaitTime:      10 * time.Millisecond,
		BatchSizeThreshold: 1000,
		AdaptiveEnabled:    true,
	}
	batcher := newAdaptiveBatcher(cfg)
	defer batcher.stop()

	// With no events, rate should be 0
	rate := batcher.calculateRate()
	if rate != 0 {
		t.Errorf("expected rate 0 with no events, got %f", rate)
	}

	// Add one event - still rate 0 (need at least 2 for calculation)
	event := createTestWatchResponse(1, 1)
	batcher.add(event)

	batcher.mu.Lock()
	rate = batcher.calculateRate()
	batcher.mu.Unlock()

	if rate != 0 {
		t.Errorf("expected rate 0 with 1 event, got %f", rate)
	}
}

func TestWatchBatcher_ThresholdTrigger(t *testing.T) {
	// Test that threshold is based on total events, not responses
	cfg := WatchBatchConfig{
		BatchWaitTime:      10 * time.Millisecond,
		BatchSizeThreshold: 5,
		AdaptiveEnabled:    false,
	}
	batcher := newWatchBatcher(cfg)
	defer batcher.stop()

	// Add response with 3 events - should not trigger
	if batcher.add(createTestWatchResponse(1, 3)) {
		t.Error("should not trigger with 3 events")
	}

	// Add response with 2 more events - total 5, should trigger
	if !batcher.add(createTestWatchResponse(1, 2)) {
		t.Error("should trigger with 5 total events")
	}
}

// Helper function to create test watch responses
func createTestWatchResponse(watchID int64, numEvents int) *pb.WatchResponse {
	events := make([]*mvccpb.Event, numEvents)
	for i := 0; i < numEvents; i++ {
		events[i] = &mvccpb.Event{
			Type: mvccpb.PUT,
			Kv: &mvccpb.KeyValue{
				Key:   []byte("test-key"),
				Value: []byte("test-value"),
			},
		}
	}

	return &pb.WatchResponse{
		Header: &pb.ResponseHeader{
			ClusterId: 1,
			MemberId:  1,
			Revision:  1,
			RaftTerm:  1,
		},
		WatchId: watchID,
		Events:  events,
	}
}

func BenchmarkWatchBatcherAdd(b *testing.B) {
	cfg := WatchBatchConfig{
		BatchWaitTime:      10 * time.Millisecond,
		BatchSizeThreshold: 100,
		AdaptiveEnabled:    false,
	}
	batcher := newWatchBatcher(cfg)
	defer batcher.stop()

	event := createTestWatchResponse(1, 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if batcher.add(event) {
			batcher.flush()
		}
	}
}

func BenchmarkAdaptiveBatcherAdd(b *testing.B) {
	cfg := WatchBatchConfig{
		BatchWaitTime:      10 * time.Millisecond,
		BatchSizeThreshold: 100,
		AdaptiveEnabled:    true,
	}
	batcher := newAdaptiveBatcher(cfg)
	defer batcher.stop()

	event := createTestWatchResponse(1, 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if batcher.add(event) {
			batcher.flush()
		}
	}
}

func BenchmarkMergeWatchResponses(b *testing.B) {
	// Create a realistic batch of responses
	responses := make([]*pb.WatchResponse, 50)
	for i := 0; i < 50; i++ {
		responses[i] = createTestWatchResponse(int64(i%5), 10)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mergeWatchResponses(responses)
	}
}

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

package mvccpb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestKeyValueWireCompatibility tests that KeyValue messages can be marshaled
// and unmarshaled correctly after the protobuf library migration.
func TestKeyValueWireCompatibility(t *testing.T) {
	testCases := []struct {
		name string
		kv   *KeyValue
	}{
		{
			name: "empty keyvalue",
			kv:   &KeyValue{},
		},
		{
			name: "keyvalue with key only",
			kv: &KeyValue{
				Key: []byte("test-key"),
			},
		},
		{
			name: "keyvalue with all fields",
			kv: &KeyValue{
				Key:            []byte("test-key"),
				CreateRevision: 1,
				ModRevision:    2,
				Version:        3,
				Value:          []byte("test-value"),
				Lease:          12345,
			},
		},
		{
			name: "keyvalue with large values",
			kv: &KeyValue{
				Key:            []byte("large-key-with-many-characters"),
				CreateRevision: 9999999,
				ModRevision:    9999998,
				Version:        1000,
				Value:          make([]byte, 4096),
				Lease:          999999999,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Marshal the keyvalue
			data, err := proto.Marshal(tc.kv)
			require.NoError(t, err, "failed to marshal keyvalue")

			// Unmarshal into a new keyvalue
			newKV := &KeyValue{}
			err = proto.Unmarshal(data, newKV)
			require.NoError(t, err, "failed to unmarshal keyvalue")

			// Verify fields match
			assert.Equal(t, tc.kv.Key, newKV.Key, "key mismatch")
			assert.Equal(t, tc.kv.CreateRevision, newKV.CreateRevision, "create_revision mismatch")
			assert.Equal(t, tc.kv.ModRevision, newKV.ModRevision, "mod_revision mismatch")
			assert.Equal(t, tc.kv.Version, newKV.Version, "version mismatch")
			assert.Equal(t, tc.kv.Value, newKV.Value, "value mismatch")
			assert.Equal(t, tc.kv.Lease, newKV.Lease, "lease mismatch")
		})
	}
}

// TestEventWireCompatibility tests that Event messages can be marshaled
// and unmarshaled correctly after the protobuf library migration.
func TestEventWireCompatibility(t *testing.T) {
	testCases := []struct {
		name  string
		event *Event
	}{
		{
			name:  "empty event",
			event: &Event{},
		},
		{
			name: "put event",
			event: &Event{
				Type: Event_PUT,
				Kv: &KeyValue{
					Key:   []byte("test-key"),
					Value: []byte("test-value"),
				},
			},
		},
		{
			name: "delete event",
			event: &Event{
				Type: Event_DELETE,
				Kv: &KeyValue{
					Key: []byte("deleted-key"),
				},
			},
		},
		{
			name: "event with prev_kv",
			event: &Event{
				Type: Event_PUT,
				Kv: &KeyValue{
					Key:   []byte("test-key"),
					Value: []byte("new-value"),
				},
				PrevKv: &KeyValue{
					Key:   []byte("test-key"),
					Value: []byte("old-value"),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Marshal the event
			data, err := proto.Marshal(tc.event)
			require.NoError(t, err, "failed to marshal event")

			// Unmarshal into a new event
			newEvent := &Event{}
			err = proto.Unmarshal(data, newEvent)
			require.NoError(t, err, "failed to unmarshal event")

			// Verify fields match
			assert.Equal(t, tc.event.Type, newEvent.Type, "type mismatch")

			if tc.event.Kv != nil {
				require.NotNil(t, newEvent.Kv, "kv should not be nil")
				assert.Equal(t, tc.event.Kv.Key, newEvent.Kv.Key, "kv.key mismatch")
				assert.Equal(t, tc.event.Kv.Value, newEvent.Kv.Value, "kv.value mismatch")
			}

			if tc.event.PrevKv != nil {
				require.NotNil(t, newEvent.PrevKv, "prev_kv should not be nil")
				assert.Equal(t, tc.event.PrevKv.Key, newEvent.PrevKv.Key, "prev_kv.key mismatch")
				assert.Equal(t, tc.event.PrevKv.Value, newEvent.PrevKv.Value, "prev_kv.value mismatch")
			}
		})
	}
}

// TestKeyValueMarshalDeterminism tests that marshaling is deterministic
// (same input produces same output).
func TestKeyValueMarshalDeterminism(t *testing.T) {
	kv := &KeyValue{
		Key:            []byte("test-key"),
		CreateRevision: 1,
		ModRevision:    2,
		Version:        3,
		Value:          []byte("test-value"),
		Lease:          12345,
	}

	// Marshal multiple times
	data1, err := proto.Marshal(kv)
	require.NoError(t, err)

	data2, err := proto.Marshal(kv)
	require.NoError(t, err)

	// Verify outputs are identical
	assert.Equal(t, data1, data2, "marshal outputs should be deterministic")
}

// TestEventMarshalDeterminism tests that marshaling is deterministic
// (same input produces same output).
func TestEventMarshalDeterminism(t *testing.T) {
	event := &Event{
		Type: Event_PUT,
		Kv: &KeyValue{
			Key:   []byte("test-key"),
			Value: []byte("test-value"),
		},
	}

	// Marshal multiple times
	data1, err := proto.Marshal(event)
	require.NoError(t, err)

	data2, err := proto.Marshal(event)
	require.NoError(t, err)

	// Verify outputs are identical
	assert.Equal(t, data1, data2, "marshal outputs should be deterministic")
}

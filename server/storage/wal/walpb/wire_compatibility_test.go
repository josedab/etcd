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

package walpb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestRecordWireCompatibility tests that Record messages can be marshaled
// and unmarshaled correctly after the protobuf library migration.
func TestRecordWireCompatibility(t *testing.T) {
	testCases := []struct {
		name   string
		record *Record
	}{
		{
			name: "empty record",
			record: &Record{},
		},
		{
			name: "record with type",
			record: &Record{
				Type: proto.Int64(1),
			},
		},
		{
			name: "record with all fields",
			record: &Record{
				Type: proto.Int64(2),
				Crc:  proto.Uint32(12345),
				Data: []byte("test data"),
			},
		},
		{
			name: "record with large data",
			record: &Record{
				Type: proto.Int64(3),
				Crc:  proto.Uint32(67890),
				Data: make([]byte, 1024),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Marshal the record
			data, err := proto.Marshal(tc.record)
			require.NoError(t, err, "failed to marshal record")

			// Unmarshal into a new record
			newRecord := &Record{}
			err = proto.Unmarshal(data, newRecord)
			require.NoError(t, err, "failed to unmarshal record")

			// Verify fields match
			if tc.record.Type != nil {
				assert.Equal(t, tc.record.GetType(), newRecord.GetType(), "type mismatch")
			}
			if tc.record.Crc != nil {
				assert.Equal(t, tc.record.GetCrc(), newRecord.GetCrc(), "crc mismatch")
			}
			assert.Equal(t, tc.record.GetData(), newRecord.GetData(), "data mismatch")
		})
	}
}

// TestSnapshotWireCompatibility tests that Snapshot messages can be marshaled
// and unmarshaled correctly after the protobuf library migration.
func TestSnapshotWireCompatibility(t *testing.T) {
	testCases := []struct {
		name     string
		snapshot *Snapshot
	}{
		{
			name:     "empty snapshot",
			snapshot: &Snapshot{},
		},
		{
			name: "snapshot with index and term",
			snapshot: &Snapshot{
				Index: proto.Uint64(100),
				Term:  proto.Uint64(5),
			},
		},
		{
			name: "snapshot with large values",
			snapshot: &Snapshot{
				Index: proto.Uint64(1000000),
				Term:  proto.Uint64(999),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Marshal the snapshot
			data, err := proto.Marshal(tc.snapshot)
			require.NoError(t, err, "failed to marshal snapshot")

			// Unmarshal into a new snapshot
			newSnapshot := &Snapshot{}
			err = proto.Unmarshal(data, newSnapshot)
			require.NoError(t, err, "failed to unmarshal snapshot")

			// Verify fields match
			if tc.snapshot.Index != nil {
				assert.Equal(t, tc.snapshot.GetIndex(), newSnapshot.GetIndex(), "index mismatch")
			}
			if tc.snapshot.Term != nil {
				assert.Equal(t, tc.snapshot.GetTerm(), newSnapshot.GetTerm(), "term mismatch")
			}
		})
	}
}

// TestRecordMarshalDeterminism tests that marshaling is deterministic
// (same input produces same output).
func TestRecordMarshalDeterminism(t *testing.T) {
	record := &Record{
		Type: proto.Int64(1),
		Crc:  proto.Uint32(12345),
		Data: []byte("test data"),
	}

	// Marshal multiple times
	data1, err := proto.Marshal(record)
	require.NoError(t, err)

	data2, err := proto.Marshal(record)
	require.NoError(t, err)

	// Verify outputs are identical
	assert.Equal(t, data1, data2, "marshal outputs should be deterministic")
}

// TestSnapshotMarshalDeterminism tests that marshaling is deterministic
// (same input produces same output).
func TestSnapshotMarshalDeterminism(t *testing.T) {
	snapshot := &Snapshot{
		Index: proto.Uint64(100),
		Term:  proto.Uint64(5),
	}

	// Marshal multiple times
	data1, err := proto.Marshal(snapshot)
	require.NoError(t, err)

	data2, err := proto.Marshal(snapshot)
	require.NoError(t, err)

	// Verify outputs are identical
	assert.Equal(t, data1, data2, "marshal outputs should be deterministic")
}

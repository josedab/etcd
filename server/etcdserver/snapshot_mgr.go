// Copyright 2015 The etcd Authors
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

package etcdserver

import (
	errorspkg "errors"
	"time"

	humanize "github.com/dustin/go-humanize"
	"go.uber.org/zap"

	"go.etcd.io/etcd/client/pkg/v3/types"
	"go.etcd.io/etcd/client/pkg/v3/verify"
	"go.etcd.io/etcd/pkg/v3/traceutil"
	"go.etcd.io/etcd/server/v3/etcdserver/api"
	"go.etcd.io/etcd/server/v3/etcdserver/api/snap"
	"go.etcd.io/etcd/server/v3/lease"
	serverstorage "go.etcd.io/etcd/server/v3/storage"
	"go.etcd.io/etcd/server/v3/storage/schema"
	"go.etcd.io/raft/v3"
	"go.etcd.io/raft/v3/raftpb"
)

// ForceSnapshot forces a snapshot to be triggered on the next apply.
func (s *EtcdServer) ForceSnapshot() {
	s.forceDiskSnapshot = true
}

// snapshotIfNeededAndCompactRaftLog checks if a snapshot should be taken
// and compacts the Raft log if necessary.
func (s *EtcdServer) snapshotIfNeededAndCompactRaftLog(ep *etcdProgress) {
	// TODO: Remove disk snapshot in v3.7
	shouldSnapshotToDisk := s.shouldSnapshotToDisk(ep)
	shouldSnapshotToMemory := s.shouldSnapshotToMemory(ep)
	if !shouldSnapshotToDisk && !shouldSnapshotToMemory {
		return
	}
	s.snapshot(ep, shouldSnapshotToDisk)
	s.compactRaftLog(ep.appliedi)
}

// shouldSnapshotToDisk returns true if a snapshot should be written to disk.
func (s *EtcdServer) shouldSnapshotToDisk(ep *etcdProgress) bool {
	return (s.forceDiskSnapshot && ep.appliedi != ep.diskSnapshotIndex) || (ep.appliedi-ep.diskSnapshotIndex > s.Cfg.SnapshotCount)
}

// shouldSnapshotToMemory returns true if a memory snapshot should be taken.
func (s *EtcdServer) shouldSnapshotToMemory(ep *etcdProgress) bool {
	return ep.appliedi > ep.memorySnapshotIndex+memorySnapshotCount
}

// snapshot creates a snapshot of the current state.
// TODO: non-blocking snapshot
func (s *EtcdServer) snapshot(ep *etcdProgress, toDisk bool) {
	lg := s.Logger()
	d := GetMembershipInfoInV2Format(lg, s.cluster)
	if toDisk {
		s.Logger().Info(
			"triggering snapshot",
			zap.String("local-member-id", s.MemberID().String()),
			zap.Uint64("local-member-applied-index", ep.appliedi),
			zap.Uint64("local-member-snapshot-index", ep.diskSnapshotIndex),
			zap.Uint64("local-member-snapshot-count", s.Cfg.SnapshotCount),
			zap.Bool("snapshot-forced", s.forceDiskSnapshot),
		)
		s.forceDiskSnapshot = false
		// commit kv to write metadata (for example: consistent index) to disk.
		//
		// This guarantees that Backend's consistent_index is >= index of last snapshot.
		//
		// KV().commit() updates the consistent index in backend.
		// All operations that update consistent index must be called sequentially
		// from applyAll function.
		// So KV().Commit() cannot run in parallel with toApply. It has to be called outside
		// the go routine created below.
		s.KV().Commit()
	}

	// For backward compatibility, generate v2 snapshot from v3 state.
	snap, err := s.r.raftStorage.CreateSnapshot(ep.appliedi, &ep.confState, d)
	if err != nil {
		// the snapshot was done asynchronously with the progress of raft.
		// raft might have already got a newer snapshot.
		if errorspkg.Is(err, raft.ErrSnapOutOfDate) {
			return
		}
		lg.Panic("failed to create snapshot", zap.Error(err))
	}
	ep.memorySnapshotIndex = ep.appliedi

	verifyConsistentIndexIsLatest(snap, s.consistIndex.ConsistentIndex())

	if toDisk {
		// SaveSnap saves the snapshot to file and appends the corresponding WAL entry.
		if err = s.r.storage.SaveSnap(snap); err != nil {
			lg.Panic("failed to save snapshot", zap.Error(err))
		}
		ep.diskSnapshotIndex = ep.appliedi
		if err = s.r.storage.Release(snap); err != nil {
			lg.Panic("failed to release wal", zap.Error(err))
		}

		lg.Info(
			"saved snapshot to disk",
			zap.Uint64("snapshot-index", snap.Metadata.Index),
		)
	}
}

// compactRaftLog compacts the Raft log up to the given snapshot index.
func (s *EtcdServer) compactRaftLog(snapi uint64) {
	lg := s.Logger()

	// When sending a snapshot, etcd will pause compaction.
	// After receives a snapshot, the slow follower needs to get all the entries right after
	// the snapshot sent to catch up. If we do not pause compaction, the log entries right after
	// the snapshot sent might already be compacted. It happens when the snapshot takes long time
	// to send and save. Pausing compaction avoids triggering a snapshot sending cycle.
	if s.inflightSnapshots.Load() != 0 {
		lg.Info("skip compaction since there is an inflight snapshot")
		return
	}

	// keep some in memory log entries for slow followers.
	compacti := uint64(1)
	if snapi > s.Cfg.SnapshotCatchUpEntries {
		compacti = snapi - s.Cfg.SnapshotCatchUpEntries
	}
	err := s.r.raftStorage.Compact(compacti)
	if err != nil {
		// the compaction was done asynchronously with the progress of raft.
		// raft log might already been compact.
		if errorspkg.Is(err, raft.ErrCompacted) {
			return
		}
		lg.Panic("failed to compact", zap.Error(err))
	}
	lg.Debug(
		"compacted Raft logs",
		zap.Uint64("compact-index", compacti),
	)
}

// sendMergedSnap sends a merged snapshot message to a peer.
func (s *EtcdServer) sendMergedSnap(merged snap.Message) {
	s.inflightSnapshots.Add(1)

	lg := s.Logger()
	fields := []zap.Field{
		zap.String("from", s.MemberID().String()),
		zap.String("to", types.ID(merged.To).String()),
		zap.Int64("bytes", merged.TotalSize),
		zap.String("size", humanize.Bytes(uint64(merged.TotalSize))),
	}

	now := time.Now()
	s.r.transport.SendSnapshot(merged)
	lg.Info("sending merged snapshot", fields...)

	s.GoAttach(func() {
		select {
		case ok := <-merged.CloseNotify():
			// delay releasing inflight snapshot for another 30 seconds to
			// block log compaction.
			// If the follower still fails to catch up, it is probably just too slow
			// to catch up. We cannot avoid the snapshot cycle anyway.
			if ok {
				select {
				case <-time.After(releaseDelayAfterSnapshot):
				case <-s.stopping:
				}
			}

			s.inflightSnapshots.Add(-1)

			lg.Info("sent merged snapshot", append(fields, zap.Duration("took", time.Since(now)))...)

		case <-s.stopping:
			lg.Warn("canceled sending merged snapshot; server stopping", fields...)
			return
		}
	})
}

// applySnapshot applies a snapshot received from a leader.
func (s *EtcdServer) applySnapshot(ep *etcdProgress, toApply *toApply) {
	if raft.IsEmptySnap(toApply.snapshot) {
		return
	}
	applySnapshotInProgress.Inc()

	lg := s.Logger()
	lg.Info(
		"applying snapshot",
		zap.Uint64("current-snapshot-index", ep.diskSnapshotIndex),
		zap.Uint64("current-applied-index", ep.appliedi),
		zap.Uint64("incoming-leader-snapshot-index", toApply.snapshot.Metadata.Index),
		zap.Uint64("incoming-leader-snapshot-term", toApply.snapshot.Metadata.Term),
	)
	defer func() {
		lg.Info(
			"applied snapshot",
			zap.Uint64("current-snapshot-index", ep.diskSnapshotIndex),
			zap.Uint64("current-applied-index", ep.appliedi),
			zap.Uint64("incoming-leader-snapshot-index", toApply.snapshot.Metadata.Index),
			zap.Uint64("incoming-leader-snapshot-term", toApply.snapshot.Metadata.Term),
		)
		applySnapshotInProgress.Dec()
	}()

	if toApply.snapshot.Metadata.Index <= ep.appliedi {
		lg.Panic(
			"unexpected leader snapshot from outdated index",
			zap.Uint64("current-snapshot-index", ep.diskSnapshotIndex),
			zap.Uint64("current-applied-index", ep.appliedi),
			zap.Uint64("incoming-leader-snapshot-index", toApply.snapshot.Metadata.Index),
			zap.Uint64("incoming-leader-snapshot-term", toApply.snapshot.Metadata.Term),
		)
	}

	// wait for raftNode to persist snapshot onto the disk
	<-toApply.notifyc

	bemuUnlocked := false
	s.bemu.Lock()
	defer func() {
		if !bemuUnlocked {
			s.bemu.Unlock()
		}
	}()

	// gofail: var applyBeforeOpenSnapshot struct{}
	newbe, err := serverstorage.OpenSnapshotBackend(s.Cfg, s.snapshotter, toApply.snapshot, s.beHooks)
	if err != nil {
		lg.Panic("failed to open snapshot backend", zap.Error(err))
	}
	lg.Info("applySnapshot: opened snapshot backend")
	// gofail: var applyAfterOpenSnapshot struct{}

	// We need to set the backend to consistIndex before recovering the lessor,
	// because lessor.Recover will commit the boltDB transaction, accordingly it
	// will get the old consistent_index persisted into the db in OnPreCommitUnsafe.
	// Eventually the new consistent_index value coming from snapshot is overwritten
	// by the old value.
	s.consistIndex.SetBackend(newbe)
	verifySnapshotIndex(toApply.snapshot, s.consistIndex.ConsistentIndex())

	// always recover lessor before kv. When we recover the mvcc.KV it will reattach keys to its leases.
	// If we recover mvcc.KV first, it will attach the keys to the wrong lessor before it recovers.
	if s.lessor != nil {
		lg.Info("restoring lease store")

		s.lessor.Recover(newbe, func() lease.TxnDelete { return s.kv.Write(traceutil.TODO()) })

		lg.Info("restored lease store")
	}

	lg.Info("restoring mvcc store")

	if err := s.kv.Restore(newbe); err != nil {
		lg.Panic("failed to restore mvcc store", zap.Error(err))
	}

	newbe.SetTxPostLockInsideApplyHook(s.getTxPostLockInsideApplyHook())

	lg.Info("restored mvcc store", zap.Uint64("consistent-index", s.consistIndex.ConsistentIndex()))

	oldbe := s.be
	s.be = newbe
	s.bemu.Unlock()
	bemuUnlocked = true

	// Closing old backend might block until all the txns
	// on the backend are finished.
	// We do not want to wait on closing the old backend.
	go func() {
		lg.Info("closing old backend file")
		defer func() {
			lg.Info("closed old backend file")
		}()
		if err := oldbe.Close(); err != nil {
			lg.Panic("failed to close old backend", zap.Error(err))
		}
	}()

	lg.Info("restoring alarm store")

	if err := s.restoreAlarms(); err != nil {
		lg.Panic("failed to restore alarm store", zap.Error(err))
	}

	lg.Info("restored alarm store")

	if s.authStore != nil {
		lg.Info("restoring auth store")

		s.authStore.Recover(schema.NewAuthBackend(lg, newbe))

		lg.Info("restored auth store")
	}

	lg.Info("restoring v2 store")
	if err := s.v2store.Recovery(toApply.snapshot.Data); err != nil {
		lg.Panic("failed to restore v2 store", zap.Error(err))
	}

	if err := serverstorage.AssertNoV2StoreContent(lg, s.v2store, s.Cfg.V2Deprecation); err != nil {
		lg.Panic("illegal v2store content", zap.Error(err))
	}

	lg.Info("restored v2 store")

	s.cluster.SetBackend(schema.NewMembershipBackend(lg, newbe))

	lg.Info("restoring cluster configuration")

	s.cluster.Recover(api.UpdateCapability)

	lg.Info("restored cluster configuration")
	lg.Info("removing old peers from network")

	// recover raft transport
	s.r.transport.RemoveAllPeers()

	lg.Info("removed old peers from network")
	lg.Info("adding peers from new cluster configuration")

	for _, m := range s.cluster.Members() {
		if m.ID == s.MemberID() {
			continue
		}
		s.r.transport.AddPeer(m.ID, m.PeerURLs)
	}

	lg.Info("added peers from new cluster configuration")

	ep.appliedt = toApply.snapshot.Metadata.Term
	ep.appliedi = toApply.snapshot.Metadata.Index
	ep.diskSnapshotIndex = ep.appliedi
	ep.memorySnapshotIndex = ep.appliedi
	ep.confState = toApply.snapshot.Metadata.ConfState

	// As backends and implementations like alarmsStore changed, we need
	// to re-bootstrap Appliers.
	s.uberApply = s.NewUberApplier()
}

// verifySnapshotIndex verifies that the consistent index matches the snapshot index.
func verifySnapshotIndex(snapshot raftpb.Snapshot, cindex uint64) {
	verify.Verify("consistent_index isn't equal to snapshot index", func() (bool, map[string]any) {
		return cindex == snapshot.Metadata.Index,
			map[string]any{
				"consistent_index": cindex,
				"snapshot_index":   snapshot.Metadata.Index,
			}
	})
}

// verifyConsistentIndexIsLatest verifies that the consistent index is at least as recent as the snapshot.
func verifyConsistentIndexIsLatest(snapshot raftpb.Snapshot, cindex uint64) {
	verify.Verify("consistent_index is older than snapshot_index", func() (bool, map[string]any) {
		return cindex >= snapshot.Metadata.Index,
			map[string]any{
				"consistent_index": cindex,
				"snapshot_index":   snapshot.Metadata.Index,
			}
	})
}

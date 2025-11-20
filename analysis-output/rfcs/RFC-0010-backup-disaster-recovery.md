# RFC-0010: Automated Backup and Disaster Recovery Framework

**Status:** Draft
**Author:** Claude Code Analysis
**Created:** November 19, 2025
**Category:** Long-term (P0 - Critical)

---

## Summary

Implement a comprehensive backup and disaster recovery framework with automated scheduling, incremental backups, point-in-time recovery, and pluggable storage backends to eliminate the need for external backup solutions.

---

## Motivation

### Problem

etcd currently provides only manual snapshot capability:

```go
// From client/v3/maintenance.go:76-80
// Snapshot provides a reader for a point-in-time snapshot of etcd.
Snapshot(ctx context.Context) (io.ReadCloser, error)
```

**Limitations:**
- Manual operation only (no automation)
- Full backups only (no incrementals)
- No built-in scheduling
- No point-in-time recovery
- No backup verification
- No retention management

### Real-World Requirements

**Production SLAs:**
- **RPO (Recovery Point Objective):** < 5 minutes
- **RTO (Recovery Time Objective):** < 5 minutes
- **Data Retention:** 30 days minimum
- **Compliance:** Backup verification required

**Current State:**
- RPO = backup interval (often hours or daily)
- RTO = full restore time (10+ minutes)
- Operators build custom solutions (cron + scripts)
- No consistency guarantees across backups

### Pain Points

1. **Operational Burden:**
   - Every operator writes custom backup scripts
   - Cron jobs, monitoring, alerting all custom
   - S3/GCS integration done manually

2. **Data Loss Risk:**
   - Missed backup windows
   - Failed backups go unnoticed
   - No validation until restore needed

3. **Recovery Complexity:**
   - Manual restore process
   - No PITR (point-in-time recovery)
   - Coordination needed for cluster recovery

4. **Storage Waste:**
   - Full backups only (no deduplication)
   - No compression
   - Retention policy manual

### Expected Benefits

- **90% reduction in backup size** (incremental)
- **80% faster recovery** (PITR)
- **Automated operations** (zero-touch backups)
- **Compliance ready** (verification, retention)
- **Better RPO/RTO** (5-minute objectives achievable)

---

## Detailed Design

### 1. Architecture Overview

```
┌────────────────────────────────────────────────┐
│              etcd Server                       │
│                                                │
│  ┌──────────────────────────────────────┐     │
│  │   Backup Manager                     │     │
│  │   - Scheduler                        │     │
│  │   - Incremental tracker              │     │
│  │   - Verification engine              │     │
│  └──────────┬───────────────────────────┘     │
│             │                                  │
│  ┌──────────┴───────────────────────────┐     │
│  │   Backup Backends (Pluggable)        │     │
│  │   - Local filesystem                 │     │
│  │   - S3                               │     │
│  │   - GCS                              │     │
│  │   - Azure Blob                       │     │
│  │   - Custom (interface)               │     │
│  └──────────────────────────────────────┘     │
└────────────────────────────────────────────────┘
```

### 2. Backup Types

#### Full Backup

```go
type FullBackup struct {
    Timestamp  time.Time
    Revision   int64
    Size       int64
    Hash       string
    Compressed bool
}

// Snapshot entire database
func (bm *BackupManager) CreateFullBackup(ctx context.Context) (*FullBackup, error) {
    // 1. Create consistent snapshot
    snapshot := bm.server.KV().Snapshot()
    defer snapshot.Close()

    // 2. Compress (zstd)
    compressed := bm.compress(snapshot)

    // 3. Upload to backend
    location, err := bm.backend.Upload(ctx, compressed, BackupTypeFull)

    // 4. Record metadata
    backup := &FullBackup{
        Timestamp:  time.Now(),
        Revision:   bm.server.KV().Rev(),
        Size:       compressed.Size(),
        Hash:       bm.hash(compressed),
        Compressed: true,
    }

    bm.recordBackup(backup)
    return backup, nil
}
```

#### Incremental Backup

```go
type IncrementalBackup struct {
    Timestamp    time.Time
    BaseRevision int64
    EndRevision  int64
    Size         int64
    Hash         string
    FullBackupID string  // Parent full backup
}

// Backup changes since last backup
func (bm *BackupManager) CreateIncrementalBackup(ctx context.Context) (*IncrementalBackup, error) {
    lastBackup := bm.getLastBackup()

    // 1. Stream WAL entries since last backup
    entries, err := bm.server.Storage().ReadWAL(lastBackup.Revision, 0)
    if err != nil {
        return nil, err
    }

    // 2. Compress and upload
    compressed := bm.compressEntries(entries)
    location, err := bm.backend.Upload(ctx, compressed, BackupTypeIncremental)

    // 3. Record metadata
    backup := &IncrementalBackup{
        Timestamp:    time.Now(),
        BaseRevision: lastBackup.Revision,
        EndRevision:  bm.server.KV().Rev(),
        Size:         compressed.Size(),
        Hash:         bm.hash(compressed),
        FullBackupID: lastBackup.ID,
    }

    bm.recordBackup(backup)
    return backup, nil
}
```

### 3. Backup Scheduling

```go
type BackupSchedule struct {
    // Full backup schedule (cron format)
    FullBackupCron string  // "0 2 * * *" = 2am daily

    // Incremental backup interval
    IncrementalInterval time.Duration  // 5 minutes

    // Retention policy
    Retention RetentionPolicy

    // Enabled
    Enabled bool
}

type RetentionPolicy struct {
    // Keep full backups for
    FullBackupDays int  // 30 days

    // Keep incrementals for
    IncrementalDays int  // 7 days

    // Minimum backups to keep
    MinBackups int  // 3

    // Archive old backups instead of delete
    Archive bool
}

// Scheduler runs in background
func (bm *BackupManager) runScheduler() {
    fullTicker := cron.New()
    fullTicker.AddFunc(bm.schedule.FullBackupCron, func() {
        bm.CreateFullBackup(context.Background())
    })

    incrementalTicker := time.NewTicker(bm.schedule.IncrementalInterval)
    defer incrementalTicker.Stop()

    for {
        select {
        case <-incrementalTicker.C:
            if bm.shouldBackup() {
                bm.CreateIncrementalBackup(context.Background())
            }

        case <-bm.stop:
            return
        }
    }
}
```

### 4. Point-in-Time Recovery (PITR)

```go
// Restore to specific revision
func (bm *BackupManager) RestoreToRevision(ctx context.Context, targetRev int64) error {
    // 1. Find appropriate backups
    fullBackup := bm.findBaseBackup(targetRev)
    incrementals := bm.findIncrementals(fullBackup.Revision, targetRev)

    // 2. Restore full backup
    if err := bm.restoreFullBackup(ctx, fullBackup); err != nil {
        return err
    }

    // 3. Apply incremental backups in order
    for _, inc := range incrementals {
        if err := bm.applyIncremental(ctx, inc, targetRev); err != nil {
            return err
        }
    }

    // 4. Verify consistency
    if err := bm.verifyRestore(targetRev); err != nil {
        return err
    }

    return nil
}

// Restore to specific timestamp
func (bm *BackupManager) RestoreToTimestamp(ctx context.Context, timestamp time.Time) error {
    // Find revision at timestamp
    revision, err := bm.findRevisionAtTime(timestamp)
    if err != nil {
        return err
    }

    return bm.RestoreToRevision(ctx, revision)
}
```

### 5. Backup Verification

```go
type VerificationReport struct {
    BackupID    string
    Status      string  // "valid", "corrupted", "incomplete"
    TestedAt    time.Time
    RestoreTime time.Duration
    Errors      []string
}

// Verify backup can be restored
func (bm *BackupManager) VerifyBackup(ctx context.Context, backupID string) (*VerificationReport, error) {
    report := &VerificationReport{
        BackupID: backupID,
        TestedAt: time.Now(),
    }

    // 1. Download backup
    start := time.Now()
    backup, err := bm.backend.Download(ctx, backupID)
    if err != nil {
        report.Status = "corrupted"
        report.Errors = append(report.Errors, err.Error())
        return report, err
    }

    // 2. Verify hash
    if !bm.verifyHash(backup) {
        report.Status = "corrupted"
        report.Errors = append(report.Errors, "hash mismatch")
        return report, errors.New("hash mismatch")
    }

    // 3. Test restore to temp directory
    tempDir := bm.createTempDir()
    defer os.RemoveAll(tempDir)

    if err := bm.testRestore(ctx, backup, tempDir); err != nil {
        report.Status = "incomplete"
        report.Errors = append(report.Errors, err.Error())
        return report, err
    }

    report.Status = "valid"
    report.RestoreTime = time.Since(start)
    return report, nil
}

// Automated verification schedule
func (bm *BackupManager) runVerification() {
    ticker := time.NewTicker(24 * time.Hour)  // Daily
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            // Verify latest backup
            latest := bm.getLatestBackup()
            report, _ := bm.VerifyBackup(context.Background(), latest.ID)
            bm.recordVerification(report)

            // Alert if invalid
            if report.Status != "valid" {
                bm.alertBackupFailure(report)
            }

        case <-bm.stop:
            return
        }
    }
}
```

### 6. Pluggable Storage Backends

```go
// Backend interface
type BackupBackend interface {
    // Upload backup
    Upload(ctx context.Context, data io.Reader, metadata BackupMetadata) (string, error)

    // Download backup
    Download(ctx context.Context, backupID string) (io.ReadCloser, error)

    // List backups
    List(ctx context.Context, filter BackupFilter) ([]BackupMetadata, error)

    // Delete backup
    Delete(ctx context.Context, backupID string) error

    // Get metadata
    Metadata(ctx context.Context, backupID string) (BackupMetadata, error)
}

// S3 backend implementation
type S3Backend struct {
    client *s3.Client
    bucket string
    prefix string
}

func (s *S3Backend) Upload(ctx context.Context, data io.Reader, metadata BackupMetadata) (string, error) {
    key := fmt.Sprintf("%s/%s/%s.backup", s.prefix, metadata.Timestamp.Format("2006/01/02"), metadata.ID)

    _, err := s.client.PutObject(ctx, &s3.PutObjectInput{
        Bucket: aws.String(s.bucket),
        Key:    aws.String(key),
        Body:   data,
        Metadata: map[string]string{
            "revision":  fmt.Sprint(metadata.Revision),
            "type":      metadata.Type,
            "timestamp": metadata.Timestamp.Format(time.RFC3339),
        },
    })

    return key, err
}

// GCS backend implementation
type GCSBackend struct {
    client *storage.Client
    bucket string
    prefix string
}

// Local filesystem backend
type FilesystemBackend struct {
    basePath string
}
```

### 7. Configuration

```go
// Server configuration
type BackupConfig struct {
    // Enable backup system
    Enabled bool

    // Schedule
    Schedule BackupSchedule

    // Backend configuration
    Backend BackendConfig

    // Retention policy
    Retention RetentionPolicy

    // Verification
    VerificationEnabled   bool
    VerificationSchedule  string  // Cron format

    // Compression
    CompressionEnabled bool
    CompressionLevel   int

    // Encryption (optional)
    EncryptionEnabled bool
    EncryptionKey     string
}

type BackendConfig struct {
    Type string  // "s3", "gcs", "filesystem", "azure"

    // S3 configuration
    S3Bucket    string
    S3Region    string
    S3Endpoint  string
    S3AccessKey string
    S3SecretKey string

    // GCS configuration
    GCSBucket      string
    GCSCredentials string

    // Filesystem configuration
    FSBasePath string
}
```

### 8. Command-Line Interface

```bash
# Configure backup
etcdctl backup config \
    --schedule-full="0 2 * * *" \
    --schedule-incremental=5m \
    --backend=s3 \
    --s3-bucket=my-etcd-backups \
    --retention-days=30

# Trigger manual backup
etcdctl backup create --type=full
etcdctl backup create --type=incremental

# List backups
etcdctl backup list

# Restore from backup
etcdctl backup restore --backup-id=abc123
etcdctl backup restore --timestamp="2025-11-19T10:30:00Z"
etcdctl backup restore --revision=12345

# Verify backup
etcdctl backup verify --backup-id=abc123
etcdctl backup verify --latest

# Retention management
etcdctl backup prune --older-than=30d
etcdctl backup prune --keep-minimum=3
```

---

## Implementation Plan

### Phase 1: Core Framework (Weeks 1-3)
- [ ] Define interfaces and types
- [ ] Implement backup manager
- [ ] Add scheduling logic
- [ ] Unit tests

### Phase 2: Storage Backends (Weeks 4-6)
- [ ] Filesystem backend
- [ ] S3 backend
- [ ] GCS backend
- [ ] Backend tests

### Phase 3: Incremental Backups (Weeks 7-9)
- [ ] WAL-based incremental
- [ ] Incremental restore
- [ ] Merge logic
- [ ] Integration tests

### Phase 4: PITR (Weeks 10-12)
- [ ] Revision tracking
- [ ] Timestamp mapping
- [ ] Restore logic
- [ ] E2E tests

### Phase 5: Verification (Weeks 13-14)
- [ ] Test restore framework
- [ ] Automated verification
- [ ] Reporting
- [ ] Alerting integration

### Phase 6: Polish (Weeks 15-16)
- [ ] Compression optimization
- [ ] Encryption support
- [ ] CLI tools
- [ ] Documentation

---

## Backwards Compatibility

**Fully backward compatible:**
- Disabled by default (opt-in)
- Existing `etcdctl snapshot` still works
- No changes to core etcd functionality
- Can coexist with external backup solutions

**Migration:**
- Operators can migrate from cron scripts gradually
- Existing backups remain accessible
- No data format changes

---

## Alternatives Considered

### Alternative 1: External Tool Only

**Approach:** Keep etcd minimal, backup via external tools.

**Pros:**
- Separation of concerns
- No additional etcd complexity

**Cons:**
- Every operator reinvents the wheel
- No integration with etcd internals
- Missed optimization opportunities

**Decision:** Built-in is critical feature for production.

### Alternative 2: etcd-backup-operator

**Approach:** Kubernetes operator for backups.

**Pros:**
- Kubernetes-native
- Declarative configuration

**Cons:**
- Kubernetes-only
- External dependency
- More moving parts

**Decision:** Useful complement, not replacement.

### Alternative 3: Snapshot Only

**Approach:** Improve snapshot but no scheduling/automation.

**Pros:**
- Minimal scope

**Cons:**
- Doesn't solve operational pain
- Still requires external orchestration

**Decision:** Insufficient for production needs.

---

## Open Questions

1. **Encryption key management:** Where to store encryption keys?
   - Proposed: Support KMS (AWS KMS, GCP KMS, Vault)
   - Also support local key file for simplicity

2. **Backup during compaction:** Should we pause backups?
   - Proposed: No, backups are read-only snapshots
   - Document that backup size may vary

3. **Multi-cluster backups:** Backup across regions?
   - Proposed: Phase 2 feature
   - Cross-region replication of backups

---

## Success Criteria

- [ ] RPO < 5 minutes (with 5-minute incremental schedule)
- [ ] RTO < 5 minutes (PITR restore)
- [ ] 90% backup size reduction (incremental vs full)
- [ ] Automated verification reports 100% success rate
- [ ] Zero manual intervention for backup/restore
- [ ] Production adoption by 3+ major users

---

## Effort Estimation

**Total: 14-16 weeks**

| Phase | Effort |
|-------|--------|
| Core framework | 3 weeks |
| Storage backends | 3 weeks |
| Incremental backups | 3 weeks |
| PITR | 3 weeks |
| Verification | 2 weeks |
| Polish and docs | 2 weeks |

**Team:** 2-3 engineers

---

## Stakeholder Approvals

- [ ] etcd maintainers
- [ ] Operations SIG
- [ ] Cloud provider users (for backend validation)
- [ ] Large deployment operators
- [ ] Community RFC review

---

## Rollback Strategy

1. **Feature flag:** `--enable-backup-system`
2. **Gradual rollout:**
   - Alpha: opt-in, experimental
   - Beta: default disabled, stable API
   - GA: production ready
3. **Rollback:**
   - Disable flag
   - Existing backups remain
   - Fallback to manual snapshots

---

## Metrics and Monitoring

```go
// Prometheus metrics
var (
    backupDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "etcd_backup_duration_seconds",
            Help: "Backup duration in seconds",
        },
        []string{"type"},  // full, incremental
    )

    backupSize = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "etcd_backup_size_bytes",
            Help: "Backup size in bytes",
        },
        []string{"type"},
    )

    backupStatus = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "etcd_backup_status_total",
            Help: "Backup status count",
        },
        []string{"status"},  // success, failure
    )

    verificationStatus = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "etcd_backup_verification_status",
            Help: "Last backup verification status (1=valid, 0=invalid)",
        },
        []string{"backup_id"},
    )
)
```

---

## References

- [PostgreSQL WAL Archiving](https://www.postgresql.org/docs/current/continuous-archiving.html)
- [MySQL Point-in-Time Recovery](https://dev.mysql.com/doc/refman/8.0/en/point-in-time-recovery.html)
- [MongoDB Backup Methods](https://www.mongodb.com/docs/manual/core/backups/)
- [etcd-backup-operator](https://github.com/coreos/etcd-operator)
- Current snapshot: [`client/v3/maintenance.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/client/v3/maintenance.go)

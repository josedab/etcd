# etcd Dependency Graph

**Commit:** `d6bc3229d81384aed0fc03a3ddd3dccfef092048`

---

## Module Dependency Structure

```
┌─────────────────────────────────────────────────────────────────┐
│                        APPLICATIONS                              │
├─────────────────────────────────────────────────────────────────┤
│  etcdctl/v3     etcdutl/v3     tools/*                          │
│      │              │                                            │
│      └──────┬───────┘                                           │
│             │                                                    │
│             ▼                                                    │
├─────────────────────────────────────────────────────────────────┤
│                        CLIENT LIBRARY                            │
├─────────────────────────────────────────────────────────────────┤
│                    client/v3                                     │
│                        │                                         │
│                        ▼                                         │
├─────────────────────────────────────────────────────────────────┤
│                         SERVER                                   │
├─────────────────────────────────────────────────────────────────┤
│                    server/v3                                     │
│           ┌──────────┼──────────┐                               │
│           ▼          ▼          ▼                               │
│        api/v3    pkg/v3    cache/v3                             │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## Internal Module Dependencies

### Dependency Matrix

| Module | Depends On |
|--------|------------|
| server/v3 | api/v3, pkg/v3, client/v3, go.etcd.io/raft/v3 |
| client/v3 | api/v3, client/pkg/v3 |
| etcdctl/v3 | client/v3, pkg/v3 |
| etcdutl/v3 | server/v3, api/v3 |
| cache/v3 | api/v3 |
| tests/v3 | all modules |

---

## External Dependencies by Category

### Core Infrastructure

```
┌─────────────────────────────────────────┐
│           CONSENSUS LAYER               │
├─────────────────────────────────────────┤
│  go.etcd.io/raft/v3  v3.6.0             │
│  └─ Raft consensus algorithm            │
└─────────────────────────────────────────┘

┌─────────────────────────────────────────┐
│           STORAGE LAYER                 │
├─────────────────────────────────────────┤
│  go.etcd.io/bbolt  v1.4.3               │
│  └─ Key-value storage (BoltDB fork)     │
└─────────────────────────────────────────┘
```

### RPC & Serialization

```
┌─────────────────────────────────────────┐
│           gRPC STACK                    │
├─────────────────────────────────────────┤
│  google.golang.org/grpc  v1.76.0        │
│  ├─ google.golang.org/genproto          │
│  └─ google.golang.org/protobuf  v1.36   │
│                                         │
│  grpc-gateway/v2  v2.29.0               │
│  └─ REST/HTTP to gRPC translation       │
│                                         │
│  grpc-ecosystem/grpc-middleware         │
│  └─ Middleware (logging, recovery)      │
└─────────────────────────────────────────┘

┌─────────────────────────────────────────┐
│       PROTOCOL BUFFERS                  │
├─────────────────────────────────────────┤
│  gogo/protobuf  v1.3.2  ⚠️ DEPRECATED   │
│  └─ Legacy protobuf (migration needed)  │
│                                         │
│  google.golang.org/protobuf  v1.36.10   │
│  └─ Official protobuf                   │
└─────────────────────────────────────────┘
```

### Observability

```
┌─────────────────────────────────────────┐
│           LOGGING                       │
├─────────────────────────────────────────┤
│  go.uber.org/zap  v1.27.0               │
│  └─ Structured logging                  │
│                                         │
│  natefinch/lumberjack/v2  v2.2.1        │
│  └─ Log rotation                        │
└─────────────────────────────────────────┘

┌─────────────────────────────────────────┐
│           METRICS                       │
├─────────────────────────────────────────┤
│  prometheus/client_golang               │
│  ├─ prometheus/client_model             │
│  ├─ prometheus/common                   │
│  └─ prometheus/procfs                   │
└─────────────────────────────────────────┘

┌─────────────────────────────────────────┐
│           TRACING                       │
├─────────────────────────────────────────┤
│  go.opentelemetry.io/otel               │
│  ├─ otel/exporters/otlp/otlptrace       │
│  ├─ otel/sdk/trace                      │
│  └─ otel/contrib/instrumentation/grpc   │
└─────────────────────────────────────────┘
```

### CLI & Configuration

```
┌─────────────────────────────────────────┐
│           CLI FRAMEWORK                 │
├─────────────────────────────────────────┤
│  spf13/cobra  v1.9.1                    │
│  └─ Command-line interface              │
│                                         │
│  spf13/pflag  v1.0.6                    │
│  └─ POSIX/GNU-style flags               │
└─────────────────────────────────────────┘

┌─────────────────────────────────────────┐
│           CONFIGURATION                 │
├─────────────────────────────────────────┤
│  json-iterator/go  v1.1.12              │
│  └─ Fast JSON parsing                   │
│                                         │
│  soheilhy/cmux  v0.1.5                  │
│  └─ Connection multiplexing             │
└─────────────────────────────────────────┘
```

### Security

```
┌─────────────────────────────────────────┐
│           CRYPTOGRAPHY                  │
├─────────────────────────────────────────┤
│  golang.org/x/crypto  v0.43.0           │
│  └─ Supplementary crypto packages       │
│                                         │
│  jonboulle/clockwork  v0.5.0            │
│  └─ Time abstraction (for JWT)          │
└─────────────────────────────────────────┘
```

### Testing

```
┌─────────────────────────────────────────┐
│           TESTING                       │
├─────────────────────────────────────────┤
│  stretchr/testify  v1.10.0              │
│  └─ Assertions and mocking              │
│                                         │
│  go.etcd.io/gofail  v0.2.0              │
│  └─ Failpoint injection                 │
└─────────────────────────────────────────┘
```

---

## Dependency Versions Summary

### Critical Dependencies

| Package | Version | Last Update | Status |
|---------|---------|-------------|--------|
| google.golang.org/grpc | v1.76.0 | Current | ✅ Active |
| go.etcd.io/bbolt | v1.4.3 | Current | ✅ Stable |
| go.etcd.io/raft/v3 | v3.6.0 | Current | ✅ Active |
| google.golang.org/protobuf | v1.36.10 | Current | ✅ Active |
| go.uber.org/zap | v1.27.0 | Current | ✅ Active |
| gogo/protobuf | v1.3.2 | Stale | ⚠️ Deprecated |

---

## Dependency Health Assessment

### Risk Analysis

| Risk Level | Count | Examples |
|------------|-------|----------|
| 🟢 Low | 16 | grpc, zap, prometheus |
| 🟡 Medium | 3 | grpc-websocket-proxy, xiang90/probing |
| 🔴 High | 1 | gogo/protobuf |

### Recommendations

1. **Immediate**: Plan gogo/protobuf migration
2. **Short-term**: Monitor pseudo-version dependencies
3. **Long-term**: Evaluate making OpenTelemetry optional

---

## Server Package Dependencies

```
server/v3
├── Core
│   ├── go.etcd.io/raft/v3
│   ├── go.etcd.io/bbolt
│   └── go.etcd.io/etcd/api/v3
│
├── Networking
│   ├── google.golang.org/grpc
│   ├── grpc-gateway/v2
│   └── soheilhy/cmux
│
├── Observability
│   ├── go.uber.org/zap
│   ├── prometheus/client_golang
│   └── go.opentelemetry.io/otel
│
├── Security
│   ├── golang.org/x/crypto
│   └── jonboulle/clockwork
│
└── Utilities
    ├── spf13/cobra
    ├── spf13/pflag
    └── json-iterator/go
```

---

## Client Package Dependencies

```
client/v3
├── Core
│   └── go.etcd.io/etcd/api/v3
│
├── Networking
│   └── google.golang.org/grpc
│
├── Observability
│   └── go.uber.org/zap
│
└── Utilities
    └── client/pkg/v3
```

**Note:** Client has minimal dependencies by design for easy integration.

---

## Dependency Update Strategy

### Current Approach
- Dependabot enabled for security updates
- Manual updates for major versions
- Go workspace for coordinated updates

### Recommended Improvements
1. Enable `govulncheck` in CI
2. Document dependency rationale
3. Create formal update policy
4. Track dependency freshness metrics

---

## Visual Dependency Flow

```
                    ┌─────────┐
                    │  User   │
                    └────┬────┘
                         │
              ┌──────────┼──────────┐
              ▼          ▼          ▼
        ┌─────────┐ ┌─────────┐ ┌─────────┐
        │ etcdctl │ │  App    │ │   K8s   │
        └────┬────┘ └────┬────┘ └────┬────┘
             │           │           │
             └─────┬─────┴─────┬─────┘
                   │           │
                   ▼           ▼
            ┌────────────────────────┐
            │     client/v3         │
            │   (grpc, protobuf)    │
            └──────────┬────────────┘
                       │
                       ▼
            ┌────────────────────────┐
            │      server/v3        │
            │  (raft, bbolt, zap)   │
            └──────────┬────────────┘
                       │
         ┌─────────────┼─────────────┐
         ▼             ▼             ▼
    ┌─────────┐  ┌──────────┐  ┌─────────┐
    │  MVCC   │  │   WAL    │  │  Raft   │
    │ (bbolt) │  │ (fsync)  │  │(raft/v3)│
    └─────────┘  └──────────┘  └─────────┘
```

---

## License Compatibility

All dependencies use permissive licenses compatible with Apache 2.0:

| License | Dependencies |
|---------|-------------|
| Apache 2.0 | grpc, prometheus, otel, cobra |
| MIT | zap, testify, pflag, cmux |
| BSD | bbolt, protobuf, golang.org/x/* |

**Assessment:** ✅ No license compatibility issues.

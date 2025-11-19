# RFC-0008: Native Observability Dashboard

**Status:** Draft
**Author:** Claude Code Analysis
**Created:** November 19, 2025
**Category:** Long-term

---

## Summary

Create a built-in web dashboard for etcd cluster monitoring, providing essential operational visibility without requiring external tools like Grafana.

---

## Motivation

### Problem

Currently, monitoring etcd requires:
1. Install Prometheus
2. Configure scraping
3. Install Grafana
4. Import dashboards
5. Configure alerts

This is a high barrier for:
- Development/testing environments
- Small deployments
- Quick troubleshooting

### Current State

etcd exposes `/metrics` but no visualization:

```bash
curl localhost:2379/metrics
# Raw Prometheus metrics - hard to interpret
```

### Expected Benefits

- Zero-dependency monitoring for development
- Faster troubleshooting (no setup required)
- Educational (understand what metrics mean)
- Operational status at a glance

---

## Detailed Design

### Dashboard Architecture

```
┌──────────────────────────────────┐
│     etcd Server                  │
├──────────────────────────────────┤
│  ┌─────────────────────────┐     │
│  │   Dashboard Handler     │     │
│  │   /dashboard/           │     │
│  └──────────┬──────────────┘     │
│             │                    │
│  ┌──────────┴──────────────┐     │
│  │   Static Assets         │     │
│  │   (embedded)            │     │
│  └──────────┬──────────────┘     │
│             │                    │
│  ┌──────────┴──────────────┐     │
│  │   Metrics API           │     │
│  │   /dashboard/api/       │     │
│  └─────────────────────────┘     │
└──────────────────────────────────┘
```

### Enable via Configuration

```bash
etcd --enable-dashboard
# or
etcd --enable-dashboard --dashboard-addr=:8080
```

Default: disabled (no overhead if not used).

### Dashboard Pages

#### 1. Overview

```
╔══════════════════════════════════════════════╗
║  etcd Dashboard - Cluster: my-cluster        ║
╠══════════════════════════════════════════════╣
║                                              ║
║  Cluster Health: ● HEALTHY                   ║
║                                              ║
║  Members:         3/3 healthy                ║
║  Leader:          member-1                   ║
║  Version:         3.7.0                      ║
║                                              ║
║  ┌────────────────────────────────────┐      ║
║  │ Requests/sec:  ████████░░ 850      │      ║
║  │ Latency P99:   ████░░░░░░ 12ms     │      ║
║  │ DB Size:       ████████░░ 1.2GB    │      ║
║  └────────────────────────────────────┘      ║
║                                              ║
╚══════════════════════════════════════════════╝
```

#### 2. Members

```
╔══════════════════════════════════════════════╗
║  Cluster Members                             ║
╠══════════════════════════════════════════════╣
║                                              ║
║  ┌────────┬─────────┬────────┬─────────┐     ║
║  │ Name   │ Status  │ Role   │ Version │     ║
║  ├────────┼─────────┼────────┼─────────┤     ║
║  │member-1│ ● Up    │ Leader │ 3.7.0   │     ║
║  │member-2│ ● Up    │Follower│ 3.7.0   │     ║
║  │member-3│ ○ Down  │Follower│ 3.7.0   │     ║
║  └────────┴─────────┴────────┴─────────┘     ║
║                                              ║
╚══════════════════════════════════════════════╝
```

#### 3. Metrics

```
╔══════════════════════════════════════════════╗
║  Key Metrics                                 ║
╠══════════════════════════════════════════════╣
║                                              ║
║  Server                                      ║
║  ├─ Proposals committed: 1,234,567           ║
║  ├─ Proposals failed: 3                      ║
║  └─ Leader changes: 2                        ║
║                                              ║
║  Storage                                     ║
║  ├─ DB size: 1.2 GB                         ║
║  ├─ DB size in use: 800 MB                  ║
║  └─ Keys: 45,678                            ║
║                                              ║
║  Network                                     ║
║  ├─ Bytes sent: 5.6 GB                      ║
║  └─ Bytes received: 4.2 GB                  ║
║                                              ║
╚══════════════════════════════════════════════╝
```

### Implementation

#### Handler Registration

```go
// server/embed/serve.go
func (e *Etcd) serveDashboard() {
    if !e.cfg.EnableDashboard {
        return
    }

    mux := http.NewServeMux()

    // Serve static assets
    mux.Handle("/dashboard/", http.FileServer(dashboardFS))

    // API endpoints
    mux.HandleFunc("/dashboard/api/status", e.handleStatus)
    mux.HandleFunc("/dashboard/api/members", e.handleMembers)
    mux.HandleFunc("/dashboard/api/metrics", e.handleMetrics)

    go http.ListenAndServe(e.cfg.DashboardAddr, mux)
}
```

#### Embedded Assets

```go
//go:embed dashboard/*
var dashboardFS embed.FS
```

#### API Endpoints

```go
// GET /dashboard/api/status
{
    "cluster_id": "abc123",
    "cluster_health": "healthy",
    "leader_id": 1,
    "members_total": 3,
    "members_healthy": 3,
    "version": "3.7.0",
    "db_size": 1234567890,
    "db_size_in_use": 800000000
}

// GET /dashboard/api/metrics
{
    "proposals_committed": 1234567,
    "proposals_failed": 3,
    "leader_changes": 2,
    "requests_per_second": 850,
    "latency_p99_ms": 12
}
```

### Frontend

Simple HTML/CSS/JavaScript (no frameworks):

```html
<!-- dashboard/index.html -->
<!DOCTYPE html>
<html>
<head>
    <title>etcd Dashboard</title>
    <link rel="stylesheet" href="style.css">
</head>
<body>
    <header>
        <h1>etcd Dashboard</h1>
        <span id="cluster-health"></span>
    </header>

    <main>
        <section id="overview">
            <!-- Overview metrics -->
        </section>
        <section id="members">
            <!-- Member table -->
        </section>
    </main>

    <script src="dashboard.js"></script>
</body>
</html>
```

```javascript
// dashboard/dashboard.js
async function fetchStatus() {
    const resp = await fetch('/dashboard/api/status');
    const data = await resp.json();
    updateUI(data);
}

function updateUI(data) {
    document.getElementById('cluster-health').textContent =
        data.cluster_health === 'healthy' ? '● HEALTHY' : '○ UNHEALTHY';
    // ...
}

// Refresh every 5 seconds
setInterval(fetchStatus, 5000);
fetchStatus();
```

---

## Implementation Plan

### Phase 1: Basic Structure (Week 1-2)
- [ ] Add configuration flags
- [ ] Create handler structure
- [ ] Set up embed for assets

### Phase 2: API Endpoints (Week 2-3)
- [ ] Status endpoint
- [ ] Members endpoint
- [ ] Metrics endpoint

### Phase 3: Frontend (Week 3-4)
- [ ] HTML structure
- [ ] CSS styling
- [ ] JavaScript updates

### Phase 4: Polish (Week 5-6)
- [ ] Error handling
- [ ] Authentication integration
- [ ] Documentation

---

## Backwards Compatibility

**Fully backward compatible:**

- Disabled by default
- No changes to existing functionality
- Optional dependency (embed is standard)

---

## Alternatives Considered

### Alternative 1: Recommend existing tools

**Pros:**
- No development effort
- More features

**Cons:**
- Doesn't solve setup barrier
- Not integrated

**Decision:** Built-in solves specific use case.

### Alternative 2: Full-featured dashboard

**Pros:**
- More capabilities
- Better for production

**Cons:**
- Larger scope
- More maintenance

**Decision:** Start minimal, expand based on feedback.

### Alternative 3: Terminal UI

**Pros:**
- No browser needed
- Simpler

**Cons:**
- Less accessible
- Limited visualization

**Decision:** Web UI is more universal.

---

## Open Questions

1. **Authentication**: Reuse etcd auth or separate?
   - Proposed: Reuse etcd auth if enabled

2. **Scope**: How much functionality?
   - Proposed: Read-only monitoring initially

3. **Updates**: SSE vs polling?
   - Proposed: Polling initially (simpler)

---

## Success Criteria

- [ ] Dashboard accessible at /dashboard/
- [ ] Shows cluster health
- [ ] Lists all members
- [ ] Displays key metrics
- [ ] Updates automatically
- [ ] Works without external dependencies

---

## Effort Estimation

**Total: 4-6 weeks**

| Task | Effort |
|------|--------|
| Backend API | 1 week |
| Frontend | 2 weeks |
| Styling | 1 week |
| Testing | 1 week |
| Documentation | 0.5 weeks |

---

## Stakeholder Approvals

- [ ] etcd maintainers

---

## Rollback Strategy

1. Disable with `--enable-dashboard=false`
2. Remove handler code
3. No user data impact

---

## Future Enhancements

- Alert configuration
- Key browser (read-only)
- Performance graphs
- Log viewer
- Configuration editor

---

## References

- [Prometheus built-in UI](https://prometheus.io/docs/visualization/browser/)
- [Consul UI](https://www.consul.io/docs/commands/ui)
- Current metrics: [`server/etcdserver/metrics.go`](https://github.com/etcd-io/etcd/blob/d6bc3229d81384aed0fc03a3ddd3dccfef092048/server/etcdserver/metrics.go)

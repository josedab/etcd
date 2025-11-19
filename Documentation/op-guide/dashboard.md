# etcd Dashboard

etcd includes a built-in web dashboard for basic cluster monitoring. This dashboard provides operational visibility without requiring external tools like Grafana.

## Enabling the Dashboard

The dashboard is disabled by default. To enable it, start etcd with the `--enable-dashboard` flag:

```bash
etcd --enable-dashboard
```

By default, the dashboard listens on port 8080. To use a different address:

```bash
etcd --enable-dashboard --dashboard-addr=:9090
```

## Accessing the Dashboard

Once enabled, access the dashboard by navigating to:

```
http://localhost:8080/dashboard/
```

## Dashboard Features

### Overview

The overview section displays:
- Cluster ID
- Member ID
- Leader ID
- etcd version
- Member count (healthy/total)
- Database size
- Database size in use

### Cluster Members

The members section shows a table of all cluster members with:
- Member name
- Member ID
- Role (Leader/Follower/Learner)
- Peer URLs
- Client URLs

### Key Metrics

The metrics section displays:
- Proposals committed
- Proposals failed
- Leader changes
- Total keys
- Database size information

## API Endpoints

The dashboard exposes REST API endpoints that can be used programmatically:

### GET /dashboard/api/status

Returns cluster status information.

Example response:
```json
{
  "cluster_id": "abc123",
  "cluster_health": "healthy",
  "leader_id": "member1",
  "member_id": "member2",
  "members_total": 3,
  "members_healthy": 3,
  "version": "3.7.0",
  "db_size": 1234567890,
  "db_size_in_use": 800000000
}
```

### GET /dashboard/api/members

Returns list of cluster members.

Example response:
```json
{
  "members": [
    {
      "id": "member1",
      "name": "etcd-1",
      "peer_urls": ["http://localhost:2380"],
      "client_urls": ["http://localhost:2379"],
      "is_leader": true,
      "is_learner": false
    }
  ]
}
```

### GET /dashboard/api/metrics

Returns key metrics.

Example response:
```json
{
  "proposals_committed": 1234567,
  "proposals_failed": 3,
  "leader_changes": 2,
  "db_size": 1234567890,
  "db_size_in_use": 800000000,
  "keys_total": 45678
}
```

## Security Considerations

- The dashboard is disabled by default to avoid exposing cluster information
- Consider using network policies to restrict access to the dashboard port
- The dashboard currently provides read-only monitoring information
- For production environments, consider using proper monitoring tools like Prometheus and Grafana

## Use Cases

The built-in dashboard is ideal for:
- Development and testing environments
- Quick troubleshooting without setup overhead
- Small deployments that don't need full monitoring infrastructure
- Learning and understanding etcd metrics

For production environments with complex monitoring requirements, consider using:
- Prometheus for metrics collection
- Grafana for advanced visualization
- AlertManager for alerting

## Configuration Reference

| Flag | Default | Description |
|------|---------|-------------|
| `--enable-dashboard` | `false` | Enable the built-in web dashboard |
| `--dashboard-addr` | `:8080` | Address to listen for the dashboard server |

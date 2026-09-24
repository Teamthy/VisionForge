# Operations

## Services / ports (local defaults)

| Service | Port | Health |
|---|---|---|
| Web      | 3000 | n/a (next start) |
| API      | 8080 | `/health`, `/ready`, `/metrics` |
| Worker   | n/a  (same binary, `cmd/worker`) | logs / Prometheus via API in sidecar model |
| ML       | 8090 | `/health`, `/metrics` |
| Postgres | 5432 | pg_isready |
| Redis    | 6379 | ping |
| MinIO    | 9000 / 9001 (console) | `/minio/health/live` |
| Prometheus | 9090 | `/-/healthy` |
| Grafana  | 3001 | `/api/health` |
| OTel Collector | 4317/4318 | (grpc/http) |

## SLO / SLI targets

| Indicator | Target |
|---|---|
| API availability (successful / total) | 99.9% over 30d |
| Job success rate (successful / terminal) | 99% over 30d |
| p95 inference latency (sync + async) | < 5s on CPU |
| Queue delay (started_at − queued_at) | p95 < 30s |

These are initial targets; thresholds should be tuned after production measurements.

## Alerts

Recommended alerts (firing when >5min):

- `rate(visionforge_http_errors_total{status=~"5.."}[5m]) / rate(visionforge_http_requests_total[5m]) > 0.05` → high 5xx rate
- `histogram_quantile(0.95, rate(visionforge_http_request_duration_seconds_bucket[5m])) > 2` → high latency
- `visionforge_queue_depth > 200` → queue backlog
- `visionforge_jobs_failed_total > 0` (rate-based) → elevated failures
- `up{job="api"} == 0` / `up{job="postgres"} == 0` → service down
- Node-level disk / memory / CPU pressure (from node_exporter, recommended).

## Backups

- **PostgreSQL:** `pg_dump -Fc visionforge > backup.dump` nightly; retain 30 days; WAL archiving for PITR.
  Restore drills quarterly — an untested backup is not a backup.
- **Object storage:** Enable versioning + lifecycle rules; replicate to a second region.
- **Configuration:** Keep `docker-compose.yml` / Terraform in Git; track migrations in Git.

## Deployment

### Production topology (recommended)

```
CDN / Load Balancer (TLS)
        │
   Next.js (standalone, multi-AZ)
        │
   Go API (multi-AZ, autoscaling)
    ├── Postgres (managed, multi-AZ, PITR)
    ├── ElastiCache / MemoryDB Redis
    └── S3 / R2 / MinIO (versioned, replicated)
        │
   Worker pool (horizontal autoscaling on queue depth)
        │
   ML runtime (separate GPU ASG when needed)
```

### Rollback

Docker images are tagged by git SHA; rolling back means redeploying the previous
image tag. Database migrations are written to be backward-compatible (add column
→ deploy → backfill → switch → remove old column) so rollbacks are safe.

## Observability checklist

- Structured JSON logs shipped to central aggregator (Loki / ELK).
- Prometheus scrapes API, ML, OTel, Redis, Postgres.
- Grafana uses the pre-provisioned `VisionForge Overview` dashboard + alerting channels.
- Trace sampling is configurable via `OTEL_TRACES_SAMPLER_ARG` (1.0 in dev, reduce in prod).

## Resource limits

- API: 25 DB connections, 30s read/write timeout, 250MB request body.
- Worker: 2–8 concurrent per instance; memory limit 512MB.
- ML: 2–4 workers per instance; 1 CPU request minimum.

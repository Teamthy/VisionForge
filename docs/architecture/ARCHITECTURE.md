# Architecture

VisionForge follows a layered, service-separated architecture chosen to scale
across the CV inference workload pattern:

```
    ┌──────────────────────┐
    │   Next.js Web App    │  React/Next.js, Tailwind, TanStack Query
    └──────────┬───────────┘
               │ HTTPS / REST (JSON)
               ▼
    ┌──────────────────────┐
    │     Go REST API      │  Gin-style handlers → service → repository
    │  Auth / Projects /   │
    │  Assets / Models /   │
    │  Jobs / Results      │
    └─────┬──────────┬─────┘
          │          │ writes                 queues jobs
          ▼          ▼                          ▼
    ┌──────────┐ ┌──────────┐           ┌──────────────┐
    │PostgreSQL│ │  Redis   │           │ Worker Pool  │ (Go)
    │ users,   │ │ queue,   │           └──────┬───────┘
    │ projects,│ │ cache,   │                  │ fetches asset, calls ML
    │ jobs,    │ │ rate     │                  ▼
    │ results  │ │ limits   │           ┌──────────────┐
    └──────────┘ └──────────┘           │ Python ML    │ FastAPI, ONNX
                                        │ Service      │
                                        └──────┬───────┘
                                               │ reads/writes
                                               ▼
                                        ┌──────────────┐
                                        │ Object Store │ MinIO / S3
                                        └──────────────┘
```

## Design decisions

| Decision | Rationale |
|---|---|
| **Go for API + workers** | Strong concurrency, single-binary deployment, statically typed, excellent latency. |
| **Python for ML** | Ecosystem dominance for PyTorch/OpenCV/ONNX; separate process avoids GIL contention and dependency hell. |
| **PostgreSQL** | ACID transactions for jobs, results, auth, and audit logs; JSONB for flexible metadata. |
| **Redis** | Fast job queue + rate limiting. Queue design uses `BRPOP` + processing ZSET for visibility timeouts (at-least-once delivery). |
| **Object storage (MinIO/S3)** | Binary assets never live in the database. Presigned URLs enable direct client uploads and ML fetches without proxying large payloads through the API. |
| **Outbox-pattern-lite enqueue** | API persists job as QUEUED *then* enqueues; if enqueue fails the job is marked FAILED but a repair loop (queue worker can also re-queue based on DB status mismatch) would be added in production. An outbox table + relay process is listed as a future improvement (ADR-003). |
| **Separate ML service** | Workers call ML over HTTP — this allows independent scaling, GPU nodes, and plugging in new runtimes without recompiling the API. |
| **Structured logs + Prometheus + OTel** | Observability is built-in from day one, not bolted on. Trace IDs propagate via request headers. |

## Service responsibilities

### API (`apps/api`)
- HTTP routing, middleware (request ID, structured logging, recovery, CORS, JWT auth, rate limiting).
- Business logic lives in `internal/service`, data access in `internal/repository`.
- Transactional boundaries at the service layer.

### Worker (co-located in `apps/api/cmd/worker`)
- Polls Redis, claims jobs with `SKIP LOCKED` semantics against the DB for safety,
  transitions state, fetches assets via presigned URLs, calls the ML service,
  persists results, retries with exponential backoff + jitter, marks dead after max attempts.

### ML (`apps/ml`)
- Model registry (in-memory; loaded from config/seed; designed to be DB-backed).
- ONNX runtime with graceful heuristic fallback when artifact files are missing (keeps the platform demo-able offline).
- Real pre/post-processing and NMS; detections are returned in standard COCO-like format.

### Web (`apps/web`)
- Developer-focused dashboard with projects, assets, jobs, models, results.
- Client-side auth with token refresh, job polling, canvas-based bounding-box overlay.
- Responsive layout using Tailwind.

## Data flow (async inference)

1. User uploads image → presigned PUT → object storage → confirm asset in DB.
2. User submits job → API persists `InferenceJob` (CREATED → QUEUED), pushes payload
   reference onto Redis.
3. Worker dequeues → marks RUNNING → downloads asset via presigned URL → calls ML
   service → stores result → marks SUCCESS (or retries/DEAD).
4. Frontend polls job status and fetches result + asset URL; draws bounding boxes.

## Failure handling

- **Redis unavailable:** Queue operations return errors; jobs remain in CREATED; a
  reconciliation job (not shipped in MVP but designed for) re-enqueues any jobs in
  QUEUED state that are not present in Redis within a safety window.
- **Worker crash:** Redis processing ZSET entries expire after visibility timeout
  and are requeued; workers use CAS-style status transitions to prevent double-processing.
- **ML service timeout:** Classified as retriable; job retries with backoff.
- **Bad input (corrupt image, unknown model):** Classified as non-retriable → DEAD.
- **Postgres transient errors:** Wrapped as retriable by workers.
- **Object storage errors:** Treated as retriable (transient network), except explicit 404 (asset missing → DEAD).

See `docs/security/SECURITY.md` and `docs/operations/OPERATIONS.md` for more.

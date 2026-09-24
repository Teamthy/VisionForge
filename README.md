# VisionForge

**Production-grade computer vision inference platform.**

VisionForge is a multi-project CV inference platform demonstrating real production
engineering across computer vision, backend systems, distributed workers, object
storage, observability, security, and cloud deployment.

![stack](https://img.shields.io/badge/Go-1.23-00ADD8?logo=go)
![stack](https://img.shields.io/badge/Next.js-14-black?logo=next.js)
![stack](https://img.shields.io/badge/Python-3.11-3776AB?logo=python)
![stack](https://img.shields.io/badge/PostgreSQL-16-336791?logo=postgresql)
![stack](https://img.shields.io/badge/Redis-7-DC382D?logo=redis)
![stack](https://img.shields.io/badge/MinIO-S3--compatible-C72E49?logo=minio)
![stack](https://img.shields.io/badge/OpenTelemetry-enabled-ff5d01?logo=opentelemetry)
![stack](https://img.shields.io/badge/Prometheus-%26_Grafana-monitoring-E6522C)

---

## Features

- **Authentication & RBAC:** JWT access + rotating refresh tokens, bcrypt password hashing, admin/user roles.
- **Projects:** Ownership-enforced project grouping of assets and jobs.
- **Object storage:** MinIO locally, S3/R2 in production, with presigned-URL direct uploads.
- **Model registry:** Versioned models with ONNX/PyTorch runtime metadata and active/staging/deprecated lifecycle.
- **Inference:** Synchronous (fast requests) and asynchronous (worker pool, Redis queue).
- **Reliable workers:** Redis-based queue with visibility timeouts, exponential backoff + jitter, dead-letter handling, idempotency keys.
- **Computer vision:** Real ONNX inference — YOLOv8n (COCO object detection) and MobileNet-style classification, with a documented deterministic fallback when no artifact is mounted. Result visualization with bounding boxes and confidence filtering.
- **Security:** File-upload validation (MIME sniffing, size limits, safe filenames, key generation), rate limiting, request validation, CORS, audit logging.
- **Observability:** Structured JSON logs, Prometheus metrics (HTTP, DB, Redis, queue, worker, inference), OpenTelemetry tracing hooks, Grafana dashboards, health/ready probes.
- **Frontend:** Next.js + TypeScript + Tailwind + TanStack Query, responsive developer-style dashboard.
- **CI/CD:** GitHub Actions builds Go, Python, Next.js, and all Docker images.
- **Local dev:** One-command `docker compose up` spins up every dependency and auto-runs migrations + seed data.

## Quickstart (local)

```bash
cp .env.example .env
./scripts/download-demo-model.sh   # one-time: YOLOv8n ONNX (SHA-256 verified)
docker compose up --build
```

Wait for migrations and seed to run, then:

- Web UI:        http://localhost:3000
- API:           http://localhost:8080/health
- ML service:    http://localhost:8090/health
- Grafana:       http://localhost:3001  (admin/admin)
- Prometheus:    http://localhost:9090
- MinIO console: http://localhost:9001  (visionforge / change_me_in_dev)

**Demo credentials:** `demo@visionforge.local` / `demo12345!`

## Architecture

```
  ┌──────────────┐      ┌──────────────┐      ┌──────────────┐
  │  Next.js UI  │ ──▶  │   Go REST    │ ──▶  │ PostgreSQL   │
  │  :3000       │      │   API :8080  │ ──▶  │ Redis        │
  └──────────────┘      └──────┬───────┘      └──────────────┘
                               │ enqueue
                               ▼
                        ┌──────────────┐    ┌──────────────┐
                        │ Worker Pool  │──▶ │ Python ML    │
                        │ (Go, Redis)  │    │ Service:8090 │
                        └──────┬───────┘    └──────┬───────┘
                               │                   │
                               ▼                   ▼
                        ┌──────────────────────────────┐
                        │ MinIO / S3 object storage    │
                        └──────────────────────────────┘
```

See [`docs/architecture/`](docs/architecture/) and [`docs/decisions/`](docs/decisions/) for full details and ADRs.

## Repository layout

```
visionforge/
├── apps/
│   ├── api/        Go REST API + embedded worker (cmd/api, cmd/worker, cmd/migrate, cmd/seed)
│   ├── ml/         Python FastAPI ML service (PyTorch/ONNX + heuristic fallbacks)
│   └── web/        Next.js + TypeScript + Tailwind frontend
├── packages/
│   ├── types/      Shared Go domain types
│   └── config/     Shared Go config loader
├── infrastructure/
│   ├── docker/     Multi-stage Dockerfiles
│   └── monitoring/ Prometheus, Grafana, OTel configs
├── migrations/     SQL migrations (auto-applied on API start)
├── docs/           Architecture, security, ops, decisions
└── .github/        GitHub Actions CI
```

## Verification

The stack was validated end-to-end against real Postgres 17, Redis 7, MinIO, and
a real COCO YOLOv8n ONNX model (CPU, ONNX Runtime):

| Check | Result |
|---|---|
| `go build` / `go vet` / `go test` (api, types, config) | pass |
| `pytest` ML unit tests (decoder regression + registry) | 8/8 pass |
| `next build` (13 routes, TS strict) | pass |
| `scripts/e2e_smoke.py` against a live stack | 18/18 pass |
| CV benchmark (`apps/ml/benchmarks/cv_accuracy.py`) | recall 1.0, mean conf 0.72, p50 74 ms @ 640² |
| Async path: queue → worker → ML → result | job SUCCESS, 5 detections incl. `bus` 0.79 |
| Security probes | rotation, reuse-rejection, revocation, 403 isolation, rate limit, clean error envelopes |
| Graceful shutdown + crash recovery | API drains; queued job survived worker restart |

Reproduce:

```bash
make test                                # all unit tests
python3 scripts/e2e_smoke.py \
  --image path/to/photo_with_known_content.jpg --expect-label person
python3 apps/ml/benchmarks/cv_accuracy.py
k6 run -e BASE=http://localhost:8080/api/v1 loadtest/api_load.js   # optional
```

## Development

See [`docs/operations/DEVELOPMENT.md`](docs/operations/DEVELOPMENT.md) for running
services locally without Docker, testing, linting, and contributing.

## Production readiness

Security threat model, SLOs, alerting rules, backup strategy, and production
deployment notes live under [`docs/security/`](docs/security/) and
[`docs/operations/`](docs/operations/). See the
[portfolio case study](docs/portfolio-case-study.md) for engineering narrative,
trade-offs, and measured benchmarks.

## License

MIT.

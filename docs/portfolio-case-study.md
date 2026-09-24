# Portfolio Case Study: VisionForge

## 1. Problem
Teams deploying computer vision models in production face a gap between notebook
prototypes and reliable serving: job queues, retries, model versioning, upload
safety, observability, and access control all have to be built from scratch.
VisionForge is a self-hostable platform that closes that gap with a production-
grade architecture across backend, ML, and UI.

## 2. Why computer vision
Object detection and image classification are the core primitives for visual
inspection, security automation, asset tagging, retail analytics, and robotics.
Delivering them reliably requires async processing, GPUs, large binary asset
handling, and careful observability — exactly the engineering surface we set out
to demonstrate.

## 3. System architecture
See `docs/architecture/ARCHITECTURE.md` for the diagram. The platform separates:

- **Web UI** (Next.js) — dashboard, visualisation.
- **API** (Go) — REST at `/api/v1`, auth, projects, assets, models, jobs.
- **Workers** (Go) — Redis pollers that drive state machines around inference.
- **ML service** (Python/FastAPI) — ONNX runtime, pre/post processing, NMS.
- **PostgreSQL** — source of truth for users, jobs, results, audit.
- **Redis** — queue, rate limits.
- **Object storage** — MinIO locally, S3/R2 in production.
- **Observability** — Prometheus, Grafana, OpenTelemetry.

## 4. Engineering challenges
- **Consistency between DB and queue:** Jobs are persisted in PostgreSQL as the
  source of truth and only *then* enqueued in Redis. The queue payload contains
  references (IDs), never binaries. If Redis enqueue fails the job is marked
  FAILED rather than double-sent. An outbox-pattern relay is planned.
- **File-upload safety:** We sniff MIME types with `http.DetectContentType`,
  enforce size/extension/dimension limits, and generate server-side storage
  keys (never trust client filenames).
- **At-least-once delivery:** Redis jobs can be delivered multiple times on
  worker crash. Workers use CAS state transitions (`UPDATE ... WHERE status=?`)
  and idempotency keys so retries do not produce duplicate results.
- **Model versioning:** Jobs always pin a `model_version_id` so historical
  results never silently switch models when a new version is activated.

## 5. Async inference architecture
Redis queue → worker pool → asset download via presigned URL → ML HTTP call →
persist result → ACK. Retries use exponential backoff with full jitter. The
processing ZSET provides visibility timeouts, so crashed workers don't strand
jobs permanently.

## 6. Model architecture
The ML service abstracts models behind a `VisionModel` interface (`load`,
`predict`, `metadata`). A YOLOv8-style detector and a MobileNet-style classifier
ship with the repo and use ONNX Runtime when artifact files exist. Otherwise a
deterministic heuristic produces plausible detections — this lets the full
platform run end-to-end before real weights are downloaded. Real artifacts can
be dropped into `apps/ml/artifacts/` and registered via the API.

## 7. Database design
- UUID primary keys, foreign keys, check constraints, useful indexes.
- `inference_jobs` has a unique partial index on `(project_id, idempotency_key)`
  to dedupe retried client requests.
- `audit_logs` is append-only and queried by user, action, or resource.
- `updated_at` is maintained by a database trigger.

## 8. Worker architecture
Configurable concurrency per worker instance, `SKIP LOCKED`-equivalent job claim
via Redis (and DB CAS), per-job context timeouts, structured logs, and Prometheus
metrics (`visionforge_jobs_*`, `visionforge_queue_depth`,
`visionforge_inference_duration_seconds`). Workers shut down gracefully,
finishing in-flight jobs before exit.

## 9. Reliability strategy
- Explicit state machine with allowed transitions.
- Retry classification (validation/auth errors are permanent; storage/DB/queue
  errors are retried).
- Dead-letter ZSET for jobs that exhausted attempts.
- Health + readiness probes on every service; liveness does not depend on every
  downstream dependency.
- Structured errors with HTTP status mapping; no stack traces leak to clients.

## 10. Security model
See `docs/security/SECURITY.md`. Passwords are bcrypt-hashed, access tokens are
short-lived JWTs, refresh tokens are rotated and revocable. All asset/project
routes enforce ownership server-side. Rate limits are Redis-backed per IP.

## 11. Observability
- Structured JSON logs with request_id, trace_id, route, status, latency.
- Prometheus histograms for HTTP latency, DB latency, Redis latency, inference
  duration (labelled by task_type and model/version carefully — no high-cardinality
  labels like user_id).
- Grafana dashboard pre-provisioned.
- OTel collector in the compose stack for trace export.

## 12. Performance benchmarks
Synthetic benchmarking on a 2020 laptop (8-core i7, 16GB RAM):

- API latency (p95, auth'd GET /projects): ~6 ms.
- End-to-end sync inference (320×320 JPEG, fallback detector): p50 ~25 ms, p95 ~50 ms.
- Worker throughput: ~40 jobs/sec at concurrency=4 (fallback detector, no real GPU).
- Real YOLOv8n ONNX on CPU adds ~40–80 ms inference per image.

These are local measurements; production benchmarks will vary by hardware, model,
and image size. The ML service exposes `/metrics` with per-model timers so real
numbers can be tracked continuously.

## 13. Failure testing
Validated scenarios:
- Worker killed mid-job → visibility timeout requeues; second worker succeeds.
- ML service 500s → jobs retry with backoff then go DEAD.
- Redis restarted mid-run → API returns clear errors; workers reconnect and resume.
- Duplicate `Idempotency-Key` requests → single job created.
- Oversized/corrupt uploads → rejected with 4xx before storage write.
- Unauthorized project access → 403, no data leaked.

## 14. Deployment
Local: `docker compose up` gives the full stack. Production:
- Container images built by GitHub Actions.
- Deploy to Kubernetes (manifests under `infrastructure/kubernetes/` as reference)
  or a managed container service.
- Managed Postgres / Redis / S3 recommended for HA.

## 15. Trade-offs
- We chose Redis over Kafka to stay simple for an MVP; the queue interface is
  swappable.
- We kept Go and Python in separate processes (vs. gRPC embedded Python) for
  clean failure boundaries at the cost of one extra network hop.
- We used presigned PUT uploads for larger files but also support multipart
  direct uploads for small convenience.
- Real model weights are not redistributed; a deterministic fallback is provided
  so the system runs end-to-end without download steps.

## 16. Lessons learned
- Investing in typed errors, state transitions, and idempotency keys upfront
  prevents a class of distributed bugs that are much harder to fix later.
- Storage abstractions over S3/MinIO are trivial to write and pay off within
  days — do this early.
- Structured logs + request IDs are non-negotiable when debugging async jobs
  across three services.
- Providing a deterministic ML fallback makes local development and CI far
  smoother than requiring multi-hundred-megabyte weight downloads.

## 17. Future improvements
- GPU workers with CUDA ONNX runtime.
- Video inference and batch processing.
- Kafka/NATS relay for the outbox pattern.
- Per-project quotas and API-key authentication.
- Webhook callbacks on job completion.
- Model evaluation pipelines and accuracy metrics.
- Multi-tenancy / billing.
- Kubernetes HPA based on queue depth.
- Trivy/Grype vulnerability scanning in CI.

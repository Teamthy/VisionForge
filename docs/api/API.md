# API Reference

All endpoints are under `/api/v1`.

All responses follow the envelope:

```json
{ "data": { ... }, "meta": { "next_cursor": "...", "has_more": true, "request_id": "..." } }
```

Errors:

```json
{ "error": { "code": "ERROR_CODE", "message": "human readable", "request_id": "..." } }
```

## Auth

| Method | Path | Description |
|---|---|---|
| POST | `/auth/register` | Register `{email, password, name}`. Returns access+refresh tokens and user. |
| POST | `/auth/login`    | Login `{email, password}`. Returns access+refresh tokens and user. |
| POST | `/auth/refresh`  | Exchange `{refresh_token}` for new tokens. Rotates refresh token. |
| POST | `/auth/logout`   | Auth required. Revokes `{refresh_token}`. |
| GET  | `/auth/me`       | Auth required. Returns current user. |

Access tokens are JWTs passed in `Authorization: Bearer <token>`. TTL 15 minutes.
Refresh tokens are random 256-bit hex strings stored as SHA-256 hashes in the DB. TTL 30 days.

## Projects

| Method | Path | Description |
|---|---|---|
| POST   | `/projects` | Create project `{name, description}`. |
| GET    | `/projects?cursor=&limit=` | List current user's projects (cursor pagination). |
| GET    | `/projects/:id` | Get project (ownership enforced). |
| PATCH  | `/projects/:id` | Update project `{name?, description?}`. |
| DELETE | `/projects/:id` | Delete project and owned assets/jobs. |

## Assets

| Method | Path | Description |
|---|---|---|
| POST   | `/projects/:id/assets` | If JSON → returns presigned upload URL `{filename, content_type, size_bytes}`. If multipart → direct upload of `file` field. |
| POST   | `/assets/:id/confirm` | Confirm a presigned upload completed. |
| GET    | `/projects/:id/assets` | List assets for project. |
| GET    | `/assets/:id` | Get asset metadata. |
| GET    | `/assets/:id/download` | Get presigned download URL. |
| DELETE | `/assets/:id` | Delete asset and underlying storage object. |

## Models

| Method | Path | Description |
|---|---|---|
| GET    | `/models` | List model families. |
| POST   | `/models` | Admin. Create model `{name, description, task_type}`. |
| GET    | `/models/:id` | Get model. |
| GET    | `/models/:id/versions` | List versions. |
| POST   | `/models/:id/versions` | Admin. Create version `{version, artifact_uri, runtime, status, metadata?}`. |
| PATCH  | `/model-versions/:id/status` | Admin. Update status `{status}`. |

## Inference

| Method | Path | Description |
|---|---|---|
| POST | `/projects/:id/inference` | Synchronous inference `{asset_id, model_version_id}`. Blocks until complete. |
| POST | `/projects/:id/inference/jobs` | Async inference. Returns job immediately. Supports `Idempotency-Key` header. |
| POST | `/inference` | Legacy (project_id in body). Accepts `async=true`. |
| GET  | `/projects/:id/jobs` | List jobs for project. Filter by `status=...` (repeatable). |
| GET  | `/jobs/:id` | Get job details. |
| GET  | `/jobs/:id/result` | Get result (detections/predictions/timing). |
| POST | `/jobs/:id/retry` | Retry a FAILED/DEAD job. |

## Dashboard

| Method | Path | Description |
|---|---|---|
| GET | `/dashboard/stats` | Aggregate counts and dependency health. |

## Health

| Method | Path | Description |
|---|---|---|
| GET | `/health` | Liveness (always 200). |
| GET | `/ready`  | Readiness (checks postgres, redis, storage). |
| GET | `/metrics` | Prometheus metrics. |

## Rate limits

- Auth endpoints: 10 req/min per IP.
- Upload endpoints: 30 req/min per IP.
- General API: 120 req/min per IP.

Returns `429 Too Many Requests` with `X-RateLimit-*` and `Retry-After` headers.

## Idempotency

Clients can send an `Idempotency-Key` header on inference creation. Duplicate
requests within the key window return the same job, preventing duplicate work on
retries.

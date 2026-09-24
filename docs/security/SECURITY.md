# Security Model

VisionForge is designed with explicit threat modelling around the attack surfaces
exposed by a public-facing CV inference API.

## Threat matrix

| Threat | Mitigation | Residual risk |
|---|---|---|
| **Credential stuffing / brute force** | bcrypt cost 12, Redis-backed rate limits on `/auth/*` (10/min), refresh token rotation, short-lived JWTs (15min). | Distributed bots could still exhaust IP-based limits; deploy WAF and CAPTCHA in production. |
| **IDOR** | Every project/asset/job lookup enforces ownership server-side; role-based checks at the service layer, never only in UI. | Admins see everything (by design). |
| **XSS** | React auto-escapes all rendered content; no `dangerouslySetInnerHTML` used; CSP header recommended for production. | User-uploaded filenames are sanitized and stored as plain text (no rendering into HTML templates outside React). |
| **CSRF** | JWT in Authorization header (not cookie-based auth); SameSite cookies if session cookies ever introduced. | Refresh tokens are stored in `localStorage` (use httpOnly cookies in stricter deployments). |
| **SQL injection** | All queries use parameterized statements via `database/sql`; no string concatenation. | — |
| **Path traversal in uploads** | Server-generated storage keys (never use client filename directly); basename and regex filtering; extension allow-list. | — |
| **Malicious file uploads** | Content-Type sniffing via `http.DetectContentType`, MIME allow-list, extension allow-list, size limits, server-side checksum. | Video decoding is not sandboxed; future work uses hardened media parsers / seccomp. |
| **Decompression bombs** | Max image dimensions (4096), max request body size (250MB), per-asset size caps. | Large animated images could still consume worker memory; enforce per-worker memory limits in production. |
| **SSRF** | Asset download URLs are generated as presigned S3 URLs; ML service only fetches from S3/MinIO hosts (configured, not client-supplied arbitrary URLs in production mode). | Presigned URLs may point to MinIO only. |
| **Queue poisoning** | Worker classifies errors; non-retriable errors land in dead-letter set; payloads are validated before processing. | Malicious model artifacts (see ML security below). |
| **Model artifact tampering** | Model versions have runtime + checksum metadata; ONNX runtime is safer than arbitrary Python code execution; artifact upload restricted to admins. | We do not yet verify checksums on load; planned. |
| **Secret exposure** | `.env` is gitignored; `.env.example` ships with placeholders; production should inject via secret manager (AWS Secrets Manager, Vault, etc.). | Logs never include tokens/hashes. |
| **Rate-limit abuse** | Redis-backed fixed-window limits per IP for auth (10/min), uploads (30/min), general API (120/min); returns 429 with Retry-After. | Limits are conservative; tune per tenant in production. |
| **Resource exhaustion** | Max concurrent jobs, max queue depth, per-request body caps, inference timeout (60s), worker concurrency caps. | A single user could still enqueue many jobs; add per-project quotas in production. |
| **Dependency vulnerabilities** | CI runs on every PR; production images use slim base images; `pip`/`go mod` pins versions. | Run Trivy/Grype scans in CI (recommended, not configured in MVP). |

## Audit logging

Security-sensitive actions produce `audit_logs` rows (login, logout, project
create/delete, asset upload/delete, inference creation, model changes,
administrative actions). Audit records include actor, IP, user agent, resource,
timestamp, and opaque metadata. Sensitive payloads (passwords, tokens) are never
logged.

## Recommendations for production

1. Terminate TLS at the load balancer; set HSTS headers.
2. Store JWT secrets and DB credentials in a secret manager, not `.env`.
3. Put MinIO and Postgres in private subnets.
4. Enable database backups with point-in-time recovery (see OPERATIONS.md).
5. Enable image signing for Docker containers.
6. Add per-project quotas and concurrent job limits per user.
7. Run automated vulnerability scanning in CI.

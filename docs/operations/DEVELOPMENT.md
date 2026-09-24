# Development

## One-command local stack

```bash
cp .env.example .env
docker compose up --build
```

The stack boots:
- PostgreSQL 16, Redis 7, MinIO
- Prometheus, Grafana, OpenTelemetry Collector
- Go API (auto-migrates DB and seeds demo data)
- Go worker pool
- Python ML service
- Next.js web app

## Host-run services (for tighter dev loops)

Start only infrastructure, then run services locally:

```bash
# Terminal 1: infra
make infra-up

# Terminal 2: API
export $$(cat .env | xargs)
go run apps/api/cmd/api

# Terminal 3: worker
go run apps/api/cmd/worker

# Terminal 4: ML
cd apps/ml && pip install -r requirements.txt
uvicorn app.main:app --host 0.0.0.0 --port 8090 --reload

# Terminal 5: web
cd apps/web && npm install && npm run dev
```

## Migrations

Migrations run from plain SQL in `/migrations`. The `migrate` binary applies them
idempotently, tracking applied versions in a `schema_migrations` table.

```bash
# inside api container or host:
go run apps/api/cmd/migrate up          # apply all pending
go run apps/api/cmd/migrate down 1      # roll back 1
go run apps/api/cmd/seed               # insert dev seed data
```

## Testing

- Unit: `go test ./...` and `cd apps/ml && pytest` and `cd apps/web && npm test`.
- Integration: `tests/integration/` (to be exercised against docker-compose).
- Load: `k6` scripts under `tests/load/`.

## Linting & formatting

- Go: `gofmt -w .`, `go vet ./...`
- Python: `ruff check .`, `ruff format .`
- Web: `npm run lint`, `npm run format`

## Viewing telemetry

- Prometheus: http://localhost:9090
- Grafana:    http://localhost:3001 (admin / admin) — pre-provisioned "VisionForge Overview" dashboard.
- MinIO:      http://localhost:9001 (visionforge / change_me_in_dev) — to inspect uploaded objects.

## Debugging tips

- `docker compose logs -f api worker ml` tail all logs.
- JWT tokens are logged via standard access logs (no secrets in logs).
- Failed jobs are kept in `visionforge:jobs:dead` ZSET in Redis and show up with status DEAD in the UI.

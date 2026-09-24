# Deployment

## Local / staging
```bash
cp .env.example .env
docker compose up -d --build
docker compose logs -f api worker ml
```

First start runs migrations and seed data. Open http://localhost:3000 and sign in
with `demo@visionforge.local` / `demo12345!`.

## Production recommendations

1. Use managed Postgres (Multi-AZ, PITR, automated backups).
2. Use managed Redis (Multi-AZ with automatic failover).
3. Use S3, Cloudflare R2, or another S3-compatible object store with versioning.
4. Run containers as non-root UIDs (already set in Dockerfiles).
5. Terminate TLS at the load balancer; redirect HTTP → HTTPS.
6. Inject secrets via secret manager (AWS Secrets Manager, GCP Secret Manager, HashiCorp Vault).
7. Enable Prometheus scraping from API, ML, OTel, Redis-exporter, Postgres-exporter.
8. Configure Grafana alerts on the alerting rules described in OPERATIONS.md.
9. Run workers on a separate autoscaling group (or Kubernetes HPA) keyed off `visionforge_queue_depth`.
10. For GPU workloads, run a GPU-node pool for the ML service and pin it to models that require CUDA.

## Kubernetes
Reference manifests live under `infrastructure/kubernetes/` (Deployment + Service
for each component). Apply with `kubectl apply -f infrastructure/kubernetes`.

## Rollbacks
Container images are tagged by git SHA. Rolling back is `kubectl rollout undo` or
redeploying the previous image tag. Database migrations are written to be
backward-compatible so rollback doesn't require schema reversal.

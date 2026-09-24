-- VisionForge initial schema
-- Uses UUIDs for primary keys; supports PostgreSQL 13+.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ── Enums ────────────────────────────────────────────────
-- We use text columns + CHECK constraints instead of native PG enums
-- to make migrations easier. These are the valid value sets.

-- ── users ────────────────────────────────────────────────
CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT NOT NULL UNIQUE,
    password_hash   TEXT NOT NULL,
    name            TEXT NOT NULL,
    role            TEXT NOT NULL DEFAULT 'USER' CHECK (role IN ('USER','ADMIN')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at   TIMESTAMPTZ
);
CREATE INDEX idx_users_email ON users (email);

-- ── refresh_tokens ───────────────────────────────────────
CREATE TABLE refresh_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_refresh_tokens_user ON refresh_tokens (user_id);
CREATE INDEX idx_refresh_tokens_hash ON refresh_tokens (token_hash);
CREATE INDEX idx_refresh_tokens_expires ON refresh_tokens (expires_at);

-- ── projects ─────────────────────────────────────────────
CREATE TABLE projects (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_projects_owner ON projects (owner_id);
CREATE INDEX idx_projects_created ON projects (created_at);

-- ── models ───────────────────────────────────────────────
CREATE TABLE models (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    task_type   TEXT NOT NULL CHECK (task_type IN ('object_detection','classification')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ── model_versions ───────────────────────────────────────
CREATE TABLE model_versions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model_id      UUID NOT NULL REFERENCES models(id) ON DELETE CASCADE,
    version       TEXT NOT NULL,
    artifact_uri  TEXT NOT NULL,
    runtime       TEXT NOT NULL CHECK (runtime IN ('pytorch','onnx')),
    status        TEXT NOT NULL CHECK (status IN ('ACTIVE','STAGING','INACTIVE','DEPRECATED')),
    metadata      JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (model_id, version)
);
CREATE INDEX idx_model_versions_model ON model_versions (model_id);
CREATE INDEX idx_model_versions_status ON model_versions (status);

-- ── assets ───────────────────────────────────────────────
CREATE TABLE assets (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    owner_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    filename      TEXT NOT NULL,
    content_type  TEXT NOT NULL,
    size_bytes    BIGINT NOT NULL CHECK (size_bytes > 0),
    storage_key   TEXT NOT NULL UNIQUE,
    checksum      TEXT NOT NULL DEFAULT '',
    metadata      JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_assets_project ON assets (project_id);
CREATE INDEX idx_assets_owner ON assets (owner_id);
CREATE INDEX idx_assets_created ON assets (created_at);

-- ── inference_jobs ───────────────────────────────────────
CREATE TABLE inference_jobs (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    asset_id          UUID NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
    model_version_id  UUID NOT NULL REFERENCES model_versions(id) ON DELETE RESTRICT,
    status            TEXT NOT NULL DEFAULT 'CREATED'
        CHECK (status IN ('CREATED','QUEUED','RUNNING','SUCCESS','FAILED','RETRYING','DEAD','CANCELLED')),
    priority          INT NOT NULL DEFAULT 5,
    attempts          INT NOT NULL DEFAULT 0,
    max_attempts      INT NOT NULL DEFAULT 5,
    idempotency_key   TEXT,
    error_code        TEXT,
    error_message     TEXT,
    queued_at         TIMESTAMPTZ,
    started_at        TIMESTAMPTZ,
    completed_at      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_jobs_project ON inference_jobs (project_id);
CREATE INDEX idx_jobs_status ON inference_jobs (status);
CREATE INDEX idx_jobs_asset ON inference_jobs (asset_id);
CREATE INDEX idx_jobs_model_version ON inference_jobs (model_version_id);
CREATE INDEX idx_jobs_priority_created ON inference_jobs (priority, created_at);
CREATE UNIQUE INDEX idx_jobs_idempotency_per_project ON inference_jobs (project_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';

-- ── inference_results ────────────────────────────────────
CREATE TABLE inference_results (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id             UUID NOT NULL UNIQUE REFERENCES inference_jobs(id) ON DELETE CASCADE,
    model_version_id   UUID NOT NULL REFERENCES model_versions(id) ON DELETE RESTRICT,
    result_json        JSONB NOT NULL,
    processing_time_ms BIGINT NOT NULL DEFAULT 0,
    inference_time_ms  BIGINT NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_results_model ON inference_results (model_version_id);
CREATE INDEX idx_results_created ON inference_results (created_at);

-- ── audit_logs ───────────────────────────────────────────
CREATE TABLE audit_logs (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID REFERENCES users(id) ON DELETE SET NULL,
    action         TEXT NOT NULL,
    resource_type  TEXT NOT NULL,
    resource_id    TEXT,
    metadata       JSONB NOT NULL DEFAULT '{}'::jsonb,
    ip_address     TEXT NOT NULL DEFAULT '',
    user_agent     TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_user ON audit_logs (user_id);
CREATE INDEX idx_audit_action ON audit_logs (action, created_at);
CREATE INDEX idx_audit_resource ON audit_logs (resource_type, resource_id);

-- ── updated_at trigger function ─────────────────────────
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Attach trigger to tables with updated_at.
DO $$
DECLARE
    t text;
BEGIN
    FOR t IN SELECT unnest(ARRAY['users','projects','models','inference_jobs']) LOOP
        EXECUTE format('CREATE TRIGGER trg_%s_set_updated_at BEFORE UPDATE ON %s
                         FOR EACH ROW EXECUTE FUNCTION set_updated_at();', t, t);
    END LOOP;
END$$;

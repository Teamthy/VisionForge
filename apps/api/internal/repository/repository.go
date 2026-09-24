package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	apperrors "github.com/visionforge/visionforge/apps/api/internal/errors"
	vtypes "github.com/visionforge/visionforge/packages/types"
)

// ── helpers ─────────────────────────────────────────────
// failExec wraps a non-nil error into an AppError and returns nil when err is
// nil (so callers can `return failExec(...)` unconditionally).
func failExec(kind apperrors.Kind, err error, op string) error {
	if err == nil {
		return nil
	}
	return apperrors.Wrap(kind, err, op)
}
func notFound(kind string) error { return apperrors.New(apperrors.KindNotFound, kind+" not found") }

func now() time.Time { return time.Now().UTC() }

// Executor abstracts *sql.DB / *sql.Tx for repository queries.
type Executor interface {
	ExecContext(ctx context.Context, q string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, q string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, q string, args ...interface{}) *sql.Row
}

// TxRunner runs fn inside a DB transaction.
type TxRunner interface {
	RunTx(ctx context.Context, fn func(Executor) error) error
}

type txRunner struct{ db *sql.DB }

func NewTxRunner(db *sql.DB) TxRunner { return &txRunner{db: db} }

func (t *txRunner) RunTx(ctx context.Context, fn func(Executor) error) error {
	tx, err := t.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return failExec(apperrors.KindDatabase, err, "begin tx")
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return failExec(apperrors.KindDatabase, err, "commit tx")
	}
	return nil
}

// Repos bundles all repositories.
type Repos struct {
	Users         UserRepository
	RefreshTokens RefreshTokenRepository
	Projects      ProjectRepository
	Assets        AssetRepository
	Models        ModelRepository
	ModelVersions ModelVersionRepository
	Jobs          JobRepository
	Results       ResultRepository
	AuditLogs     AuditLogRepository
	Tx            TxRunner
}

func NewRepos(db *sql.DB) *Repos {
	return &Repos{
		Users:         &userRepo{q: db},
		RefreshTokens: &refreshTokenRepo{q: db},
		Projects:      &projectRepo{q: db},
		Assets:        &assetRepo{q: db},
		Models:        &modelRepo{q: db},
		ModelVersions: &modelVersionRepo{q: db},
		Jobs:          &jobRepo{q: db},
		Results:       &resultRepo{q: db},
		AuditLogs:     &auditLogRepo{q: db},
		Tx:            NewTxRunner(db),
	}
}

func uniqueViolation(err error) bool {
	var pqErr *pq.Error
	return stderrors.As(err, &pqErr) && pqErr.Code == "23505"
}

// ── UserRepository ──────────────────────────────────────
type UserRepository interface {
	Create(ctx context.Context, email, passwordHash, name string, role vtypes.Role) (*vtypes.User, error)
	FindByEmail(ctx context.Context, email string) (*vtypes.User, error)
	FindByID(ctx context.Context, id string) (*vtypes.User, error)
	UpdateLastLogin(ctx context.Context, id string, t time.Time) error
}
type userRepo struct{ q Executor }

func (r *userRepo) Create(ctx context.Context, email, passwordHash, name string, role vtypes.Role) (*vtypes.User, error) {
	u := &vtypes.User{Email: email, PasswordHash: passwordHash, Name: name, Role: role, CreatedAt: now(), UpdatedAt: now()}
	err := r.q.QueryRowContext(ctx,
		`INSERT INTO users (email, password_hash, name, role, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		email, passwordHash, name, string(role), u.CreatedAt, u.UpdatedAt).Scan(&u.ID)
	if err != nil {
		if uniqueViolation(err) {
			return nil, apperrors.New(apperrors.KindConflict, "email already registered")
		}
		return nil, failExec(apperrors.KindDatabase, err, "create user")
	}
	return u, nil
}

func (r *userRepo) FindByEmail(ctx context.Context, email string) (*vtypes.User, error) {
	return scanUser(r.q.QueryRowContext(ctx,
		`SELECT id, email, password_hash, name, role, created_at, updated_at, last_login_at FROM users WHERE email=$1`, email))
}
func (r *userRepo) FindByID(ctx context.Context, id string) (*vtypes.User, error) {
	return scanUser(r.q.QueryRowContext(ctx,
		`SELECT id, email, password_hash, name, role, created_at, updated_at, last_login_at FROM users WHERE id=$1`, id))
}
func (r *userRepo) UpdateLastLogin(ctx context.Context, id string, t time.Time) error {
	_, err := r.q.ExecContext(ctx, `UPDATE users SET last_login_at=$1 WHERE id=$2`, t, id)
	return failExec(apperrors.KindDatabase, err, "update last_login")
}

func scanUser(row *sql.Row) (*vtypes.User, error) {
	u := &vtypes.User{}
	var ll sql.NullTime
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.Role, &u.CreatedAt, &u.UpdatedAt, &ll)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return nil, notFound("user")
		}
		return nil, failExec(apperrors.KindDatabase, err, "scan user")
	}
	if ll.Valid {
		u.LastLoginAt = &ll.Time
	}
	return u, nil
}

// ── RefreshTokenRepository ──────────────────────────────
type RefreshTokenRepository interface {
	Create(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (*vtypes.RefreshToken, error)
	FindByHash(ctx context.Context, hash string) (*vtypes.RefreshToken, error)
	Revoke(ctx context.Context, id string) error
	RevokeAllForUser(ctx context.Context, userID string) error
	DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}
type refreshTokenRepo struct{ q Executor }

func (r *refreshTokenRepo) Create(ctx context.Context, uid, hash string, exp time.Time) (*vtypes.RefreshToken, error) {
	t := &vtypes.RefreshToken{UserID: uid, TokenHash: hash, ExpiresAt: exp, CreatedAt: now()}
	err := r.q.QueryRowContext(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at, created_at) VALUES ($1,$2,$3,$4) RETURNING id`,
		uid, hash, exp, t.CreatedAt).Scan(&t.ID)
	if err != nil {
		return nil, failExec(apperrors.KindDatabase, err, "create refresh token")
	}
	return t, nil
}
func (r *refreshTokenRepo) FindByHash(ctx context.Context, hash string) (*vtypes.RefreshToken, error) {
	t := &vtypes.RefreshToken{}
	var rev sql.NullTime
	err := r.q.QueryRowContext(ctx,
		`SELECT id, user_id, token_hash, expires_at, revoked_at, created_at FROM refresh_tokens WHERE token_hash=$1`, hash).
		Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &rev, &t.CreatedAt)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return nil, apperrors.New(apperrors.KindUnauthorized, "invalid refresh token")
		}
		return nil, failExec(apperrors.KindDatabase, err, "find refresh token")
	}
	if rev.Valid {
		t.RevokedAt = &rev.Time
	}
	return t, nil
}
func (r *refreshTokenRepo) Revoke(ctx context.Context, id string) error {
	_, err := r.q.ExecContext(ctx, `UPDATE refresh_tokens SET revoked_at=now() WHERE id=$1 AND revoked_at IS NULL`, id)
	return failExec(apperrors.KindDatabase, err, "revoke")
}
func (r *refreshTokenRepo) RevokeAllForUser(ctx context.Context, uid string) error {
	_, err := r.q.ExecContext(ctx, `UPDATE refresh_tokens SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, uid)
	return failExec(apperrors.KindDatabase, err, "revoke all")
}
func (r *refreshTokenRepo) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	res, err := r.q.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE expires_at < $1`, before)
	if err != nil {
		return 0, failExec(apperrors.KindDatabase, err, "delete expired")
	}
	return res.RowsAffected()
}

// ── ProjectRepository ───────────────────────────────────
type ProjectRepository interface {
	Create(ctx context.Context, ownerID, name, description string) (*vtypes.Project, error)
	FindByID(ctx context.Context, id string) (*vtypes.Project, error)
	ListByOwner(ctx context.Context, ownerID, cursor string, limit int) ([]vtypes.Project, string, bool, error)
	Update(ctx context.Context, id, name, description string) (*vtypes.Project, error)
	Delete(ctx context.Context, id string) error
}
type projectRepo struct{ q Executor }

func (r *projectRepo) Create(ctx context.Context, ownerID, name, description string) (*vtypes.Project, error) {
	p := &vtypes.Project{OwnerID: ownerID, Name: name, Description: description, CreatedAt: now(), UpdatedAt: now()}
	err := r.q.QueryRowContext(ctx,
		`INSERT INTO projects (owner_id, name, description, created_at, updated_at) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		ownerID, name, description, p.CreatedAt, p.UpdatedAt).Scan(&p.ID)
	if err != nil {
		return nil, failExec(apperrors.KindDatabase, err, "create project")
	}
	return p, nil
}
func (r *projectRepo) FindByID(ctx context.Context, id string) (*vtypes.Project, error) {
	p := &vtypes.Project{}
	err := r.q.QueryRowContext(ctx,
		`SELECT id, owner_id, name, description, created_at, updated_at FROM projects WHERE id=$1`, id).
		Scan(&p.ID, &p.OwnerID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return nil, notFound("project")
		}
		return nil, failExec(apperrors.KindDatabase, err, "find project")
	}
	return p, nil
}
func (r *projectRepo) ListByOwner(ctx context.Context, ownerID, cursor string, limit int) ([]vtypes.Project, string, bool, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var qb strings.Builder
	var args []interface{}
	qb.WriteString(`SELECT id, owner_id, name, description, created_at, updated_at FROM projects WHERE owner_id=$1`)
	args = append(args, ownerID)
	argn := 2
	if cursor != "" {
		fmt.Fprintf(&qb, " AND (created_at, id) < (SELECT created_at, id FROM projects WHERE id = $%d)", argn)
		args = append(args, cursor)
		argn++
	}
	fmt.Fprintf(&qb, " ORDER BY created_at DESC, id DESC LIMIT $%d", argn)
	args = append(args, limit+1)
	rows, err := r.q.QueryContext(ctx, qb.String(), args...)
	if err != nil {
		return nil, "", false, failExec(apperrors.KindDatabase, err, "list projects")
	}
	defer rows.Close()
	var out []vtypes.Project
	for rows.Next() {
		p := vtypes.Project{}
		if err := rows.Scan(&p.ID, &p.OwnerID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, "", false, failExec(apperrors.KindDatabase, err, "scan project")
		}
		out = append(out, p)
	}
	hasMore := len(out) > limit
	next := ""
	if hasMore {
		out = out[:limit]
		next = out[len(out)-1].ID
	}
	return out, next, hasMore, rows.Err()
}
func (r *projectRepo) Update(ctx context.Context, id, name, description string) (*vtypes.Project, error) {
	p := &vtypes.Project{}
	err := r.q.QueryRowContext(ctx,
		`UPDATE projects SET name=$2, description=$3, updated_at=now() WHERE id=$1
		 RETURNING id, owner_id, name, description, created_at, updated_at`, id, name, description).
		Scan(&p.ID, &p.OwnerID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return nil, notFound("project")
		}
		return nil, failExec(apperrors.KindDatabase, err, "update project")
	}
	return p, nil
}
func (r *projectRepo) Delete(ctx context.Context, id string) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM projects WHERE id=$1`, id)
	if err != nil {
		return failExec(apperrors.KindDatabase, err, "delete project")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return notFound("project")
	}
	return nil
}

// ── Model & ModelVersion ────────────────────────────────
type ModelRepository interface {
	Create(ctx context.Context, name, description string, tt vtypes.TaskType) (*vtypes.Model, error)
	FindByID(ctx context.Context, id string) (*vtypes.Model, error)
	FindByName(ctx context.Context, name string) (*vtypes.Model, error)
	List(ctx context.Context, cursor string, limit int) ([]vtypes.Model, string, bool, error)
}
type modelRepo struct{ q Executor }

func (r *modelRepo) Create(ctx context.Context, name, desc string, tt vtypes.TaskType) (*vtypes.Model, error) {
	m := &vtypes.Model{Name: name, Description: desc, TaskType: tt, CreatedAt: now(), UpdatedAt: now()}
	err := r.q.QueryRowContext(ctx,
		`INSERT INTO models (name, description, task_type, created_at, updated_at) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		name, desc, string(tt), m.CreatedAt, m.UpdatedAt).Scan(&m.ID)
	if err != nil {
		return nil, failExec(apperrors.KindDatabase, err, "create model")
	}
	return m, nil
}
func (r *modelRepo) FindByID(ctx context.Context, id string) (*vtypes.Model, error) {
	m := &vtypes.Model{}
	err := r.q.QueryRowContext(ctx,
		`SELECT id, name, description, task_type, created_at, updated_at FROM models WHERE id=$1`, id).
		Scan(&m.ID, &m.Name, &m.Description, &m.TaskType, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return nil, notFound("model")
		}
		return nil, failExec(apperrors.KindDatabase, err, "find model")
	}
	return m, nil
}
func (r *modelRepo) FindByName(ctx context.Context, name string) (*vtypes.Model, error) {
	m := &vtypes.Model{}
	err := r.q.QueryRowContext(ctx,
		`SELECT id, name, description, task_type, created_at, updated_at FROM models WHERE name=$1`, name).
		Scan(&m.ID, &m.Name, &m.Description, &m.TaskType, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return nil, notFound("model")
		}
		return nil, failExec(apperrors.KindDatabase, err, "find model")
	}
	return m, nil
}
func (r *modelRepo) List(ctx context.Context, cursor string, limit int) ([]vtypes.Model, string, bool, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var qb strings.Builder
	var args []interface{}
	if cursor != "" {
		qb.WriteString(`SELECT m.id, m.name, m.description, m.task_type, m.created_at, m.updated_at FROM models m
		  JOIN models c ON c.id=$2 WHERE (m.created_at, m.id) < (c.created_at, c.id)
		  ORDER BY m.created_at DESC, m.id DESC LIMIT $1`)
		args = []interface{}{limit + 1, cursor}
	} else {
		qb.WriteString(`SELECT id, name, description, task_type, created_at, updated_at FROM models ORDER BY created_at DESC, id DESC LIMIT $1`)
		args = []interface{}{limit + 1}
	}
	rows, err := r.q.QueryContext(ctx, qb.String(), args...)
	if err != nil {
		return nil, "", false, failExec(apperrors.KindDatabase, err, "list models")
	}
	defer rows.Close()
	var out []vtypes.Model
	for rows.Next() {
		m := vtypes.Model{}
		if err := rows.Scan(&m.ID, &m.Name, &m.Description, &m.TaskType, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, "", false, failExec(apperrors.KindDatabase, err, "scan model")
		}
		out = append(out, m)
	}
	hasMore := len(out) > limit
	next := ""
	if hasMore {
		out = out[:limit]
		next = out[len(out)-1].ID
	}
	return out, next, hasMore, rows.Err()
}

type ModelVersionRepository interface {
	Create(ctx context.Context, mv *vtypes.ModelVersion) (*vtypes.ModelVersion, error)
	FindByID(ctx context.Context, id string) (*vtypes.ModelVersion, error)
	ListByModel(ctx context.Context, modelID string) ([]vtypes.ModelVersion, error)
	SetStatus(ctx context.Context, id string, status vtypes.ModelVersionStatus) error
}
type modelVersionRepo struct{ q Executor }

func (r *modelVersionRepo) Create(ctx context.Context, mv *vtypes.ModelVersion) (*vtypes.ModelVersion, error) {
	if mv.CreatedAt.IsZero() {
		mv.CreatedAt = now()
	}
	if mv.Metadata == nil {
		mv.Metadata = vtypes.JSONB("{}")
	}
	meta, _ := json.Marshal(mv.Metadata)
	err := r.q.QueryRowContext(ctx,
		`INSERT INTO model_versions (model_id, version, artifact_uri, runtime, status, metadata, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		mv.ModelID, mv.Version, mv.ArtifactURI, string(mv.Runtime), string(mv.Status), meta, mv.CreatedAt).Scan(&mv.ID)
	if err != nil {
		return nil, failExec(apperrors.KindDatabase, err, "create model version")
	}
	return mv, nil
}
func (r *modelVersionRepo) FindByID(ctx context.Context, id string) (*vtypes.ModelVersion, error) {
	mv := &vtypes.ModelVersion{}
	var rt, st string
	var meta []byte
	err := r.q.QueryRowContext(ctx,
		`SELECT id, model_id, version, artifact_uri, runtime, status, metadata, created_at FROM model_versions WHERE id=$1`, id).
		Scan(&mv.ID, &mv.ModelID, &mv.Version, &mv.ArtifactURI, &rt, &st, &meta, &mv.CreatedAt)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return nil, notFound("model version")
		}
		return nil, failExec(apperrors.KindDatabase, err, "find model version")
	}
	mv.Runtime = vtypes.Runtime(rt)
	mv.Status = vtypes.ModelVersionStatus(st)
	mv.Metadata = vtypes.JSONB(meta)
	return mv, nil
}
func (r *modelVersionRepo) ListByModel(ctx context.Context, modelID string) ([]vtypes.ModelVersion, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, model_id, version, artifact_uri, runtime, status, metadata, created_at FROM model_versions WHERE model_id=$1 ORDER BY created_at DESC`, modelID)
	if err != nil {
		return nil, failExec(apperrors.KindDatabase, err, "list model versions")
	}
	defer rows.Close()
	var out []vtypes.ModelVersion
	for rows.Next() {
		mv := vtypes.ModelVersion{}
		var rt, st string
		var meta []byte
		if err := rows.Scan(&mv.ID, &mv.ModelID, &mv.Version, &mv.ArtifactURI, &rt, &st, &meta, &mv.CreatedAt); err != nil {
			return nil, failExec(apperrors.KindDatabase, err, "scan model version")
		}
		mv.Runtime = vtypes.Runtime(rt)
		mv.Status = vtypes.ModelVersionStatus(st)
		mv.Metadata = vtypes.JSONB(meta)
		out = append(out, mv)
	}
	return out, rows.Err()
}
func (r *modelVersionRepo) SetStatus(ctx context.Context, id string, status vtypes.ModelVersionStatus) error {
	res, err := r.q.ExecContext(ctx, `UPDATE model_versions SET status=$2 WHERE id=$1`, id, string(status))
	if err != nil {
		return failExec(apperrors.KindDatabase, err, "set status")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return notFound("model version")
	}
	return nil
}

// ── AssetRepository ─────────────────────────────────────
type AssetRepository interface {
	Create(ctx context.Context, a *vtypes.Asset) (*vtypes.Asset, error)
	FindByID(ctx context.Context, id string) (*vtypes.Asset, error)
	FindByStorageKey(ctx context.Context, key string) (*vtypes.Asset, error)
	ListByProject(ctx context.Context, projectID, cursor string, limit int) ([]vtypes.Asset, string, bool, error)
	Delete(ctx context.Context, id string) error
}
type assetRepo struct{ q Executor }

func (r *assetRepo) Create(ctx context.Context, a *vtypes.Asset) (*vtypes.Asset, error) {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now()
	}
	if a.Metadata == nil {
		a.Metadata = vtypes.JSONB("{}")
	}
	meta, _ := json.Marshal(a.Metadata)
	err := r.q.QueryRowContext(ctx,
		`INSERT INTO assets (project_id, owner_id, filename, content_type, size_bytes, storage_key, checksum, metadata, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		a.ProjectID, a.OwnerID, a.Filename, a.ContentType, a.SizeBytes, a.StorageKey, a.Checksum, meta, a.CreatedAt).Scan(&a.ID)
	if err != nil {
		return nil, failExec(apperrors.KindDatabase, err, "create asset")
	}
	return a, nil
}
func (r *assetRepo) FindByID(ctx context.Context, id string) (*vtypes.Asset, error) {
	a := &vtypes.Asset{}
	var meta []byte
	err := r.q.QueryRowContext(ctx,
		`SELECT id, project_id, owner_id, filename, content_type, size_bytes, storage_key, checksum, metadata, created_at FROM assets WHERE id=$1`, id).
		Scan(&a.ID, &a.ProjectID, &a.OwnerID, &a.Filename, &a.ContentType, &a.SizeBytes, &a.StorageKey, &a.Checksum, &meta, &a.CreatedAt)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return nil, notFound("asset")
		}
		return nil, failExec(apperrors.KindDatabase, err, "find asset")
	}
	a.Metadata = vtypes.JSONB(meta)
	return a, nil
}
func (r *assetRepo) FindByStorageKey(ctx context.Context, key string) (*vtypes.Asset, error) {
	a := &vtypes.Asset{}
	var meta []byte
	err := r.q.QueryRowContext(ctx,
		`SELECT id, project_id, owner_id, filename, content_type, size_bytes, storage_key, checksum, metadata, created_at FROM assets WHERE storage_key=$1`, key).
		Scan(&a.ID, &a.ProjectID, &a.OwnerID, &a.Filename, &a.ContentType, &a.SizeBytes, &a.StorageKey, &a.Checksum, &meta, &a.CreatedAt)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return nil, notFound("asset")
		}
		return nil, failExec(apperrors.KindDatabase, err, "find asset")
	}
	a.Metadata = vtypes.JSONB(meta)
	return a, nil
}
func (r *assetRepo) ListByProject(ctx context.Context, projectID, cursor string, limit int) ([]vtypes.Asset, string, bool, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var qb strings.Builder
	var args []interface{}
	qb.WriteString(`SELECT id, project_id, owner_id, filename, content_type, size_bytes, storage_key, checksum, metadata, created_at FROM assets WHERE project_id=$1`)
	args = append(args, projectID)
	argn := 2
	if cursor != "" {
		fmt.Fprintf(&qb, " AND (created_at, id) < (SELECT created_at, id FROM assets WHERE id=$%d)", argn)
		args = append(args, cursor)
		argn++
	}
	fmt.Fprintf(&qb, " ORDER BY created_at DESC, id DESC LIMIT $%d", argn)
	args = append(args, limit+1)
	rows, err := r.q.QueryContext(ctx, qb.String(), args...)
	if err != nil {
		return nil, "", false, failExec(apperrors.KindDatabase, err, "list assets")
	}
	defer rows.Close()
	var out []vtypes.Asset
	for rows.Next() {
		a := vtypes.Asset{}
		var meta []byte
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.OwnerID, &a.Filename, &a.ContentType, &a.SizeBytes, &a.StorageKey, &a.Checksum, &meta, &a.CreatedAt); err != nil {
			return nil, "", false, failExec(apperrors.KindDatabase, err, "scan asset")
		}
		a.Metadata = vtypes.JSONB(meta)
		out = append(out, a)
	}
	hasMore := len(out) > limit
	next := ""
	if hasMore {
		out = out[:limit]
		next = out[len(out)-1].ID
	}
	return out, next, hasMore, rows.Err()
}
func (r *assetRepo) Delete(ctx context.Context, id string) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM assets WHERE id=$1`, id)
	if err != nil {
		return failExec(apperrors.KindDatabase, err, "delete asset")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return notFound("asset")
	}
	return nil
}

// New is an alias for NewRepos for backward compatibility with wiring code.
func New(db *sql.DB) *Repos { return NewRepos(db) }

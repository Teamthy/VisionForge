package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/visionforge/visionforge/apps/api/internal/audit"
	apperrors "github.com/visionforge/visionforge/apps/api/internal/errors"
	"github.com/visionforge/visionforge/apps/api/internal/mlclient"
	"github.com/visionforge/visionforge/apps/api/internal/queue"
	"github.com/visionforge/visionforge/apps/api/internal/repository"
	"github.com/visionforge/visionforge/apps/api/internal/storage"
	"github.com/visionforge/visionforge/packages/config"
	vtypes "github.com/visionforge/visionforge/packages/types"
)

// ── Auth ────────────────────────────────────────────────
type AuthService struct {
	users         repository.UserRepository
	refreshTokens repository.RefreshTokenRepository
	audit         audit.Recorder
	hasher        hasher
	jwt           jwtM
	refreshTTL    time.Duration
}

type hasher interface {
	Hash(string) (string, error)
	Verify(hash, pw string) error
}
type jwtM interface {
	MintAccessToken(u *vtypes.User) (string, time.Time, error)
}

func NewAuthService(u repository.UserRepository, rt repository.RefreshTokenRepository, a audit.Recorder, h hasher, j jwtM, rttl time.Duration) *AuthService {
	return &AuthService{users: u, refreshTokens: rt, audit: a, hasher: h, jwt: j, refreshTTL: rttl}
}

func (s *AuthService) Register(ctx context.Context, email, pw, name, ip, ua string) (*vtypes.User, string, string, time.Time, error) {
	if email == "" || pw == "" || name == "" {
		return nil, "", "", time.Time{}, apperrors.New(apperrors.KindValidation, "email, password, name required")
	}
	hash, err := s.hasher.Hash(pw)
	if err != nil { return nil, "", "", time.Time{}, apperrors.Wrap(apperrors.KindInternal, err, "hash") }
	u, err := s.users.Create(ctx, email, hash, name, vtypes.RoleUser)
	if err != nil { return nil, "", "", time.Time{}, err }
	at, exp, err := s.jwt.MintAccessToken(u)
	if err != nil { return nil, "", "", time.Time{}, apperrors.Wrap(apperrors.KindInternal, err, "mint") }
	rt, rh, err := genRefresh()
	if err != nil { return nil, "", "", time.Time{}, err }
	if _, err := s.refreshTokens.Create(ctx, u.ID, rh, time.Now().Add(s.refreshTTL)); err != nil {
		return nil, "", "", time.Time{}, err
	}
	_ = s.users.UpdateLastLogin(ctx, u.ID, time.Now().UTC())
	audit.Record(ctx, s.audit, audit.Actor{UserID: u.ID, IPAddress: ip, UserAgent: ua}, "user.register", "user", u.ID, nil)
	return u, at, rt, exp, nil
}

func (s *AuthService) Login(ctx context.Context, email, pw, ip, ua string) (*vtypes.User, string, string, time.Time, error) {
	u, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		if apperrors.Is(err, apperrors.KindNotFound) { return nil, "", "", time.Time{}, apperrors.New(apperrors.KindUnauthorized, "invalid credentials") }
		return nil, "", "", time.Time{}, err
	}
	if err := s.hasher.Verify(u.PasswordHash, pw); err != nil {
		return nil, "", "", time.Time{}, apperrors.New(apperrors.KindUnauthorized, "invalid credentials")
	}
	at, exp, err := s.jwt.MintAccessToken(u)
	if err != nil { return nil, "", "", time.Time{}, apperrors.Wrap(apperrors.KindInternal, err, "mint") }
	rt, rh, err := genRefresh()
	if err != nil { return nil, "", "", time.Time{}, err }
	if _, err := s.refreshTokens.Create(ctx, u.ID, rh, time.Now().Add(s.refreshTTL)); err != nil { return nil, "", "", time.Time{}, err }
	_ = s.users.UpdateLastLogin(ctx, u.ID, time.Now().UTC())
	audit.Record(ctx, s.audit, audit.Actor{UserID: u.ID, IPAddress: ip, UserAgent: ua}, "user.login", "user", u.ID, nil)
	return u, at, rt, exp, nil
}

func (s *AuthService) Refresh(ctx context.Context, token string) (string, string, time.Time, error) {
	if token == "" { return "", "", time.Time{}, apperrors.New(apperrors.KindUnauthorized, "refresh token required") }
	h := hashRefresh(token)
	rt, err := s.refreshTokens.FindByHash(ctx, h)
	if err != nil { return "", "", time.Time{}, err }
	if rt.RevokedAt != nil { return "", "", time.Time{}, apperrors.New(apperrors.KindUnauthorized, "refresh token revoked") }
	if time.Now().After(rt.ExpiresAt) { return "", "", time.Time{}, apperrors.New(apperrors.KindUnauthorized, "refresh expired") }
	u, err := s.users.FindByID(ctx, rt.UserID)
	if err != nil { return "", "", time.Time{}, err }
	if err := s.refreshTokens.Revoke(ctx, rt.ID); err != nil { return "", "", time.Time{}, err }
	at, exp, err := s.jwt.MintAccessToken(u)
	if err != nil { return "", "", time.Time{}, err }
	newTok, newHash, err := genRefresh()
	if err != nil { return "", "", time.Time{}, err }
	if _, err := s.refreshTokens.Create(ctx, u.ID, newHash, time.Now().Add(s.refreshTTL)); err != nil {
		return "", "", time.Time{}, err
	}
	return at, newTok, exp, nil
}

func (s *AuthService) Logout(ctx context.Context, token, userID, ip, ua string) error {
	if token == "" { return apperrors.New(apperrors.KindValidation, "refresh_token required") }
	rt, err := s.refreshTokens.FindByHash(ctx, hashRefresh(token))
	if err != nil {
		if apperrors.Is(err, apperrors.KindUnauthorized) { return nil }
		return err
	}
	if rt.UserID != userID { return apperrors.New(apperrors.KindForbidden, "token does not belong to user") }
	_ = s.refreshTokens.Revoke(ctx, rt.ID)
	audit.Record(ctx, s.audit, audit.Actor{UserID: userID, IPAddress: ip, UserAgent: ua}, "user.logout", "user", userID, nil)
	return nil
}

func (s *AuthService) Me(ctx context.Context, userID string) (*vtypes.User, error) {
	if userID == "" { return nil, apperrors.ErrUnauthorized }
	u, err := s.users.FindByID(ctx, userID)
	if err != nil { return nil, err }
	u.PasswordHash = ""; return u, nil
}

func genRefresh() (string, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil { return "", "", err }
	tok := hex.EncodeToString(b)
	h := sha256.Sum256([]byte(tok))
	return tok, hex.EncodeToString(h[:]), nil
}
func hashRefresh(tok string) string {
	h := sha256.Sum256([]byte(tok)); return hex.EncodeToString(h[:])
}

// ── Projects ────────────────────────────────────────────
type ProjectService struct {
	projects repository.ProjectRepository
	audit    audit.Recorder
}

func NewProjectService(r repository.ProjectRepository, a audit.Recorder) *ProjectService { return &ProjectService{projects: r, audit: a} }

func (s *ProjectService) Create(ctx context.Context, actor audit.Actor, name, desc string) (*vtypes.Project, error) {
	if name == "" { return nil, apperrors.New(apperrors.KindValidation, "name required") }
	p, err := s.projects.Create(ctx, actor.UserID, name, desc)
	if err != nil { return nil, err }
	audit.Record(ctx, s.audit, actor, "project.create", "project", p.ID, nil)
	return p, nil
}
func (s *ProjectService) Get(ctx context.Context, uid, role, id string) (*vtypes.Project, error) {
	p, err := s.projects.FindByID(ctx, id)
	if err != nil { return nil, err }
	if role != string(vtypes.RoleAdmin) && p.OwnerID != uid { return nil, apperrors.ErrForbidden }
	return p, nil
}
func (s *ProjectService) List(ctx context.Context, uid, cursor string, limit int) ([]vtypes.Project, string, bool, error) {
	return s.projects.ListByOwner(ctx, uid, cursor, limit)
}
func (s *ProjectService) Update(ctx context.Context, actor audit.Actor, id string, name, desc *string) (*vtypes.Project, error) {
	p, err := s.projects.FindByID(ctx, id)
	if err != nil { return nil, err }
	if p.OwnerID != actor.UserID { return nil, apperrors.ErrForbidden }
	newName, newDesc := p.Name, p.Description
	if name != nil {
		if *name == "" { return nil, apperrors.New(apperrors.KindValidation, "name cannot be empty") }
		newName = *name
	}
	if desc != nil { newDesc = *desc }
	up, err := s.projects.Update(ctx, id, newName, newDesc)
	if err != nil { return nil, err }
	audit.Record(ctx, s.audit, actor, "project.update", "project", id, nil)
	return up, nil
}
func (s *ProjectService) Delete(ctx context.Context, actor audit.Actor, id string) error {
	p, err := s.projects.FindByID(ctx, id)
	if err != nil { return err }
	if p.OwnerID != actor.UserID { return apperrors.ErrForbidden }
	if err := s.projects.Delete(ctx, id); err != nil { return err }
	audit.Record(ctx, s.audit, actor, "project.delete", "project", id, nil)
	return nil
}
func (s *ProjectService) Authorize(ctx context.Context, uid, role, pid string) error {
	p, err := s.projects.FindByID(ctx, pid)
	if err != nil { return err }
	if role != string(vtypes.RoleAdmin) && p.OwnerID != uid { return apperrors.ErrForbidden }
	return nil
}

// ── Assets ──────────────────────────────────────────────
var allowedImageMIME = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true, "image/gif": true}
var allowedImageExt = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true}
var safeFN = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

type AssetService struct {
	assets   repository.AssetRepository
	projects repository.ProjectRepository
	store    storage.ObjectStorage
	limits   config.LimitsConfig
	audit    audit.Recorder
}

func NewAssetService(a repository.AssetRepository, p repository.ProjectRepository, s storage.ObjectStorage, lim config.LimitsConfig, ar audit.Recorder) *AssetService {
	return &AssetService{assets: a, projects: p, store: s, limits: lim, audit: ar}
}

func (s *AssetService) PresignUpload(ctx context.Context, actor audit.Actor, pid, filename, ctype string, size int64) (*vtypes.Asset, string, error) {
	proj, err := s.projects.FindByID(ctx, pid)
	if err != nil { return nil, "", err }
	if proj.OwnerID != actor.UserID { return nil, "", apperrors.ErrForbidden }
	if err := validateFilename(filename); err != nil { return nil, "", err }
	ext := strings.ToLower(filepath.Ext(filename))
	isImage := allowedImageMIME[ctype] && allowedImageExt[ext]
	if !isImage { return nil, "", apperrors.New(apperrors.KindValidation, "unsupported file type") }
	if size <= 0 || size > s.limits.MaxImageUploadBytes {
		return nil, "", apperrors.New(apperrors.KindValidation, "file size out of allowed range")
	}
	key := storageKey(actor.UserID, pid, ext)
	url, err := s.store.GetPresignedPUT(ctx, key, ctype, 15*time.Minute)
	if err != nil { return nil, "", apperrors.Wrap(apperrors.KindStorage, err, "presign put") }
	a, err := s.assets.Create(ctx, &vtypes.Asset{ProjectID: pid, OwnerID: actor.UserID, Filename: sanitize(filename), ContentType: ctype, SizeBytes: size, StorageKey: key})
	if err != nil { return nil, "", err }
	audit.Record(ctx, s.audit, actor, "asset.upload_initiated", "asset", a.ID, nil)
	return a, url, nil
}

func (s *AssetService) ConfirmUpload(ctx context.Context, actor audit.Actor, id string) (*vtypes.Asset, error) {
	a, err := s.assets.FindByID(ctx, id)
	if err != nil { return nil, err }
	if a.OwnerID != actor.UserID { return nil, apperrors.ErrForbidden }
	ok, err := s.store.Exists(ctx, a.StorageKey)
	if err != nil { return nil, apperrors.Wrap(apperrors.KindStorage, err, "check upload") }
	if !ok { return nil, apperrors.New(apperrors.KindValidation, "upload not found") }
	audit.Record(ctx, s.audit, actor, "asset.upload_confirmed", "asset", a.ID, nil)
	return a, nil
}

func (s *AssetService) UploadDirect(ctx context.Context, actor audit.Actor, pid string, fh *multipart.FileHeader) (*vtypes.Asset, error) {
	proj, err := s.projects.FindByID(ctx, pid)
	if err != nil { return nil, err }
	if proj.OwnerID != actor.UserID { return nil, apperrors.ErrForbidden }
	if err := validateFilename(fh.Filename); err != nil { return nil, err }
	ext := strings.ToLower(filepath.Ext(fh.Filename))
	if !allowedImageExt[ext] { return nil, apperrors.New(apperrors.KindValidation, "unsupported extension") }
	if fh.Size <= 0 || fh.Size > s.limits.MaxImageUploadBytes { return nil, apperrors.New(apperrors.KindValidation, "size out of range") }
	f, err := fh.Open()
	if err != nil { return nil, err }
	defer f.Close()
	buf := make([]byte, 512); n, _ := io.ReadFull(f, buf)
	detected := http.DetectContentType(buf[:n])
	if _, err := f.Seek(0, io.SeekStart); err != nil { return nil, err }
	if !strings.HasPrefix(detected, "image/") { return nil, apperrors.New(apperrors.KindValidation, "content not an image") }
	key := storageKey(actor.UserID, pid, ext)
	if err := s.store.Put(ctx, key, detected, f, fh.Size); err != nil { return nil, apperrors.Wrap(apperrors.KindStorage, err, "put") }
	if _, err := f.Seek(0, io.SeekStart); err != nil { return nil, err }
	h := sha256.New(); io.Copy(h, f)
	a, err := s.assets.Create(ctx, &vtypes.Asset{ProjectID: pid, OwnerID: actor.UserID, Filename: sanitize(fh.Filename), ContentType: detected, SizeBytes: fh.Size, StorageKey: key, Checksum: hex.EncodeToString(h.Sum(nil))})
	if err != nil { return nil, err }
	audit.Record(ctx, s.audit, actor, "asset.direct_upload", "asset", a.ID, nil)
	return a, nil
}
func (s *AssetService) Get(ctx context.Context, uid, role, id string) (*vtypes.Asset, error) {
	a, err := s.assets.FindByID(ctx, id)
	if err != nil { return nil, err }
	if role != string(vtypes.RoleAdmin) && a.OwnerID != uid { return nil, apperrors.ErrForbidden }
	return a, nil
}
func (s *AssetService) ListByProject(ctx context.Context, uid, role, pid, cursor string, limit int) ([]vtypes.Asset, string, bool, error) {
	p, err := s.projects.FindByID(ctx, pid)
	if err != nil { return nil, "", false, err }
	if role != string(vtypes.RoleAdmin) && p.OwnerID != uid { return nil, "", false, apperrors.ErrForbidden }
	return s.assets.ListByProject(ctx, pid, cursor, limit)
}
func (s *AssetService) Delete(ctx context.Context, actor audit.Actor, id string) error {
	a, err := s.assets.FindByID(ctx, id)
	if err != nil { return err }
	if a.OwnerID != actor.UserID { return apperrors.ErrForbidden }
	if err := s.assets.Delete(ctx, id); err != nil { return err }
	_ = s.store.Delete(ctx, a.StorageKey)
	audit.Record(ctx, s.audit, actor, "asset.delete", "asset", id, nil)
	return nil
}
func (s *AssetService) DownloadURL(ctx context.Context, uid, role, id string, exp time.Duration) (string, *vtypes.Asset, error) {
	a, err := s.Get(ctx, uid, role, id)
	if err != nil { return "", nil, err }
	u, err := s.store.GetPresignedURL(ctx, a.StorageKey, exp)
	if err != nil { return "", nil, apperrors.Wrap(apperrors.KindStorage, err, "presign get") }
	return u, a, nil
}

func validateFilename(name string) error {
	if name == "" || len(name) > 255 { return apperrors.New(apperrors.KindValidation, "invalid filename") }
	if strings.ContainsAny(name, "/\\\x00") { return apperrors.New(apperrors.KindValidation, "invalid filename chars") }
	return nil
}
func sanitize(name string) string {
	name = filepath.Base(name); name = strings.ReplaceAll(name, "..", "_")
	name = safeFN.ReplaceAllString(name, "_")
	if len(name) > 200 { name = name[:200] }
	return name
}
func storageKey(uid, pid, ext string) string {
	ts := time.Now().UTC().Format("20060102T150405")
	return fmt.Sprintf("projects/%s/users/%s/%s-%x%s", pid, uid, ts, time.Now().UnixNano()&0xffff, ext)
}

// ── Models ──────────────────────────────────────────────
type ModelService struct {
	models   repository.ModelRepository
	versions repository.ModelVersionRepository
	audit    audit.Recorder
}

func NewModelService(m repository.ModelRepository, mv repository.ModelVersionRepository, a audit.Recorder) *ModelService {
	return &ModelService{models: m, versions: mv, audit: a}
}
func (s *ModelService) CreateModel(ctx context.Context, actor audit.Actor, name, desc string, tt vtypes.TaskType) (*vtypes.Model, error) {
	m, err := s.models.Create(ctx, name, desc, tt)
	if err != nil { return nil, err }
	audit.Record(ctx, s.audit, actor, "model.create", "model", m.ID, nil)
	return m, nil
}
func (s *ModelService) ListModels(ctx context.Context, cursor string, limit int) ([]vtypes.Model, string, bool, error) {
	return s.models.List(ctx, cursor, limit)
}
func (s *ModelService) GetModel(ctx context.Context, id string) (*vtypes.Model, error) { return s.models.FindByID(ctx, id) }
func (s *ModelService) CreateVersion(ctx context.Context, actor audit.Actor, mid, ver, art string, rt vtypes.Runtime, st vtypes.ModelVersionStatus, meta vtypes.JSONB) (*vtypes.ModelVersion, error) {
	if _, err := s.models.FindByID(ctx, mid); err != nil { return nil, err }
	mv, err := s.versions.Create(ctx, &vtypes.ModelVersion{ModelID: mid, Version: ver, ArtifactURI: art, Runtime: rt, Status: st, Metadata: meta})
	if err != nil { return nil, err }
	audit.Record(ctx, s.audit, actor, "model_version.create", "model_version", mv.ID, nil)
	return mv, nil
}
func (s *ModelService) ListVersions(ctx context.Context, mid string) ([]vtypes.ModelVersion, error) {
	if _, err := s.models.FindByID(ctx, mid); err != nil { return nil, err }
	return s.versions.ListByModel(ctx, mid)
}
func (s *ModelService) SetVersionStatus(ctx context.Context, actor audit.Actor, id string, st vtypes.ModelVersionStatus) error {
	if err := s.versions.SetStatus(ctx, id, st); err != nil { return err }
	audit.Record(ctx, s.audit, actor, "model_version.status_change", "model_version", id, nil)
	return nil
}
func (s *ModelService) ValidateVersion(ctx context.Context, id string) (*vtypes.ModelVersion, error) {
	mv, err := s.versions.FindByID(ctx, id)
	if err != nil { return nil, err }
	if mv.Status != vtypes.ModelVersionActive && mv.Status != vtypes.ModelVersionStaging {
		return nil, apperrors.New(apperrors.KindValidation, "model version not active")
	}
	return mv, nil
}

// ── Inference ───────────────────────────────────────────
type InferenceService struct {
	db       *sql.DB
	jobs     repository.JobRepository
	results  repository.ResultRepository
	assets   repository.AssetRepository
	projects repository.ProjectRepository
	versions repository.ModelVersionRepository
	queue    queue.Queue
	store    storage.ObjectStorage
	ml       *mlclient.Client
	audit    audit.Recorder

	// OnJobCreated is invoked after a job row is created (sync or async).
	// Wired from main.go to the Prometheus JobsCreated counter.
	OnJobCreated func()
}

func NewInferenceService(db *sql.DB, j repository.JobRepository, r repository.ResultRepository, a repository.AssetRepository, p repository.ProjectRepository, v repository.ModelVersionRepository, q queue.Queue, s storage.ObjectStorage, ml *mlclient.Client, ar audit.Recorder) *InferenceService {
	return &InferenceService{db: db, jobs: j, results: r, assets: a, projects: p, versions: v, queue: q, store: s, ml: ml, audit: ar}
}

func (s *InferenceService) CreateSync(ctx context.Context, actor audit.Actor, pid, aid, mvid string) (*vtypes.InferenceJob, *vtypes.JobResult, error) {
	if err := s.authorize(ctx, actor.UserID, pid, aid, mvid); err != nil { return nil, nil, err }
	now := time.Now().UTC()
	j, err := s.jobs.Create(ctx, &vtypes.InferenceJob{ProjectID: pid, AssetID: aid, ModelVersionID: mvid, Status: vtypes.JobStatusRunning, Priority: vtypes.PriorityNormal, Attempts: 1, MaxAttempts: 1, StartedAt: &now})
	if err != nil { return nil, nil, err }
	a, err := s.assets.FindByID(ctx, aid)
	if err != nil { _ = s.jobs.MarkFailed(ctx, j.ID, vtypes.ErrCodeStorage, err.Error(), nil); return nil, nil, err }
	r, _, err := s.store.Get(ctx, a.StorageKey)
	if err != nil { _ = s.jobs.MarkFailed(ctx, j.ID, vtypes.ErrCodeStorage, err.Error(), nil); return nil, nil, apperrors.Wrap(apperrors.KindStorage, err, "fetch asset") }
	defer r.Close()
	buf := &bytes.Buffer{}; io.Copy(buf, r)
	start := time.Now()
	ref := mlclient.ModelRef{ModelVersionID: mvid}
	if mv, err := s.versions.FindByID(ctx, mvid); err == nil {
		ref.ArtifactURI = mv.ArtifactURI
	}
	resp, err := s.ml.PredictFromBytes(ctx, ref, buf.Bytes(), a.ContentType)
	if err != nil { _ = s.jobs.MarkFailed(ctx, j.ID, vtypes.ErrCodeMLService, err.Error(), nil); return nil, nil, apperrors.Wrap(apperrors.KindInference, err, "ml") }
	inferMs := resp.InferenceTimeMs; if inferMs == 0 { inferMs = time.Since(start).Milliseconds() }
	total := time.Since(start).Milliseconds()
	jr := &vtypes.JobResult{JobID: j.ID, ModelVersionID: mvid, Status: vtypes.JobStatusSuccess, ProcessingTimeMs: total, InferenceTimeMs: inferMs, Detections: resp.Detections, Predictions: resp.Predictions, CompletedAt: time.Now().UTC()}
	raw, _ := json.Marshal(jr)
	if _, err := s.results.Create(ctx, &vtypes.InferenceResult{JobID: j.ID, ModelVersionID: mvid, ResultJSON: raw, ProcessingTimeMs: total, InferenceTimeMs: inferMs}); err != nil {
		_ = s.jobs.MarkFailed(ctx, j.ID, vtypes.ErrCodeDatabase, err.Error(), nil); return nil, nil, err
	}
	comp := time.Now().UTC()
	if err := s.jobs.MarkSucceeded(ctx, j.ID, comp); err != nil { slog.Warn("mark success failed", "error", err) }
	audit.Record(ctx, s.audit, actor, "inference.sync", "job", j.ID, nil)
	if s.OnJobCreated != nil { s.OnJobCreated() }
	return j, jr, nil
}

func (s *InferenceService) CreateAsync(ctx context.Context, actor audit.Actor, pid, aid, mvid string, prio vtypes.Priority, idem *string) (*vtypes.InferenceJob, error) {
	if err := s.authorize(ctx, actor.UserID, pid, aid, mvid); err != nil { return nil, err }
	if idem != nil && *idem != "" {
		if ex, err := s.jobs.FindByIdempotencyKey(ctx, pid, *idem); err == nil { return ex, nil }
	}
	j, err := s.jobs.Create(ctx, &vtypes.InferenceJob{ProjectID: pid, AssetID: aid, ModelVersionID: mvid, Status: vtypes.JobStatusCreated, Priority: prio, MaxAttempts: 5, IdempotencyKey: idem})
	if err != nil { return nil, err }
	queuedAt := time.Now().UTC()
	if err := s.jobs.MarkQueued(ctx, j.ID, 1, queuedAt); err != nil { return nil, err }
	payload := vtypes.JobPayload{JobID: j.ID, ProjectID: pid, AssetID: aid, ModelVersionID: mvid, Priority: prio, Attempt: 1}
	if idem != nil { payload.IdempotencyKey = *idem }
	if err := s.queue.Enqueue(ctx, payload, 0); err != nil {
		_ = s.jobs.MarkFailed(ctx, j.ID, vtypes.ErrCodeQueue, err.Error(), nil)
		return nil, apperrors.Wrap(apperrors.KindQueue, err, "enqueue")
	}
	audit.Record(ctx, s.audit, actor, "inference.async_create", "job", j.ID, nil)
	if s.OnJobCreated != nil { s.OnJobCreated() }
	return j, nil
}

func (s *InferenceService) Get(ctx context.Context, uid, role, jid string) (*vtypes.InferenceJob, error) {
	j, err := s.jobs.FindByID(ctx, jid)
	if err != nil { return nil, err }
	if err := s.authzProject(ctx, uid, role, j.ProjectID); err != nil { return nil, err }
	return j, nil
}
func (s *InferenceService) GetResult(ctx context.Context, uid, role, jid string) (*vtypes.JobResult, error) {
	j, err := s.jobs.FindByID(ctx, jid)
	if err != nil { return nil, err }
	if err := s.authzProject(ctx, uid, role, j.ProjectID); err != nil { return nil, err }
	r, err := s.results.FindByJobID(ctx, jid)
	if err != nil { return nil, err }
	var jr vtypes.JobResult
	if err := json.Unmarshal(r.ResultJSON, &jr); err != nil { return nil, apperrors.Wrap(apperrors.KindInternal, err, "decode result") }
	return &jr, nil
}
func (s *InferenceService) List(ctx context.Context, uid, role, pid string, statuses []vtypes.JobStatus, cursor string, limit int) ([]vtypes.InferenceJob, string, bool, error) {
	if err := s.authzProject(ctx, uid, role, pid); err != nil { return nil, "", false, err }
	return s.jobs.List(ctx, pid, statuses, cursor, limit)
}
func (s *InferenceService) Retry(ctx context.Context, actor audit.Actor, jid string) (*vtypes.InferenceJob, error) {
	j, err := s.jobs.FindByID(ctx, jid)
	if err != nil { return nil, err }
	if err := s.authzProject(ctx, actor.UserID, "", j.ProjectID); err != nil { return nil, err }
	if j.Status != vtypes.JobStatusFailed && j.Status != vtypes.JobStatusDead { return nil, apperrors.New(apperrors.KindConflict, "only FAILED/DEAD can be retried") }
	if err := s.jobs.MarkRetrying(ctx, j.ID); err != nil { return nil, err }
	attempt := j.Attempts + 1
	if err := s.jobs.MarkQueued(ctx, j.ID, attempt, time.Now().UTC()); err != nil { return nil, err }
	if err := s.queue.Enqueue(ctx, vtypes.JobPayload{JobID: j.ID, ProjectID: j.ProjectID, AssetID: j.AssetID, ModelVersionID: j.ModelVersionID, Priority: j.Priority, Attempt: attempt}, 0); err != nil {
		return nil, apperrors.Wrap(apperrors.KindQueue, err, "retry enqueue")
	}
	audit.Record(ctx, s.audit, actor, "inference.retry", "job", j.ID, nil)
	return s.jobs.FindByID(ctx, j.ID)
}

type DashboardStats struct {
	TotalProjects   int64 `json:"total_projects"`
	TotalAssets     int64 `json:"total_assets"`
	JobsProcessed   int64 `json:"jobs_processed"`
	JobsSucceeded   int64 `json:"jobs_succeeded"`
	JobsFailed      int64 `json:"jobs_failed"`
	QueueDepth      int64 `json:"queue_depth"`
	MLServiceOK     bool  `json:"ml_service_ok"`
	ObjectStorageOK bool  `json:"object_storage_ok"`
}

func (s *InferenceService) Stats(ctx context.Context, uid string) (*DashboardStats, error) {
	var projects, assets int64
	if s.db != nil {
		_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE owner_id=$1`, uid).Scan(&projects)
		_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM assets WHERE owner_id=$1`, uid).Scan(&assets)
	}
	sc, _ := s.jobs.CountByStatus(ctx)
	depth, _ := s.queue.Depth(ctx)
	mlOK := true
	if _, err := s.ml.Health(ctx); err != nil { mlOK = false }
	storeOK := true
	if err := s.store.Health(ctx); err != nil { storeOK = false }
	processed := sc[vtypes.JobStatusSuccess] + sc[vtypes.JobStatusFailed] + sc[vtypes.JobStatusDead]
	return &DashboardStats{TotalProjects: projects, TotalAssets: assets, JobsProcessed: processed, JobsSucceeded: sc[vtypes.JobStatusSuccess], JobsFailed: sc[vtypes.JobStatusFailed] + sc[vtypes.JobStatusDead], QueueDepth: depth, MLServiceOK: mlOK, ObjectStorageOK: storeOK}, nil
}

func (s *InferenceService) authorize(ctx context.Context, uid, pid, aid, mvid string) error {
	if err := s.authzProject(ctx, uid, "", pid); err != nil { return err }
	a, err := s.assets.FindByID(ctx, aid)
	if err != nil { return err }
	if a.ProjectID != pid { return apperrors.New(apperrors.KindValidation, "asset not in project") }
	mv, err := s.versions.FindByID(ctx, mvid)
	if err != nil { return err }
	if mv.Status != vtypes.ModelVersionActive && mv.Status != vtypes.ModelVersionStaging {
		return apperrors.New(apperrors.KindValidation, "model version not active")
	}
	return nil
}
func (s *InferenceService) authzProject(ctx context.Context, uid, role, pid string) error {
	p, err := s.projects.FindByID(ctx, pid)
	if err != nil { return err }
	if role != string(vtypes.RoleAdmin) && p.OwnerID != uid { return apperrors.ErrForbidden }
	return nil
}

package repository

import (
	"context"
	"database/sql"
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	apperrors "github.com/visionforge/visionforge/apps/api/internal/errors"
	vtypes "github.com/visionforge/visionforge/packages/types"
)

type JobRepository interface {
	Create(ctx context.Context, j *vtypes.InferenceJob) (*vtypes.InferenceJob, error)
	FindByID(ctx context.Context, id string) (*vtypes.InferenceJob, error)
	FindByIdempotencyKey(ctx context.Context, projectID, key string) (*vtypes.InferenceJob, error)
	List(ctx context.Context, projectID string, statuses []vtypes.JobStatus, cursor string, limit int) ([]vtypes.InferenceJob, string, bool, error)
	MarkQueued(ctx context.Context, id string, attempts int, queuedAt time.Time) error
	MarkRunning(ctx context.Context, id string, startedAt time.Time) (bool, error)
	MarkSucceeded(ctx context.Context, id string, completedAt time.Time) error
	MarkFailed(ctx context.Context, id string, code vtypes.ErrorCode, message string, completedAt *time.Time) error
	MarkRetrying(ctx context.Context, id string) error
	MarkDead(ctx context.Context, id string, code vtypes.ErrorCode, message string) error
	IncrementAttempts(ctx context.Context, id string) (int, error)
	PickNextQueued(ctx context.Context) (*vtypes.InferenceJob, error)
	CountByStatus(ctx context.Context) (map[vtypes.JobStatus]int64, error)
	CountQueueDepth(ctx context.Context) (int64, error)
}

type jobRepo struct{ q Executor }

func (r *jobRepo) Create(ctx context.Context, j *vtypes.InferenceJob) (*vtypes.InferenceJob, error) {
	if j.CreatedAt.IsZero() {
		j.CreatedAt = now()
	}
	if j.UpdatedAt.IsZero() {
		j.UpdatedAt = j.CreatedAt
	}
	if j.Status == "" {
		j.Status = vtypes.JobStatusCreated
	}
	if j.MaxAttempts <= 0 {
		j.MaxAttempts = 5
	}
	var idem interface{}
	if j.IdempotencyKey != nil && *j.IdempotencyKey != "" {
		idem = *j.IdempotencyKey
	}
	err := r.q.QueryRowContext(ctx,
		`INSERT INTO inference_jobs
		 (project_id, asset_id, model_version_id, status, priority, attempts, max_attempts, idempotency_key, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
		j.ProjectID, j.AssetID, j.ModelVersionID, string(j.Status), j.Priority, j.Attempts, j.MaxAttempts, idem, j.CreatedAt, j.UpdatedAt).Scan(&j.ID)
	if err != nil {
		if uniqueViolation(err) {
			return nil, apperrors.New(apperrors.KindConflict, "duplicate idempotency key")
		}
		return nil, failExec(apperrors.KindDatabase, err, "create job")
	}
	return j, nil
}

func (r *jobRepo) FindByID(ctx context.Context, id string) (*vtypes.InferenceJob, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT id, project_id, asset_id, model_version_id, status, priority, attempts, max_attempts,
		        idempotency_key, error_code, error_message, queued_at, started_at, completed_at, created_at, updated_at
		 FROM inference_jobs WHERE id=$1`, id)
	return scanJob(row)
}
func (r *jobRepo) FindByIdempotencyKey(ctx context.Context, pid, key string) (*vtypes.InferenceJob, error) {
	return scanJob(r.q.QueryRowContext(ctx,
		`SELECT id, project_id, asset_id, model_version_id, status, priority, attempts, max_attempts,
		        idempotency_key, error_code, error_message, queued_at, started_at, completed_at, created_at, updated_at
		 FROM inference_jobs WHERE project_id=$1 AND idempotency_key=$2`, pid, key))
}

func (r *jobRepo) List(ctx context.Context, projectID string, statuses []vtypes.JobStatus, cursor string, limit int) ([]vtypes.InferenceJob, string, bool, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var qb strings.Builder
	var args []interface{}
	qb.WriteString(`SELECT id, project_id, asset_id, model_version_id, status, priority, attempts, max_attempts,
		idempotency_key, error_code, error_message, queued_at, started_at, completed_at, created_at, updated_at
	 FROM inference_jobs WHERE project_id=$1`)
	args = append(args, projectID)
	argn := 2
	if len(statuses) > 0 {
		ss := make([]string, len(statuses))
		for i, s := range statuses {
			ss[i] = string(s)
		}
		fmt.Fprintf(&qb, " AND status = ANY($%d)", argn)
		args = append(args, pq.Array(ss))
		argn++
	}
	if cursor != "" {
		fmt.Fprintf(&qb, " AND (created_at, id) < (SELECT created_at, id FROM inference_jobs WHERE id = $%d)", argn)
		args = append(args, cursor)
		argn++
	}
	fmt.Fprintf(&qb, " ORDER BY created_at DESC, id DESC LIMIT $%d", argn)
	args = append(args, limit+1)
	rows, err := r.q.QueryContext(ctx, qb.String(), args...)
	if err != nil {
		return nil, "", false, failExec(apperrors.KindDatabase, err, "list jobs")
	}
	defer rows.Close()
	var out []vtypes.InferenceJob
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, "", false, err
		}
		out = append(out, *j)
	}
	hasMore := len(out) > limit
	next := ""
	if hasMore {
		out = out[:limit]
		next = out[len(out)-1].ID
	}
	return out, next, hasMore, rows.Err()
}

func (r *jobRepo) transition(ctx context.Context, id string, current, next vtypes.JobStatus, extra string, args ...interface{}) (bool, error) {
	args = append([]interface{}{string(next), string(current), id}, args...)
	q := `UPDATE inference_jobs SET status=$1, updated_at=now()`
	if extra != "" {
		q += ", " + extra
	}
	q += ` WHERE id=$3 AND status=$2`
	res, err := r.q.ExecContext(ctx, q, args...)
	if err != nil {
		return false, failExec(apperrors.KindDatabase, err, "transition to "+string(next))
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (r *jobRepo) MarkQueued(ctx context.Context, id string, attempts int, queuedAt time.Time) error {
	ok, err := r.transition(ctx, id, vtypes.JobStatusCreated, vtypes.JobStatusQueued, `queued_at=$4, attempts=$5`, queuedAt, attempts)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	ok2, err := r.transition(ctx, id, vtypes.JobStatusRetrying, vtypes.JobStatusQueued, `queued_at=$4, attempts=$5, error_code=NULL, error_message=NULL`, queuedAt, attempts)
	if err != nil || !ok2 {
		return apperrors.New(apperrors.KindConflict, "job not in queuable state")
	}
	return nil
}
func (r *jobRepo) MarkRunning(ctx context.Context, id string, startedAt time.Time) (bool, error) {
	return r.transition(ctx, id, vtypes.JobStatusQueued, vtypes.JobStatusRunning, `started_at=$4, error_code=NULL, error_message=NULL`, startedAt)
}
func (r *jobRepo) MarkSucceeded(ctx context.Context, id string, completedAt time.Time) error {
	ok, err := r.transition(ctx, id, vtypes.JobStatusRunning, vtypes.JobStatusSuccess, `completed_at=$4, error_code=NULL, error_message=NULL`, completedAt)
	if err != nil || !ok {
		return apperrors.New(apperrors.KindConflict, "job not in RUNNING state")
	}
	return nil
}
func (r *jobRepo) MarkFailed(ctx context.Context, id string, code vtypes.ErrorCode, message string, completedAt *time.Time) error {
	extra := `error_code=$4, error_message=$5`
	args := []interface{}{string(code), message}
	if completedAt != nil {
		extra += `, completed_at=$6`
		args = append(args, *completedAt)
	}
	mkErr := func() error { return apperrors.New(apperrors.KindConflict, "job not in fail-able state") }
	ok, err := r.transition(ctx, id, vtypes.JobStatusRunning, vtypes.JobStatusFailed, extra, args...)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	for _, from := range []vtypes.JobStatus{vtypes.JobStatusCreated, vtypes.JobStatusQueued} {
		ok2, err2 := r.transition(ctx, id, from, vtypes.JobStatusFailed, extra, args...)
		if err2 != nil {
			return err2
		}
		if ok2 {
			return nil
		}
	}
	return mkErr()
}
func (r *jobRepo) MarkRetrying(ctx context.Context, id string) error {
	ok, err := r.transition(ctx, id, vtypes.JobStatusFailed, vtypes.JobStatusRetrying, "")
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	ok2, err2 := r.transition(ctx, id, vtypes.JobStatusRunning, vtypes.JobStatusRetrying, "")
	if err2 != nil || !ok2 {
		return apperrors.New(apperrors.KindConflict, "job not retryable")
	}
	return nil
}
func (r *jobRepo) MarkDead(ctx context.Context, id string, code vtypes.ErrorCode, message string) error {
	ok, err := r.transition(ctx, id, vtypes.JobStatusRetrying, vtypes.JobStatusDead,
		`error_code=$4, error_message=$5, completed_at=now()`, string(code), message)
	if err != nil || !ok {
		return apperrors.New(apperrors.KindConflict, "job not in RETRYING state")
	}
	return nil
}
func (r *jobRepo) IncrementAttempts(ctx context.Context, id string) (int, error) {
	var a int
	err := r.q.QueryRowContext(ctx, `UPDATE inference_jobs SET attempts=attempts+1, updated_at=now() WHERE id=$1 RETURNING attempts`, id).Scan(&a)
	if err != nil {
		return 0, failExec(apperrors.KindDatabase, err, "increment attempts")
	}
	return a, nil
}
func (r *jobRepo) PickNextQueued(ctx context.Context) (*vtypes.InferenceJob, error) {
	j, err := scanJob(r.q.QueryRowContext(ctx,
		`SELECT id, project_id, asset_id, model_version_id, status, priority, attempts, max_attempts,
		        idempotency_key, error_code, error_message, queued_at, started_at, completed_at, created_at, updated_at
		 FROM inference_jobs WHERE status='QUEUED' ORDER BY priority ASC, created_at ASC FOR UPDATE SKIP LOCKED LIMIT 1`))
	if err != nil {
		if apperrors.Is(err, apperrors.KindNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return j, nil
}
func (r *jobRepo) CountByStatus(ctx context.Context) (map[vtypes.JobStatus]int64, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT status, count(*) FROM inference_jobs GROUP BY status`)
	if err != nil {
		return nil, failExec(apperrors.KindDatabase, err, "count by status")
	}
	defer rows.Close()
	out := map[vtypes.JobStatus]int64{}
	for rows.Next() {
		var s string
		var n int64
		if err := rows.Scan(&s, &n); err != nil {
			return nil, err
		}
		out[vtypes.JobStatus(s)] = n
	}
	return out, rows.Err()
}
func (r *jobRepo) CountQueueDepth(ctx context.Context) (int64, error) {
	var n int64
	err := r.q.QueryRowContext(ctx, `SELECT count(*) FROM inference_jobs WHERE status IN ('QUEUED','RETRYING')`).Scan(&n)
	if err != nil {
		return 0, failExec(apperrors.KindDatabase, err, "queue depth")
	}
	return n, nil
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanJob(row rowScanner) (*vtypes.InferenceJob, error) {
	j := &vtypes.InferenceJob{}
	var status string
	var idem, errCode, errMsg sql.NullString
	var queuedAt, startedAt, comp sql.NullTime
	var prio int
	err := row.Scan(&j.ID, &j.ProjectID, &j.AssetID, &j.ModelVersionID, &status, &prio,
		&j.Attempts, &j.MaxAttempts, &idem, &errCode, &errMsg, &queuedAt, &startedAt, &comp,
		&j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return nil, notFound("job")
		}
		return nil, failExec(apperrors.KindDatabase, err, "scan job")
	}
	j.Status = vtypes.JobStatus(status)
	j.Priority = vtypes.Priority(prio)
	if idem.Valid {
		s := idem.String
		j.IdempotencyKey = &s
	}
	if errCode.Valid {
		c := vtypes.ErrorCode(errCode.String)
		j.ErrorCode = &c
	}
	if errMsg.Valid {
		s := errMsg.String
		j.ErrorMessage = &s
	}
	if queuedAt.Valid {
		j.QueuedAt = &queuedAt.Time
	}
	if startedAt.Valid {
		j.StartedAt = &startedAt.Time
	}
	if comp.Valid {
		j.CompletedAt = &comp.Time
	}
	return j, nil
}

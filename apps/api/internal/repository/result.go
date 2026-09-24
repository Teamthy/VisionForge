package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	stderrors "errors"

	apperrors "github.com/visionforge/visionforge/apps/api/internal/errors"
	vtypes "github.com/visionforge/visionforge/packages/types"
)

type ResultRepository interface {
	Create(ctx context.Context, r *vtypes.InferenceResult) (*vtypes.InferenceResult, error)
	FindByJobID(ctx context.Context, jobID string) (*vtypes.InferenceResult, error)
}
type resultRepo struct{ q Executor }

func (r *resultRepo) Create(ctx context.Context, res *vtypes.InferenceResult) (*vtypes.InferenceResult, error) {
	if res.CreatedAt.IsZero() { res.CreatedAt = now() }
	if res.ResultJSON == nil { res.ResultJSON = json.RawMessage("{}") }
	err := r.q.QueryRowContext(ctx,
		`INSERT INTO inference_results (job_id, model_version_id, result_json, processing_time_ms, inference_time_ms, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		res.JobID, res.ModelVersionID, []byte(res.ResultJSON), res.ProcessingTimeMs, res.InferenceTimeMs, res.CreatedAt).Scan(&res.ID)
	if err != nil { return nil, failExec(apperrors.KindDatabase, err, "create result") }
	return res, nil
}
func (r *resultRepo) FindByJobID(ctx context.Context, jobID string) (*vtypes.InferenceResult, error) {
	res := &vtypes.InferenceResult{}; var raw []byte
	err := r.q.QueryRowContext(ctx,
		`SELECT id, job_id, model_version_id, result_json, processing_time_ms, inference_time_ms, created_at FROM inference_results WHERE job_id=$1`, jobID).
		Scan(&res.ID, &res.JobID, &res.ModelVersionID, &raw, &res.ProcessingTimeMs, &res.InferenceTimeMs, &res.CreatedAt)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) { return nil, notFound("result") }
		return nil, failExec(apperrors.KindDatabase, err, "find result")
	}
	res.ResultJSON = raw; return res, nil
}

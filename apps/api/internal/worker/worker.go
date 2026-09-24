// Package worker implements the async job worker that polls Redis, downloads
// assets, calls the Python ML service, persists results, and updates job state.
package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/visionforge/visionforge/apps/api/internal/audit"
	"github.com/visionforge/visionforge/apps/api/internal/mlclient"
	apperrors "github.com/visionforge/visionforge/apps/api/internal/errors"
	"github.com/visionforge/visionforge/apps/api/internal/queue"
	"github.com/visionforge/visionforge/apps/api/internal/repository"
	"github.com/visionforge/visionforge/apps/api/internal/storage"
	vtypes "github.com/visionforge/visionforge/packages/types"
)

// MLHTTPClient is the minimal ML service contract used by the worker.
type MLHTTPClient interface {
	PredictFromURL(ctx context.Context, ref mlclient.ModelRef, storageKey, assetURL string) (*MLPredictResponse, error)
}

// MLPredictResponse mirrors the ML service's output. It is an alias so the
// concrete mlclient.Client satisfies the MLHTTPClient interface directly.
type MLPredictResponse = mlclient.PredictResponse

// Worker processes inference jobs from Redis.
type Worker struct {
	db           *sql.DB
	repos        *repository.Repos
	q            queue.Queue
	store        storage.ObjectStorage
	ml           MLHTTPClient
	audit        audit.Recorder
	concurrency  int
	pollInterval time.Duration
	maxAttempts  int
	metrics      Metrics
}

// Metrics are hooks for prometheus updates.
type Metrics struct {
	JobsSucceeded func()
	JobsFailed    func()
	JobsRetried   func()
	ActiveJobs    func(d float64)
	InferenceHist func(taskType string, seconds float64)
	QueueDepth    func(d float64)
}

// Config bundles worker configuration.
type Config struct {
	Concurrency       int
	PollInterval      time.Duration
	MaxAttempts       int
}

// New creates a Worker.
func New(db *sql.DB, repos *repository.Repos, q queue.Queue, store storage.ObjectStorage, ml MLHTTPClient, ar audit.Recorder, cfg Config, m Metrics) *Worker {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	return &Worker{
		db: db, repos: repos, q: q, store: store, ml: ml, audit: ar,
		concurrency: cfg.Concurrency, pollInterval: cfg.PollInterval, maxAttempts: cfg.MaxAttempts,
		metrics: m,
	}
}

// Run starts the worker pool and blocks until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) error {
	slog.Info("worker starting", "concurrency", w.concurrency)
	var wg sync.WaitGroup
	for i := 0; i < w.concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			w.loop(ctx, id)
		}(i)
	}

	stop := make(chan struct{})
	var hkWG sync.WaitGroup
	hkWG.Add(1)
	go func() {
		defer hkWG.Done()
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if rq, ok := w.q.(*queue.RedisQueue); ok {
					if n, err := rq.RequeueExpired(ctx); err == nil && n > 0 {
						slog.Warn("requeued expired jobs", "count", n)
					}
				}
				depth, err := w.q.Depth(ctx)
				if err == nil && w.metrics.QueueDepth != nil {
					w.metrics.QueueDepth(float64(depth))
				}
			}
		}
	}()

	wg.Wait()
	close(stop)
	hkWG.Wait()
	slog.Info("worker stopped")
	return nil
}

func (w *Worker) loop(ctx context.Context, id int) {
	log := slog.With("worker_id", id)
	for {
		if ctx.Err() != nil {
			return
		}
		jobCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		payload, err := w.q.Dequeue(jobCtx, 2*time.Minute)
		if err != nil {
			cancel()
			if ctx.Err() != nil {
				return
			}
			log.Warn("dequeue error", "error", err)
			time.Sleep(w.pollInterval)
			continue
		}
		if payload == nil {
			cancel()
			continue
		}
		if w.metrics.ActiveJobs != nil {
			w.metrics.ActiveJobs(1)
		}
		w.process(jobCtx, log, payload)
		if w.metrics.ActiveJobs != nil {
			w.metrics.ActiveJobs(-1)
		}
		cancel()
	}
}

func (w *Worker) process(ctx context.Context, log *slog.Logger, p *vtypes.JobPayload) {
	log = log.With("job_id", p.JobID)
	startedAt := time.Now().UTC()
	ok, err := w.repos.Jobs.MarkRunning(ctx, p.JobID, startedAt)
	if err != nil || !ok {
		log.Warn("could not mark job running; acking", "err", err)
		_ = w.q.Ack(ctx, p.JobID)
		return
	}
	result, err := w.runInference(ctx, p)
	if err != nil {
		w.handleFailure(ctx, log, p, err)
		return
	}
	raw, _ := json.Marshal(result)
	if _, err := w.repos.Results.Create(ctx, &vtypes.InferenceResult{
		JobID:            p.JobID,
		ModelVersionID:   p.ModelVersionID,
		ResultJSON:       raw,
		ProcessingTimeMs: result.ProcessingTimeMs,
		InferenceTimeMs:  result.InferenceTimeMs,
	}); err != nil {
		log.Error("persist result failed", "error", err)
		w.handleFailure(ctx, log, p, apperrors.Wrap(apperrors.KindDatabase, err, "persist result"))
		return
	}
	comp := time.Now().UTC()
	if err := w.repos.Jobs.MarkSucceeded(ctx, p.JobID, comp); err != nil {
		log.Error("mark success failed", "error", err)
	}
	if err := w.q.Ack(ctx, p.JobID); err != nil {
		log.Warn("ack failed", "error", err)
	}
	if w.metrics.JobsSucceeded != nil {
		w.metrics.JobsSucceeded()
	}
	if w.metrics.InferenceHist != nil {
		w.metrics.InferenceHist(string(resultTaskType(result)), float64(result.ProcessingTimeMs)/1000.0)
	}
	audit.Record(ctx, w.audit, audit.Actor{}, "job.complete", "job", p.JobID, nil)
	log.Info("job succeeded", "duration_ms", result.ProcessingTimeMs, "inference_ms", result.InferenceTimeMs)
}

func resultTaskType(r *vtypes.JobResult) vtypes.TaskType {
	if len(r.Detections) > 0 {
		return vtypes.TaskObjectDetection
	}
	return vtypes.TaskClassification
}

func (w *Worker) runInference(ctx context.Context, p *vtypes.JobPayload) (*vtypes.JobResult, error) {
	a, err := w.repos.Assets.FindByID(ctx, p.AssetID)
	if err != nil {
		return nil, apperrors.Wrap(apperrors.KindStorage, err, "find asset")
	}
	assetURL := ""
	if psURL, err := w.store.GetPresignedURL(ctx, a.StorageKey, 5*time.Minute); err == nil {
		assetURL = psURL
	}
	ref := mlclient.ModelRef{ModelVersionID: p.ModelVersionID}
	if mv, err := w.repos.ModelVersions.FindByID(ctx, p.ModelVersionID); err == nil {
		ref.ArtifactURI = mv.ArtifactURI
		if m, err := w.repos.Models.FindByID(ctx, mv.ModelID); err == nil {
			ref.TaskType = m.TaskType
		}
	}
	start := time.Now()
	resp, err := w.ml.PredictFromURL(ctx, ref, a.StorageKey, assetURL)
	if err != nil {
		return nil, apperrors.Wrap(apperrors.KindInference, err, "ml predict")
	}
	total := time.Since(start).Milliseconds()
	im := resp.InferenceTimeMs
	if im == 0 {
		im = total
	}
	return &vtypes.JobResult{
		JobID:            p.JobID,
		ModelVersionID:   p.ModelVersionID,
		Status:           vtypes.JobStatusSuccess,
		ProcessingTimeMs: total,
		InferenceTimeMs:  im,
		Detections:       resp.Detections,
		Predictions:      resp.Predictions,
		CompletedAt:      time.Now().UTC(),
	}, nil
}

func (w *Worker) handleFailure(ctx context.Context, log *slog.Logger, p *vtypes.JobPayload, err error) {
	ae, ok := apperrors.AsAppError(err)
	if !ok {
		ae = apperrors.New(apperrors.KindInternal, err.Error())
	}
	code := classifyErrorCode(ae.Kind)
	msg := ae.Message
	attempt := p.Attempt + 1
	if attempt < w.maxAttempts && code.Retriable() {
		backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
		jitter := time.Duration(rand.Int63n(int64(time.Second)))
		delay := backoff + jitter
		log.Warn("job failed; retrying", "attempt", attempt, "backoff", delay, "error", err)
		if err := w.repos.Jobs.MarkRetrying(ctx, p.JobID); err != nil {
			log.Error("mark retrying failed", "error", err)
		}
		_ = w.q.Ack(ctx, p.JobID)
		newPayload := *p
		newPayload.Attempt = attempt
		if err := w.q.Enqueue(ctx, newPayload, delay); err != nil {
			log.Error("re-enqueue failed", "error", err)
		}
		if w.metrics.JobsRetried != nil {
			w.metrics.JobsRetried()
		}
		return
	}
	log.Error("job failed permanently", "error", err)
	if err := w.repos.Jobs.MarkDead(ctx, p.JobID, code, msg); err != nil {
		log.Error("mark dead failed", "error", err)
	}
	_ = w.q.Fail(ctx, p.JobID, true, msg)
	if w.metrics.JobsFailed != nil {
		w.metrics.JobsFailed()
	}
	failResult := &vtypes.JobResult{
		JobID: p.JobID, ModelVersionID: p.ModelVersionID, Status: vtypes.JobStatusDead,
		Error: &vtypes.ResultError{Code: code, Message: msg}, CompletedAt: time.Now().UTC(),
	}
	raw, _ := json.Marshal(failResult)
	_, _ = w.repos.Results.Create(ctx, &vtypes.InferenceResult{
		JobID: p.JobID, ModelVersionID: p.ModelVersionID, ResultJSON: raw,
	})
}

func classifyErrorCode(k apperrors.Kind) vtypes.ErrorCode {
	switch k {
	case apperrors.KindValidation:
		return vtypes.ErrCodeInvalidInput
	case apperrors.KindStorage:
		return vtypes.ErrCodeStorage
	case apperrors.KindDatabase:
		return vtypes.ErrCodeDatabase
	case apperrors.KindQueue:
		return vtypes.ErrCodeQueue
	case apperrors.KindInference, apperrors.KindModel:
		return vtypes.ErrCodeMLService
	case apperrors.KindInternal:
		return vtypes.ErrCodeInternal
	case apperrors.KindNotFound:
		return vtypes.ErrCodeNotFound
	default:
		return vtypes.ErrCodeInternal
	}
}

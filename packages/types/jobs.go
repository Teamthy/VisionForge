package types

import (
	"encoding/json"
	"time"
)

// JobStatus represents the lifecycle state of an inference job.
type JobStatus string

const (
	JobStatusCreated   JobStatus = "CREATED"
	JobStatusQueued    JobStatus = "QUEUED"
	JobStatusRunning   JobStatus = "RUNNING"
	JobStatusSuccess   JobStatus = "SUCCESS"
	JobStatusFailed    JobStatus = "FAILED"
	JobStatusRetrying  JobStatus = "RETRYING"
	JobStatusDead      JobStatus = "DEAD"
	JobStatusCancelled JobStatus = "CANCELLED"
)

// ValidTransition returns true if moving from current -> next is allowed.
func (s JobStatus) ValidTransition(next JobStatus) bool {
	allowed := map[JobStatus]map[JobStatus]bool{
		JobStatusCreated: {
			JobStatusQueued:    true,
			JobStatusCancelled: true,
			JobStatusFailed:    true,
		},
		JobStatusQueued: {
			JobStatusRunning:   true,
			JobStatusRetrying:  true,
			JobStatusDead:      true,
			JobStatusCancelled: true,
		},
		JobStatusRunning: {
			JobStatusSuccess:  true,
			JobStatusFailed:   true,
			JobStatusRetrying: true,
		},
		JobStatusRetrying: {
			JobStatusQueued: true,
			JobStatusDead:   true,
		},
		JobStatusFailed: {
			JobStatusRetrying:  true,
			JobStatusDead:      true,
			JobStatusCancelled: true,
		},
		// Terminal states: no outgoing transitions
		JobStatusSuccess:   {},
		JobStatusDead:      {},
		JobStatusCancelled: {},
	}
	nexts, ok := allowed[s]
	if !ok {
		return false
	}
	return nexts[next]
}

// IsTerminal reports whether the status is a terminal state.
func (s JobStatus) IsTerminal() bool {
	return s == JobStatusSuccess || s == JobStatusDead || s == JobStatusCancelled
}

// ErrorCode classifies failures for retry decisions.
type ErrorCode string

const (
	ErrCodeInvalidInput     ErrorCode = "INVALID_INPUT"
	ErrCodeUnauthorized     ErrorCode = "UNAUTHORIZED"
	ErrCodeNotFound         ErrorCode = "NOT_FOUND"
	ErrCodeUnsupportedModel ErrorCode = "UNSUPPORTED_MODEL"
	ErrCodeCorruptFile      ErrorCode = "CORRUPT_FILE"
	ErrCodeStorage          ErrorCode = "STORAGE_ERROR"
	ErrCodeDatabase         ErrorCode = "DATABASE_ERROR"
	ErrCodeQueue            ErrorCode = "QUEUE_ERROR"
	ErrCodeMLService        ErrorCode = "ML_SERVICE_ERROR"
	ErrCodeTimeout          ErrorCode = "TIMEOUT"
	ErrCodeRateLimited      ErrorCode = "RATE_LIMITED"
	ErrCodeInternal         ErrorCode = "INTERNAL_ERROR"
)

// Retriable returns true if a worker should retry the job.
func (c ErrorCode) Retriable() bool {
	switch c {
	case ErrCodeStorage, ErrCodeDatabase, ErrCodeQueue, ErrCodeMLService, ErrCodeTimeout:
		return true
	default:
		return false
	}
}

// Priority is the job scheduling priority (lower = higher priority).
type Priority int

const (
	PriorityHigh   Priority = 1
	PriorityNormal Priority = 5
	PriorityLow    Priority = 10
)

// JobPayload is what gets enqueued into Redis (references only, no large binaries).
type JobPayload struct {
	JobID          string   `json:"job_id"`
	ProjectID      string   `json:"project_id"`
	AssetID        string   `json:"asset_id"`
	ModelVersionID string   `json:"model_version_id"`
	Priority       Priority `json:"priority"`
	Attempt        int      `json:"attempt"`
	IdempotencyKey string   `json:"idempotency_key,omitempty"`
	TraceID        string   `json:"trace_id,omitempty"`
}

// Marshal encodes the payload as JSON bytes.
func (p JobPayload) Marshal() ([]byte, error) { return json.Marshal(p) }

// UnmarshalJobPayload decodes JSON bytes into a JobPayload.
func UnmarshalJobPayload(data []byte) (JobPayload, error) {
	var p JobPayload
	err := json.Unmarshal(data, &p)
	return p, err
}

// JobResult is the canonical output produced by the ML runtime.
type JobResult struct {
	JobID            string          `json:"job_id"`
	ModelVersionID   string          `json:"model_version_id"`
	Status           JobStatus       `json:"status"`
	ProcessingTimeMs int64           `json:"processing_time_ms"`
	InferenceTimeMs  int64           `json:"inference_time_ms"`
	Detections       []Detection     `json:"detections,omitempty"`
	Predictions      []Prediction    `json:"predictions,omitempty"`
	Error            *ResultError    `json:"error,omitempty"`
	CompletedAt      time.Time       `json:"completed_at"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
}

// Detection is a single object-detection bounding box.
type Detection struct {
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
	BBox       BBox    `json:"bbox"`
}

// BBox is an axis-aligned bounding box.
type BBox struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Prediction is a single class prediction for image classification.
type Prediction struct {
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
}

// ResultError carries structured error info from the ML service.
type ResultError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

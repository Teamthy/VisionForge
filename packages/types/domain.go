package types

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// ── Core domain entities ─────────────────────────────────

// Role represents an RBAC role.
type Role string

const (
	RoleUser  Role = "USER"
	RoleAdmin Role = "ADMIN"
)

// TaskType identifies the CV task a model performs.
type TaskType string

const (
	TaskObjectDetection TaskType = "object_detection"
	TaskClassification  TaskType = "classification"
)

// ModelVersionStatus describes the lifecycle state of a model version.
type ModelVersionStatus string

const (
	ModelVersionActive  ModelVersionStatus = "ACTIVE"
	ModelVersionStaging ModelVersionStatus = "STAGING"
	ModelVersionInactive ModelVersionStatus = "INACTIVE"
	ModelVersionDeprecated ModelVersionStatus = "DEPRECATED"
)

// Runtime indicates which ML runtime loads a model artifact.
type Runtime string

const (
	RuntimePyTorch Runtime = "pytorch"
	RuntimeONNX    Runtime = "onnx"
)

// User represents a platform user.
type User struct {
	ID           string     `json:"id"`
	Email        string     `json:"email"`
	PasswordHash string     `json:"-"`
	Name         string     `json:"name"`
	Role         Role       `json:"role"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
}

// RefreshToken is a stored refresh-token record.
type RefreshToken struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	TokenHash string     `json:"-"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Project groups assets, jobs, and model configurations under an owner.
type Project struct {
	ID          string    `json:"id"`
	OwnerID     string    `json:"owner_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Model is a computer vision model family (e.g., "YOLOv8n").
type Model struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	TaskType    TaskType  `json:"task_type"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ModelVersion is a concrete versioned artifact of a Model.
type ModelVersion struct {
	ID          string             `json:"id"`
	ModelID     string             `json:"model_id"`
	Version     string             `json:"version"`
	ArtifactURI string             `json:"artifact_uri"`
	Runtime     Runtime            `json:"runtime"`
	Status      ModelVersionStatus `json:"status"`
	Metadata    JSONB              `json:"metadata,omitempty"`
	CreatedAt   time.Time          `json:"created_at"`
}

// Asset is an uploaded image/video.
type Asset struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	OwnerID     string    `json:"owner_id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	StorageKey  string    `json:"storage_key"`
	Checksum    string    `json:"checksum"`
	Metadata    JSONB     `json:"metadata,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// InferenceJob tracks an inference request through its lifecycle.
type InferenceJob struct {
	ID              string    `json:"id"`
	ProjectID       string    `json:"project_id"`
	AssetID         string    `json:"asset_id"`
	ModelVersionID  string    `json:"model_version_id"`
	Status          JobStatus `json:"status"`
	Priority        Priority  `json:"priority"`
	Attempts        int       `json:"attempts"`
	MaxAttempts     int       `json:"max_attempts"`
	IdempotencyKey  *string   `json:"idempotency_key,omitempty"`
	ErrorCode       *ErrorCode `json:"error_code,omitempty"`
	ErrorMessage    *string   `json:"error_message,omitempty"`
	QueuedAt        *time.Time `json:"queued_at,omitempty"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// InferenceResult stores model output for a successful or failed job.
type InferenceResult struct {
	ID               string          `json:"id"`
	JobID            string          `json:"job_id"`
	ModelVersionID   string          `json:"model_version_id"`
	ResultJSON       json.RawMessage `json:"result_json"`
	ProcessingTimeMs int64           `json:"processing_time_ms"`
	InferenceTimeMs  int64           `json:"inference_time_ms"`
	CreatedAt        time.Time       `json:"created_at"`
}

// AuditLog records a security-sensitive action.
type AuditLog struct {
	ID           string    `json:"id"`
	UserID       *string   `json:"user_id,omitempty"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type"`
	ResourceID   *string   `json:"resource_id,omitempty"`
	Metadata     JSONB     `json:"metadata,omitempty"`
	IPAddress    string    `json:"ip_address"`
	UserAgent    string    `json:"user_agent"`
	CreatedAt    time.Time `json:"created_at"`
}

// ── JSONB helper ────────────────────────────────────────

// JSONB is a helper for PostgreSQL jsonb columns.
type JSONB json.RawMessage

// Value implements driver.Valuer.
func (j JSONB) Value() (driver.Value, error) {
	if len(j) == 0 {
		return []byte("{}"), nil
	}
	return []byte(j), nil
}

// Scan implements sql.Scanner.
func (j *JSONB) Scan(src interface{}) error {
	if src == nil {
		*j = JSONB("{}")
		return nil
	}
	switch v := src.(type) {
	case []byte:
		*j = JSONB(append([]byte(nil), v...))
	case string:
		*j = JSONB(v)
	default:
		return errors.New("unsupported scan type for JSONB")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (j JSONB) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("{}"), nil
	}
	return []byte(j), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (j *JSONB) UnmarshalJSON(data []byte) error {
	if j == nil {
		return errors.New("JSONB: UnmarshalJSON on nil pointer")
	}
	*j = JSONB(append([]byte(nil), data...))
	return nil
}

package types

// ── Standard API envelope ────────────────────────────────

// Response is the standard success envelope.
type Response struct {
	Data interface{} `json:"data"`
	Meta *Meta       `json:"meta,omitempty"`
}

// Meta carries pagination and similar metadata.
type Meta struct {
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
	RequestID  string `json:"request_id,omitempty"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error APIError `json:"error"`
}

// APIError represents a structured API error.
type APIError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
	// Details is optional structured information for validation errors.
	Details interface{} `json:"details,omitempty"`
}

// NewError builds an ErrorResponse.
func NewError(code, message, requestID string) ErrorResponse {
	return ErrorResponse{Error: APIError{Code: code, Message: message, RequestID: requestID}}
}

// Paginated is a generic helper for paginated responses.
type Paginated[T any] struct {
	Items []T   `json:"items"`
	Meta  *Meta `json:"meta,omitempty"`
}

// ── Auth request/response DTOs ──────────────────────────

type RegisterRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8,max=128"`
	Name     string `json:"name" binding:"required,min=1,max=100"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type LoginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	User         User   `json:"user"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type RefreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int `json:"expires_in"`
}

type LogoutRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// ── Project DTOs ────────────────────────────────────────

type CreateProjectRequest struct {
	Name        string `json:"name" binding:"required,min=1,max=100"`
	Description string `json:"description" binding:"max=500"`
}

type UpdateProjectRequest struct {
	Name        *string `json:"name" binding:"omitempty,min=1,max=100"`
	Description *string `json:"description" binding:"omitempty,max=500"`
}

// ── Asset DTOs ──────────────────────────────────────────

type CreateAssetUploadRequest struct {
	Filename    string `json:"filename" binding:"required,min=1,max=255"`
	ContentType string `json:"content_type" binding:"required"`
	SizeBytes   int64  `json:"size_bytes" binding:"required,min=1"`
	Checksum    string `json:"checksum" binding:"omitempty"`
}

type CreateAssetUploadResponse struct {
	AssetID      string            `json:"asset_id"`
	UploadURL    string            `json:"upload_url"`
	StorageKey   string            `json:"storage_key"`
	ExpiresIn    int               `json:"expires_in"`
	Fields       map[string]string `json:"fields,omitempty"` // form fields for presigned POST
}

type ConfirmAssetUploadRequest struct {
	AssetID string `json:"asset_id" binding:"required"`
}

// ── Model DTOs ──────────────────────────────────────────

type CreateModelRequest struct {
	Name        string   `json:"name" binding:"required,min=1,max=100"`
	Description string   `json:"description" binding:"max=500"`
	TaskType    TaskType `json:"task_type" binding:"required"`
}

type CreateModelVersionRequest struct {
	Version     string             `json:"version" binding:"required,min=1,max=50"`
	ArtifactURI string             `json:"artifact_uri" binding:"required"`
	Runtime     Runtime            `json:"runtime" binding:"required"`
	Status      ModelVersionStatus `json:"status" binding:"required"`
	Metadata    JSONB              `json:"metadata,omitempty"`
}

type UpdateModelVersionStatusRequest struct {
	Status ModelVersionStatus `json:"status" binding:"required"`
}

// ── Inference DTOs ──────────────────────────────────────

type CreateInferenceRequest struct {
	ProjectID      string   `json:"project_id"`
	AssetID        string   `json:"asset_id" binding:"required"`
	ModelVersionID string   `json:"model_version_id" binding:"required"`
	Priority       Priority `json:"priority"`
	Async          bool     `json:"async"`
}

type CreateInferenceResponse struct {
	JobID  string                 `json:"job_id"`
	Status JobStatus              `json:"status"`
	Result map[string]interface{} `json:"result,omitempty"`
}

type ListJobsQuery struct {
	ProjectID string   `form:"project_id"`
	Status    []JobStatus `form:"status"`
	Limit     int      `form:"limit,default=50"`
	Cursor    string   `form:"cursor"`
}

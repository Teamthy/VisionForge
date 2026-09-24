// Package mlclient provides an HTTP client for the Python ML inference service.
package mlclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	vtypes "github.com/visionforge/visionforge/packages/types"
)

// Client talks to the Python ML inference service.
type Client struct {
	baseURL string
	httpc   *http.Client
}

// New builds a new ML client.
func New(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		httpc:   &http.Client{Timeout: timeout},
	}
}

// HealthResponse is returned by /health.
type HealthResponse struct {
	Status  string   `json:"status"`
	Models  []string `json:"loaded_models"`
	Version string   `json:"version"`
}

// ModelRef identifies the model to run, mirroring the ML service's dynamic
// registry resolution (id first, artifact/task as fallback).
type ModelRef struct {
	ModelVersionID string
	ArtifactURI    string
	TaskType       vtypes.TaskType
}

// PredictRequest is the ML inference request body.
type PredictRequest struct {
	ModelVersionID string `json:"model_version_id"`
	ArtifactURI    string `json:"artifact_uri,omitempty"`
	TaskType       string `json:"task_type,omitempty"`
	// AssetBytes contains raw image bytes for sync paths; the worker path uses
	// presigned URLs / shared volume mounts instead.
	AssetBytes []byte `json:"-"`
	AssetURL   string `json:"asset_url,omitempty"`
	StorageKey string `json:"storage_key,omitempty"`
}

// PredictResponse is the ML inference response.
type PredictResponse struct {
	ModelVersionID   string              `json:"model_version_id"`
	TaskType         vtypes.TaskType     `json:"task_type"`
	Detections       []vtypes.Detection  `json:"detections,omitempty"`
	Predictions      []vtypes.Prediction `json:"predictions,omitempty"`
	InferenceTimeMs  int64               `json:"inference_time_ms"`
	ProcessingTimeMs int64               `json:"processing_time_ms"`
}

// Health pings the ML service.
func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ml health: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ml health status %d: %s", resp.StatusCode, b)
	}
	var h HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return nil, err
	}
	return &h, nil
}

// PredictFromBytes performs inference from a byte buffer (used for synchronous
// small-payload inference; async workers use storage URLs). The image is sent
// as multipart/form-data matching the ML service /predict contract.
func (c *Client) PredictFromBytes(ctx context.Context, ref ModelRef, img []byte, contentType string) (*PredictResponse, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("model_version_id", ref.ModelVersionID); err != nil {
		return nil, err
	}
	if ref.ArtifactURI != "" {
		if err := mw.WriteField("artifact_uri", ref.ArtifactURI); err != nil {
			return nil, err
		}
	}
	if ref.TaskType != "" {
		if err := mw.WriteField("task_type", string(ref.TaskType)); err != nil {
			return nil, err
		}
	}
	fname := "image"
	if idx := strings.LastIndex(contentType, "/"); idx >= 0 {
		switch contentType[idx+1:] {
		case "jpeg", "jpg":
			fname = "image.jpg"
		case "png":
			fname = "image.png"
		case "webp":
			fname = "image.webp"
		case "gif":
			fname = "image.gif"
		}
	}
	part, err := mw.CreateFormFile("file", fname)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(img); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/predict", &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ml predict: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ml predict status %d: %s", resp.StatusCode, string(b))
	}
	var out PredictResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PredictFromURL asks the ML service to download from a URL/key (for async worker flow).
func (c *Client) PredictFromURL(ctx context.Context, ref ModelRef, storageKey, assetURL string) (*PredictResponse, error) {
	body, err := json.Marshal(PredictRequest{
		ModelVersionID: ref.ModelVersionID,
		ArtifactURI:    ref.ArtifactURI,
		TaskType:       string(ref.TaskType),
		AssetURL:       assetURL,
		StorageKey:     storageKey,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/predict_url", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ml predict_url: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ml predict_url status %d: %s", resp.StatusCode, string(b))
	}
	var out PredictResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

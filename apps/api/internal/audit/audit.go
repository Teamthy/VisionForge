// Package audit provides a small helper that services use to record audit events.
package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	vtypes "github.com/visionforge/visionforge/packages/types"
)

// Recorder is the minimal interface for writing audit entries.
type Recorder interface {
	Record(ctx context.Context, entry *vtypes.AuditLog) error
}

// Actor captures the authenticated user (if any) making a request.
type Actor struct {
	UserID    string
	IPAddress string
	UserAgent string
}

// Record emits an audit entry; failures are logged but never propagated
// (audit logging must never break request flow).
func Record(ctx context.Context, r Recorder, actor Actor, action, resourceType, resourceID string, meta interface{}) {
	if r == nil {
		return
	}
	entry := &vtypes.AuditLog{
		Action:       action,
		ResourceType: resourceType,
		IPAddress:    actor.IPAddress,
		UserAgent:    actor.UserAgent,
		CreatedAt:    time.Now().UTC(),
	}
	if actor.UserID != "" {
		uid := actor.UserID
		entry.UserID = &uid
	}
	if resourceID != "" {
		rid := resourceID
		entry.ResourceID = &rid
	}
	if meta != nil {
		b, err := json.Marshal(meta)
		if err == nil {
			entry.Metadata = vtypes.JSONB(b)
		} else {
			entry.Metadata = vtypes.JSONB(`{}`)
		}
	} else {
		entry.Metadata = vtypes.JSONB(`{}`)
	}
	if err := r.Record(ctx, entry); err != nil {
		slog.Warn("audit record failed", "action", action, "error", err)
	}
}

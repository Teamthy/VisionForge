package repository

import (
	"context"
	"encoding/json"

	apperrors "github.com/visionforge/visionforge/apps/api/internal/errors"
	vtypes "github.com/visionforge/visionforge/packages/types"
)

type AuditLogRepository interface {
	Record(ctx context.Context, entry *vtypes.AuditLog) error
	ListByUser(ctx context.Context, userID string, limit int) ([]vtypes.AuditLog, error)
}
type auditLogRepo struct{ q Executor }

func (r *auditLogRepo) Record(ctx context.Context, e *vtypes.AuditLog) error {
	if e.CreatedAt.IsZero() { e.CreatedAt = now() }
	if e.Metadata == nil { e.Metadata = vtypes.JSONB("{}") }
	meta, _ := json.Marshal(e.Metadata)
	var uid, rid interface{}
	if e.UserID != nil && *e.UserID != "" { uid = *e.UserID }
	if e.ResourceID != nil && *e.ResourceID != "" { rid = *e.ResourceID }
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO audit_logs (user_id, action, resource_type, resource_id, metadata, ip_address, user_agent, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		uid, e.Action, e.ResourceType, rid, meta, e.IPAddress, e.UserAgent, e.CreatedAt)
	if err != nil { return failExec(apperrors.KindDatabase, err, "record audit") }
	return nil
}
func (r *auditLogRepo) ListByUser(ctx context.Context, userID string, limit int) ([]vtypes.AuditLog, error) {
	if limit <= 0 || limit > 200 { limit = 50 }
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, user_id, action, resource_type, resource_id, metadata, ip_address, user_agent, created_at
		 FROM audit_logs WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if err != nil { return nil, failExec(apperrors.KindDatabase, err, "list audit") }
	defer rows.Close()
	var out []vtypes.AuditLog
	for rows.Next() {
		e := vtypes.AuditLog{}; var uid, rid interface{}; var meta []byte
		if err := rows.Scan(&e.ID, &uid, &e.Action, &e.ResourceType, &rid, &meta, &e.IPAddress, &e.UserAgent, &e.CreatedAt); err != nil {
			return nil, failExec(apperrors.KindDatabase, err, "scan audit")
		}
		if uid != nil { if s, ok := uid.(string); ok && s != "" { e.UserID = &s } }
		if rid != nil { if s, ok := rid.(string); ok && s != "" { e.ResourceID = &s } }
		e.Metadata = vtypes.JSONB(meta); out = append(out, e)
	}
	return out, nil
}

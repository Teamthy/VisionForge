// Package handler defines HTTP handlers for VisionForge's v1 API.
// Every handler returns the standard envelope {data, meta} on success and
// {error: {code, message, request_id}} on failure.
package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/visionforge/visionforge/apps/api/internal/audit"
	apperrors "github.com/visionforge/visionforge/apps/api/internal/errors"
	"github.com/visionforge/visionforge/apps/api/internal/middleware"
	"github.com/visionforge/visionforge/apps/api/internal/observability"
	"github.com/visionforge/visionforge/apps/api/internal/service"
	vtypes "github.com/visionforge/visionforge/packages/types"
)

type Services struct {
	Auth      *service.AuthService
	Projects  *service.ProjectService
	Assets    *service.AssetService
	Models    *service.ModelService
	Inference *service.InferenceService
}

func New(s *Services) *Handler { return &Handler{s: s} }

type Handler struct{ s *Services }

// ── envelope helpers ────────────────────────────────────
func requestID(c *gin.Context) string { return observability.RequestID(c.Request.Context()) }

func ok(c *gin.Context, status int, data interface{}) {
	c.JSON(status, vtypes.Response{Data: data})
}
func okMeta(c *gin.Context, status int, data interface{}, meta *vtypes.Meta) {
	c.JSON(status, vtypes.Response{Data: data, Meta: meta})
}
func fail(c *gin.Context, err error) {
	if err == nil {
		c.JSON(http.StatusInternalServerError, vtypes.NewError("INTERNAL_ERROR", "nil error", requestID(c)))
		return
	}
	var ae *apperrors.AppError
	if errors.As(err, &ae) {
		status := ae.Kind.Status()
		if status >= 500 {
			// Internal errors: log full cause for operators (never returned to clients).
			slog.Error("api error", "kind", ae.Kind, "message", ae.Message, "cause", ae.Cause, "request_id", requestID(c))
		} else {
			slog.Warn("api error", "kind", ae.Kind, "message", ae.Message, "request_id", requestID(c))
		}
		c.JSON(status, vtypes.NewError(string(ae.Kind), ae.Message, requestID(c)))
		return
	}
	slog.Error("unhandled error", "err", err, "request_id", requestID(c))
	c.JSON(http.StatusInternalServerError, vtypes.NewError("INTERNAL_ERROR", "internal server error", requestID(c)))
}

// currentUserID reads the authenticated user from context.
func currentUserID(c *gin.Context) string {
	u := middleware.CurrentUser(c)
	if u == nil {
		return ""
	}
	return u.ID
}

// parseLimit parses an int query parameter with a default and max.
func parseLimit(c *gin.Context, def, max int) int {
	v := c.Query("limit")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// errBind converts a binding error into an AppError (validation).
func errBind(err error) error {
	if err == nil {
		return nil
	}
	return apperrors.New(apperrors.KindValidation, err.Error())
}

func clientIP(c *gin.Context) string { return middleware.ClientIP(c) }

func actorFrom(c *gin.Context) audit.Actor {
	return audit.Actor{UserID: currentUserID(c), IPAddress: clientIP(c), UserAgent: c.Request.UserAgent()}
}

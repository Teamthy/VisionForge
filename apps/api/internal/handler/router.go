package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/visionforge/visionforge/apps/api/internal/middleware"
	vtypes "github.com/visionforge/visionforge/packages/types"
)

// RegisterRoutes wires all /api/v1 routes on the given engine.
func RegisterRoutes(r *gin.Engine, h *Handler, jwtm gin.HandlerFunc, adminMW gin.HandlerFunc, rl *middleware.RateLimiter) {
	// Global rate limit: 120 req/min per client IP.
	r.Use(rateLimitGin(rl, 120, time.Minute))

	v1 := r.Group("/api/v1")

	// Auth (tight rate limit: 10 req/min per IP).
	authLimiter := rateLimitGin(rl, 10, time.Minute)
	a := v1.Group("/auth", authLimiter)
	{
		a.POST("/register", h.Register)
		a.POST("/login", h.Login)
		a.POST("/refresh", h.Refresh)
		a.POST("/logout", jwtm, h.Logout)
		a.GET("/me", jwtm, h.Me)
	}

	// Upload-heavy endpoints: 30 req/min.
	uploadLimiter := rateLimitGin(rl, 30, time.Minute)

	pj := v1.Group("/projects", jwtm)
	{
		pj.GET("", h.ListProjects)
		pj.POST("", h.CreateProject)
		pj.GET("/:id", h.GetProject)
		pj.PATCH("/:id", h.UpdateProject)
		pj.DELETE("/:id", h.DeleteProject)
		pj.GET("/:id/assets", h.ListAssets)
		pj.POST("/:id/assets", uploadLimiter, h.dispatchAssetUpload)
		pj.POST("/:id/inference", h.SyncInference)
		pj.POST("/:id/inference/jobs", h.AsyncInference)
		pj.GET("/:id/jobs", h.ListProjectJobs)
	}

	as := v1.Group("/assets", jwtm)
	{
		as.GET("/:id", h.GetAsset)
		as.DELETE("/:id", h.DeleteAsset)
		as.POST("/:id/confirm", h.ConfirmAsset)
		as.GET("/:id/download", h.DownloadAsset)
	}

	md := v1.Group("/models", jwtm)
	{
		md.GET("", h.ListModels)
		md.POST("", adminMW, h.CreateModel)
		md.GET("/:id", h.GetModel)
		md.GET("/:id/versions", h.ListModelVersions)
		md.POST("/:id/versions", adminMW, h.CreateModelVersion)
	}
	v1.PATCH("/model-versions/:id/status", jwtm, adminMW, h.SetModelVersionStatus)

	inf := v1.Group("/inference", jwtm)
	{
		inf.POST("", h.CreateInference)
	}
	jobs := v1.Group("/jobs", jwtm)
	{
		jobs.GET("/:id", h.GetJob)
		jobs.GET("/:id/result", h.GetJobResult)
		jobs.POST("/:id/retry", h.RetryJob)
	}

	v1.GET("/dashboard/stats", jwtm, h.DashboardStats)
}

// dispatchAssetUpload switches between presign (JSON) and direct upload (multipart).
func (h *Handler) dispatchAssetUpload(c *gin.Context) {
	ct := c.GetHeader("Content-Type")
	if ct != "" && len(ct) >= 19 && ct[:19] == "multipart/form-data" {
		h.UploadAsset(c)
		return
	}
	h.PresignAsset(c)
}

// rateLimitGin returns a gin-native fixed-window limiter keyed on client IP.
func rateLimitGin(rl *middleware.RateLimiter, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := "ip:" + middleware.ClientIP(c) + ":" + c.FullPath()
		allowed, remaining, reset, err := rl.Allow(c.Request.Context(), key, limit, window)
		if err != nil {
			// Limper backend unavailable: fail open rather than take the API down,
			// but log so operators can see it.
			slog.Warn("rate limiter unavailable, failing open", "error", err)
			c.Next()
			return
		}
		c.Header("X-RateLimit-Limit", strconv.Itoa(limit))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
		c.Header("X-RateLimit-Reset", strconv.Itoa(reset))
		if !allowed {
			c.Header("Retry-After", strconv.Itoa(reset))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, vtypes.NewError("RATE_LIMITED", "too many requests", requestID(c)))
			return
		}
		c.Next()
	}
}

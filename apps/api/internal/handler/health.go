package handler

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/visionforge/visionforge/apps/api/internal/storage"
)

// Health is a liveness probe — always 200 as long as the process is up.
func Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// HealthDeps bundles the dependencies checked by the readiness probe.
type HealthDeps struct {
	DB    *sql.DB
	Redis *redis.Client
	Store storage.ObjectStorage
}

// Ready returns a readiness handler wired to the given dependencies.
func Ready(deps HealthDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		out := gin.H{"postgres": "ok", "redis": "ok", "object_storage": "ok"}
		status := http.StatusOK
		if err := deps.DB.PingContext(ctx); err != nil {
			out["postgres"] = "error"
			status = http.StatusServiceUnavailable
		}
		if deps.Redis != nil {
			if err := deps.Redis.Ping(ctx).Err(); err != nil {
				out["redis"] = "error"
				status = http.StatusServiceUnavailable
			}
		}
		if deps.Store != nil {
			if err := deps.Store.Health(ctx); err != nil {
				out["object_storage"] = "error"
				status = http.StatusServiceUnavailable
			}
		}
		c.JSON(status, gin.H{"status": map[bool]string{true: "ready", false: "degraded"}[status == http.StatusOK], "checks": out})
	}
}

// Command api runs the VisionForge REST API.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/visionforge/visionforge/apps/api/internal/auth"
	"github.com/visionforge/visionforge/apps/api/internal/db"
	"github.com/visionforge/visionforge/apps/api/internal/handler"
	"github.com/visionforge/visionforge/apps/api/internal/mlclient"
	"github.com/visionforge/visionforge/apps/api/internal/middleware"
	"github.com/visionforge/visionforge/apps/api/internal/observability"
	"github.com/visionforge/visionforge/apps/api/internal/queue"
	"github.com/visionforge/visionforge/apps/api/internal/repository"
	"github.com/visionforge/visionforge/apps/api/internal/service"
	"github.com/visionforge/visionforge/apps/api/internal/storage"
	"github.com/visionforge/visionforge/apps/api/internal/validator"
	"github.com/visionforge/visionforge/packages/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	log := observability.NewLogger(cfg.LogLevel, cfg.Environment)
	slog.SetDefault(log)
	_ = validator.Get() // initialise validator

	// ── Database ────────────────────────────────────────
	pg, err := db.Open(cfg.DB)
	if err != nil {
		return fmt.Errorf("db: %w", err)
	}
	defer pg.Close()

	// ── Redis ───────────────────────────────────────────
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr(),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping: %w", err)
	}

	// ── Object Storage ──────────────────────────────────
	store, err := storage.NewS3(context.Background(), cfg.Storage)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}

	// ── Auth ────────────────────────────────────────────
	hasher := auth.NewHasher(cfg.Auth.BcryptCost)
	jwtm := auth.NewJWTManager(cfg.Auth)

	// ── Metrics ─────────────────────────────────────────
	reg := prometheus.NewRegistry()
	reg.MustRegister(prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	metrics := observability.NewMetrics(reg)

	// ── Repository, Queue, ML client ────────────────────
	repos := repository.NewRepos(pg)
	q := queue.NewRedisQueue(rdb, cfg.Worker.QueueName)
	defer q.Close()
	mlc := mlclient.New(cfg.ML.ServiceURL, time.Duration(cfg.ML.MaxInferenceMs)*time.Millisecond)

	// ── Services ────────────────────────────────────────
	authSvc := service.NewAuthService(repos.Users, repos.RefreshTokens, repos.AuditLogs, hasher, jwtm, cfg.Auth.JWTRefreshTTL)
	projectSvc := service.NewProjectService(repos.Projects, repos.AuditLogs)
	assetSvc := service.NewAssetService(repos.Assets, repos.Projects, store, cfg.Limits, repos.AuditLogs)
	modelSvc := service.NewModelService(repos.Models, repos.ModelVersions, repos.AuditLogs)
	inferSvc := service.NewInferenceService(pg, repos.Jobs, repos.Results, repos.Assets, repos.Projects, repos.ModelVersions, q, store, mlc, repos.AuditLogs)
	inferSvc.OnJobCreated = metrics.JobsCreated.Inc

	// ── Gin Router ──────────────────────────────────────
	if cfg.Environment != "development" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	// Replace trusted proxies for real deployment config.
	_ = r.SetTrustedProxies(nil)

	// Global middleware.
	r.Use(middleware.Recovery())
	r.Use(middleware.RequestID())
	r.Use(middleware.StructuredLogger())
	r.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.HTTP.CORSOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Authorization", "Content-Type", "Idempotency-Key", "X-Request-ID"},
		ExposeHeaders:    []string{"X-Request-ID", "X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"},
		AllowCredentials: true,
		MaxAge:           10 * time.Minute,
	}))
	r.Use(middleware.MetricsMiddleware(metrics.HTTPRequestsTotal, metrics.HTTPRequestDuration))
	r.MaxMultipartMemory = cfg.Limits.MaxRequestBodyBytes

	// Standalone endpoints.
	r.GET("/health", handler.Health)
	r.GET("/ready", handler.Ready(handler.HealthDeps{DB: pg, Redis: rdb, Store: store}))
	r.GET("/metrics", gin.WrapH(promhttp.HandlerFor(reg, promhttp.HandlerOpts{EnableOpenMetrics: true})))

	// Rate limiters (Redis-backed).
	rl := middleware.NewRateLimiter(rdb)

	// API routes.
	h := handler.New(&handler.Services{
		Auth:      authSvc,
		Projects:  projectSvc,
		Assets:    assetSvc,
		Models:    modelSvc,
		Inference: inferSvc,
	})
	handler.RegisterRoutes(r, h,
		middleware.JWTAuth(jwtm),
		chain(middleware.JWTAuth(jwtm), middleware.RequireRoleString("ADMIN")),
		rl)

	// ── HTTP Server ─────────────────────────────────────
	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.HTTP.Host, cfg.HTTP.Port),
		Handler:      r,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  cfg.HTTP.IdleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("api listening", "addr", srv.Addr, "env", cfg.Environment)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// Apply rate limits for auth endpoints using gin wrapper:
	applyRateLimits(r, rl, cfg)

	// ── Graceful shutdown ──────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	select {
	case <-quit:
		log.Info("shutdown signal received")
	case err := <-errCh:
		log.Error("server error", "error", err)
		return err
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("server shutdown error", "error", err)
		return err
	}
	log.Info("server stopped cleanly")
	return nil
}

// chain combines multiple gin.HandlerFunc into a single handler that executes them in order.
func chain(hs ...gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, h := range hs {
			h(c)
			if c.IsAborted() {
				return
			}
		}
	}
}

func applyRateLimits(_ *gin.Engine, _ *middleware.RateLimiter, _ *config.Config) {
	// Rate limiting is applied per-route; kept as a hook for per-group limits.
	// Global middleware is inserted above via specific groups when needed.
}

// Reference unused imports in a way that doesn't trigger compilation failure if unused.
var _ = (*sql.DB)(nil)

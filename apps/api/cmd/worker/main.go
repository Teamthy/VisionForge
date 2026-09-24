// Command worker runs the VisionForge async inference worker.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/visionforge/visionforge/apps/api/internal/db"
	"github.com/visionforge/visionforge/apps/api/internal/mlclient"
	"github.com/visionforge/visionforge/apps/api/internal/observability"
	"github.com/visionforge/visionforge/apps/api/internal/queue"
	"github.com/visionforge/visionforge/apps/api/internal/repository"
	"github.com/visionforge/visionforge/apps/api/internal/storage"
	"github.com/visionforge/visionforge/apps/api/internal/worker"
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
		return err
	}
	log := observability.NewLogger(cfg.LogLevel, cfg.Environment)
	slog.SetDefault(log)

	pg, err := db.Open(cfg.DB)
	if err != nil {
		return fmt.Errorf("db: %w", err)
	}
	defer pg.Close()

	rdb := redis.NewClient(&redis.Options{
		Addr: cfg.Redis.Addr(), Password: cfg.Redis.Password, DB: cfg.Redis.DB,
	})
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis: %w", err)
	}

	store, err := storage.NewS3(context.Background(), cfg.Storage)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}

	q := queue.NewRedisQueue(rdb, cfg.Worker.QueueName)
	defer q.Close()

	repos := repository.New(pg)
	ml := mlclient.New(cfg.ML.ServiceURL, time.Duration(cfg.ML.MaxInferenceMs)*time.Millisecond)

	// Worker-local Prometheus registry served on WORKER_METRICS_PORT (default 8081).
	reg := prometheus.NewRegistry()
	reg.MustRegister(prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	mets := observability.NewMetrics(reg)
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{EnableOpenMetrics: true}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	metricsSrv := &http.Server{Addr: fmt.Sprintf(":%d", cfg.Worker.MetricsPort()), Handler: mux}
	go func() {
		log.Info("worker metrics listening", "addr", metricsSrv.Addr)
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("metrics server error", "error", err)
		}
	}()

	w := worker.New(pg, repos, q, store, ml, repos.AuditLogs, worker.Config{
		Concurrency:  cfg.Worker.Concurrency,
		PollInterval: cfg.Worker.PollInterval,
		MaxAttempts:  cfg.Worker.MaxAttempts,
	}, worker.Metrics{
		JobsSucceeded: mets.JobsSucceeded.Inc,
		JobsFailed:    mets.JobsFailed.Inc,
		JobsRetried:   mets.JobsRetried.Inc,
		ActiveJobs:    func(d float64) { mets.ActiveWorkers.Add(d) },
		InferenceHist: func(task string, secs float64) {
			mets.InferenceDuration.WithLabelValues(task).Observe(secs)
		},
		QueueDepth: func(d float64) { mets.QueueDepth.Set(d) },
	})

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	errCh := make(chan error, 1)
	go func() { errCh <- w.Run(rootCtx) }()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	select {
	case <-quit:
		log.Info("shutdown signal received")
		rootCancel()
	case err := <-errCh:
		return err
	}
	select {
	case <-errCh:
	case <-time.After(30 * time.Second):
		log.Warn("worker shutdown timed out")
	}
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	_ = metricsSrv.Shutdown(shutCtx)
	log.Info("worker stopped cleanly")
	return nil
}

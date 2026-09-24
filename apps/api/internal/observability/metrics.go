package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics bundles all prometheus metrics for the API server.
type Metrics struct {
	HTTPRequestsTotal   *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec
	HTTPErrorsTotal     *prometheus.CounterVec

	DBLatency *prometheus.HistogramVec
	RedisLatency *prometheus.HistogramVec

	JobsCreated    prometheus.Counter
	JobsSucceeded  prometheus.Counter
	JobsFailed     prometheus.Counter
	JobsRetried    prometheus.Counter
	QueueDepth     prometheus.Gauge
	ActiveWorkers  prometheus.Gauge

	InferenceDuration *prometheus.HistogramVec
	ModelInferenceDuration *prometheus.HistogramVec

	UploadFailures prometheus.Counter

	// Go runtime metrics are registered by default via prometheus default gatherer.
}

// NewMetrics registers and returns all application metrics.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	factory := promauto.With(reg)

	return &Metrics{
		HTTPRequestsTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "visionforge_http_requests_total",
			Help: "Total number of HTTP requests",
		}, []string{"method", "route", "status"}),

		HTTPRequestDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "visionforge_http_request_duration_seconds",
			Help:    "HTTP request latency in seconds",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),

		HTTPErrorsTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "visionforge_http_errors_total",
			Help: "Total HTTP 4xx/5xx errors",
		}, []string{"method", "route", "status"}),

		DBLatency: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "visionforge_db_latency_seconds",
			Help:    "Database query latency in seconds",
			Buckets: prometheus.DefBuckets,
		}, []string{"operation"}),

		RedisLatency: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "visionforge_redis_latency_seconds",
			Help:    "Redis operation latency in seconds",
			Buckets: prometheus.DefBuckets,
		}, []string{"operation"}),

		JobsCreated: factory.NewCounter(prometheus.CounterOpts{
			Name: "visionforge_jobs_created_total",
			Help: "Total inference jobs created",
		}),
		JobsSucceeded: factory.NewCounter(prometheus.CounterOpts{
			Name: "visionforge_jobs_succeeded_total",
			Help: "Total inference jobs completed successfully",
		}),
		JobsFailed: factory.NewCounter(prometheus.CounterOpts{
			Name: "visionforge_jobs_failed_total",
			Help: "Total inference jobs that permanently failed",
		}),
		JobsRetried: factory.NewCounter(prometheus.CounterOpts{
			Name: "visionforge_jobs_retried_total",
			Help: "Total job retry attempts",
		}),
		QueueDepth: factory.NewGauge(prometheus.GaugeOpts{
			Name: "visionforge_queue_depth",
			Help: "Current number of jobs in the ready queue",
		}),
		ActiveWorkers: factory.NewGauge(prometheus.GaugeOpts{
			Name: "visionforge_worker_active_jobs",
			Help: "Number of jobs currently being processed by workers",
		}),
		InferenceDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "visionforge_inference_duration_seconds",
			Help:    "End-to-end inference duration per job (seconds)",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
		}, []string{"task_type"}),
		ModelInferenceDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "visionforge_model_inference_duration_seconds",
			Help:    "Model-specific inference duration in seconds",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"model", "version"}),
		UploadFailures: factory.NewCounter(prometheus.CounterOpts{
			Name: "visionforge_upload_failures_total",
			Help: "Total asset upload failures",
		}),
	}
}

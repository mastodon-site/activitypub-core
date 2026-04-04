// Package observability sets up Prometheus metrics and HTTP instrumentation helpers.
package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsHandler serves Prometheus metrics in text exposition format.
func MetricsHandler() http.Handler {
	return promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{EnableOpenMetrics: true},
	)
}

var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "activitypub_core",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "HTTP requests processed.",
		},
		[]string{"method", "handler", "status"},
	)
	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "activitypub_core",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request latencies in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"method", "handler"},
	)
)

// InstrumentHandler wraps h with request count and duration metrics (low-cardinality handler label).
func InstrumentHandler(handlerLabel string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(rw, r)
		status := strconv.Itoa(rw.status)
		httpRequestsTotal.WithLabelValues(r.Method, handlerLabel, status).Inc()
		httpRequestDuration.WithLabelValues(r.Method, handlerLabel).Observe(time.Since(start).Seconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

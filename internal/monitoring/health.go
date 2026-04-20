package monitoring

import (
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// RequestStats tracks request statistics.
type RequestStats struct {
	Count       uint64
	Successes  uint64
	Errors      uint64
	TotalLatencyMs int64 // accumulated latency in ms
	mu          sync.Mutex
}

// Metrics holds global metrics for the agent.
type Metrics struct {
	requests   RequestStats
	startTime  time.Time
}

// Global metrics instance
var globalMetrics = &Metrics{startTime: time.Now()}

// RecordRequest records a request with its latency and success status.
func (m *Metrics) RecordRequest(latencyMs int64, success bool) {
	atomic.AddUint64(&m.requests.Count, 1)
	if success {
		atomic.AddUint64(&m.requests.Successes, 1)
	} else {
		atomic.AddUint64(&m.requests.Errors, 1)
	}
	atomic.AddInt64(&m.requests.TotalLatencyMs, latencyMs)
}

// Snapshot returns a point-in-time copy of metrics.
func (m *Metrics) Snapshot() MetricsSnapshot {
	count := atomic.LoadUint64(&m.requests.Count)
	successes := atomic.LoadUint64(&m.requests.Successes)
	errors := atomic.LoadUint64(&m.requests.Errors)
	totalLatency := atomic.LoadInt64(&m.requests.TotalLatencyMs)

	avgLatency := int64(0)
	if count > 0 {
		avgLatency = totalLatency / int64(count)
	}

	errorRate := 0.0
	if count > 0 {
		errorRate = float64(errors) / float64(count) * 100
	}

	return MetricsSnapshot{
		Requests:     count,
		Successes:    successes,
		Errors:       errors,
		AvgLatencyMs: avgLatency,
		ErrorRate:    errorRate,
		Uptime:       time.Since(m.startTime).String(),
	}
}

// MetricsSnapshot is a JSON-serializable metrics snapshot.
type MetricsSnapshot struct {
	Requests     uint64  `json:"requests"`
	Successes    uint64  `json:"successes"`
	Errors       uint64  `json:"errors"`
	AvgLatencyMs int64   `json:"avg_latency_ms"`
	ErrorRate    float64 `json:"error_rate_percent"`
	Uptime       string  `json:"uptime"`
}

// Handler returns an http.Handler for the health and metrics endpoints.
func (m *Metrics) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", m.healthHandler)
	mux.HandleFunc("/metrics", m.metricsHandler)
	return mux
}

func (m *Metrics) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

func (m *Metrics) metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(m.Snapshot())
}

// StartHealthServer starts the health and metrics HTTP server on the given address.
// Returns the server instance for graceful shutdown.
func StartHealthServer(addr string) *http.Server {
	mux := globalMetrics.Handler()
	srv := &http.Server{Addr: addr, Handler: mux}
	go srv.ListenAndServe()
	return srv
}

// RecordGlobalRequest is a convenience function to record a request in global metrics.
func RecordGlobalRequest(latencyMs int64, success bool) {
	globalMetrics.RecordRequest(latencyMs, success)
}

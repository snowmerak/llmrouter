package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// RequestsTotal tracks the total number of HTTP requests sent to backends.
	RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "llmrouter_requests_total",
			Help: "Total number of HTTP requests processed, labeled by node, model, status, and client_id.",
		},
		[]string{"node", "model", "status", "client_id"},
	)

	// RequestDuration tracks the latency of requests to backends.
	RequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "llmrouter_request_duration_seconds",
			Help:    "Histogram of response latency (seconds) of LLM requests.",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 20, 30, 60, 120},
		},
		[]string{"node", "model", "client_id"},
	)

	// ActiveRequests tracks the current number of in-flight requests per node.
	ActiveRequests = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "llmrouter_node_active_requests",
			Help: "Current number of active, in-flight requests per node.",
		},
		[]string{"node"},
	)

	// CircuitBreakerState tracks the state of the circuit breaker per node.
	// 0: Closed (Healthy), 1: Open (Failing), 2: Half-Open (Recovering)
	CircuitBreakerState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "llmrouter_circuit_breaker_state",
			Help: "Current state of the circuit breaker (0: Closed, 1: Open, 2: Half-Open).",
		},
		[]string{"node"},
	)

	// TokenUsageTotal tracks the number of tokens used.
	TokenUsageTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "llmrouter_token_usage_total",
			Help: "Total number of tokens processed, labeled by node, model, type (prompt/completion), and client_id.",
		},
		[]string{"node", "model", "type", "client_id"},
	)
)

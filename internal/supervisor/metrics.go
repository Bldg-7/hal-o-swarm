package supervisor

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the supervisor
type Metrics struct {
	// Counters
	CommandsTotal    prometheus.CounterVec
	EventsTotal      prometheus.CounterVec
	ConnectionsTotal prometheus.CounterVec
	ErrorsTotal      prometheus.CounterVec

	// Gauges
	ConnectionsActive prometheus.Gauge
	SessionsActive    prometheus.GaugeVec
	NodesOnline       prometheus.Gauge

	// Histograms
	CommandDuration         prometheus.HistogramVec
	EventProcessingDuration prometheus.HistogramVec

	mu sync.Mutex
}

var (
	globalMetrics *Metrics
	metricsOnce   sync.Once
)

// InitMetrics initializes global Prometheus metrics
func InitMetrics() *Metrics {
	metricsOnce.Do(func() {
		globalMetrics = &Metrics{
			CommandsTotal: *promauto.NewCounterVec(
				prometheus.CounterOpts{
					Name: "hal_o_swarm_commands_total",
					Help: "Total commands executed",
				},
				[]string{"type", "status"},
			),
			EventsTotal: *promauto.NewCounterVec(
				prometheus.CounterOpts{
					Name: "hal_o_swarm_events_total",
					Help: "Total events processed",
				},
				[]string{"type"},
			),
			ConnectionsTotal: *promauto.NewCounterVec(
				prometheus.CounterOpts{
					Name: "hal_o_swarm_connections_total",
					Help: "Total connections (accepted/rejected)",
				},
				[]string{"status"},
			),
			ErrorsTotal: *promauto.NewCounterVec(
				prometheus.CounterOpts{
					Name: "hal_o_swarm_errors_total",
					Help: "Total errors by component",
				},
				[]string{"component", "type"},
			),
			ConnectionsActive: promauto.NewGauge(
				prometheus.GaugeOpts{
					Name: "hal_o_swarm_connections_active",
					Help: "Current active connections",
				},
			),
			SessionsActive: *promauto.NewGaugeVec(
				prometheus.GaugeOpts{
					Name: "hal_o_swarm_sessions_active",
					Help: "Current sessions by status",
				},
				[]string{"status"},
			),
			NodesOnline: promauto.NewGauge(
				prometheus.GaugeOpts{
					Name: "hal_o_swarm_nodes_online",
					Help: "Current online nodes",
				},
			),
			CommandDuration: *promauto.NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "hal_o_swarm_command_duration_seconds",
					Help:    "Command execution duration",
					Buckets: prometheus.DefBuckets,
				},
				[]string{"type"},
			),
			EventProcessingDuration: *promauto.NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "hal_o_swarm_event_processing_duration_seconds",
					Help:    "Event processing duration",
					Buckets: prometheus.DefBuckets,
				},
				[]string{"type"},
			),
		}
	})
	return globalMetrics
}

// GetMetrics returns the global metrics instance
func GetMetrics() *Metrics {
	if globalMetrics == nil {
		return InitMetrics()
	}
	return globalMetrics
}

// RecordCommand records a command execution
func (m *Metrics) RecordCommand(commandType string, status string) {
	if m == nil {
		return
	}
	m.CommandsTotal.WithLabelValues(commandType, status).Inc()
}

// RecordCommandDuration records command execution duration
func (m *Metrics) RecordCommandDuration(commandType string, seconds float64) {
	if m == nil {
		return
	}
	m.CommandDuration.WithLabelValues(commandType).Observe(seconds)
}

// RecordEvent records an event
func (m *Metrics) RecordEvent(eventType string) {
	if m == nil {
		return
	}
	m.EventsTotal.WithLabelValues(eventType).Inc()
}

// RecordEventProcessingDuration records event processing duration
func (m *Metrics) RecordEventProcessingDuration(eventType string, seconds float64) {
	if m == nil {
		return
	}
	m.EventProcessingDuration.WithLabelValues(eventType).Observe(seconds)
}

// RecordConnection records a connection attempt
func (m *Metrics) RecordConnection(status string) {
	if m == nil {
		return
	}
	m.ConnectionsTotal.WithLabelValues(status).Inc()
}

// SetActiveConnections sets the current active connection count
func (m *Metrics) SetActiveConnections(count int64) {
	if m == nil {
		return
	}
	m.ConnectionsActive.Set(float64(count))
}

// SetActiveSessions sets the current active session count by status
func (m *Metrics) SetActiveSessions(status string, count int64) {
	if m == nil {
		return
	}
	m.SessionsActive.WithLabelValues(status).Set(float64(count))
}

// SetOnlineNodes sets the current online node count
func (m *Metrics) SetOnlineNodes(count int64) {
	if m == nil {
		return
	}
	m.NodesOnline.Set(float64(count))
}

// RecordError records an error
func (m *Metrics) RecordError(component string, errorType string) {
	if m == nil {
		return
	}
	m.ErrorsTotal.WithLabelValues(component, errorType).Inc()
}

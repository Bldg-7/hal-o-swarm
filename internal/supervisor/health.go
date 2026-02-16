package supervisor

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

// ComponentStatus represents the health status of a component
type ComponentStatus string

const (
	StatusOK          ComponentStatus = "ok"
	StatusError       ComponentStatus = "error"
	StatusUnavailable ComponentStatus = "unavailable"
)

// HealthStatus represents the overall health status
type HealthStatus string

const (
	HealthHealthy   HealthStatus = "healthy"
	HealthDegraded  HealthStatus = "degraded"
	HealthUnhealthy HealthStatus = "unhealthy"
)

// ComponentHealth holds the health status of a single component
type ComponentHealth struct {
	Status ComponentStatus `json:"status"`
	Error  string          `json:"error,omitempty"`
}

// HealthCheckResult holds the result of a health check
type HealthCheckResult struct {
	Status     HealthStatus               `json:"status"`
	Components map[string]ComponentHealth `json:"components"`
	Timestamp  time.Time                  `json:"timestamp"`
}

// HealthChecker performs health checks on supervisor components
type HealthChecker struct {
	db         *sql.DB
	hub        *Hub
	dispatcher *CommandDispatcher
	costs      *CostAggregator
	mu         sync.RWMutex
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(db *sql.DB, hub *Hub, dispatcher *CommandDispatcher, costs *CostAggregator) *HealthChecker {
	return &HealthChecker{
		db:         db,
		hub:        hub,
		dispatcher: dispatcher,
		costs:      costs,
	}
}

// CheckLiveness performs a liveness check (always returns healthy if server is running)
func (hc *HealthChecker) CheckLiveness(ctx context.Context) HealthCheckResult {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	return HealthCheckResult{
		Status:     HealthHealthy,
		Components: map[string]ComponentHealth{},
		Timestamp:  time.Now().UTC(),
	}
}

// CheckReadiness performs a readiness check (checks all components)
func (hc *HealthChecker) CheckReadiness(ctx context.Context) HealthCheckResult {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	components := make(map[string]ComponentHealth)

	// Check database
	dbStatus := hc.checkDatabase(ctx)
	components["database"] = dbStatus

	// Check WebSocket hub
	hubStatus := hc.checkHub(ctx)
	components["websocket_hub"] = hubStatus

	// Check command dispatcher
	dispatcherStatus := hc.checkDispatcher(ctx)
	components["command_dispatcher"] = dispatcherStatus

	// Check cost aggregator
	costStatus := hc.checkCostAggregator(ctx)
	components["cost_aggregator"] = costStatus

	// Determine overall status
	overallStatus := HealthHealthy
	for _, comp := range components {
		if comp.Status == StatusError {
			overallStatus = HealthUnhealthy
			break
		}
		if comp.Status == StatusUnavailable {
			overallStatus = HealthDegraded
		}
	}

	return HealthCheckResult{
		Status:     overallStatus,
		Components: components,
		Timestamp:  time.Now().UTC(),
	}
}

// checkDatabase checks the database connection
func (hc *HealthChecker) checkDatabase(ctx context.Context) ComponentHealth {
	if hc.db == nil {
		return ComponentHealth{
			Status: StatusUnavailable,
			Error:  "database not configured",
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if err := hc.db.PingContext(ctx); err != nil {
		return ComponentHealth{
			Status: StatusError,
			Error:  err.Error(),
		}
	}

	return ComponentHealth{Status: StatusOK}
}

// checkHub checks the WebSocket hub
func (hc *HealthChecker) checkHub(ctx context.Context) ComponentHealth {
	if hc.hub == nil {
		return ComponentHealth{
			Status: StatusUnavailable,
			Error:  "websocket hub not configured",
		}
	}

	// Check if hub is running by checking client count
	if hc.hub.ClientCount() >= 0 {
		return ComponentHealth{Status: StatusOK}
	}

	return ComponentHealth{
		Status: StatusError,
		Error:  "websocket hub not responding",
	}
}

// checkDispatcher checks the command dispatcher
func (hc *HealthChecker) checkDispatcher(ctx context.Context) ComponentHealth {
	if hc.dispatcher == nil {
		return ComponentHealth{
			Status: StatusUnavailable,
			Error:  "command dispatcher not configured",
		}
	}

	return ComponentHealth{Status: StatusOK}
}

// checkCostAggregator checks the cost aggregator
func (hc *HealthChecker) checkCostAggregator(ctx context.Context) ComponentHealth {
	if hc.costs == nil {
		return ComponentHealth{
			Status: StatusUnavailable,
			Error:  "cost aggregator not configured",
		}
	}

	return ComponentHealth{Status: StatusOK}
}

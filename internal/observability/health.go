package observability

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Check represents a dependency health probe.
type Check struct {
	Name string
	Fn   func(ctx context.Context) error
}

// HealthStatus aggregates the results of all checks.
type HealthStatus struct {
	Status    string            `json:"status"`
	Timestamp time.Time         `json:"timestamp"`
	Checks    map[string]string `json:"checks"`
}

// HealthCollector holds dependency checks for /health and /ready endpoints.
type HealthCollector struct {
	mu     sync.RWMutex
	checks []Check
}

// NewHealthCollector creates a collector.
func NewHealthCollector() *HealthCollector {
	return &HealthCollector{}
}

// Register adds a dependency check.
func (h *HealthCollector) Register(c Check) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checks = append(h.checks, c)
}

// Check runs all probes with a timeout.
func (h *HealthCollector) Check(ctx context.Context) HealthStatus {
	h.mu.RLock()
	checks := make([]Check, len(h.checks))
	copy(checks, h.checks)
	h.mu.RUnlock()

	status := HealthStatus{
		Status:    "healthy",
		Timestamp: time.Now().UTC(),
		Checks:    make(map[string]string, len(checks)),
	}

	for _, c := range checks {
		checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := c.Fn(checkCtx)
		cancel()
		if err != nil {
			status.Checks[c.Name] = "unhealthy: " + err.Error()
			status.Status = "unhealthy"
		} else {
			status.Checks[c.Name] = "ok"
		}
	}
	if len(checks) == 0 {
		status.Checks["noop"] = "ok"
	}
	return status
}

// Handler returns a Gin handler that reports liveness.
func (h *HealthCollector) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "nexa"})
	}
}

// ReadyHandler returns a Gin handler that reports readiness based on checks.
func (h *HealthCollector) ReadyHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		status := h.Check(c.Request.Context())
		code := http.StatusOK
		if status.Status != "healthy" {
			code = http.StatusServiceUnavailable
		}
		c.JSON(code, status)
	}
}

// PostgresCheck pings a PostgreSQL pool.
func PostgresCheck(db *sql.DB) Check {
	return Check{
		Name: "postgres",
		Fn: func(ctx context.Context) error {
			if db == nil {
				return fmt.Errorf("db not configured")
			}
			return db.PingContext(ctx)
		},
	}
}

// RedisCheck pings a Redis client (uses a minimal interface).
type RedisPinger interface {
	Ping(ctx context.Context) error
}

func RedisCheck(client RedisPinger) Check {
	return Check{
		Name: "redis",
		Fn: func(ctx context.Context) error {
			if client == nil {
				return fmt.Errorf("redis not configured")
			}
			return client.Ping(ctx)
		},
	}
}

// SimpleCheck is a convenience helper for static checks.
func SimpleCheck(name string, fn func() error) Check {
	return Check{
		Name: name,
		Fn: func(ctx context.Context) error {
			return fn()
		},
	}
}

// ExchangeChecker is implemented by the matching exchange facade.
type ExchangeChecker interface {
	EngineCount() int
}

// ExchangeHealthCheck verifies that at least one matching engine is registered.
func ExchangeHealthCheck(ex ExchangeChecker) Check {
	return Check{
		Name: "matching_engines",
		Fn: func(ctx context.Context) error {
			if ex == nil {
				return fmt.Errorf("exchange not configured")
			}
			if ex.EngineCount() == 0 {
				return fmt.Errorf("no matching engines registered")
			}
			return nil
		},
	}
}

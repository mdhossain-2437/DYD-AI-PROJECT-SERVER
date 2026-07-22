// Health probes. Kept separate from the business handlers because they answer a
// different question: liveness ("is the process up?") vs. readiness ("can it
// actually serve traffic — are its dependencies reachable?"). Kubernetes /
// load-balancer health checks and the rate limiter both special-case these
// paths, so they must stay cheap and dependency-aware.
package handlers

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
)

// Pinger is anything with a context-aware Ping — satisfied by database.DB and
// redisstore.Storage. Declared here (consumer side) so the health handler
// depends on behavior, not concrete types, and a nil Redis is simply skipped.
type Pinger interface {
	Ping(ctx context.Context) error
}

type Health struct {
	db    Pinger
	redis Pinger // may be nil when running with in-memory rate limiting
}

func NewHealth(db, redis Pinger) *Health {
	return &Health{db: db, redis: redis}
}

// Live answers the liveness probe: if the process can handle this request at
// all, it is alive. Deliberately does NOT touch the database — a DB blip must
// not cause the orchestrator to kill and restart otherwise-healthy pods.
func (h *Health) Live(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "ok"})
}

// Ready answers the readiness probe: only report ready if every backing
// dependency is reachable, so the load balancer stops routing traffic here
// while we're degraded instead of serving errors.
func (h *Health) Ready(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
	defer cancel()

	checks := fiber.Map{}
	ready := true

	if err := h.db.Ping(ctx); err != nil {
		checks["database"] = "unavailable"
		ready = false
	} else {
		checks["database"] = "ok"
	}

	if h.redis != nil {
		if err := h.redis.Ping(ctx); err != nil {
			checks["redis"] = "unavailable"
			ready = false
		} else {
			checks["redis"] = "ok"
		}
	}

	status := fiber.StatusOK
	if !ready {
		status = fiber.StatusServiceUnavailable
	}
	return c.Status(status).JSON(fiber.Map{"ready": ready, "checks": checks})
}

// Package middleware assembles the security and reliability middleware stack.
// It is layered deliberately: recover (never crash) → request id (traceability)
// → structured access log → security headers → CORS → body limit → timeout →
// rate limiting. Rate limiting is Redis-backed when a Redis storage is provided
// so limits hold ACROSS all instances behind the load balancer, not per-process.
package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/helmet"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/rs/zerolog"
)

// SecurityHeaders applies a strict set of HTTP security headers via helmet plus
// a couple this API specifically wants. This is a JSON API, so the CSP is
// locked down hard — no scripts, no framing.
func SecurityHeaders() fiber.Handler {
	return helmet.New(helmet.Config{
		XSSProtection:             "0", // deprecated header; modern browsers ignore, CSP covers it
		ContentTypeNosniff:        "nosniff",
		XFrameOptions:             "DENY",
		ReferrerPolicy:            "no-referrer",
		CrossOriginOpenerPolicy:   "same-origin",
		CrossOriginResourcePolicy: "same-site",
		ContentSecurityPolicy:     "default-src 'none'; frame-ancestors 'none'",
		HSTSMaxAge:                63072000, // 2 years
		// HSTSExcludeSubdomains defaults to false, so `includeSubDomains` is emitted.
		HSTSPreloadEnabled: true,
		PermissionPolicy:   "geolocation=(), microphone=(), camera=()",
	})
}

// CORS restricts cross-origin access to the configured frontend origins only.
func CORS(allowedOrigins []string) fiber.Handler {
	origins := "*"
	allowCreds := false
	if len(allowedOrigins) > 0 {
		origins = joinOrigins(allowedOrigins)
		allowCreds = true
	}
	return cors.New(cors.Config{
		AllowOrigins:     origins,
		AllowMethods:     "GET,POST,OPTIONS",
		AllowHeaders:     "Origin,Content-Type,Accept,X-Requested-With,CF-Turnstile-Response",
		AllowCredentials: allowCreds,
		MaxAge:           600,
	})
}

// RequestID attaches a UUID to every request for cross-log correlation.
func RequestID() fiber.Handler {
	return requestid.New()
}

// requestIDOf pulls the request id the requestid middleware stored in Locals.
// Fiber v2 has no requestid.FromContext helper — the value lives under the
// middleware's configured context key (default "requestid").
func requestIDOf(c *fiber.Ctx) string {
	if v, ok := c.Locals(requestid.ConfigDefault.ContextKey).(string); ok {
		return v
	}
	return ""
}

// Recover turns any panic into a 500 instead of taking the process down — a
// crashing handler must never become downtime.
func Recover(log zerolog.Logger) fiber.Handler {
	return recover.New(recover.Config{
		EnableStackTrace: true,
		StackTraceHandler: func(c *fiber.Ctx, e any) {
			log.Error().
				Str("request_id", requestIDOf(c)).
				Str("path", c.Path()).
				Interface("panic", e).
				Msg("recovered from panic")
		},
	})
}

// AccessLog emits one structured JSON line per request. It records status,
// latency, method, path, and request id — but never query strings or bodies,
// which could contain PII.
func AccessLog(log zerolog.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		ev := log.Info()
		if c.Response().StatusCode() >= 500 {
			ev = log.Error()
		} else if c.Response().StatusCode() >= 400 {
			ev = log.Warn()
		}
		ev.
			Str("request_id", requestIDOf(c)).
			Str("method", c.Method()).
			Str("path", c.Path()).
			Int("status", c.Response().StatusCode()).
			Dur("latency", time.Since(start)).
			Str("ip", c.IP()).
			Msg("request")
		return err
	}
}

// RateLimiterConfig configures a limiter instance.
type RateLimiterConfig struct {
	Max        int
	Expiration time.Duration
	Storage    fiber.Storage // Redis-backed for cross-instance limits; nil = in-memory
	Name       string
}

// RateLimiter builds a limiter keyed by client IP. Exceeding the limit returns
// 429 with a JSON body. Health checks are skipped so probes never get throttled.
func RateLimiter(cfg RateLimiterConfig) fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        cfg.Max,
		Expiration: cfg.Expiration,
		Storage:    cfg.Storage,
		KeyGenerator: func(c *fiber.Ctx) string {
			return cfg.Name + ":" + c.IP()
		},
		Next: func(c *fiber.Ctx) bool {
			p := c.Path()
			return p == "/healthz" || p == "/readyz"
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "rate_limited",
				"message": "Too many requests. Please slow down and try again shortly.",
			})
		},
	})
}

func joinOrigins(origins []string) string {
	out := ""
	for i, o := range origins {
		if i > 0 {
			out += ","
		}
		out += o
	}
	return out
}

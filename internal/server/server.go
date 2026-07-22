// Package server assembles the Fiber application: it wires the middleware stack
// (in the deliberate order documented in the middleware package), mounts the
// routes, and owns graceful startup/shutdown. Nothing here talks to the
// database or crypto directly — it receives already-constructed handlers and
// just plugs them into the HTTP surface.
package server

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"

	"github.com/dyd/dyd-server/internal/config"
	"github.com/dyd/dyd-server/internal/handlers"
	"github.com/dyd/dyd-server/internal/middleware"
)

// minute is the window all per-endpoint rate limits are expressed against.
const minute = time.Minute

// Deps are everything the HTTP layer needs, constructed by main and handed in.
type Deps struct {
	Config       *config.Config
	Log          zerolog.Logger
	Handler      *handlers.Handler
	Health       *handlers.Health
	LimiterStore fiber.Storage // Redis-backed for cross-instance limits; nil = in-memory
}

// New builds the configured Fiber app with the full middleware stack and routes.
func New(d Deps) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:               "dyd-api",
		ReadTimeout:           d.Config.ReadTimeout,
		WriteTimeout:          d.Config.WriteTimeout,
		IdleTimeout:           d.Config.IdleTimeout,
		BodyLimit:             d.Config.BodyLimitBytes,
		DisableStartupMessage: true,
		// We front this behind Caddy/Cloudflare, so trust proxy headers only from
		// the configured hops — otherwise c.IP() (our rate-limit key) is spoofable.
		EnableTrustedProxyCheck: len(d.Config.TrustedProxies) > 0,
		TrustedProxies:          d.Config.TrustedProxies,
		ProxyHeader:             fiber.HeaderXForwardedFor,
		// A JSON API: any unhandled error becomes a clean JSON 500, never HTML.
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}
			d.Log.Error().Err(err).Int("status", code).Str("path", c.Path()).Msg("unhandled error")
			return c.Status(code).JSON(fiber.Map{
				"error":   "internal_error",
				"message": "Something went wrong. Please try again.",
			})
		},
	})

	// ---- middleware stack (order matters — see package middleware) ----------
	app.Use(middleware.Recover(d.Log))
	app.Use(middleware.RequestID())
	app.Use(middleware.AccessLog(d.Log))
	app.Use(middleware.SecurityHeaders())
	app.Use(middleware.CORS(d.Config.CORSAllowedOrigins))

	// A broad limiter over everything guards against blunt flooding; the tighter
	// per-endpoint limiters below stack on top for the sensitive write paths.
	app.Use(middleware.RateLimiter(middleware.RateLimiterConfig{
		Max:        d.Config.RateGeneralPerMin,
		Expiration: minute,
		Storage:    d.LimiterStore,
		Name:       "general",
	}))

	// ---- health probes (no rate limit — see RateLimiter.Next) ---------------
	app.Get("/healthz", d.Health.Live)
	app.Get("/readyz", d.Health.Ready)

	// ---- API v1 -------------------------------------------------------------
	v1 := app.Group("/v1")

	v1.Get("/geo", d.Handler.Geo)

	submitLimit := middleware.RateLimiter(middleware.RateLimiterConfig{
		Max: d.Config.RateSubmitPerMin, Expiration: minute, Storage: d.LimiterStore, Name: "submit",
	})
	lookupLimit := middleware.RateLimiter(middleware.RateLimiterConfig{
		Max: d.Config.RateLookupPerMin, Expiration: minute, Storage: d.LimiterStore, Name: "lookup",
	})

	v1.Post("/applications", submitLimit, d.Handler.CreateApplication)
	v1.Post("/admit-card/lookup", lookupLimit, d.Handler.LookupAdmitCard)
	v1.Post("/contact", submitLimit, d.Handler.CreateContact)
	v1.Post("/newsletter", submitLimit, d.Handler.Subscribe)

	// ---- 404 fallback -------------------------------------------------------
	app.Use(func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error":   "not_found",
			"message": "The requested resource does not exist.",
		})
	})

	return app
}

// Shutdown gives in-flight requests up to the configured timeout to finish
// before the listener is torn down — the core of zero-downtime deploys.
func Shutdown(ctx context.Context, app *fiber.App) error {
	return app.ShutdownWithContext(ctx)
}

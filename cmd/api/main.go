// Command api is the entry point for the DYD admissions API. It wires the
// dependency graph in one place — config → logger → database → redis → crypto →
// repository → handlers → HTTP server — then runs the server until a shutdown
// signal arrives and drains in-flight requests gracefully (zero-downtime).
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dyd/dyd-server/internal/config"
	"github.com/dyd/dyd-server/internal/crypto"
	"github.com/dyd/dyd-server/internal/database"
	"github.com/dyd/dyd-server/internal/handlers"
	"github.com/dyd/dyd-server/internal/logger"
	"github.com/dyd/dyd-server/internal/redisstore"
	"github.com/dyd/dyd-server/internal/repository"
	"github.com/dyd/dyd-server/internal/server"
	"github.com/dyd/dyd-server/internal/turnstile"
	"github.com/dyd/dyd-server/internal/validate"

	"github.com/gofiber/fiber/v2"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		// No logger yet — this is a fatal misconfiguration; write plainly and exit.
		println("config error:", err.Error())
		os.Exit(1)
	}

	log := logger.New(cfg.Prod)
	log.Info().Str("env", cfg.Env).Str("port", cfg.HTTPPort).Msg("starting dyd-api")

	// ---- database (fail fast) ----------------------------------------------
	dbCtx, dbCancel := context.WithTimeout(context.Background(), 10*time.Second)
	db, err := database.Connect(dbCtx, database.Options{
		PrimaryURL:      cfg.DatabaseURL,
		ReplicaURL:      cfg.DatabaseReplicaURL,
		MaxConns:        cfg.DBMaxConns,
		MinConns:        cfg.DBMinConns,
		MaxConnLifetime: cfg.DBMaxConnLifetime,
	}, log)
	dbCancel()
	if err != nil {
		log.Fatal().Err(err).Msg("database connection failed")
	}
	defer db.Close()

	// ---- schema migrations (idempotent; on by default) ---------------------
	// Applies the embedded SQL so a fresh managed database is usable without a
	// manual psql step. Disable with AUTO_MIGRATE=false to manage schema yourself.
	if cfg.AutoMigrate {
		migCtx, migCancel := context.WithTimeout(context.Background(), 60*time.Second)
		err := db.Migrate(migCtx, log)
		migCancel()
		if err != nil {
			log.Fatal().Err(err).Msg("schema migration failed")
		}
	}

	// ---- redis (optional; nil => in-memory rate limiting) ------------------
	redisStore, err := redisstore.Connect(cfg.RedisURL, log)
	if err != nil {
		log.Fatal().Err(err).Msg("redis connection failed")
	}
	if redisStore != nil {
		defer redisStore.Close()
	} else {
		log.Warn().Msg("no REDIS_URL set: rate limiting is per-instance (in-memory) only")
	}

	// ---- crypto -------------------------------------------------------------
	cipher, err := crypto.NewCipher(cfg.PIIEncryptionKey)
	if err != nil {
		log.Fatal().Err(err).Msg("cipher init failed")
	}
	bidx := crypto.NewBlindIndex(cfg.BlindIndexKey)

	// ---- application wiring -------------------------------------------------
	repo := repository.New(db, cipher, bidx, cfg.CurrentBatch)
	validator := validate.New()
	ts := turnstile.New(cfg.TurnstileSecretKey, cfg.TurnstileEnabled)
	h := handlers.New(repo, validator, ts, log)

	// Avoid the typed-nil interface trap: only hand the health handler a Redis
	// Pinger when one actually exists, so Ready() skips the check cleanly.
	var redisPinger handlers.Pinger
	if redisStore != nil {
		redisPinger = redisStore
	}
	health := handlers.NewHealth(db, redisPinger)

	// Same trap for the limiter Storage: Fiber treats a non-nil interface
	// holding a nil pointer as "use this store" and would panic. Pass real nil.
	var limiterStore fiber.Storage
	if redisStore != nil {
		limiterStore = redisStore
	}

	app := server.New(server.Deps{
		Config:       cfg,
		Log:          log,
		Handler:      h,
		Health:       health,
		LimiterStore: limiterStore,
	})

	// ---- run with graceful shutdown ----------------------------------------
	serverErr := make(chan error, 1)
	go func() {
		if err := app.Listen(":" + cfg.HTTPPort); err != nil {
			serverErr <- err
		}
	}()
	log.Info().Msgf("listening on :%s", cfg.HTTPPort)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		log.Fatal().Err(err).Msg("server failed to start")
	case sig := <-stop:
		log.Info().Str("signal", sig.String()).Msg("shutdown signal received; draining connections")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx, app); err != nil {
		log.Error().Err(err).Msg("graceful shutdown failed; forcing exit")
		os.Exit(1)
	}
	log.Info().Msg("shutdown complete")
}

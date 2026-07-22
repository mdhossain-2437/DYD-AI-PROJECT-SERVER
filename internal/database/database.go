// Package database wires up PostgreSQL connection pools via pgx. It supports an
// optional read replica: writes always go to the primary; reads can be routed
// to a replica to scale read-heavy traffic (admit-card lookups) without loading
// the primary. If no replica URL is configured, reads fall back to the primary.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

type DB struct {
	Primary *pgxpool.Pool
	Replica *pgxpool.Pool // == Primary when no replica configured
	log     zerolog.Logger
}

type Options struct {
	PrimaryURL      string
	ReplicaURL      string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
}

// Connect opens the primary pool (and replica, if configured) and verifies each
// with a ping. It fails fast so a bad DATABASE_URL aborts startup.
func Connect(ctx context.Context, opts Options, log zerolog.Logger) (*DB, error) {
	primary, err := open(ctx, opts.PrimaryURL, opts)
	if err != nil {
		return nil, fmt.Errorf("primary db: %w", err)
	}

	db := &DB{Primary: primary, Replica: primary, log: log}

	if opts.ReplicaURL != "" {
		replica, err := open(ctx, opts.ReplicaURL, opts)
		if err != nil {
			primary.Close()
			return nil, fmt.Errorf("replica db: %w", err)
		}
		db.Replica = replica
		log.Info().Msg("read replica connected")
	} else {
		log.Info().Msg("no read replica configured; reads served by primary")
	}

	return db, nil
}

func open(ctx context.Context, url string, opts Options) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	if opts.MaxConns > 0 {
		cfg.MaxConns = opts.MaxConns
	}
	if opts.MinConns > 0 {
		cfg.MinConns = opts.MinConns
	}
	if opts.MaxConnLifetime > 0 {
		cfg.MaxConnLifetime = opts.MaxConnLifetime
	}
	cfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// Ping verifies the primary is reachable — used by the readiness probe.
func (db *DB) Ping(ctx context.Context) error {
	return db.Primary.Ping(ctx)
}

// Close tears down both pools.
func (db *DB) Close() {
	if db.Replica != nil && db.Replica != db.Primary {
		db.Replica.Close()
	}
	if db.Primary != nil {
		db.Primary.Close()
	}
}

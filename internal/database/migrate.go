package database

import (
	"context"
	"fmt"
	"io/fs"
	"sort"

	"github.com/rs/zerolog"

	"github.com/dyd/dyd-server/migrations"
)

// Migrate applies every embedded *.sql migration to the primary database, in
// lexical filename order (0001_, 0002_, …). The migration files are written to
// be idempotent (CREATE TABLE/INDEX IF NOT EXISTS, ADD COLUMN IF NOT EXISTS), so
// running them on every startup is safe and needs no version-tracking table.
//
// Each file may contain multiple statements, so we execute it over pgx's simple
// query protocol (conn.Exec on the raw pgconn), which accepts multi-statement
// SQL — the normal pooled Exec uses the extended protocol and would reject it.
//
// Writes go to the primary only; a fresh managed database with no shell (e.g.
// Render's free tier) becomes fully usable without any manual psql step.
func (db *DB) Migrate(ctx context.Context, log zerolog.Logger) error {
	entries, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("no embedded migrations found")
	}
	sort.Strings(entries)

	conn, err := db.Primary.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn for migrate: %w", err)
	}
	defer conn.Release()

	for _, name := range entries {
		sqlBytes, err := migrations.FS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		// Raw pgconn Exec runs the whole file (multiple statements) at once.
		mrr := conn.Conn().PgConn().Exec(ctx, string(sqlBytes))
		if _, err := mrr.ReadAll(); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		log.Info().Str("migration", name).Msg("migration applied")
	}
	log.Info().Int("count", len(entries)).Msg("all migrations applied")
	return nil
}

// Package logger provides a single configured zerolog.Logger for the whole
// service. Logs are structured JSON in production (machine-parseable for log
// aggregation) and human-friendly console output in development. PII is never
// logged — callers must pass identifiers (request id, roll no), never names,
// phones, or emails.
package logger

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

// New builds the root logger. `prod` selects JSON vs. console output.
func New(prod bool) zerolog.Logger {
	zerolog.TimeFieldFormat = time.RFC3339Nano

	if prod {
		return zerolog.New(os.Stdout).
			Level(zerolog.InfoLevel).
			With().
			Timestamp().
			Str("service", "dyd-api").
			Logger()
	}

	cw := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.Kitchen}
	return zerolog.New(cw).
		Level(zerolog.DebugLevel).
		With().
		Timestamp().
		Logger()
}

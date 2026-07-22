// Package migrations embeds the .sql schema files into the binary so they can
// be applied at startup without shipping a separate migration tool or needing a
// shell in the (distroless) runtime image. The files stay authoritative SQL —
// this just makes them reachable from Go via an embed.FS.
package migrations

import "embed"

// FS holds every migration file, applied in lexical filename order (0001, 0002…).
//
//go:embed *.sql
var FS embed.FS

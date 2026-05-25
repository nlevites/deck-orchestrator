// Package executormigrations exposes the executor's SQLite migrations as an
// embed.FS for goose. Mirrors the orchestrator-side pattern in
// backend/sql/orchestrator.
package executormigrations

import "embed"

//go:embed *.sql
var FS embed.FS

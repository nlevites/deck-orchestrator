// Package orchestratormigrations exposes the orchestrator's SQLite migrations
// as an embed.FS for goose. Mirrors the executor-side pattern in
// backend/sql/executor.
//
// The SQL files in this directory are the source of truth for the schema;
// sqlc consumes them via backend/sqlc.yaml, and the store package consumes
// them via this FS.
package orchestratormigrations

import "embed"

//go:embed *.sql
var FS embed.FS

// Package migrations holds the MySQL schema files applied on startup.
package migrations

import "embed"

// FS contains ordered *.sql migrations. schema_migrations is created by the runner.
//
//go:embed *.sql
var FS embed.FS

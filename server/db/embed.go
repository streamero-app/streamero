// Package dbfs embeds the SQL migrations so the binary is self-contained.
package dbfs

import "embed"

// Migrations holds goose SQL migrations.
//
//go:embed migrations/*.sql
var Migrations embed.FS

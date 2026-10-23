// Package migrations embeds the canonical outbox_messages DDL so that
// services and test suites can apply the schema without copying SQL files.
//
// Usage in a test suite (via postgrestest.OpenConfig.MigrationsFS):
//
//	import outboxmigrations "github.com/whagency/core-common/outbox/postgres/migrations"
//
//	db := suite.OpenGormDB(t, postgrestest.OpenConfig{
//	    MigrationsFS: outboxmigrations.FS,
//	})
//
// # Version numbering
//
// golang-migrate keys migrations by the integer prefix and rejects duplicates,
// so a shared migration cannot use the low numbers services allocate for their
// own schema. These files therefore live in a reserved 9_000_000+ band, which
// lets a service combine its own MigrationsDir with this FS without collisions.
// Keep any future shared migration inside that band.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS

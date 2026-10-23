// Package migrations embeds the canonical ledger_compensations DDL so services
// and test suites can apply the schema without copying SQL files.
//
// # Version numbering
//
// golang-migrate keys migrations by the integer prefix and rejects duplicates, so
// a shared migration cannot use the low numbers services allocate for their own
// schema. These files live in the same reserved 9_000_000+ band as the outbox
// migrations, but in a separate FS with its own history table: a service that
// wants the outbox without compensation should not inherit an unused table.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS

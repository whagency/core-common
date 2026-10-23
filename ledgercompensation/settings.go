package ledgercompensation

import "time"

// Settings is the environment-driven configuration for a service's compensation
// runtime.
//
// It lives here rather than in each service for the same reason outbox.Settings
// does: adopting services share one dashboard and one set of alert rules, and
// those only describe every service correctly while the numbers agree. Override
// through the environment when a service genuinely needs a different value, and
// say why.
//
// Nest it in the service's own cron settings; envconfig recurses into it and
// reads the tags below without adding a prefix:
//
//	type CronSettings struct {
//	    // ... service-owned jobs ...
//	    LedgerCompensation ledgercompensation.Settings
//	}
type Settings struct {
	// ResolveSpec schedules the job that reverses orphaned postings.
	ResolveSpec string `envconfig:"CRON_LEDGER_COMPENSATION_SPEC" default:"@every 1m"`

	// Grace is how long a row may stay open before it counts as an orphan.
	//
	// Open is the normal state for the length of one guarded request, so this must
	// comfortably exceed that: too short and the resolver races a transaction that
	// is about to commit, reversing a payment that was going to succeed. Two
	// minutes is roughly an order of magnitude above a checkout confirmation,
	// which is bounded by a handful of gRPC calls and one transaction.
	//
	// It is also the delay before a genuinely orphaned posting is reversed, so it
	// should not be padded beyond what that safety margin needs.
	Grace time.Duration `envconfig:"CRON_LEDGER_COMPENSATION_GRACE" default:"2m"`

	// BatchSize bounds how many rows one resolver run claims.
	BatchSize int `envconfig:"CRON_LEDGER_COMPENSATION_BATCH_SIZE" default:"100"`

	// Retention is how long resolved rows are kept. They are the audit trail for
	// every reversal the service requested, so this is deliberately longer than
	// the outbox retention.
	Retention time.Duration `envconfig:"CRON_LEDGER_COMPENSATION_RETENTION" default:"720h"` // 30 days

	// StatsSpec schedules the open-row gauge sample. This is the primary
	// "postings are piling up unaccounted for" alert: counters cannot show it,
	// because a stalled resolver simply stops incrementing them, which is
	// indistinguishable from having nothing to compensate.
	StatsSpec string `envconfig:"CRON_LEDGER_COMPENSATION_STATS_SPEC" default:"@every 1m"`

	// PruneSpec schedules deletion of resolved rows past Retention.
	PruneSpec string `envconfig:"CRON_LEDGER_COMPENSATION_PRUNE_SPEC" default:"@daily"`
}

package outbox

import "time"

// Settings is the environment-driven configuration for a service's outbox
// runtime.
//
// It lives here rather than in each service because the numbers are not
// independent tuning knobs: services that adopt the outbox share one Grafana
// dashboard and one set of alert rules, and those only describe every service
// correctly while the defaults agree. A service that needs a different value
// should override it through the environment and say why, not fork the struct.
//
// Nest it in the service's own cron settings; envconfig recurses into it and
// reads the tags below without adding a prefix:
//
//	type CronSettings struct {
//	    // ... service-owned jobs ...
//	    Outbox outbox.Settings
//	}
//
// Call sites then read config.Cron().Outbox.RelayLease. It is a named field
// rather than an embedded one so that promoted names like RelayLease and
// PruneSpec cannot be confused with a service's own lease and schedule
// settings.
type Settings struct {
	// HandlerTimeout bounds a single handler call.
	//
	// Two seconds is an order of magnitude above what the current handlers need
	// — a RabbitMQ publish or a gRPC call, both well inside 200ms — which is
	// enough head-room to ride out a broker reconnect or a gRPC retry without
	// cancelling a call that would have succeeded.
	//
	// It is not free margin: it sets the floor under the lease, bounds how long
	// one stuck call holds a worker, and a larger value buys fewer queue slots
	// for the same lease. Revisit it against the
	// <service>_outbox_handler_duration_seconds histogram once there is
	// production data.
	HandlerTimeout time.Duration `envconfig:"CRON_OUTBOX_HANDLER_TIMEOUT" default:"2s"`

	// RelayLease must cover a message's whole claimed life: waiting in the
	// hand-off queue, the handler call, and the status write. It is also the
	// delay before another instance reclaims work from a pod that was killed, so
	// it should be as short as that budget allows rather than padded.
	//
	// With HandlerTimeout=2s this affords two queued rounds, giving 4 workers a
	// 12-message in-flight ceiling and an 8s recovery window.
	RelayLease time.Duration `envconfig:"CRON_OUTBOX_RELAY_LEASE" default:"8s"`

	// BaseBackoff and MaxBackoff bound the exponential retry schedule applied to
	// a failed message.
	BaseBackoff time.Duration `envconfig:"CRON_OUTBOX_BASE_BACKOFF" default:"30s"`
	MaxBackoff  time.Duration `envconfig:"CRON_OUTBOX_MAX_BACKOFF" default:"6h"`

	// PoolWorkers is the number of concurrent delivery goroutines.
	PoolWorkers int `envconfig:"CRON_OUTBOX_POOL_WORKERS" default:"4"`

	// PoolRelayInterval is how often an idle pool checks for new work. It is not
	// a throughput setting — the relay keeps claiming while a backlog is draining
	// and only sleeps until the next tick once it comes back empty.
	PoolRelayInterval time.Duration `envconfig:"CRON_OUTBOX_POOL_RELAY_INTERVAL" default:"500ms"`

	// PruneSpec schedules the GC that deletes delivered and dead rows. Hourly
	// rather than daily: at payment volumes a daily sweep lets a day's rows
	// accumulate and then deletes them in one burst, which is exactly the shape
	// that causes vacuum pressure.
	PruneSpec string `envconfig:"CRON_OUTBOX_PRUNE_SPEC" default:"@every 1h"`

	// DeliveredRetention is how long successfully delivered rows are kept. This
	// is the knob that controls table size — at N messages/second the table holds
	// roughly N × retention rows.
	//
	// It also bounds dedupe_key protection at the insert: once a delivered row is
	// pruned, its key no longer blocks a re-insert of the same logical event.
	// Downstream idempotency then becomes the only protection, so do not drop
	// this below the shortest downstream TTL (48h across the platform today) for
	// event types whose consumer has no fence of its own.
	DeliveredRetention time.Duration `envconfig:"CRON_OUTBOX_DELIVERED_RETENTION" default:"48h"`

	// DeadRetention is how long dead-lettered rows are kept. This is a forensic
	// window, not a replay window: replay is only mechanically safe while the
	// consumer's own idempotency key is still alive, and beyond that an operator
	// must verify state by hand regardless. Seven days covers a long weekend plus
	// a couple of working days to investigate.
	//
	// Longer is not safer. A dead row's dedupe_key still blocks re-insertion, so
	// every extra day is another day in which a legitimate re-run of the business
	// path cannot recreate that side effect — long retention obstructs recovery
	// rather than aiding it. The payload also carries user identifiers and
	// amounts, which is not data to keep past its usefulness.
	DeadRetention time.Duration `envconfig:"CRON_OUTBOX_DEAD_RETENTION" default:"168h"` // 7 days

	// LagSpec schedules the sampling of the oldest undelivered message per
	// event_type. This is the primary "delivery has silently stopped" alert:
	// counters alone cannot show it, because a stalled relay simply stops
	// incrementing them, which is indistinguishable from an idle service.
	LagSpec string `envconfig:"CRON_OUTBOX_LAG_SPEC" default:"@every 30s"`
}

// WorkerConfig builds the worker configuration these settings describe.
// Hooks are supplied by the caller because instrumentation is service-owned.
func (s Settings) WorkerConfig(hooks WorkerHooks) WorkerConfig {
	return WorkerConfig{
		Lease:          s.RelayLease,
		HandlerTimeout: s.HandlerTimeout,
		BaseBackoff:    s.BaseBackoff,
		MaxBackoff:     s.MaxBackoff,
		Hooks:          hooks,
	}
}

// PoolConfig builds the pool configuration these settings describe.
func (s Settings) PoolConfig() PoolConfig {
	return PoolConfig{
		Workers:       s.PoolWorkers,
		RelayInterval: s.PoolRelayInterval,
	}
}

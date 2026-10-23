// Package cronjobs registers the maintenance jobs every outbox deployment needs.
//
// Both jobs are pure infrastructure — they carry no service-specific behaviour —
// and both are easy to forget when adopting the outbox. Forgetting prune means
// the table grows without bound; forgetting lag means delivery can stop silently,
// because a stalled relay looks exactly like an idle service to every counter.
//
// It lives in its own package so the outbox itself stays free of a scheduler
// dependency.
package cronjobs

import (
	"context"
	"fmt"
	"time"

	"github.com/whagency/core-common/cron"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/outbox"
)

// Job names, exported so services can reference them in alert rules and runbooks
// without re-deriving the strings.
const (
	PruneJobName = "outbox.prune"
	LagJobName   = "outbox.lag"
)

// JobRegistrar is the part of a cron manager these jobs need.
//
// It is an interface rather than *cron.Manager because services wrap the manager
// behind their own domain interface; taking the concrete type would force a type
// assertion at every call site.
type JobRegistrar interface {
	AddFunc(spec, jobName string, cmd cron.FuncWithContext) error
}

// SetLagFunc publishes the per-event_type lag sample. Services pass their own
// metrics binding, typically outbox/metrics.Metrics.SetLag.
type SetLagFunc func(ages map[string]time.Duration)

// Register adds the outbox prune and lag jobs to the manager.
//
// Call it from the service's cron setup after its own jobs; the two are
// independent and order does not matter.
func Register(manager JobRegistrar, repo outbox.Repository, settings outbox.Settings, setLag SetLagFunc) error {
	if manager == nil {
		return fmt.Errorf("[outbox:cronjobs:Register][err: manager is required]")
	}
	if repo == nil {
		return fmt.Errorf("[outbox:cronjobs:Register][err: repository is required]")
	}

	if err := manager.AddFunc(settings.PruneSpec, PruneJobName, func(ctx context.Context) error {
		n, pruneErr := repo.Prune(ctx, settings.DeliveredRetention, settings.DeadRetention)
		if pruneErr != nil {
			return pruneErr
		}
		if n > 0 {
			logger.LogCtx(ctx).Infof(
				"[outbox][Cron:prune][rows: %d][delivered_retention: %s][dead_retention: %s]",
				n, settings.DeliveredRetention, settings.DeadRetention,
			)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("[outbox:cronjobs:Register]%w", err)
	}

	if err := manager.AddFunc(settings.LagSpec, LagJobName, func(ctx context.Context) error {
		ages, lagErr := repo.OldestPendingAge(ctx)
		if lagErr != nil {
			return lagErr
		}
		if setLag != nil {
			setLag(ages)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("[outbox:cronjobs:Register]%w", err)
	}

	return nil
}

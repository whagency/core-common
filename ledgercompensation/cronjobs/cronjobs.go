// Package cronjobs registers the maintenance jobs every compensation deployment
// needs.
//
// All three are pure infrastructure — they carry no service-specific behaviour —
// and all three are easy to forget when adopting the package. Forgetting resolve
// means orphaned postings are never reversed; forgetting stats means that failure
// is invisible, because a stalled resolver looks exactly like a service with
// nothing to compensate; forgetting prune means the audit table grows unbounded.
//
// It lives in its own package so the compensation runtime stays free of a
// scheduler dependency.
package cronjobs

import (
	"context"
	"fmt"

	"github.com/whagency/core-common/cron"
	"github.com/whagency/core-common/ledgercompensation"
	"github.com/whagency/core-common/logger"
)

// Job names, exported so services can reference them in alert rules and runbooks
// without re-deriving the strings.
const (
	ResolveJobName = "ledger.compensation.resolve"
	StatsJobName   = "ledger.compensation.stats"
	PruneJobName   = "ledger.compensation.prune"
)

// JobRegistrar is the part of a cron manager these jobs need.
//
// It is an interface rather than *cron.Manager because services wrap the manager
// behind their own domain interface; taking the concrete type would force a type
// assertion at every call site.
type JobRegistrar interface {
	AddFunc(spec, jobName string, cmd cron.FuncWithContext) error
}

// SetCurrentFunc publishes the per-status gauge sample. Services pass their own
// metrics binding, typically ledgercompensation/metrics.Metrics.SetCurrent.
type SetCurrentFunc func(byStatus map[string]float64)

// Register adds the resolve, stats, and prune jobs to the manager.
func Register(
	manager JobRegistrar,
	resolver *ledgercompensation.Resolver,
	settings ledgercompensation.Settings,
	setCurrent SetCurrentFunc,
) error {
	if manager == nil {
		return fmt.Errorf("[ledgercompensation:cronjobs:Register][err: manager is required]")
	}
	if resolver == nil {
		return fmt.Errorf("[ledgercompensation:cronjobs:Register][err: resolver is required]")
	}

	if err := manager.AddFunc(settings.ResolveSpec, ResolveJobName, func(ctx context.Context) error {
		return resolver.ResolveOrphans(ctx)
	}); err != nil {
		return fmt.Errorf("[ledgercompensation:cronjobs:Register]%w", err)
	}

	if setCurrent != nil {
		if err := manager.AddFunc(settings.StatsSpec, StatsJobName, func(ctx context.Context) error {
			counts, err := resolver.CountByStatus(ctx)
			if err != nil {
				return err
			}
			setCurrent(counts)
			return nil
		}); err != nil {
			return fmt.Errorf("[ledgercompensation:cronjobs:Register]%w", err)
		}
	}

	if err := manager.AddFunc(settings.PruneSpec, PruneJobName, func(ctx context.Context) error {
		n, pruneErr := resolver.Prune(ctx)
		if pruneErr != nil {
			return pruneErr
		}
		if n > 0 {
			logger.LogCtx(ctx).Infof(
				"[ledgercompensation][Cron:prune][rows: %d][retention: %s]",
				n, settings.Retention,
			)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("[ledgercompensation:cronjobs:Register]%w", err)
	}

	return nil
}

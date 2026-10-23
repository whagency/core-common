package ledgercompensation

import (
	"context"
	"fmt"
	"time"

	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/outbox"
	"github.com/whagency/core-common/services/account"
)

// Verifier answers the one business question this package cannot answer itself:
// is the local state the posting belongs to actually committed?
//
// It exists as a seam because "committed" is service-specific — pay-service loads
// the order and checks that it is succeeded with a matching ledger_tx_id.
//
// Returning false means "reverse the posting", so an implementation that cannot
// tell must return an error rather than false.
type Verifier interface {
	IsSettled(ctx context.Context, aggregateType, aggregateID string, journalID uint64) (bool, error)
}

// JournalFinder resolves a journal id that was never recorded locally.
//
// The account-service gRPC client satisfies this structurally, so services pass
// it directly. It must be a read-only lookup: MakeTransaction creates a posting
// when the key does not match and therefore cannot be used to ask whether one
// exists.
type JournalFinder interface {
	GetJournalByIdempotencyKey(context.Context, account.GetJournalByIdempotencyKeyParams) (account.GetJournalByIdempotencyKeyResult, error)
}

// TransactionManager is the part of a service's transaction manager the resolver
// needs.
type TransactionManager interface {
	Begin(ctx context.Context) context.Context
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// ReversalObserver is notified when a reversal is requested, so services can
// count them. Optional; nil is fine.
type ReversalObserver func(bizType string)

// Resolver reverses ledger postings left without a committed local counterpart.
type Resolver struct {
	repo       Repository
	outboxRepo outbox.Repository
	verifier   Verifier
	journals   JournalFinder
	txManager  TransactionManager
	settings   Settings

	onReversal ReversalObserver
}

// ResolverConfig collects the resolver's dependencies.
type ResolverConfig struct {
	Repository         Repository
	Outbox             outbox.Repository
	Verifier           Verifier
	Journals           JournalFinder
	TransactionManager TransactionManager
	Settings           Settings

	// OnReversal is called after a reversal is committed. Optional.
	OnReversal ReversalObserver
}

// NewResolver creates a Resolver, failing fast on missing dependencies: every one
// of them is required for a correct decision, and a nil here would otherwise
// surface as a panic on a money path at 3am.
func NewResolver(cfg ResolverConfig) (*Resolver, error) {
	switch {
	case cfg.Repository == nil:
		return nil, fmt.Errorf("[ledgercompensation][NewResolver][err: repository is required]")
	case cfg.Outbox == nil:
		return nil, fmt.Errorf("[ledgercompensation][NewResolver][err: outbox repository is required]")
	case cfg.Verifier == nil:
		return nil, fmt.Errorf("[ledgercompensation][NewResolver][err: verifier is required]")
	case cfg.Journals == nil:
		return nil, fmt.Errorf("[ledgercompensation][NewResolver][err: journal finder is required]")
	case cfg.TransactionManager == nil:
		return nil, fmt.Errorf("[ledgercompensation][NewResolver][err: transaction manager is required]")
	case cfg.Settings.Grace <= 0:
		// A zero grace would let the resolver reverse a posting whose transaction
		// is still in flight, turning a successful payment into a failed one.
		return nil, fmt.Errorf("[ledgercompensation][NewResolver][err: grace must be positive]")
	case cfg.Settings.BatchSize <= 0:
		return nil, fmt.Errorf("[ledgercompensation][NewResolver][err: batch size must be positive]")
	}

	return &Resolver{
		repo:       cfg.Repository,
		outboxRepo: cfg.Outbox,
		verifier:   cfg.Verifier,
		journals:   cfg.Journals,
		txManager:  cfg.TransactionManager,
		settings:   cfg.Settings,
		onReversal: cfg.OnReversal,
	}, nil
}

// ResolveOrphans processes one batch of rows that have stayed open longer than
// the grace period.
//
// Each row is handled in its own transaction so one undecidable row cannot block
// the rest of the batch, and so a decision and the outbox row it produces commit
// together.
func (r *Resolver) ResolveOrphans(ctx context.Context) error {
	claimed, err := r.claim(ctx)
	if err != nil {
		return err
	}
	if len(claimed) == 0 {
		return nil
	}

	logger.LogCtx(ctx).Warnf(
		"[ledgercompensation][Resolver][msg: found ledger postings without a local counterpart][count:%d][grace:%s]",
		len(claimed), r.settings.Grace,
	)

	var (
		resolved int
		failed   int
		firstErr error
	)
	for i := range claimed {
		if errResolve := r.resolveOne(ctx, claimed[i]); errResolve != nil {
			failed++
			if firstErr == nil {
				firstErr = errResolve
			}
			logger.LogCtx(ctx).Errorf(
				"[ledgercompensation][Resolver][id:%d][aggregate:%s/%s]%s",
				claimed[i].ID, claimed[i].AggregateType, claimed[i].AggregateID, errResolve,
			)
			continue
		}
		resolved++
	}

	logger.LogCtx(ctx).Infof(
		"[ledgercompensation][Resolver][resolved:%d][failed:%d][claimed:%d]",
		resolved, failed, len(claimed),
	)

	if firstErr != nil {
		return fmt.Errorf("[ledgercompensation][Resolver][resolved:%d][failed:%d]%w", resolved, failed, firstErr)
	}

	return nil
}

// claim reads one batch of orphan candidates.
//
// The claim transaction only reads, and is committed before the per-row work
// begins: holding its locks across a ledger lookup would pin rows this run may
// never reach.
func (r *Resolver) claim(ctx context.Context) ([]Compensation, error) {
	cutoff := time.Now().UTC().Add(-r.settings.Grace)

	txCtx := r.txManager.Begin(ctx)
	claimed, err := r.repo.WithTxFromContext(txCtx).ClaimOrphans(txCtx, cutoff, r.settings.BatchSize)
	if err != nil {
		if errRollback := r.txManager.Rollback(txCtx); errRollback != nil {
			logger.LogCtx(txCtx).Errorf("[ledgercompensation][Resolver:claim][rollback]%s", errRollback)
		}
		return nil, fmt.Errorf("[ledgercompensation][Resolver:claim]%w", err)
	}

	if errCommit := r.txManager.Commit(txCtx); errCommit != nil {
		return nil, fmt.Errorf("[ledgercompensation][Resolver:claim][commit]%w", errCommit)
	}

	return claimed, nil
}

// resolveOne decides the fate of a single row.
func (r *Resolver) resolveOne(ctx context.Context, row Compensation) error {
	journalID, found, err := r.resolveJournalID(ctx, row)
	if err != nil {
		return err
	}
	if !found {
		// Either the ledger call never landed or the posting has already been
		// reversed — a rollback frees the original key. Both mean there is nothing
		// left to compensate.
		return r.finish(ctx, row, StatusVoid, "no live ledger posting for idempotency key", 0)
	}

	// Belt and braces. Clearing the switch shares the business transaction's fate,
	// so a surviving row should mean the local side never committed. Confirming
	// that against committed state costs one read on a path that is rare by
	// construction, and it is the difference between reversing a real orphan and
	// reversing a payment that actually went through.
	settled, errVerify := r.verifier.IsSettled(ctx, row.AggregateType, row.AggregateID, journalID)
	if errVerify != nil {
		return fmt.Errorf("[ledgercompensation][Resolver:resolveOne][verify][aggregate:%s/%s]%w",
			row.AggregateType, row.AggregateID, errVerify)
	}
	if settled {
		return r.finish(ctx, row, StatusSettled, "local state committed with this ledger posting", journalID)
	}

	return r.finish(ctx, row, StatusReversalRequested, "ledger posting has no committed local counterpart", journalID)
}

// resolveJournalID returns the journal id to act on.
//
// A row that never advanced past pending has none: the process died between the
// ledger call and recording its result. The ledger is asked for it, which is safe
// only because the lookup is read-only.
func (r *Resolver) resolveJournalID(ctx context.Context, row Compensation) (uint64, bool, error) {
	if row.JournalID != nil && *row.JournalID != 0 {
		return *row.JournalID, true, nil
	}

	lookup, err := r.journals.GetJournalByIdempotencyKey(ctx, account.GetJournalByIdempotencyKeyParams{
		Type:           row.BizType,
		IdempotencyKey: row.IdempotencyKey,
	})
	if err != nil {
		return 0, false, fmt.Errorf("[ledgercompensation][Resolver:resolveJournalID][key:%s]%w", row.IdempotencyKey, err)
	}
	if !lookup.Found || lookup.Status == account.JournalStatusReversed {
		return 0, false, nil
	}

	return lookup.JournalID, true, nil
}

// finish writes the terminal status and, for a reversal, the outbox row carrying
// the request. Both happen in one transaction so a row can never be marked
// reversal_requested without the request existing.
func (r *Resolver) finish(
	ctx context.Context,
	row Compensation,
	status Status,
	reason string,
	journalID uint64,
) error {
	txCtx := r.txManager.Begin(ctx)
	committed := false
	defer func() {
		if committed {
			return
		}
		if errRollback := r.txManager.Rollback(txCtx); errRollback != nil {
			logger.LogCtx(txCtx).Errorf("[ledgercompensation][Resolver:finish][rollback]%s", errRollback)
		}
	}()

	repo := r.repo.WithTxFromContext(txCtx)

	// Persist a journal id recovered from the ledger so a later run need not ask
	// again.
	if row.JournalID == nil && journalID != 0 {
		if err := repo.AttachJournalID(txCtx, row.ID, journalID); err != nil {
			return fmt.Errorf("[ledgercompensation][Resolver:finish]%w", err)
		}
	}

	if status == StatusReversalRequested {
		if err := r.outboxRepo.Create(txCtx, reversalOutboxParams(row.AggregateID, journalID)); err != nil {
			return fmt.Errorf("[ledgercompensation][Resolver:finish][outbox]%w", err)
		}
	}

	if err := repo.Resolve(txCtx, row.ID, status, reason); err != nil {
		return fmt.Errorf("[ledgercompensation][Resolver:finish]%w", err)
	}

	if err := r.txManager.Commit(txCtx); err != nil {
		return fmt.Errorf("[ledgercompensation][Resolver:finish]%w", err)
	}
	committed = true

	if status == StatusReversalRequested {
		if r.onReversal != nil {
			r.onReversal(row.BizType.String())
		}
		logger.LogCtx(ctx).Warnf(
			"[ledgercompensation][Resolver][msg: requested ledger reversal][aggregate:%s/%s][biz_type:%s][journalID:%d]",
			row.AggregateType, row.AggregateID, row.BizType.String(), journalID,
		)
	}

	return nil
}

// CountByStatus returns the current row count per status, with every status
// present. Callers publishing a gauge must sample all of them; see AllStatuses.
func (r *Resolver) CountByStatus(ctx context.Context) (map[string]float64, error) {
	counts, err := r.repo.CountByStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("[ledgercompensation][Resolver:CountByStatus]%w", err)
	}

	out := make(map[string]float64, len(AllStatuses()))
	for _, status := range AllStatuses() {
		out[status.String()] = float64(counts[status])
	}

	return out, nil
}

// Prune deletes resolved rows past the retention window. Open rows are never
// pruned: an unresolved switch is unfinished business, however old.
func (r *Resolver) Prune(ctx context.Context) (int64, error) {
	n, err := r.repo.Prune(ctx, time.Now().UTC().Add(-r.settings.Retention))
	if err != nil {
		return 0, fmt.Errorf("[ledgercompensation][Resolver:Prune]%w", err)
	}
	return n, nil
}

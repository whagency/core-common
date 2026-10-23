package outbox

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/whagency/core-common/logger"
)

const (
	_defaultRelayInterval = 500 * time.Millisecond
	_defaultWorkerCount   = 4
)

// saturatedPollInterval is how long the relay waits for a worker slot while a
// backlog is still draining. It is deliberately much shorter than
// RelayInterval: saturation is transient, and dropping back to the idle tick
// would stall a draining backlog for a whole interval.
const saturatedPollInterval = 25 * time.Millisecond

// PoolConfig controls the runtime behaviour of a Pool.
// Zero values are replaced with safe defaults by NewPool.
type PoolConfig struct {
	// Workers is the number of concurrent goroutines that process messages.
	// Default 4.
	Workers int

	// RelayInterval is how often an idle pool looks for new work. Default 500ms.
	//
	// It is not a throughput setting: once a backlog exists the relay keeps
	// claiming until a claim comes back short, so the interval only governs how
	// quickly an idle pool notices the first new message. Lower it for latency,
	// raise it to cut idle queries.
	RelayInterval time.Duration
}

func (c *PoolConfig) applyDefaults() {
	if c.Workers <= 0 {
		c.Workers = _defaultWorkerCount
	}
	if c.RelayInterval <= 0 {
		c.RelayInterval = _defaultRelayInterval
	}
}

// queueSizeFor derives the hand-off channel capacity from the lease.
//
// This is not a tuning choice, which is why it is not configurable. A claimed
// message holds its lease from the moment ClaimDue returns, including the time
// it spends waiting in the channel, so the lease has to cover the whole
// queueing delay:
//
//	Lease > (queueSize / workers) × handlerTimeout + handlerTimeout + grace
//
// Solving for queueSize gives the value below. Anything larger would let leases
// expire on messages that are merely queued, and the relay would then reclaim
// and re-dispatch work that is already in flight. Anything smaller only costs
// throughput.
//
// A result of zero means the lease affords no queueing at all: the channel is
// unbuffered and a hand-off completes only once a worker is ready to receive,
// so a claimed message is never parked.
func queueSizeFor(workers int, lease, handlerTimeout time.Duration) int {
	queueBudget := lease - handlerTimeout - statusUpdateGrace
	if queueBudget < 0 {
		return 0
	}
	return workers * int(queueBudget/handlerTimeout)
}

// Pool runs N worker goroutines that each process one already-claimed Message
// per iteration, plus one internal relay goroutine that periodically claims
// pending/failed messages from the DB and feeds them to the workers.
//
// With multiple service instances running in parallel, the DB-level
// FOR UPDATE SKIP LOCKED in ClaimDue ensures each row is claimed by exactly
// one worker across all instances. If a pod crashes mid-processing, the lease
// expires and another instance picks up the message automatically.
//
// Within a single instance the pool additionally tracks in-flight message IDs,
// so a stale-lease reclaim by this instance's own relay can never put a message
// that is still being processed back into the queue. Across instances the lease
// ownership check on the status updates prevents a slow worker from overwriting
// the new owner's outcome.
//
// Lifecycle: register Run in the application errgroup so the pool exits cleanly
// on SIGTERM:
//
//	g.Go(func() error { return core.OutboxPool.Run(gctx) })
type Pool struct {
	worker *Worker
	cfg    PoolConfig

	// queueSize is derived from the worker's lease, not configured; see queueSizeFor.
	queueSize int
	queue     chan Message // each element = already-claimed Message awaiting a worker

	mu       sync.Mutex
	inFlight map[int64]struct{}
}

// NewPool creates a Pool backed by the given Worker.
func NewPool(worker *Worker, cfg PoolConfig) *Pool {
	cfg.applyDefaults()
	queueSize := queueSizeFor(cfg.Workers, worker.cfg.Lease, worker.cfg.HandlerTimeout)

	return &Pool{
		worker:    worker,
		cfg:       cfg,
		queueSize: queueSize,
		queue:     make(chan Message, queueSize),
		inFlight:  make(map[int64]struct{}),
	}
}

// submit hands an already-claimed Message to a worker.
//
// Blocks until a slot is free or ctx is cancelled. Blocking is the back-pressure
// mechanism: it stops the relay from claiming while workers are saturated,
// without spawning unbounded goroutines. On cancellation the message is left
// alone; its lease expires and another instance reclaims it.
func (p *Pool) submit(ctx context.Context, msg Message) {
	select {
	case p.queue <- msg:
	case <-ctx.Done():
		p.releaseInFlight(msg.ID)
	}
}

// tryAcquireInFlight registers id as in-flight and reports whether it was newly
// acquired. A false result means this instance is already processing the
// message — the relay reclaimed it after the lease expired while it was still
// queued — and it must not be dispatched a second time.
func (p *Pool) tryAcquireInFlight(id int64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.inFlight[id]; exists {
		return false
	}
	p.inFlight[id] = struct{}{}
	return true
}

// releaseInFlight removes id from the in-flight set.
func (p *Pool) releaseInFlight(id int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.inFlight, id)
}

// claimBudget is how many further messages the pool can take on right now:
// every queue slot plus every worker slot, minus what it is already holding.
//
// This is what keeps a claim from outliving its own lease. A message begins
// burning its lease the moment ClaimDue returns, so claiming a batch bigger
// than the pool can dispatch would leave the tail of that batch expiring while
// it waits — and the relay would then reclaim messages that are still queued.
func (p *Pool) claimBudget() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.queueSize + p.cfg.Workers - len(p.inFlight)
}

// Run starts the workers and the relay, then blocks until ctx is cancelled and
// every goroutine has exited. Goroutine count is fixed at Workers+1 under any
// load; back-pressure is absorbed by blocking, never by spawning.
//
// Returns nil — per-message outcomes are recorded on the row and logged.
func (p *Pool) Run(ctx context.Context) error {
	var wg sync.WaitGroup

	// Worker goroutines — each waits for a claimed Message and processes it.
	// On shutdown they finish what is already queued before exiting, bounded by
	// one handler timeout; see drain.
	for range p.cfg.Workers {
		wg.Go(func() {
			for {
				select {
				case msg := <-p.queue:
					p.safeProcessOne(ctx, msg)
				case <-ctx.Done():
					p.drain(ctx)
					return
				}
			}
		})
	}

	// Relay goroutine — the only one that reads the database. It drains the
	// backlog whenever there is one and otherwise wakes on the ticker. Being a
	// single goroutine is what guarantees there is never a second concurrent
	// claim; ticks that arrive while it is working are dropped by time.Ticker.
	wg.Go(func() {
		ticker := time.NewTicker(p.cfg.RelayInterval)
		defer ticker.Stop()

		// Drain once before waiting on the ticker: a pod that starts up to an
		// existing backlog should begin working immediately, not after an
		// interval of idling.
		p.safeClaimUntilDrained(ctx)

		for {
			select {
			case <-ticker.C:
				p.safeClaimUntilDrained(ctx)
			case <-ctx.Done():
				return
			}
		}
	})

	wg.Wait()
	return nil
}

// drain finishes messages already handed to the pool before it shuts down.
//
// The handlers get a fresh, bounded context rather than the pool's cancelled
// one. Running them under the cancelled context would be worse than not
// draining at all: every handler doing I/O would fail instantly, consume a retry
// attempt and be rescheduled a full backoff away, whereas an abandoned message
// simply waits out its lease and is reclaimed with its budget intact.
//
// The budget is one handler timeout for the whole drain. Whatever does not fit
// is left in the channel and falls back to lease expiry, which is the same
// safety net that covers a hard crash.
func (p *Pool) drain(ctx context.Context) {
	drainCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		p.worker.cfg.HandlerTimeout,
	)
	defer cancel()

	for {
		select {
		case msg := <-p.queue:
			p.safeProcessOne(drainCtx, msg)
			if drainCtx.Err() != nil {
				return
			}
		default:
			return
		}
	}
}

// safeClaimUntilDrained wraps the claim loop with a recover.
//
// Without it a panic anywhere in the claim path — a driver bug, a scan of an
// unexpected column type — takes down the whole process. In a service that also
// answers HTTP and gRPC that is a wildly disproportionate blast radius: an
// outbox defect would stop the service accepting deposits.
//
// Recovering leaves delivery stalled until the next tick, which is visible
// through the lag gauge and the claim-error counter, while the rest of the
// service keeps serving. That is the better failure mode, and it is the same
// containment handlers already get from safeProcessOne.
func (p *Pool) safeClaimUntilDrained(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.LogCtx(ctx).Errorf("[outbox][Pool:relay][panic]%v\n%s", r, debug.Stack())
			p.worker.reportClaimError(fmt.Errorf("relay panic: %v", r))
		}
	}()
	p.claimUntilDrained(ctx)
}

// claimUntilDrained keeps claiming while a backlog is draining.
//
// RelayInterval is the idle poll interval, not a throughput ceiling: claiming a
// single batch per tick would cap the pool at (QueueSize+Workers)/RelayInterval
// messages per second no matter how fast the handlers are. Submit blocks when
// the pool is full and claimBudget accounts for in-flight work, so this loop
// cannot outrun the workers.
func (p *Pool) claimUntilDrained(ctx context.Context) {
	for ctx.Err() == nil {
		budget := p.claimBudget()
		if budget <= 0 {
			// Fully saturated: claiming now would only start leases the pool
			// cannot serve before they expire. Wait for a slot instead of
			// returning — the backlog is still there, and the workers are about
			// to free capacity.
			if !sleepCtx(ctx, saturatedPollInterval) {
				return
			}
			continue
		}
		if !p.relayOnce(ctx, budget) {
			return
		}
	}
}

// sleepCtx waits for d and reports whether it completed rather than being
// cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// relayOnce claims up to want messages and hands them to the workers. It reports
// whether the claim returned everything it asked for, which means more work is
// probably due; a short claim means the queue is drained and the relay can go
// back to sleeping until the next tick.
func (p *Pool) relayOnce(ctx context.Context, want int) bool {
	msgs, err := p.worker.Claim(ctx, want)
	if err != nil {
		logger.LogCtx(ctx).Errorf("[outbox][Pool:relay]%s", err)
		p.worker.reportClaimError(err)
		return false
	}

	for _, msg := range msgs {
		// Skip anything this instance is already processing: the row may have
		// been stale-reclaimed while still sitting in the queue, and dispatching
		// it twice would double the side effect.
		if !p.tryAcquireInFlight(msg.ID) {
			logger.LogCtx(ctx).Warnf(
				"[outbox][Pool:relay][msg: reclaimed message still in flight, skipping][outbox_id: %d][event_type: %s]",
				msg.ID, msg.EventType,
			)
			continue
		}
		p.submit(ctx, msg)
	}

	return len(msgs) == want
}

// safeProcessOne wraps ProcessOne with a recover so a handler panic cannot
// silently kill a worker goroutine. A panic is a deterministic code defect, so
// the message is dead-lettered rather than left to be reclaimed on every lease
// expiry — an endless retry loop that never fires the OnDead hook and therefore
// never alerts.
func (p *Pool) safeProcessOne(ctx context.Context, msg Message) {
	defer p.releaseInFlight(msg.ID)
	defer func() {
		if r := recover(); r != nil {
			logger.LogCtx(ctx).Errorf("[outbox][Pool:worker][panic][outbox_id: %d][event_type: %s]%v",
				msg.ID, msg.EventType, r)
			p.worker.DeadLetter(ctx, msg, fmt.Sprintf("handler panic: %v", r))
		}
	}()
	p.worker.ProcessOne(ctx, msg)
}

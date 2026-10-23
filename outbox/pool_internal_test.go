package outbox

import (
	"testing"
	"time"
)

// A claimed message holds its lease while it waits in the hand-off channel, so
// the capacity has to fit inside the lease. Oversizing it lets leases expire on
// merely-queued messages, which the relay then reclaims and re-dispatches.
func TestQueueSizeFor_DerivesCapacityFromLease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		workers        int
		lease          time.Duration
		handlerTimeout time.Duration
		want           int
	}{
		{
			name:           "shipped defaults afford one queued round",
			workers:        4,
			lease:          15 * time.Second,
			handlerTimeout: 5 * time.Second,
			// budget = 15 - 5 - 2 = 8s → 8/5 = 1 round → 4 slots
			want: 4,
		},
		{
			name:           "a longer lease affords more rounds",
			workers:        4,
			lease:          62 * time.Second,
			handlerTimeout: 5 * time.Second,
			// budget = 55s → 11 rounds → 44 slots
			want: 44,
		},
		{
			name:           "more workers widen each round",
			workers:        16,
			lease:          15 * time.Second,
			handlerTimeout: 5 * time.Second,
			want:           16,
		},
		{
			name:           "a lease with no room to queue gives an unbuffered hand-off",
			workers:        3,
			lease:          7 * time.Second,
			handlerTimeout: 5 * time.Second,
			want:           0,
		},
		{
			name:           "a lease below the enforced floor cannot go negative",
			workers:        4,
			lease:          time.Second,
			handlerTimeout: 5 * time.Second,
			want:           0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := queueSizeFor(tt.workers, tt.lease, tt.handlerTimeout)
			if got != tt.want {
				t.Errorf("queueSizeFor(%d, %s, %s) = %d, want %d",
					tt.workers, tt.lease, tt.handlerTimeout, got, tt.want)
			}

			// The invariant the derivation exists to protect: the message at the
			// back of a full queue must still finish inside the lease.
			worstCase := time.Duration(got/tt.workers)*tt.handlerTimeout + tt.handlerTimeout + statusUpdateGrace
			if got > 0 && worstCase > tt.lease {
				t.Errorf("worst-case dispatch %s exceeds lease %s with %d slots", worstCase, tt.lease, got)
			}
		})
	}
}

// The relay must never hand the same message to two workers. A stale-lease
// reclaim by this instance's own relay is the realistic way that happens:
// locked_by still matches, so the database cannot reject it.
func TestPool_InFlightSet_RejectsDuplicateDispatch(t *testing.T) {
	t.Parallel()

	p := &Pool{inFlight: make(map[int64]struct{})}

	if !p.tryAcquireInFlight(42) {
		t.Fatal("first acquire must succeed")
	}
	if p.tryAcquireInFlight(42) {
		t.Error("second acquire of an in-flight id must be rejected")
	}
	if !p.tryAcquireInFlight(43) {
		t.Error("a different id must still be acquirable")
	}

	p.releaseInFlight(42)
	if !p.tryAcquireInFlight(42) {
		t.Error("id must be acquirable again after release")
	}
}

// The relay must claim only what the pool can still absorb. Claiming ahead
// starts leases the pool cannot serve, and the tail of such a batch expires
// while it is merely waiting.
func TestPool_ClaimBudget_ReflectsInFlight(t *testing.T) {
	t.Parallel()

	p := &Pool{
		cfg:       PoolConfig{Workers: 4},
		queueSize: 4,
		inFlight:  make(map[int64]struct{}),
	}

	if got, want := p.claimBudget(), 8; got != want {
		t.Errorf("idle budget = %d, want %d (queue + workers)", got, want)
	}

	for id := int64(1); id <= 8; id++ {
		p.tryAcquireInFlight(id)
	}
	if got := p.claimBudget(); got != 0 {
		t.Errorf("saturated budget = %d, want 0", got)
	}

	p.releaseInFlight(3)
	if got := p.claimBudget(); got != 1 {
		t.Errorf("budget after one release = %d, want 1", got)
	}
}

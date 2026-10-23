package metrics_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/whagency/core-common/outbox/metrics"
)

// The names are the contract with dashboards and alert rules across every
// service. Pinning them here is the whole reason this package exists: a rename
// is only cheap before the first deployment scrapes it.
func TestNew_MetricNames(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m, err := metrics.New("account_service", reg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	// A *Vec with no observed labels produces no series, and Gather only reports
	// families that have one. Touch each so the names are actually visible.
	touchAll(m)

	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range families {
		got[f.GetName()] = true
	}

	for _, want := range []string{
		"account_service_outbox_delivered_total",
		"account_service_outbox_failed_total",
		"account_service_outbox_dead_total",
		"account_service_outbox_lease_lost_total",
		"account_service_outbox_claim_errors_total",
		"account_service_outbox_handler_duration_seconds",
		"account_service_outbox_oldest_pending_age_seconds",
	} {
		if !got[want] {
			t.Errorf("metric %q is missing", want)
		}
	}
}

// A trailing underscore in the prefix is the existing in-service convention;
// accepting both spellings keeps adopters from producing doubled underscores.
func TestNew_PrefixNormalisation(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"pay_service", "pay_service_", "  pay_service  "} {
		reg := prometheus.NewRegistry()
		m, err := metrics.New(name, reg)
		if err != nil {
			t.Fatalf("new(%q): %v", name, err)
		}
		touchAll(m)

		families, err := reg.Gather()
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range families {
			if !strings.HasPrefix(f.GetName(), "pay_service_outbox_") {
				t.Errorf("new(%q) produced %q", name, f.GetName())
			}
		}
	}
}

func TestHooks_RecordOutcomes(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m, err := metrics.New("svc", reg)
	if err != nil {
		t.Fatal(err)
	}
	h := m.Hooks()

	h.OnDelivered("ev.a")
	h.OnDelivered("ev.a")
	h.OnFailed("ev.a")
	h.OnDead("ev.b")
	h.OnLeaseLost("ev.a")
	h.OnClaimError(errors.New("connection refused"))
	h.OnHandlerDuration("ev.a", 30*time.Millisecond)

	for _, tc := range []struct {
		metric string
		want   float64
	}{
		{metric: "svc_outbox_delivered_total", want: 2},
		{metric: "svc_outbox_failed_total", want: 1},
		{metric: "svc_outbox_dead_total", want: 1},
		{metric: "svc_outbox_lease_lost_total", want: 1},
		{metric: "svc_outbox_claim_errors_total", want: 1},
	} {
		if got := testutil.ToFloat64(collector(t, reg, tc.metric)); got != tc.want {
			t.Errorf("%s = %v, want %v", tc.metric, got, tc.want)
		}
	}
}

// A drained event type disappears from the sample. Without the reset its gauge
// would keep reporting the old backlog and keep firing the lag alert.
func TestSetLag_ResetsDrainedEventTypes(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m, err := metrics.New("svc", reg)
	if err != nil {
		t.Fatal(err)
	}

	m.SetLag(map[string]time.Duration{"ev.a": 90 * time.Second, "ev.b": 5 * time.Second})
	if got := gaugeValue(t, reg, "svc_outbox_oldest_pending_age_seconds", "ev.a"); got != 90 {
		t.Fatalf("ev.a lag = %v, want 90", got)
	}

	// ev.a drained; only ev.b is still behind.
	m.SetLag(map[string]time.Duration{"ev.b": 5 * time.Second})

	if series := seriesFor(t, reg, "svc_outbox_oldest_pending_age_seconds"); len(series) != 1 {
		t.Errorf("lag series = %d, want 1 (the drained event type must be dropped)", len(series))
	}
}

func TestNew_DuplicateRegistrationFails(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	if _, err := metrics.New("svc", reg); err != nil {
		t.Fatal(err)
	}
	if _, err := metrics.New("svc", reg); err == nil {
		t.Error("registering the same metrics twice must fail rather than silently shadow")
	}
}

// Package metrics provides the standard Prometheus instrumentation for the
// outbox runtime.
//
// It exists to fix the metric names once, across every service. Names are the
// contract between a service and the dashboards and alert rules that watch it:
// if each adopting service hand-rolls its own, one shared dashboard and one set
// of alert rules stop being possible, and the mistake is expensive to undo
// because renaming a metric in production breaks its history.
package metrics

import (
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/whagency/core-common/outbox"
)

const eventTypeLabel = "event_type"

// handlerDurationBuckets span a queue publish through a slow gRPC call.
var handlerDurationBuckets = []float64{0.005, 0.025, 0.1, 0.5, 1, 5, 25}

// Metrics holds the outbox collectors for one service.
type Metrics struct {
	delivered       *prometheus.CounterVec
	failed          *prometheus.CounterVec
	dead            *prometheus.CounterVec
	leaseLost       *prometheus.CounterVec
	handlerDuration *prometheus.HistogramVec
	lagSeconds      *prometheus.GaugeVec
	claimErrors     prometheus.Counter
}

// New builds the standard outbox collectors for serviceName and registers them
// with reg. Pass prometheus.DefaultRegisterer in a service, and a fresh
// prometheus.NewRegistry() in tests.
//
// serviceName is the metric prefix, with or without a trailing underscore:
// "account_service" and "account_service_" both yield
// account_service_outbox_delivered_total.
func New(serviceName string, reg prometheus.Registerer) (*Metrics, error) {
	prefix := strings.TrimSuffix(strings.TrimSpace(serviceName), "_") + "_outbox_"

	m := &Metrics{
		delivered: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: prefix + "delivered_total",
			Help: "Outbox messages successfully delivered, by event_type.",
		}, []string{eventTypeLabel}),

		failed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: prefix + "failed_total",
			Help: "Retryable outbox delivery failures, by event_type.",
		}, []string{eventTypeLabel}),

		dead: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: prefix + "dead_total",
			Help: "Outbox messages moved to dead status, by event_type. " +
				"Nothing retries these automatically; alert on any increase.",
		}, []string{eventTypeLabel}),

		leaseLost: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: prefix + "lease_lost_total",
			Help: "Status updates discarded because another worker had reclaimed the message, " +
				"by event_type. A sustained rate means work is being done twice and thrown away.",
		}, []string{eventTypeLabel}),

		handlerDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    prefix + "handler_duration_seconds",
			Help:    "Outbox handler wall-clock execution time, by event_type.",
			Buckets: handlerDurationBuckets,
		}, []string{eventTypeLabel}),

		lagSeconds: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: prefix + "oldest_pending_age_seconds",
			Help: "Age of the oldest undelivered outbox message, by event_type. " +
				"Alert on sustained growth.",
		}, []string{eventTypeLabel}),

		claimErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: prefix + "claim_errors_total",
			Help: "Relay failures to claim work — the database is unreachable, or the claim " +
				"path panicked. The only immediate signal that delivery has stopped: sampling " +
				"the lag reads the same database, so an outage freezes that gauge rather than " +
				"making it climb.",
		}),
	}

	for _, c := range m.collectors() {
		if err := reg.Register(c); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (m *Metrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{
		m.delivered, m.failed, m.dead, m.leaseLost,
		m.handlerDuration, m.lagSeconds, m.claimErrors,
	}
}

// Hooks returns WorkerHooks wired to these collectors. Pass the result to
// outbox.WorkerConfig.
func (m *Metrics) Hooks() outbox.WorkerHooks {
	return outbox.WorkerHooks{
		OnDelivered:  func(eventType string) { m.delivered.WithLabelValues(eventType).Inc() },
		OnFailed:     func(eventType string) { m.failed.WithLabelValues(eventType).Inc() },
		OnDead:       func(eventType string) { m.dead.WithLabelValues(eventType).Inc() },
		OnLeaseLost:  func(eventType string) { m.leaseLost.WithLabelValues(eventType).Inc() },
		OnClaimError: func(error) { m.claimErrors.Inc() },
		OnHandlerDuration: func(eventType string, d time.Duration) {
			m.handlerDuration.WithLabelValues(eventType).Observe(d.Seconds())
		},
	}
}

// SetLag publishes a lag sample from Repository.OldestPendingAge.
//
// The gauge is reset first: event types whose backlog has drained are absent
// from the map, and a GaugeVec left alone keeps reporting their last value
// forever — alerting on a backlog that cleared hours ago.
func (m *Metrics) SetLag(ages map[string]time.Duration) {
	m.lagSeconds.Reset()
	for eventType, age := range ages {
		m.lagSeconds.WithLabelValues(eventType).Set(age.Seconds())
	}
}

// Package metrics provides the standard Prometheus instrumentation for the
// ledger compensation runtime.
//
// It exists to fix the metric names once, across every service. Names are the
// contract between a service and the dashboards and alert rules that watch it: if
// each adopting service hand-rolls its own, one shared dashboard and one set of
// alert rules stop being possible, and the mistake is expensive to undo because
// renaming a metric in production breaks its history.
package metrics

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds the compensation collectors for one service.
type Metrics struct {
	current       *prometheus.GaugeVec
	reversalTotal *prometheus.CounterVec
}

// New builds the standard compensation collectors for serviceName and registers
// them with reg. Pass prometheus.DefaultRegisterer in a service, and a fresh
// prometheus.NewRegistry() in tests.
//
// serviceName is the metric prefix, with or without a trailing underscore:
// "pay_service" and "pay_service_" both yield
// pay_service_ledger_compensation_current.
func New(serviceName string, reg prometheus.Registerer) (*Metrics, error) {
	prefix := strings.TrimSuffix(strings.TrimSpace(serviceName), "_") + "_ledger_compensation_"

	m := &Metrics{
		current: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: prefix + "current",
			Help: "Current ledger compensation rows by status. A rising pending/posted " +
				"count means ledger postings are accumulating without a committed local " +
				"counterpart; this is the primary alert.",
		}, []string{"status"}),

		reversalTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: prefix + "reversal_total",
			Help: "Ledger reversals requested by the compensation resolver, by journal " +
				"business type. Any sustained increase means a flow is losing its local " +
				"transaction after moving money.",
		}, []string{"biz_type"}),
	}

	for _, c := range []prometheus.Collector{m.current, m.reversalTotal} {
		if err := reg.Register(c); err != nil {
			return nil, err
		}
	}

	return m, nil
}

// SetCurrent replaces the gauge with a full sample.
//
// Callers must pass every status, including the zeroes — Resolver.CountByStatus
// already does. A GaugeVec keeps the last value for a label it stops hearing
// about, so omitting a status that dropped to zero would leave a stale non-zero
// reading alerting forever; Reset clears the previous sample first.
func (m *Metrics) SetCurrent(byStatus map[string]float64) {
	m.current.Reset()
	for status, value := range byStatus {
		m.current.WithLabelValues(status).Set(value)
	}
}

// ObserveReversal counts one requested reversal. Pass it as
// ResolverConfig.OnReversal.
func (m *Metrics) ObserveReversal(bizType string) {
	m.reversalTotal.WithLabelValues(bizType).Inc()
}

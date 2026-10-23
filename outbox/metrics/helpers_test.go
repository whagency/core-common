package metrics_test

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/whagency/core-common/outbox/metrics"
)

// touchAll records one observation on every collector so each metric family has
// at least one series and therefore appears in Gather.
func touchAll(m *metrics.Metrics) {
	h := m.Hooks()
	h.OnDelivered("probe")
	h.OnFailed("probe")
	h.OnDead("probe")
	h.OnLeaseLost("probe")
	h.OnClaimError(errors.New("probe"))
	h.OnHandlerDuration("probe", time.Millisecond)
	m.SetLag(map[string]time.Duration{"probe": time.Second})
}

// collector finds a single-series collector by metric name so tests can use
// testutil.ToFloat64 on it.
func collector(t *testing.T, reg *prometheus.Registry, name string) prometheus.Collector {
	t.Helper()
	return gathererCollector{reg: reg, name: name}
}

type gathererCollector struct {
	reg  *prometheus.Registry
	name string
}

func (g gathererCollector) Describe(chan<- *prometheus.Desc) {}

func (g gathererCollector) Collect(ch chan<- prometheus.Metric) {
	families, err := g.reg.Gather()
	if err != nil {
		return
	}
	for _, f := range families {
		if f.GetName() != g.name {
			continue
		}
		for _, m := range f.GetMetric() {
			ch <- constFrom(g.name, m)
		}
	}
}

func constFrom(name string, m *dto.Metric) prometheus.Metric {
	desc := prometheus.NewDesc(name, name, nil, nil)
	switch {
	case m.GetCounter() != nil:
		return prometheus.MustNewConstMetric(desc, prometheus.CounterValue, m.GetCounter().GetValue())
	default:
		return prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, m.GetGauge().GetValue())
	}
}

func seriesFor(t *testing.T, reg *prometheus.Registry, name string) []*dto.Metric {
	t.Helper()

	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() == name {
			return f.GetMetric()
		}
	}
	return nil
}

func gaugeValue(t *testing.T, reg *prometheus.Registry, name, eventType string) float64 {
	t.Helper()

	for _, m := range seriesFor(t, reg, name) {
		for _, l := range m.GetLabel() {
			if l.GetName() == "event_type" && l.GetValue() == eventType {
				return m.GetGauge().GetValue()
			}
		}
	}
	t.Fatalf("no series for %s{event_type=%q}", name, eventType)
	return 0
}

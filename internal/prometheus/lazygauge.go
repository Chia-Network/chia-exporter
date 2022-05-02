package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"
)

// LazyGauge wraps a prometheus Gauge but doesn't register itself until Set is called
// This avoids a bunch of registered metrics with "0" initial values showing up when scraped, and generally helps
// the resulting data in graphs look cleaner
type LazyGauge struct {
	Gauge    prometheus.Gauge
	Registry *prometheus.Registry

	registered bool
}

// Set wraps prometheus.Set with a call to MustRegister
func (l *LazyGauge) Set(val float64) {
	if !l.registered {
		l.registered = true
		l.Registry.MustRegister(l.Gauge)
	}

	l.Gauge.Set(val)
}

// Unregister removes the metric from the Registry to stop reporting it until it is registered again
func (l *LazyGauge) Unregister() {
	if l.registered {
		l.registered = false
		l.Registry.Unregister(l.Gauge)
	}
}

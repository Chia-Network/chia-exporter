package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"
)

// LazyCounter wraps a prometheus Gauge but doesn't register itself until Set is called
// This avoids a bunch of registered metrics with "0" initial values showing up when scraped, and generally helps
// the resulting data in graphs look cleaner
type LazyCounter struct {
	Counter  prometheus.Counter
	Registry *prometheus.Registry

	registered bool
}

// Inc wraps prometheus.Counter.Inc with a call to MustRegister
func (l *LazyCounter) Inc() {
	if l.registered != true {
		l.registered = true
		l.Registry.MustRegister(l.Counter)
	}

	l.Counter.Inc()
}

// Add wraps prometheus.Counter.Add with a call to MustRegister
func (l *LazyCounter) Add(val float64) {
	if l.registered != true {
		l.registered = true
		l.Registry.MustRegister(l.Counter)
	}

	l.Counter.Add(val)
}

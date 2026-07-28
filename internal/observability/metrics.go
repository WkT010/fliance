package observability

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

// MetricType is the Prometheus metric type.
type MetricType string

const (
	Counter   MetricType = "counter"
	Gauge     MetricType = "gauge"
	Histogram MetricType = "histogram"
)

// Metric is the common interface for all metrics.
type Metric interface {
	Name() string
	Help() string
	Type() MetricType
	Write(w io.Writer)
}

// Registry holds named metrics and emits them in Prometheus text format.
type Registry struct {
	mu      sync.RWMutex
	metrics map[string]Metric
}

// NewRegistry creates a metric registry.
func NewRegistry() *Registry {
	return &Registry{metrics: make(map[string]Metric)}
}

// Register adds a metric. Panics if the name is already used by a different metric.
func (r *Registry) Register(m Metric) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.metrics[m.Name()]; ok && existing != m {
		panic(fmt.Sprintf("metric %s already registered", m.Name()))
	}
	r.metrics[m.Name()] = m
}

// Get returns a metric by name, or nil.
func (r *Registry) Get(name string) Metric {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.metrics[name]
}

// WritePrometheus writes all metrics in Prometheus exposition format.
func (r *Registry) WritePrometheus(w io.Writer) {
	r.mu.RLock()
	names := make([]string, 0, len(r.metrics))
	for n := range r.metrics {
		names = append(names, n)
	}
	r.mu.RUnlock()
	sort.Strings(names)
	for _, n := range names {
		r.mu.RLock()
		m := r.metrics[n]
		r.mu.RUnlock()
		if m == nil {
			continue
		}
		fmt.Fprintf(w, "# HELP %s %s\n", m.Name(), m.Help())
		fmt.Fprintf(w, "# TYPE %s %s\n", m.Name(), m.Type())
		m.Write(w)
	}
}

// CounterMetric is a monotonically increasing counter.
type CounterMetric struct {
	name string
	help string
	val  uint64
	mu   sync.RWMutex
}

// NewCounter creates a counter.
func NewCounter(name, help string) *CounterMetric {
	if !validName(name) {
		panic("invalid metric name: " + name)
	}
	return &CounterMetric{name: name, help: help}
}

func (c *CounterMetric) Name() string     { return c.name }
func (c *CounterMetric) Help() string     { return c.help }
func (c *CounterMetric) Type() MetricType { return Counter }
func (c *CounterMetric) Inc() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.val++
}
func (c *CounterMetric) Add(n uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.val += n
}
func (c *CounterMetric) Value() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.val
}
func (c *CounterMetric) Write(w io.Writer) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	fmt.Fprintf(w, "%s %d\n", c.name, c.val)
}

// GaugeMetric is a value that can go up and down.
type GaugeMetric struct {
	name string
	help string
	val  float64
	mu   sync.RWMutex
}

// NewGauge creates a gauge.
func NewGauge(name, help string) *GaugeMetric {
	if !validName(name) {
		panic("invalid metric name: " + name)
	}
	return &GaugeMetric{name: name, help: help}
}

func (g *GaugeMetric) Name() string     { return g.name }
func (g *GaugeMetric) Help() string     { return g.help }
func (g *GaugeMetric) Type() MetricType { return Gauge }
func (g *GaugeMetric) Set(v float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.val = v
}
func (g *GaugeMetric) Add(v float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.val += v
}
func (g *GaugeMetric) Sub(v float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.val -= v
}
func (g *GaugeMetric) Value() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.val
}
func (g *GaugeMetric) Write(w io.Writer) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	fmt.Fprintf(w, "%s %g\n", g.name, g.val)
}

// HistogramMetric counts observations in configurable buckets.
type HistogramMetric struct {
	name    string
	help    string
	buckets []float64
	counts  []uint64
	sum     float64
	total   uint64
	mu      sync.RWMutex
}

// NewHistogram creates a histogram with the supplied bucket upper bounds.
func NewHistogram(name, help string, buckets []float64) *HistogramMetric {
	if !validName(name) {
		panic("invalid metric name: " + name)
	}
	if len(buckets) == 0 {
		buckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
	}
	sorted := make([]float64, len(buckets))
	copy(sorted, buckets)
	sort.Float64s(sorted)
	return &HistogramMetric{
		name:    name,
		help:    help,
		buckets: sorted,
		counts:  make([]uint64, len(sorted)),
	}
}

func (h *HistogramMetric) Name() string     { return h.name }
func (h *HistogramMetric) Help() string     { return h.help }
func (h *HistogramMetric) Type() MetricType { return Histogram }

// Observe records a sample.
func (h *HistogramMetric) Observe(v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sum += v
	h.total++
	for i, b := range h.buckets {
		if v <= b {
			h.counts[i]++
		}
	}
}

func (h *HistogramMetric) Write(w io.Writer) {
	base := h.name + "_bucket"
	h.mu.RLock()
	defer h.mu.RUnlock()
	for i, b := range h.buckets {
		fmt.Fprintf(w, "%s{le=\"%g\"} %d\n", base, b, h.counts[i])
	}
	fmt.Fprintf(w, "%s{le=\"+Inf\"} %d\n", base, h.total)
	fmt.Fprintf(w, "%s_sum %g\n", h.name, h.sum)
	fmt.Fprintf(w, "%s_count %d\n", h.name, h.total)
}

// Default registry used by the exchange.
var Default = NewRegistry()

// MustCounter registers and returns a counter on the default registry.
func MustCounter(name, help string) *CounterMetric {
	c := NewCounter(name, help)
	Default.Register(c)
	return c
}

// MustGauge registers and returns a gauge on the default registry.
func MustGauge(name, help string) *GaugeMetric {
	g := NewGauge(name, help)
	Default.Register(g)
	return g
}

// MustHistogram registers and returns a histogram on the default registry.
func MustHistogram(name, help string, buckets []float64) *HistogramMetric {
	h := NewHistogram(name, help, buckets)
	Default.Register(h)
	return h
}

func validName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == ':') {
			return false
		}
	}
	return !strings.HasPrefix(name, "_")
}

package observability

import (
	"runtime"
	"sync/atomic"
	"time"
)

var (
	goGoroutines    = MustGauge("go_goroutines", "Number of goroutines.")
	goThreads       = MustGauge("go_threads", "Number of logical CPUs.")
	goMemAlloc      = MustGauge("go_mem_alloc_bytes", "Bytes allocated and still in use.")
	goMemSys        = MustGauge("go_mem_sys_bytes", "Total bytes obtained from the OS.")
	goMemHeap       = MustGauge("go_mem_heap_inuse_bytes", "Heap in-use bytes.")
	goGCFrac        = MustGauge("go_gc_cpu_fraction", "Fraction of CPU used by GC since program start.")
	goNumGC         = MustGauge("go_gc_total", "Total number of completed GC cycles.")
	goNextGC        = MustGauge("go_gc_next_heap_goal_bytes", "Target heap size for next GC cycle.")
	processUptime   = MustGauge("process_uptime_seconds", "Process uptime in seconds.")
	processRSSBytes = MustGauge("process_rss_bytes", "Resident set size in bytes (approximated by MemStats.Sys).")
)

var (
	processStartTime    = time.Now().Unix()
	lastCollectUnixNano atomic.Int64
)

// CollectGoRuntime reads runtime.MemStats and updates the go_* gauges. It is
// safe to call concurrently; calls are throttled to once per second.
func CollectGoRuntime() {
	now := time.Now().UnixNano()
	if prev := lastCollectUnixNano.Load(); prev != 0 && now-prev < int64(time.Second) {
		processUptime.Set(float64(time.Now().Unix() - processStartTime))
		return
	}
	lastCollectUnixNano.Store(now)

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	goGoroutines.Set(float64(runtime.NumGoroutine()))
	goThreads.Set(float64(runtime.NumCPU()))
	goMemAlloc.Set(float64(m.Alloc))
	goMemSys.Set(float64(m.Sys))
	goMemHeap.Set(float64(m.HeapInuse))
	goGCFrac.Set(m.GCCPUFraction)
	goNumGC.Set(float64(m.NumGC))
	goNextGC.Set(float64(m.NextGC))
	processUptime.Set(float64(time.Now().Unix() - processStartTime))
	processRSSBytes.Set(float64(m.Sys))
}

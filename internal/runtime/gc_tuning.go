package runtime

import (
	"log"
	"runtime"
	"runtime/debug"
	"time"
)

// GCConfig configures garbage collection settings
type GCConfig struct {
	// TargetGCPercent sets the target heap growth percentage before GC
	// Default is 100 (GC when heap doubles)
	// Lower values = more frequent GC, lower memory usage
	// Higher values = less frequent GC, higher memory usage
	TargetGCPercent int

	// MaxMemoryMB sets the soft memory limit in megabytes
	MaxMemoryMB int64

	// EnableMemoryLimit enables the soft memory limit
	EnableMemoryLimit bool
}

// OptimizeForLowLatency configures GC for low-latency workloads
func OptimizeForLowLatency() {
	// Set GOGC to 200 for less frequent GC
	debug.SetGCPercent(200)

	// Set memory limit to 80% of available memory (if known)
	// This helps prevent OOM while allowing heap to grow

	log.Println("GC optimized for low latency: GOGC=200")
}

// OptimizeForHighThroughput configures GC for high-throughput workloads
func OptimizeForHighThroughput() {
	// Set GOGC to 300 for even less frequent GC
	debug.SetGCPercent(300)

	log.Println("GC optimized for high throughput: GOGC=300")
}

// OptimizeForMemoryEfficiency configures GC for memory-constrained environments
func OptimizeForMemoryEfficiency() {
	// Set GOGC to 50 for more frequent GC
	debug.SetGCPercent(50)

	log.Println("GC optimized for memory efficiency: GOGC=50")
}

// ApplyGCConfig applies custom GC configuration
func ApplyGCConfig(config GCConfig) {
	if config.TargetGCPercent > 0 {
		debug.SetGCPercent(config.TargetGCPercent)
		log.Printf("GC percent set to: %d", config.TargetGCPercent)
	}

	if config.EnableMemoryLimit && config.MaxMemoryMB > 0 {
		// Set soft memory limit (Go 1.19+)
		limit := config.MaxMemoryMB * 1024 * 1024
		debug.SetMemoryLimit(limit)
		log.Printf("Memory limit set to: %d MB", config.MaxMemoryMB)
	}
}

// MonitorGCStats monitors and logs GC statistics
func MonitorGCStats(interval time.Duration, stopChan <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastStats debug.GCStats
	debug.ReadGCStats(&lastStats)

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			var stats debug.GCStats
			debug.ReadGCStats(&stats)

			// Calculate GC metrics since last check
			numGC := stats.NumGC - lastStats.NumGC
			if numGC > 0 {
				avgPause := stats.PauseTotal - lastStats.PauseTotal
				if numGC > 0 {
					avgPause = avgPause / time.Duration(numGC)
				}

				log.Printf("GC Stats: NumGC=%d, AvgPause=%v, LastGC=%v",
					numGC, avgPause, stats.LastGC)
			}

			lastStats = stats
		}
	}
}

// GetMemoryStats returns current memory statistics
func GetMemoryStats() MemoryStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return MemoryStats{
		Alloc:        m.Alloc,
		TotalAlloc:   m.TotalAlloc,
		Sys:          m.Sys,
		NumGC:        m.NumGC,
		PauseTotalNs: m.PauseTotalNs,
		HeapAlloc:    m.HeapAlloc,
		HeapSys:      m.HeapSys,
		HeapIdle:     m.HeapIdle,
		HeapInuse:    m.HeapInuse,
		HeapReleased: m.HeapReleased,
		HeapObjects:  m.HeapObjects,
		StackInuse:   m.StackInuse,
		StackSys:     m.StackSys,
		MSpanInuse:   m.MSpanInuse,
		MSpanSys:     m.MSpanSys,
		MCacheInuse:  m.MCacheInuse,
		MCacheSys:    m.MCacheSys,
		GCSys:        m.GCSys,
		NextGC:       m.NextGC,
		LastGC:       m.LastGC,
	}
}

// MemoryStats represents memory statistics
type MemoryStats struct {
	Alloc        uint64 // Bytes allocated and in use
	TotalAlloc   uint64 // Bytes allocated (cumulative)
	Sys          uint64 // Bytes obtained from system
	NumGC        uint32 // Number of GC runs
	PauseTotalNs uint64 // Total GC pause time in nanoseconds
	HeapAlloc    uint64 // Bytes allocated on heap
	HeapSys      uint64 // Bytes obtained from system for heap
	HeapIdle     uint64 // Bytes in idle spans
	HeapInuse    uint64 // Bytes in in-use spans
	HeapReleased uint64 // Bytes released to OS
	HeapObjects  uint64 // Number of allocated objects
	StackInuse   uint64 // Bytes in stack spans
	StackSys     uint64 // Bytes obtained from system for stack
	MSpanInuse   uint64 // Bytes in mspan structures
	MSpanSys     uint64 // Bytes obtained from system for mspan
	MCacheInuse  uint64 // Bytes in mcache structures
	MCacheSys    uint64 // Bytes obtained from system for mcache
	GCSys        uint64 // Bytes used for GC metadata
	NextGC       uint64 // Target heap size for next GC
	LastGC       uint64 // Time of last GC (Unix timestamp)
}

// ForceGC triggers a garbage collection
func ForceGC() {
	runtime.GC()
}

// SetMaxProcs sets the maximum number of CPUs that can execute simultaneously
func SetMaxProcs(n int) int {
	return runtime.GOMAXPROCS(n)
}

// GetNumCPU returns the number of logical CPUs
func GetNumCPU() int {
	return runtime.NumCPU()
}

// GetNumGoroutine returns the number of goroutines
func GetNumGoroutine() int {
	return runtime.NumGoroutine()
}

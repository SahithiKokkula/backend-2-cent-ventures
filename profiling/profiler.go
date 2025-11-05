package profiling

import (
	"fmt"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"runtime"
	rpprof "runtime/pprof"
	"runtime/trace"
	"time"
)

// ProfilerConfig holds profiler configuration
type ProfilerConfig struct {
	EnableCPUProfile       bool
	EnableMemProfile       bool
	EnableBlockProfile     bool
	EnableMutexProfile     bool
	EnableGoroutineProfile bool
	EnableTracing          bool
	ProfileDuration        time.Duration
	OutputDir              string
}

// DefaultProfilerConfig returns default profiler configuration
func DefaultProfilerConfig() *ProfilerConfig {
	return &ProfilerConfig{
		EnableCPUProfile:       true,
		EnableMemProfile:       true,
		EnableBlockProfile:     true,
		EnableMutexProfile:     true,
		EnableGoroutineProfile: true,
		EnableTracing:          false, // Can be expensive
		ProfileDuration:        30 * time.Second,
		OutputDir:              "./profiles",
	}
}

// Profiler manages runtime profiling
type Profiler struct {
	config *ProfilerConfig
	server *http.Server
}

// NewProfiler creates a new profiler
func NewProfiler(config *ProfilerConfig) *Profiler {
	if config == nil {
		config = DefaultProfilerConfig()
	}

	// Create output directory
	if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
		log.Printf("Failed to create profiles directory: %v", err)
	}

	return &Profiler{
		config: config,
	}
}

// StartPProfServer starts the pprof HTTP server
// Access profiles at:
//
//	http://localhost:6060/debug/pprof/
//	http://localhost:6060/debug/pprof/heap
//	http://localhost:6060/debug/pprof/goroutine
//	http://localhost:6060/debug/pprof/threadcreate
//	http://localhost:6060/debug/pprof/block
//	http://localhost:6060/debug/pprof/mutex
func (p *Profiler) StartPProfServer(port int) error {
	mux := http.NewServeMux()

	// Register pprof handlers
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	// Register profile handlers
	mux.Handle("/debug/pprof/heap", pprof.Handler("heap"))
	mux.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	mux.Handle("/debug/pprof/threadcreate", pprof.Handler("threadcreate"))
	mux.Handle("/debug/pprof/block", pprof.Handler("block"))
	mux.Handle("/debug/pprof/mutex", pprof.Handler("mutex"))

	p.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	log.Printf("🔍 pprof server started on http://localhost:%d/debug/pprof/", port)
	log.Printf("   Available profiles:")
	log.Printf("   - CPU: http://localhost:%d/debug/pprof/profile?seconds=30", port)
	log.Printf("   - Heap: http://localhost:%d/debug/pprof/heap", port)
	log.Printf("   - Goroutines: http://localhost:%d/debug/pprof/goroutine", port)
	log.Printf("   - Block: http://localhost:%d/debug/pprof/block", port)
	log.Printf("   - Mutex: http://localhost:%d/debug/pprof/mutex", port)

	go func() {
		if err := p.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("pprof server error: %v", err)
		}
	}()

	return nil
}

// StopPProfServer stops the pprof server
func (p *Profiler) StopPProfServer() error {
	if p.server != nil {
		return p.server.Close()
	}
	return nil
}

// EnableRuntimeProfiling enables block and mutex profiling
func (p *Profiler) EnableRuntimeProfiling() {
	if p.config.EnableBlockProfile {
		// Enable block profiling (captures goroutine blocking events)
		// Set rate to 1 to capture every blocking event
		runtime.SetBlockProfileRate(1)
		log.Println("Block profiling enabled (rate=1)")
	}

	if p.config.EnableMutexProfile {
		// Enable mutex profiling (captures lock contention)
		// Fraction=1 means sample 100% of mutex events
		runtime.SetMutexProfileFraction(1)
		log.Println("Mutex profiling enabled (fraction=1)")
	}
}

// CaptureProfiles captures all enabled profiles to files
func (p *Profiler) CaptureProfiles() error {
	timestamp := time.Now().Format("20060102_150405")

	// CPU Profile
	if p.config.EnableCPUProfile {
		if err := p.captureCPUProfile(timestamp); err != nil {
			log.Printf("Failed to capture CPU profile: %v", err)
		}
	}

	// Memory Profile
	if p.config.EnableMemProfile {
		if err := p.captureMemProfile(timestamp); err != nil {
			log.Printf("Failed to capture memory profile: %v", err)
		}
	}

	// Goroutine Profile
	if p.config.EnableGoroutineProfile {
		if err := p.captureGoroutineProfile(timestamp); err != nil {
			log.Printf("Failed to capture goroutine profile: %v", err)
		}
	}

	// Block Profile
	if p.config.EnableBlockProfile {
		if err := p.captureBlockProfile(timestamp); err != nil {
			log.Printf("Failed to capture block profile: %v", err)
		}
	}

	// Mutex Profile
	if p.config.EnableMutexProfile {
		if err := p.captureMutexProfile(timestamp); err != nil {
			log.Printf("Failed to capture mutex profile: %v", err)
		}
	}

	// Trace
	if p.config.EnableTracing {
		if err := p.captureTrace(timestamp); err != nil {
			log.Printf("Failed to capture trace: %v", err)
		}
	}

	return nil
}

// captureCPUProfile captures a CPU profile
func (p *Profiler) captureCPUProfile(timestamp string) error {
	filename := fmt.Sprintf("%s/cpu_%s.prof", p.config.OutputDir, timestamp)
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	log.Printf("🔍 Capturing CPU profile for %v...", p.config.ProfileDuration)
	if err := rpprof.StartCPUProfile(f); err != nil {
		return err
	}

	time.Sleep(p.config.ProfileDuration)
	rpprof.StopCPUProfile()

	log.Printf("CPU profile saved: %s", filename)
	log.Printf("   Analyze: go tool pprof -http=:8081 %s", filename)
	return nil
}

// captureMemProfile captures a memory profile
func (p *Profiler) captureMemProfile(timestamp string) error {
	filename := fmt.Sprintf("%s/mem_%s.prof", p.config.OutputDir, timestamp)
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	// Force GC to get accurate memory stats
	runtime.GC()

	if err := rpprof.WriteHeapProfile(f); err != nil {
		return err
	}

	log.Printf("Memory profile saved: %s", filename)
	log.Printf("   Analyze: go tool pprof -http=:8082 %s", filename)
	return nil
}

// captureGoroutineProfile captures a goroutine profile
func (p *Profiler) captureGoroutineProfile(timestamp string) error {
	filename := fmt.Sprintf("%s/goroutine_%s.prof", p.config.OutputDir, timestamp)
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := rpprof.Lookup("goroutine").WriteTo(f, 2); err != nil {
		return err
	}

	log.Printf("Goroutine profile saved: %s", filename)
	log.Printf("   Current goroutines: %d", runtime.NumGoroutine())
	return nil
}

// captureBlockProfile captures a block profile
func (p *Profiler) captureBlockProfile(timestamp string) error {
	filename := fmt.Sprintf("%s/block_%s.prof", p.config.OutputDir, timestamp)
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := rpprof.Lookup("block").WriteTo(f, 0); err != nil {
		return err
	}

	log.Printf("Block profile saved: %s", filename)
	log.Printf("   Analyze: go tool pprof -http=:8083 %s", filename)
	return nil
}

// captureMutexProfile captures a mutex profile
func (p *Profiler) captureMutexProfile(timestamp string) error {
	filename := fmt.Sprintf("%s/mutex_%s.prof", p.config.OutputDir, timestamp)
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := rpprof.Lookup("mutex").WriteTo(f, 0); err != nil {
		return err
	}

	log.Printf("Mutex profile saved: %s", filename)
	log.Printf("   Analyze: go tool pprof -http=:8084 %s", filename)
	return nil
}

// captureTrace captures an execution trace
func (p *Profiler) captureTrace(timestamp string) error {
	filename := fmt.Sprintf("%s/trace_%s.out", p.config.OutputDir, timestamp)
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	log.Printf("🔍 Capturing trace for %v...", p.config.ProfileDuration)
	if err := trace.Start(f); err != nil {
		return err
	}

	time.Sleep(p.config.ProfileDuration)
	trace.Stop()

	log.Printf("Trace saved: %s", filename)
	log.Printf("   Analyze: go tool trace %s", filename)
	return nil
}

// PrintMemStats prints current memory statistics
func (p *Profiler) PrintMemStats() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	log.Println(">Memory Statistics:")
	log.Printf("   Alloc      = %v MB (currently allocated)", m.Alloc/1024/1024)
	log.Printf("   TotalAlloc = %v MB (cumulative allocations)", m.TotalAlloc/1024/1024)
	log.Printf("   Sys        = %v MB (obtained from OS)", m.Sys/1024/1024)
	log.Printf("   NumGC      = %v (garbage collections)", m.NumGC)
	log.Printf("   GC Pause   = %v ms (last pause)", m.PauseNs[(m.NumGC+255)%256]/1000000)
	log.Printf("   Goroutines = %v (active goroutines)", runtime.NumGoroutine())
}

// MonitorMemory starts periodic memory monitoring
func (p *Profiler) MonitorMemory(interval time.Duration, stopCh <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf(">Starting memory monitoring (interval: %v)", interval)

	for {
		select {
		case <-ticker.C:
			p.PrintMemStats()
		case <-stopCh:
			log.Println(">Memory monitoring stopped")
			return
		}
	}
}

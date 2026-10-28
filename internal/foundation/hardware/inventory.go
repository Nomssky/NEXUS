// Package hardware provides a best-effort, platform-aware inventory of the
// local machine's compute resources for model-suitability decisions.
//
// Design rules (hardware-aware routing contract):
//   - Probes are BEST EFFORT. A missing GPU, absent driver, unsupported OS or a
//     restricted system interface never crashes NEXUS and never fails a request.
//   - Observed, estimated and unknown values are kept distinct. An estimate is
//     never represented as a measured fact.
//   - Detection is injectable so tests are deterministic and never depend on the
//     real host hardware.
//   - Dynamic observations are cached and refreshed at a bounded interval; no
//     unbounded probe or poll loop is ever spawned.
package hardware

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GPU is one best-effort GPU observation. MemoryBytes is only meaningful when
// MemoryKnown is true; an advertised figure is never assumed to be entirely
// available to NEXUS.
type GPU struct {
	Vendor           string `json:"vendor,omitempty"`
	Name             string `json:"name,omitempty"`
	MemoryBytes      int64  `json:"memory_bytes,omitempty"`
	MemoryKnown      bool   `json:"memory_known"`
	ComputeAvailable bool   `json:"compute_available"`
	Source           string `json:"source,omitempty"`
}

// Inventory is a point-in-time, best-effort snapshot of local resources.
type Inventory struct {
	OS                string    `json:"os"`
	Arch              string    `json:"arch"`
	CPUCores          int       `json:"cpu_cores"`
	TotalRAMBytes     int64     `json:"total_ram_bytes,omitempty"`
	AvailableRAMBytes int64     `json:"available_ram_bytes,omitempty"`
	RAMKnown          bool      `json:"ram_known"`
	GPUs              []GPU     `json:"gpus,omitempty"`
	CollectedAt       time.Time `json:"collected_at"`
	// ProbeNotes are sanitized, non-secret explanations of degraded detection
	// (e.g. "GPU detection unavailable on this platform").
	ProbeNotes []string `json:"probe_notes,omitempty"`
}

// Probe collects a raw inventory. Implementations must honor ctx and must not
// return secrets in ProbeNotes.
type Probe interface {
	Collect(ctx context.Context) (Inventory, error)
}

// FuncProbe adapts a function to a Probe (used by tests and embedders).
type FuncProbe func(ctx context.Context) (Inventory, error)

// Collect implements Probe.
func (f FuncProbe) Collect(ctx context.Context) (Inventory, error) { return f(ctx) }

// Collector caches a snapshot and refreshes it at most once per TTL.
type Collector struct {
	probe Probe
	ttl   time.Duration
	now   func() time.Time

	mu   sync.Mutex
	snap Inventory
	have bool
}

// NewCollector wires a collector. A nil probe uses DefaultProbe; a non-positive
// TTL selects the shipped default.
func NewCollector(p Probe, ttl time.Duration) *Collector {
	if p == nil {
		p = DefaultProbe{}
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &Collector{probe: p, ttl: ttl, now: time.Now}
}

// WithClock overrides the clock (tests).
func (c *Collector) WithClock(now func() time.Time) *Collector {
	if now != nil {
		c.now = now
	}
	return c
}

// Snapshot returns the cached inventory, refreshing it when stale. A failed
// probe degrades gracefully: the last good snapshot (if any) is returned with a
// sanitized note; otherwise a minimal, valid snapshot is returned.
func (c *Collector) Snapshot(ctx context.Context) Inventory {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.have && c.now().Sub(c.snap.CollectedAt) < c.ttl {
		return c.snap
	}
	return c.refresh(ctx)
}

// ForceRefresh bypasses the TTL.
func (c *Collector) ForceRefresh(ctx context.Context) Inventory {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.refresh(ctx)
}

func (c *Collector) refresh(ctx context.Context) Inventory {
	inv, err := c.probe.Collect(ctx)
	if err != nil {
		if c.have {
			s := c.snap
			s.ProbeNotes = append([]string(nil), s.ProbeNotes...)
			s.ProbeNotes = append(s.ProbeNotes, "probe failed: "+sanitize(err.Error()))
			return s
		}
		inv = baseline(c.now())
		inv.ProbeNotes = append(inv.ProbeNotes, "probe failed: "+sanitize(err.Error()))
	}
	normalize(&inv, c.now())
	c.snap = inv
	c.have = true
	return inv
}

func baseline(now time.Time) Inventory {
	return Inventory{OS: runtime.GOOS, Arch: runtime.GOARCH, CPUCores: runtime.NumCPU(), CollectedAt: now}
}

func normalize(inv *Inventory, now time.Time) {
	if inv.CollectedAt.IsZero() {
		inv.CollectedAt = now
	}
	if inv.OS == "" {
		inv.OS = runtime.GOOS
	}
	if inv.Arch == "" {
		inv.Arch = runtime.GOARCH
	}
	if inv.CPUCores <= 0 {
		inv.CPUCores = runtime.NumCPU()
	}
}

// DefaultProbe is the shipping, dependency-free probe: runtime identity plus
// Linux /proc/meminfo. GPU detection is intentionally absent without a reliable,
// dependency-free interface; it is reported as degraded rather than guessed.
type DefaultProbe struct{}

// Collect implements Probe.
func (DefaultProbe) Collect(ctx context.Context) (Inventory, error) {
	inv := baseline(time.Now())
	switch runtime.GOOS {
	case "linux":
		if b, err := os.ReadFile("/proc/meminfo"); err == nil {
			if total, avail, ok := parseMeminfo(string(b)); ok {
				inv.TotalRAMBytes = total
				inv.AvailableRAMBytes = avail
				inv.RAMKnown = true
			} else {
				inv.ProbeNotes = append(inv.ProbeNotes, "RAM detection: /proc/meminfo unparsable; RAM unknown")
			}
		} else {
			inv.ProbeNotes = append(inv.ProbeNotes, "RAM detection: /proc/meminfo unavailable; RAM unknown")
		}
	default:
		inv.ProbeNotes = append(inv.ProbeNotes, "RAM detection not implemented on "+runtime.GOOS+"; RAM unknown")
	}
	// GPU detection is not implemented without an optional tool/driver boundary.
	inv.ProbeNotes = append(inv.ProbeNotes, "GPU detection not implemented; CPU/RAM-only selection in effect")
	return inv, nil
}

// parseMeminfo extracts MemTotal and MemAvailable (kB) from /proc/meminfo.
func parseMeminfo(s string) (total, available int64, ok bool) {
	var haveTotal bool
	for _, line := range strings.Split(s, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			if n, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
				total = n * 1024
				haveTotal = true
			}
		case "MemAvailable":
			if n, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
				available = n * 1024
			}
		}
	}
	return total, available, haveTotal
}

// ModelMemoryInput describes the inputs to a conservative memory estimate.
type ModelMemoryInput struct {
	// WeightsBytes is the on-disk/quantized weight size when known (0 = unknown).
	WeightsBytes int64
	// ContextTokens is the requested context window in tokens.
	ContextTokens int64
	// KVBytesPerToken estimates KV-cache bytes per token (0 = unknown).
	KVBytesPerToken int64
	// Concurrency is the number of concurrent generations to budget for (>=1).
	Concurrency int
	// OverheadFraction covers runtime/driver overhead (e.g. 0.15).
	OverheadFraction float64
	// MarginFraction is the safety margin added on top (e.g. 0.10).
	MarginFraction float64
}

// MemoryEstimate is an explicit, conservative estimate. Confidence is one of
// "measured" (weights supplied), "estimated" (derived) or "unknown".
type MemoryEstimate struct {
	WeightsBytes  int64  `json:"weights_bytes"`
	KVBytes       int64  `json:"kv_bytes"`
	OverheadBytes int64  `json:"overhead_bytes"`
	MarginBytes   int64  `json:"margin_bytes"`
	TotalBytes    int64  `json:"total_bytes"`
	Confidence    string `json:"confidence"`
}

// EstimateModelMemory produces a conservative estimate. It never claims a model
// fits from parameter count alone: when weights are unknown the confidence is
// "unknown" and TotalBytes is 0, forcing callers to treat the result as
// insufficient information rather than a fit.
func EstimateModelMemory(in ModelMemoryInput) MemoryEstimate {
	if in.WeightsBytes <= 0 {
		return MemoryEstimate{Confidence: "unknown"}
	}
	conc := in.Concurrency
	if conc < 1 {
		conc = 1
	}
	kv := in.KVBytesPerToken * in.ContextTokens * int64(conc)
	base := in.WeightsBytes + kv
	overhead := int64(float64(base) * clampFraction(in.OverheadFraction))
	margin := int64(float64(base+overhead) * clampFraction(in.MarginFraction))
	total := base + overhead + margin
	return MemoryEstimate{
		WeightsBytes:  in.WeightsBytes,
		KVBytes:       kv,
		OverheadBytes: overhead,
		MarginBytes:   margin,
		TotalBytes:    total,
		Confidence:    "estimated",
	}
}

func clampFraction(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// sanitize strips control characters and bounds length so a probe error can be
// surfaced as a diagnostic without leaking or bloating logs.
func sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	if len(s) > 200 {
		s = s[:200]
	}
	return strings.TrimSpace(s)
}

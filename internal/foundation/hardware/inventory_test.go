package hardware

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestParseMeminfo(t *testing.T) {
	sample := "MemTotal:       16384000 kB\nMemFree:        2000000 kB\nMemAvailable:    8192000 kB\n"
	total, avail, ok := parseMeminfo(sample)
	if !ok {
		t.Fatal("expected parse success")
	}
	if total != 16384000*1024 {
		t.Fatalf("total=%d", total)
	}
	if avail != 8192000*1024 {
		t.Fatalf("available=%d", avail)
	}
	if _, _, ok := parseMeminfo("no meminfo here"); ok {
		t.Fatal("expected parse failure on unrelated content")
	}
}

func TestCollectorCachesAndRefreshes(t *testing.T) {
	var calls int
	probe := FuncProbe(func(_ context.Context) (Inventory, error) {
		calls++
		return Inventory{OS: "linux", Arch: "amd64", CPUCores: 8, RAMKnown: true,
			TotalRAMBytes: 16 << 30, AvailableRAMBytes: 8 << 30, CollectedAt: time.Now()}, nil
	})
	now := time.Now()
	c := NewCollector(probe, 30*time.Second).WithClock(func() time.Time { return now })
	_ = c.Snapshot(context.Background())
	_ = c.Snapshot(context.Background())
	if calls != 1 {
		t.Fatalf("expected one probe within TTL, got %d", calls)
	}
	now = now.Add(31 * time.Second)
	if inv := c.Snapshot(context.Background()); !inv.RAMKnown {
		t.Fatal("expected refreshed snapshot")
	}
	if calls != 2 {
		t.Fatalf("expected refresh after TTL, got %d calls", calls)
	}
	_ = c.ForceRefresh(context.Background())
	if calls != 3 {
		t.Fatalf("expected forced refresh, got %d", calls)
	}
}

func TestCollectorDegradesOnProbeError(t *testing.T) {
	probe := FuncProbe(func(_ context.Context) (Inventory, error) { return Inventory{}, errors.New("boom\x00secret") })
	c := NewCollector(probe, time.Second)
	inv := c.Snapshot(context.Background())
	if inv.OS == "" || inv.CPUCores == 0 {
		t.Fatalf("baseline must still be populated: %+v", inv)
	}
	if len(inv.ProbeNotes) == 0 {
		t.Fatal("expected a sanitized probe note")
	}
	for _, n := range inv.ProbeNotes {
		for _, r := range n {
			if r == 0 {
				t.Fatal("probe note must be sanitized (no control chars)")
			}
		}
	}
}

func TestEstimateModelMemory(t *testing.T) {
	unknown := EstimateModelMemory(ModelMemoryInput{})
	if unknown.Confidence != "unknown" || unknown.TotalBytes != 0 {
		t.Fatalf("unknown weights must yield unknown confidence and zero total: %+v", unknown)
	}
	est := EstimateModelMemory(ModelMemoryInput{
		WeightsBytes: 4 << 30, ContextTokens: 8192, KVBytesPerToken: 100_000,
		Concurrency: 2, OverheadFraction: 0.15, MarginFraction: 0.10,
	})
	if est.Confidence != "estimated" {
		t.Fatalf("confidence=%s", est.Confidence)
	}
	if est.TotalBytes <= est.WeightsBytes {
		t.Fatalf("total must exceed weights (overhead+margin+kv): %+v", est)
	}
	if est.KVBytes <= 0 {
		t.Fatalf("kv must be accounted for: %+v", est)
	}
	// Concurrency must multiply the KV component.
	one := EstimateModelMemory(ModelMemoryInput{WeightsBytes: 1 << 30, ContextTokens: 1000, KVBytesPerToken: 10})
	two := EstimateModelMemory(ModelMemoryInput{WeightsBytes: 1 << 30, ContextTokens: 1000, KVBytesPerToken: 10, Concurrency: 2})
	if two.KVBytes <= one.KVBytes {
		t.Fatalf("concurrency must increase KV: one=%d two=%d", one.KVBytes, two.KVBytes)
	}
}

package agentintel

// Working memory (contracts/AGENT_MEMORY_CONTEXT_CONTRACTS.md §2): bounded,
// process-local execution state for ONE objective.
//
// It holds exactly what the active objective needs — objective, plan, current
// step, recent observations, the pending action and budget/deadline metadata —
// and it dies with the execution. It is never persisted, never promoted
// implicitly, and nothing here becomes durable memory without an explicit,
// validated write.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// Default working-memory bounds. Working memory is scratch state: if it needs to
// be big, the objective is doing too much in one iteration.
const (
	defaultWorkingEntries   = 64
	defaultWorkingValueSize = 4 * 1024
	defaultWorkingObs       = 16
	// defaultWorkingEffects bounds the completed-effect fingerprints below.
	// One iteration performs at most one consequential action, so this is
	// far above any reachable count inside the iteration budget.
	defaultWorkingEffects = 64
)

// WorkingMemory is per-execution, process-local scratch state (§2). It is
// bounded on every axis and discarded when the execution ends.
type WorkingMemory struct {
	mu           sync.Mutex
	maxEntries   int
	maxValue     int
	maxObs       int
	entries      map[string]string
	objective    string
	stepID       string
	stepIntent   string
	observations []Observation
	pending      string
	deadline     time.Time
	discarded    bool
	// effects fingerprints every COMPLETED consequential effect of this
	// execution (contract §10). A proposal identical to a recorded one is a
	// duplicate the runtime must not blindly re-execute.
	effects map[string]struct{}
}

// NewWorkingMemory returns empty, bounded working memory.
func NewWorkingMemory() *WorkingMemory {
	return &WorkingMemory{
		maxEntries: defaultWorkingEntries,
		maxValue:   defaultWorkingValueSize,
		maxObs:     defaultWorkingObs,
		entries:    map[string]string{},
		effects:    map[string]struct{}{},
	}
}

// Bind records the objective this working memory belongs to.
func (m *WorkingMemory) Bind(objective string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objective = truncate(objective, 512)
}

// SetDeadline records the execution deadline for budget bookkeeping.
func (m *WorkingMemory) SetDeadline(at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deadline = at
}

// SetStep records the step the loop is currently executing.
func (m *WorkingMemory) SetStep(stepID, intent string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stepID, m.stepIntent = truncate(stepID, 64), truncate(intent, 256)
}

// SetPending records the action being proposed or performed.
func (m *WorkingMemory) SetPending(action string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending = truncate(action, 256)
}

// AddObservation appends an observation, keeping the newest ones (newest first)
// within the bound.
func (m *WorkingMemory) AddObservation(o Observation) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.observations) >= m.maxObs {
		m.observations = m.observations[:m.maxObs-1]
	}
	m.observations = append([]Observation{o}, m.observations...)
}

// Put stores a bounded scratch entry. Once the entry bound is reached the write
// is refused rather than growing without limit.
func (m *WorkingMemory) Put(k, v string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.discarded {
		return fmt.Errorf("working memory has been discarded")
	}
	key := truncate(k, 64)
	if key == "" {
		return fmt.Errorf("working memory key is required")
	}
	if _, exists := m.entries[key]; !exists && len(m.entries) >= m.maxEntries {
		return fmt.Errorf("working memory holds its %d entry bound", m.maxEntries)
	}
	m.entries[key] = truncate(v, m.maxValue)
	return nil
}

// Get returns one scratch entry.
func (m *WorkingMemory) Get(k string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.entries[k]
	return v, ok
}

// Delete removes one scratch entry.
func (m *WorkingMemory) Delete(k string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, k)
}

// WorkingSnapshot is a bounded, read-only view of working memory.
type WorkingSnapshot struct {
	Objective    string
	StepID       string
	StepIntent   string
	Pending      string
	Deadline     time.Time
	Entries      map[string]string
	Observations []Observation
}

// Snapshot returns the current contents, newest observations first.
func (m *WorkingMemory) Snapshot() WorkingSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries := make(map[string]string, len(m.entries))
	for k, v := range m.entries {
		entries[k] = v
	}
	obs := append([]Observation(nil), m.observations...)
	return WorkingSnapshot{
		Objective: m.objective, StepID: m.stepID, StepIntent: m.stepIntent,
		Pending: m.pending, Deadline: m.deadline, Entries: entries, Observations: obs,
	}
}

// NoteEffect records a completed consequential effect. It reports false when
// the identical effect already completed in this execution — the caller must
// refuse to duplicate it. Only effects the runtime itself performed are ever
// recorded; model claims are never fingerprints.
func (m *WorkingMemory) NoteEffect(fp string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if fp == "" {
		return false, fmt.Errorf("working memory: empty effect fingerprint")
	}
	if _, dup := m.effects[fp]; dup {
		return false, nil
	}
	if len(m.effects) >= defaultWorkingEffects {
		return false, fmt.Errorf("working memory holds its %d effect bound", defaultWorkingEffects)
	}
	m.effects[fp] = struct{}{}
	return true, nil
}

// HasEffect reports whether the identical effect already completed.
func (m *WorkingMemory) HasEffect(fp string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, dup := m.effects[fp]
	return dup
}

// EffectCount reports how many completed effects are recorded.
func (m *WorkingMemory) EffectCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.effects)
}

// repeatGuardFingerprint returns the duplicate-suppression fingerprint for an
// action proposal, or "" when repetition is harmless. Reads may repeat — the
// reliability layer itself retries reads — but anything that mutates,
// delegates, or persists must never run twice blindly in one execution
// (contract §10). An unresolvable side-effect class fails closed toward
// fingerprinting.
func repeatGuardFingerprint(r ScopeResolver, a Action) string {
	switch a.Type {
	case ActionDelegate, ActionMemoryWrite, ActionMemoryDelete:
		return effectFingerprint(a)
	case ActionToolCall:
		if se, ok := r.(SideEffectResolver); ok {
			if class, known := se.ToolSideEffect(a.Tool); known && class == string(tool.SideEffectRead) {
				return ""
			}
		}
		return effectFingerprint(a)
	default:
		return ""
	}
}

// effectFingerprint renders the identity of one consequential action proposal:
// type, effective target and payload, deterministically ordered. Two proposals
// with the same fingerprint would cause the same side effect, so the second
// one must not run blindly. Control and read-only actions have no fingerprint.
func effectFingerprint(a Action) string {
	h := sha256.New()
	write := func(s string) { _, _ = h.Write([]byte(s + "\x00")) }
	switch a.Type {
	case ActionToolCall:
		write("tool_call")
		write(a.Tool)
		write(a.Operation)
		keys := make([]string, 0, len(a.Input))
		for k := range a.Input {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			write(k)
			write(a.Input[k])
		}
	case ActionDelegate:
		write("delegate")
		write(a.AgentID)
		write(a.Objective)
	case ActionMemoryWrite:
		write("memory_write")
		write(a.Key)
		write(a.Value)
		write(a.MemoryType)
		write(a.MemoryScope)
		write(a.ObservationID)
	case ActionMemoryDelete:
		write("memory_delete")
		write(a.Key)
	default:
		return ""
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:])
}

// Discard drops every entry when the execution ends. Durable memory is never
// touched here: promotion is an explicit, validated write (contract §2, §36).
func (m *WorkingMemory) Discard() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = map[string]string{}
	m.observations = nil
	m.objective, m.stepID, m.stepIntent, m.pending = "", "", "", ""
	m.discarded = true
}

// Keys returns the scratch keys in deterministic order.
func (m *WorkingMemory) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]string, 0, len(m.entries))
	for k := range m.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) > max {
		return s[:max]
	}
	return s
}

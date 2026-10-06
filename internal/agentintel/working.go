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
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Default working-memory bounds. Working memory is scratch state: if it needs to
// be big, the objective is doing too much in one iteration.
const (
	defaultWorkingEntries   = 64
	defaultWorkingValueSize = 4 * 1024
	defaultWorkingObs       = 16
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
}

// NewWorkingMemory returns empty, bounded working memory.
func NewWorkingMemory() *WorkingMemory {
	return &WorkingMemory{
		maxEntries: defaultWorkingEntries,
		maxValue:   defaultWorkingValueSize,
		maxObs:     defaultWorkingObs,
		entries:    map[string]string{},
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

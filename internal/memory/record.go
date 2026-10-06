// Package memory implements the Agent Memory & Context Platform v1
// (contracts/AGENT_MEMORY_CONTEXT_CONTRACTS.md).
//
// It is the single durable store for agent memory, the single authorized
// retrieval path and the single bounded context-assembly boundary. It owns:
//
//   - record identity that is stable and content-independent
//   - scope authorization through the injected ScopeChecker, which the launcher
//     binds to identity.MembershipSet.AllowsScope — memory defines no roles
//   - platform-controlled provenance and trust (a writer proposes; the platform
//     decides)
//   - explicit bounds on content, metadata, record counts and retrieval
//   - optimistic versioning, explicit deletion and expiry
//   - deterministic conflict marking and deterministic ranking
//
// It never grants authority. A memory record can inform an agent; it can never
// widen a scope, grant a tool, change an actor or bypass governance.
package memory

import (
	"strings"
	"time"
)

// Scope is the visibility class of a memory record. It is authorized with the
// existing MembershipSet.AllowsScope semantics (contract §4).
type Scope string

const (
	ScopeBusiness Scope = "business"
	ScopeDivision Scope = "division"
	ScopeAgent    Scope = "agent"
)

// Valid reports whether s is one of the three canonical scopes.
func (s Scope) Valid() bool {
	switch s {
	case ScopeBusiness, ScopeDivision, ScopeAgent:
		return true
	}
	return false
}

// Narrower reports whether s is strictly narrower than other.
func (s Scope) Narrower(other Scope) bool { return scopeRank(s) > scopeRank(other) }

func scopeRank(s Scope) int {
	switch s {
	case ScopeAgent:
		return 3
	case ScopeDivision:
		return 2
	case ScopeBusiness:
		return 1
	}
	return 0
}

// Type is what a durable record IS. It is bounded on purpose: v1 has four.
type Type string

const (
	TypeFact        Type = "fact"
	TypeInstruction Type = "instruction"
	TypePreference  Type = "preference"
	TypeObservation Type = "observation"
)

// Valid reports whether t is canonical.
func (t Type) Valid() bool {
	switch t {
	case TypeFact, TypeInstruction, TypePreference, TypeObservation:
		return true
	}
	return false
}

// Source is provenance. It is assigned by the platform from the writer kind, not
// by the payload (contract §6).
type Source string

const (
	SourceUserInstruction      Source = "user_instruction"
	SourceSystemRecord         Source = "system_record"
	SourceApplicationEvent     Source = "application_event"
	SourceToolObservation      Source = "tool_observation"
	SourceValidatedAgentOutput Source = "validated_agent_output"
)

// Trust is the bounded trust classification, derived from Source (contract §6).
type Trust string

const (
	TrustUnverified Trust = "unverified"
	TrustObserved   Trust = "observed"
	TrustValidated  Trust = "validated"
	TrustExplicit   Trust = "explicit"
)

// Valid reports whether t is canonical.
func (t Trust) Valid() bool {
	switch t {
	case TrustUnverified, TrustObserved, TrustValidated, TrustExplicit:
		return true
	}
	return false
}

// WriterKind says WHO is writing, which is what determines provenance and trust.
type WriterKind string

const (
	// WriterSystem is application/platform code (validated).
	WriterSystem WriterKind = "system"
	// WriterApplicationEvent is an application event handler (validated).
	WriterApplicationEvent WriterKind = "application_event"
	// WriterUser is an authenticated human/API caller acting for themselves
	// (explicit).
	WriterUser WriterKind = "user"
	// WriterAgent is a model-proposed write (unverified, never promoted).
	WriterAgent WriterKind = "agent"
	// WriterObservation is a promotion of a real execution observation
	// (observed).
	WriterObservation WriterKind = "observation"
)

// writerProvenance is the platform-owned mapping from writer kind to
// (source, trust). There is deliberately no way for a payload to name its own
// source or trust.
var writerProvenance = map[WriterKind]struct {
	source Source
	trust  Trust
}{
	WriterSystem:           {SourceSystemRecord, TrustValidated},
	WriterApplicationEvent: {SourceApplicationEvent, TrustValidated},
	WriterUser:             {SourceUserInstruction, TrustExplicit},
	WriterAgent:            {SourceValidatedAgentOutput, TrustUnverified},
	WriterObservation:      {SourceToolObservation, TrustObserved},
}

// ProvenanceOf returns the platform-assigned source and trust for a writer kind.
// It is the only source of provenance in this package.
func ProvenanceOf(kind WriterKind) (Source, Trust, bool) {
	p, ok := writerProvenance[kind]
	if !ok {
		return "", "", false
	}
	return p.source, p.trust, true
}

// Status is the record lifecycle state (contract §10).
type Status string

const (
	StatusActive  Status = "active"
	StatusExpired Status = "expired"
	StatusDeleted Status = "deleted"
)

// Record is one durable memory entry. It extends the key/value record the
// intelligence layer used before this milestone: key, value, scope, agent,
// business and timestamps are unchanged, everything else is new.
type Record struct {
	ID         string `json:"id"`
	BusinessID string `json:"business_id"`
	DivisionID string `json:"division_id,omitempty"`
	AgentID    string `json:"agent_id"`
	Scope      Scope  `json:"scope"`
	Type       Type   `json:"type"`
	Key        string `json:"key"`
	Value      string `json:"value"`
	// Subject is the conflict namespace; it defaults to Key (contract §8).
	Subject string     `json:"subject,omitempty"`
	Source  Source     `json:"source"`
	Trust   Trust      `json:"trust"`
	Writer  WriterKind `json:"writer"`
	// Metadata is bounded structured context (never secrets — values are
	// redacted before persistence).
	Metadata map[string]string `json:"metadata,omitempty"`
	// Reliability fields carried by observation memories (contract §13).
	Outcome                string `json:"outcome,omitempty"`
	Attempts               int    `json:"attempts,omitempty"`
	RetryRecommended       bool   `json:"retry_recommended,omitempty"`
	ReconciliationRequired bool   `json:"reconciliation_required,omitempty"`

	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	Version      int        `json:"version"`
	Status       Status     `json:"status"`
	Conflict     bool       `json:"conflict,omitempty"`
	ConflictWith []string   `json:"conflict_with,omitempty"`
	// CorrelationID links the record to the execution that produced it.
	CorrelationID string `json:"correlation_id,omitempty"`
}

// Candidate is a proposed write. Every field is untrusted input: the platform
// clamps scope, assigns provenance and trust, applies bounds and redacts
// content before anything is persisted (contract §7).
type Candidate struct {
	Key   string
	Value string
	Type  Type
	Scope Scope
	// DivisionID is required for division scope and ignored for business scope.
	DivisionID string
	// Subject defaults to Key.
	Subject string
	// ExpiresAt is optional; a past timestamp is rejected.
	ExpiresAt              *time.Time
	Metadata               map[string]string
	Outcome                string
	Attempts               int
	RetryRecommended       bool
	ReconciliationRequired bool
	// ObservationID documents the observation this candidate was promoted
	// from (WriterObservation only). It is provenance metadata, never content.
	ObservationID string
}

// Identity is the runtime-established caller of a memory operation. Every field
// comes from the authenticated request or the executing agent — never from a
// model payload.
type Identity struct {
	ActorID    string
	BusinessID string
	DivisionID string
	AgentID    string
	// AgentMemoryMode is the acting agent's declared memory mode
	// (agentexec.MemoryConfig): none | business | division. It can only narrow
	// what the agent may write, never widen it.
	AgentMemoryMode string
}

// ScopeChecker is the injected canonical scope authority. The launcher binds it
// to identity.MembershipSet.AllowsScope; memory itself defines no permission
// model (contract §4).
type ScopeChecker interface {
	AllowsScope(identityID, businessID, divisionID string) bool
}

// Bounds are the explicit limits (contract §5).
type Bounds struct {
	MaxValueBytes      int
	MaxMetadataFields  int
	MaxMetadataBytes   int
	MaxRecordsPerScope int
	MaxQueryResults    int
}

// DefaultBounds are the shipped v1 limits.
func DefaultBounds() Bounds {
	return Bounds{
		MaxValueBytes:      8 * 1024,
		MaxMetadataFields:  16,
		MaxMetadataBytes:   256,
		MaxRecordsPerScope: 500,
		MaxQueryResults:    50,
	}
}

// Redactor removes secrets from content before it is persisted or returned. It
// is the existing platform redaction layer, injected by the launcher.
type Redactor interface {
	Redact(string) string
}

// RedactorFunc adapts a function to Redactor.
type RedactorFunc func(string) string

func (f RedactorFunc) Redact(s string) string {
	if f == nil {
		return s
	}
	return f(s)
}

// Options configures a Platform.
type Options struct {
	Now    func() time.Time
	Bounds Bounds
	Scopes ScopeChecker
	Redact Redactor
	Bus    EventSink
}

// EventSink is the slice of the event bus this package needs. The launcher binds
// it to the existing bus; memory creates no bus of its own (contract §16).
type EventSink interface {
	Publish(event Event) error
}

// Event is one metadata-only memory event. It carries ids and scope metadata,
// never memory content (contract §16).
type Event struct {
	Type          string
	BusinessID    string
	DivisionID    string
	AgentID       string
	CorrelationID string
	Fields        map[string]string
}

// memoryID is the canonical record id: stable, scope-qualified and independent
// of the value. It extends the historical mem:<business>:<agent>:<key> shape.
func memoryID(businessID, divisionID, agentID, key string) string {
	if agentID == "" {
		agentID = "_"
	}
	if divisionID != "" {
		divisionID = divisionID + "@"
	}
	return "mem:" + businessID + ":" + divisionID + agentID + ":" + key
}

// MemoryID exposes the canonical id derivation for tests and tooling.
func MemoryID(businessID, divisionID, agentID, key string) string {
	return memoryID(businessID, divisionID, agentID, key)
}

// normalizeKey bounds and canonicalizes a key: it becomes a lowercase token.
func normalizeKey(k string) string {
	k = strings.ToLower(strings.TrimSpace(k))
	if len(k) > 128 {
		k = k[:128]
	}
	return k
}

// normalizeSubject bounds a conflict namespace, defaulting to the key.
func normalizeSubject(subject, key string) string {
	s := strings.ToLower(strings.TrimSpace(subject))
	if s == "" {
		s = key
	}
	if len(s) > 128 {
		s = s[:128]
	}
	return s
}

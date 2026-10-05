// Escalation queue (C: CTR-GOV-002 full handoff).
//
// D3 (`4266ba3`) surfaces ESCALATE as an ESCALATION_REQUIRED envelope and
// publishes governance.escalated — but nothing consumed that event, so the
// handoff went nowhere. This file completes the contract:
//
//	CTR-GOV-002: Governance → Attention
//	  Pattern: async_command
//	  Input:  { escalation_id, reason, context, urgency, deadline }
//	  Output: { escalation_id } (ack)
//	  Failure: Logged, retry once
//
// The intake is a bus consumer on governance.escalated (async_command over
// the event bus; the ack is the bus acceptance of the event — see
// emitEscalation). It queues an Escalation record (the contract's
// escalation_id space) and links a submitted attention item.
//
// Statuses follow SCHEMA_GOVERNANCE_ATTENTION §4.2 escalation_state:
// pending → acknowledged → resolved, or expired once the deadline passes
// (§4.6: attention expiration = silence = no action). Expired is terminal —
// CTR-ATT-001's timeout re-escalation is a later milestone.
//
// Persistence is in-memory (same decision as P1 approvals, C-019).
package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/attention"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
)

// Escalation decision errors (CTR-ATT-002 human response surface). The
// gateway maps them to 404/403/409 and answers contract-shaped {accepted}.
var (
	ErrEscalationNotFound     = errors.New("escalation not found")
	ErrEscalationNotDecidable = errors.New("escalation not in a decidable state")
	ErrEscalationScope        = errors.New("escalation business scope mismatch")
)

// escalationAlertOptions is the CTR-ATT-001 options list presented with the
// human notification — the decisions a human may return via CTR-ATT-002.
var escalationAlertOptions = []string{"acknowledge", "resolve"}

// defaultEscalationTTL bounds how long an escalation waits for an answer
// when the originating request carries no deadline. The contract requires a
// deadline in the input and attention items must expire (schema §4.2
// expires_at), so one always exists.
const defaultEscalationTTL = 24 * time.Hour

// defaultEscalationUrgency maps an unset request priority (0) to the
// "high" level the schema assigns to governance escalations (§4.4);
// a set priority passes through on the shared 0-10 scale.
const defaultEscalationUrgency = 7

// EscalationStatus is the lifecycle state of a queued escalation
// (SCHEMA_GOVERNANCE_ATTENTION §4.2 escalation_state enum).
type EscalationStatus string

const (
	EscalationPending      EscalationStatus = "pending"
	EscalationAcknowledged EscalationStatus = "acknowledged"
	EscalationResolved     EscalationStatus = "resolved"
	EscalationExpired      EscalationStatus = "expired"
)

// Escalation is one queued Governance → Attention handoff (CTR-GOV-002),
// presented to humans through the CTR-ATT-001/002 surface.
type Escalation struct {
	EscalationID   string            `json:"escalation_id"`
	RequestID      string            `json:"request_id,omitempty"`
	BusinessID     string            `json:"business_id,omitempty"`
	Reason         string            `json:"reason"`
	Context        map[string]string `json:"context,omitempty"`
	Urgency        int               `json:"urgency"`
	Deadline       time.Time         `json:"deadline"`
	Status         EscalationStatus  `json:"status"`
	AttentionID    string            `json:"attention_id,omitempty"`
	Gate           string            `json:"gate,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	AcknowledgedAt *time.Time        `json:"acknowledged_at,omitempty"`
	AcknowledgedBy string            `json:"acknowledged_by,omitempty"`
	// AckReason is the CTR-ATT-002 reasoning recorded with an acknowledge.
	AckReason  string     `json:"ack_reason,omitempty"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy string     `json:"resolved_by,omitempty"`
	// Resolution carries the CTR-ATT-002 reasoning for the human response.
	Resolution string `json:"resolution,omitempty"`
}

// escalationQueue is the in-memory queue keyed by escalation_id.
type escalationQueue struct {
	mu    sync.RWMutex
	now   func() time.Time
	items map[string]*Escalation
	order []string // insertion order for listing
}

func newEscalationQueue(now func() time.Time) *escalationQueue {
	return &escalationQueue{
		now:   now,
		items: make(map[string]*Escalation),
	}
}

// put queues the escalation idempotently; false means it was already there
// (a redelivered event must not duplicate the attention item either).
func (q *escalationQueue) put(esc *Escalation) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, exists := q.items[esc.EscalationID]; exists {
		return false
	}
	q.items[esc.EscalationID] = esc
	q.order = append(q.order, esc.EscalationID)
	return true
}

// linkAttention records the attention item id on a queued escalation under
// the queue lock. The record is already published by put() when the attention
// submit returns, so the write must serialize with list/get readers (R-1).
// False means the record disappeared (never happens today: entries are never
// removed) — callers then leave the link empty rather than race.
func (q *escalationQueue) linkAttention(id, attentionID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	esc, ok := q.items[id]
	if !ok {
		return false
	}
	esc.AttentionID = attentionID
	return true
}

// sweepLocked expires escalations past their deadline. Callers hold q.mu.
func (q *escalationQueue) sweepLocked() {
	now := q.now()
	for _, id := range q.order {
		esc := q.items[id]
		if esc.Status != EscalationPending && esc.Status != EscalationAcknowledged {
			continue
		}
		if now.After(esc.Deadline) {
			esc.Status = EscalationExpired
		}
	}
}

// get returns a copy of the escalation, deadline-swept.
func (q *escalationQueue) get(id string) (*Escalation, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.sweepLocked()
	esc, ok := q.items[id]
	if !ok {
		return nil, false
	}
	return cloneEscalation(esc), true
}

// list returns copies of all escalations in insertion order, swept.
func (q *escalationQueue) list() []*Escalation {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.sweepLocked()
	out := make([]*Escalation, 0, len(q.order))
	for _, id := range q.order {
		out = append(out, cloneEscalation(q.items[id]))
	}
	return out
}

// acknowledge moves pending → acknowledged, attributing the responding
// authority and reasoning (CTR-ATT-002 decision + reasoning).
func (q *escalationQueue) acknowledge(id, actorID, reasoning string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.sweepLocked()
	esc, ok := q.items[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrEscalationNotFound, id)
	}
	switch esc.Status {
	case EscalationPending:
		now := q.now()
		esc.Status = EscalationAcknowledged
		esc.AcknowledgedAt = &now
		esc.AcknowledgedBy = actorID
		esc.AckReason = reasoning
		return nil
	case EscalationExpired:
		return fmt.Errorf("%w: escalation %q already expired", ErrEscalationNotDecidable, id)
	default:
		return fmt.Errorf("%w: escalation %q is %s, not pending", ErrEscalationNotDecidable, id, esc.Status)
	}
}

// resolve moves pending or acknowledged → resolved, recording the human
// response (CTR-ATT-002 decision=resolve with reasoning).
func (q *escalationQueue) resolve(id, actorID, reasoning string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.sweepLocked()
	esc, ok := q.items[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrEscalationNotFound, id)
	}
	switch esc.Status {
	case EscalationPending, EscalationAcknowledged:
		now := q.now()
		esc.Status = EscalationResolved
		esc.ResolvedAt = &now
		esc.ResolvedBy = actorID
		esc.Resolution = reasoning
		return nil
	case EscalationExpired:
		return fmt.Errorf("%w: escalation %q already expired", ErrEscalationNotDecidable, id)
	default:
		return fmt.Errorf("%w: escalation %q is %s, not resolvable", ErrEscalationNotDecidable, id, esc.Status)
	}
}

func cloneEscalation(esc *Escalation) *Escalation {
	cp := *esc
	if esc.Context != nil {
		cp.Context = make(map[string]string, len(esc.Context))
		for k, v := range esc.Context {
			cp.Context[k] = v
		}
	}
	if esc.AcknowledgedAt != nil {
		t := *esc.AcknowledgedAt
		cp.AcknowledgedAt = &t
	}
	if esc.ResolvedAt != nil {
		t := *esc.ResolvedAt
		cp.ResolvedAt = &t
	}
	return &cp
}

// Escalations returns all queued escalations (deadline-swept), oldest first.
func (e *Engine) Escalations() []*Escalation {
	return e.escalations.list()
}

// ListEscalations returns the escalations of one business scope (CTR-ATT-001
// pull surface — the list endpoint serves alert_id/summary/context/deadline).
func (e *Engine) ListEscalations(businessID string) []*Escalation {
	all := e.escalations.list()
	out := make([]*Escalation, 0, len(all))
	for _, esc := range all {
		if esc.BusinessID == businessID {
			out = append(out, esc)
		}
	}
	return out
}

// GetEscalation returns one escalation by id (deadline-swept).
func (e *Engine) GetEscalation(id string) (*Escalation, bool) {
	return e.escalations.get(id)
}

// AcknowledgeEscalation is the CTR-ATT-002 human response (decision =
// acknowledge) scoped to the caller's business; reasoning is recorded with
// the acknowledgement.
func (e *Engine) AcknowledgeEscalation(id, businessID, actorID, reasoning string) error {
	if err := e.escalationScope(id, businessID); err != nil {
		return err
	}
	return e.escalations.acknowledge(id, actorID, reasoning)
}

// ResolveEscalation is the CTR-ATT-002 human response (decision = resolve);
// reasoning is stored as the resolution.
func (e *Engine) ResolveEscalation(id, businessID, actorID, reasoning string) error {
	if err := e.escalationScope(id, businessID); err != nil {
		return err
	}
	return e.escalations.resolve(id, actorID, reasoning)
}

// escalationScope fails closed unless the escalation exists inside the
// caller's business scope (G-002 posture: no cross-tenant decisions).
func (e *Engine) escalationScope(id, businessID string) error {
	esc, ok := e.escalations.get(id)
	if !ok {
		return fmt.Errorf("%w: %q", ErrEscalationNotFound, id)
	}
	if esc.BusinessID != businessID {
		return fmt.Errorf("%w: escalation %q", ErrEscalationScope, id)
	}
	return nil
}

// escalationPayload is the governance.escalated Data shape: the CTR-GOV-002
// input (extended by D3 with urgency/deadline) plus the CTR-ATT-001
// notification fields (summary=reason, context, options, deadline).
type escalationPayload struct {
	EscalationRef string   `json:"escalation_ref"`
	RequestID     string   `json:"request_id"`
	RequesterID   string   `json:"requester_id"`
	Reason        string   `json:"reason"`
	Gate          string   `json:"gate"`
	Urgency       string   `json:"urgency"`
	Deadline      string   `json:"deadline"`
	Options       []string `json:"options,omitempty"`
}

// handleGovernanceEscalated is the CTR-GOV-002 attention-side intake. The
// contract's failure rule is "Logged, retry once": the escalation record is
// queued first (it is the contract's escalation_id ack), the attention item
// gets exactly one retry, and anything unparseable is logged and dropped —
// a malformed event will not fix itself on redelivery.
func (e *Engine) handleGovernanceEscalated(ev *event.Event) error {
	if ev == nil {
		return nil
	}
	var p escalationPayload
	if err := json.Unmarshal(ev.Data, &p); err != nil || p.EscalationRef == "" {
		log.Printf("core: dropping malformed governance.escalated event %q: %v", ev.ID, err)
		return nil
	}

	esc := &Escalation{
		EscalationID: p.EscalationRef,
		RequestID:    p.RequestID,
		BusinessID:   ev.BusinessID,
		Reason:       p.Reason,
		Gate:         p.Gate,
		Status:       EscalationPending,
		CreatedAt:    e.now(),
		Context: map[string]string{
			"requester_id":   p.RequesterID,
			"gate":           p.Gate,
			"business_id":    ev.BusinessID,
			"correlation_id": ev.CorrelationID,
		},
	}
	esc.Urgency = defaultEscalationUrgency
	if p.Urgency != "" {
		if u, err := strconv.Atoi(p.Urgency); err == nil && u > 0 {
			esc.Urgency = u
		}
	}
	esc.Deadline = e.now().Add(defaultEscalationTTL)
	if p.Deadline != "" {
		if d, err := time.Parse(time.RFC3339, p.Deadline); err == nil {
			esc.Deadline = d
		}
	}

	// Capture locally: the record is published below, and readers may hold
	// clones of it while this handler finishes.
	deadline := esc.Deadline

	// Idempotent queue: a redelivered event neither duplicates the record
	// nor the attention item below.
	if !e.escalations.put(esc) {
		return nil
	}

	// Attention item (CTR-GOV-002 → attention intake). Title is the reason —
	// an empty reason makes SubmitItem fail, exercising the retry-once path.
	submit := func() (*attention.AttentionItem, error) {
		return e.attentionEng.SubmitItem(
			esc.Reason, // title (required, non-empty)
			esc.Reason, // description
			ev.BusinessID,
			"governance",
			esc.Urgency,
			esc.Urgency,
			0,                  // risk: no risk datum travels with the handoff
			1.0,                // confidence: deterministic governance decision
			false, true, false, // not security / is policy violation / not owner message
		)
	}
	item, err := submit()
	if err != nil {
		item, err = submit() // contract: retry once
		if err != nil {
			log.Printf("core: escalation %s queued but attention submit failed after retry: %v", esc.EscalationID, err)
			return nil
		}
	}
	// R-1: the record is already visible (put above) and the item is live
	// in the attention engine (SubmitItem stores the returned pointer), so
	// both links go through lock-holding setters instead of direct writes.
	e.escalations.linkAttention(esc.EscalationID, item.ID)
	if deadline.After(e.now()) {
		if err := e.attentionEng.SetExpiresAt(item.ID, deadline); err != nil {
			log.Printf("core: escalation %s attention expiry stamp failed: %v", esc.EscalationID, err)
		}
	}
	return nil
}

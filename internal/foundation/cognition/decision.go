package cognition

import (
	"fmt"
	"sync"
	"time"
)

// DecisionStatus tracks the lifecycle of a decision.
type DecisionStatus string

const (
	DecisionStatusFraming     DecisionStatus = "framing"
	DecisionStatusEvaluating  DecisionStatus = "evaluating"
	DecisionStatusRecommended DecisionStatus = "recommended"
	DecisionStatusDecided     DecisionStatus = "decided"
	DecisionStatusAuthorized  DecisionStatus = "authorized"
	DecisionStatusExecuting   DecisionStatus = "executing"
	DecisionStatusEvaluated   DecisionStatus = "evaluated"
	DecisionStatusAbstained   DecisionStatus = "abstained"
	DecisionStatusEscalated   DecisionStatus = "escalated"
)

// Option represents a possible course of action.
type Option struct {
	// ID is the unique option identifier.
	ID string `json:"id"`
	// Description describes what this option entails.
	Description string `json:"description"`
	// ExpectedOutcome is the predicted result.
	ExpectedOutcome string `json:"expected_outcome"`
	// Risks are identified risks of this option.
	Risks []string `json:"risks,omitempty"`
	// Reversibility indicates how easily this can be undone.
	Reversibility string `json:"reversibility"` // "fully", "partially", "irreversible"
	// BlastRadius describes the scope of impact.
	BlastRadius string `json:"blast_radius"`
	// ResourceEstimate describes estimated resource requirements.
	ResourceEstimate string `json:"resource_estimate,omitempty"`
}

// Decision represents a structured decision between options.
// Decisions are framed from objectives and lead to plans.
type Decision struct {
	// ID is the unique decision identifier.
	ID string `json:"id"`
	// Status tracks the decision lifecycle.
	Status DecisionStatus `json:"status"`
	// Question is the decision question being addressed.
	Question string `json:"question"`
	// ObjectiveIDs links to relevant objectives.
	ObjectiveIDs []string `json:"objective_ids,omitempty"`
	// Constraints are boundaries for the decision.
	Constraints []string `json:"constraints,omitempty"`
	// Evidence collected for evaluation.
	Evidence []Evidence `json:"evidence,omitempty"`
	// Options are the alternatives being considered.
	Options []Option `json:"options,omitempty"`
	// RecommendedOption is the recommended option ID.
	RecommendedOption string `json:"recommended_option,omitempty"`
	// DecidedOption is the chosen option ID.
	DecidedOption string `json:"decided_option,omitempty"`
	// Reasoning explains the rationale.
	Reasoning string `json:"reasoning,omitempty"`
	// Uncertainty captures what is unknown.
	Uncertainty string `json:"uncertainty,omitempty"`
	// DecisionMaker who made or will make the decision.
	DecisionMaker string `json:"decision_maker"`
	// BusinessID scopes the decision.
	BusinessID string `json:"business_id,omitempty"`
	// Version is the optimistic concurrency version.
	Version int `json:"version"`
	// CreatedAt is when the decision was created.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is when the decision was last updated.
	UpdatedAt time.Time `json:"updated_at"`
}

// Evidence represents a piece of evidence for a decision.
type Evidence struct {
	// Source identifies where this evidence came from.
	Source string `json:"source"`
	// Content describes the evidence.
	Content string `json:"content"`
	// Reliability indicates how trustworthy this evidence is.
	Reliability string `json:"reliability"` // "high", "medium", "low"
	// Timestamp is when the evidence was collected.
	Timestamp time.Time `json:"timestamp"`
}

// DecisionEngine manages structured decisions.
// It prevents NEXUS from confusing options, evaluations, recommendations,
// decisions, authorizations, and executions.
type DecisionEngine struct {
	mu        sync.RWMutex
	decisions map[string]*Decision
	now       func() time.Time
}

// NewDecisionEngine creates a new decision engine.
func NewDecisionEngine() *DecisionEngine {
	return &DecisionEngine{
		decisions: make(map[string]*Decision),
		now:       time.Now,
	}
}

// NewDecisionEngineWithClock creates a new decision engine with an injectable clock.
func NewDecisionEngineWithClock(now func() time.Time) *DecisionEngine {
	return &DecisionEngine{
		decisions: make(map[string]*Decision),
		now:       now,
	}
}

// FrameDecision creates a new decision in framing status.
func (de *DecisionEngine) FrameDecision(
	question string,
	objectiveIDs []string,
	decisionMaker string,
	businessID string,
) (*Decision, error) {
	if question == "" {
		return nil, fmt.Errorf("decision question is required")
	}

	de.mu.Lock()
	defer de.mu.Unlock()

	now := de.now()
	d := &Decision{
		ID:            fmt.Sprintf("dec-%d", now.UnixNano()),
		Status:        DecisionStatusFraming,
		Question:      question,
		ObjectiveIDs:  objectiveIDs,
		DecisionMaker: decisionMaker,
		BusinessID:    businessID,
		Version:       1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	de.decisions[d.ID] = d
	return d, nil
}

// AddEvidence adds evidence to a decision.
func (de *DecisionEngine) AddEvidence(decisionID string, evidence Evidence) error {
	de.mu.Lock()
	defer de.mu.Unlock()

	d, ok := de.decisions[decisionID]
	if !ok {
		return fmt.Errorf("decision %s not found", decisionID)
	}

	evidence.Timestamp = de.now()
	d.Evidence = append(d.Evidence, evidence)
	d.UpdatedAt = de.now()
	return nil
}

// AddOption adds an option to a decision.
func (de *DecisionEngine) AddOption(decisionID string, option Option) error {
	de.mu.Lock()
	defer de.mu.Unlock()

	d, ok := de.decisions[decisionID]
	if !ok {
		return fmt.Errorf("decision %s not found", decisionID)
	}

	d.Options = append(d.Options, option)
	d.UpdatedAt = de.now()
	return nil
}

// Evaluate transitions a decision to evaluating status.
func (de *DecisionEngine) Evaluate(decisionID string) error {
	de.mu.Lock()
	defer de.mu.Unlock()

	d, ok := de.decisions[decisionID]
	if !ok {
		return fmt.Errorf("decision %s not found", decisionID)
	}

	d.Status = DecisionStatusEvaluating
	d.UpdatedAt = de.now()
	return nil
}

// Recommend sets the recommended option for a decision.
func (de *DecisionEngine) Recommend(decisionID, optionID, reasoning string) error {
	de.mu.Lock()
	defer de.mu.Unlock()

	d, ok := de.decisions[decisionID]
	if !ok {
		return fmt.Errorf("decision %s not found", decisionID)
	}

	d.RecommendedOption = optionID
	d.Reasoning = reasoning
	d.Status = DecisionStatusRecommended
	d.UpdatedAt = de.now()
	return nil
}

// Decide records the final decision.
func (de *DecisionEngine) Decide(decisionID, optionID string) error {
	de.mu.Lock()
	defer de.mu.Unlock()

	d, ok := de.decisions[decisionID]
	if !ok {
		return fmt.Errorf("decision %s not found", decisionID)
	}

	d.DecidedOption = optionID
	d.Status = DecisionStatusDecided
	d.UpdatedAt = de.now()
	return nil
}

// Abstain records that the decision engine abstains due to insufficient evidence.
func (de *DecisionEngine) Abstain(decisionID, reason string) error {
	de.mu.Lock()
	defer de.mu.Unlock()

	d, ok := de.decisions[decisionID]
	if !ok {
		return fmt.Errorf("decision %s not found", decisionID)
	}

	d.Uncertainty = reason
	d.Status = DecisionStatusAbstained
	d.UpdatedAt = de.now()
	return nil
}

// Escalate marks a decision as needing higher authority.
func (de *DecisionEngine) Escalate(decisionID, reason string) error {
	de.mu.Lock()
	defer de.mu.Unlock()

	d, ok := de.decisions[decisionID]
	if !ok {
		return fmt.Errorf("decision %s not found", decisionID)
	}

	d.Uncertainty = reason
	d.Status = DecisionStatusEscalated
	d.UpdatedAt = de.now()
	return nil
}

// Get returns a decision by ID.
func (de *DecisionEngine) Get(decisionID string) (*Decision, bool) {
	de.mu.RLock()
	defer de.mu.RUnlock()
	d, ok := de.decisions[decisionID]
	return d, ok
}

// DecisionCount returns the total number of decisions.
func (de *DecisionEngine) DecisionCount() int {
	de.mu.RLock()
	defer de.mu.RUnlock()
	return len(de.decisions)
}

# NEXUS DECISION ENGINE

**Status:** LOCKED  
**Role:** Structured Decision Intelligence  
**Layer:** Cognitive Control

---

## 1. Purpose

The Decision Engine provides structured decision intelligence between intent and execution.

Its purpose is to prevent NEXUS from confusing:

```text
OPTION
≠
EVALUATION
≠
RECOMMENDATION
≠
DECISION
≠
AUTHORIZATION
≠
EXECUTION
```

---

## 2. Canonical Flow

```text
QUESTION
 ↓
CONTEXT
 ↓
OBJECTIVE
 ↓
EVIDENCE
 ↓
OPTIONS
 ↓
EVALUATION
 ↓
RECOMMENDATION
 ↓
DECISION
 ↓
AUTHORIZATION
 ↓
EXECUTION
 ↓
OUTCOME
 ↓
EVALUATION
```

---

## 3. Responsibilities

Decision Engine MUST:

- formulate decision questions;
- identify relevant objectives;
- identify constraints;
- collect and evaluate evidence;
- generate or compare options;
- model expected outcomes;
- identify risks;
- represent uncertainty;
- evaluate reversibility and blast radius;
- identify tradeoffs;
- identify missing information;
- provide recommendations;
- abstain when evidence is insufficient;
- escalate decisions requiring authority;
- evaluate outcomes after execution.

It MUST NOT:

- redefine objectives;
- override Governance;
- authorize itself;
- execute arbitrary actions;
- fabricate evidence;
- hide uncertainty.

---

## 4. Decision Record

Minimum structure:

```yaml
decision_id:
scope:
question:
objective_refs:
mission_refs:
constraints:
deadline:
resources:
evidence_refs:
memory_refs:
current_state:
authority:
risk_tolerance:
decision_class:
status:
```

---

## 5. Decision Classes

```text
STRATEGIC
TACTICAL
OPERATIONAL
ROUTINE
EXPERIMENTAL
EMERGENCY
```

Decision class influences evidence, approval, risk, and escalation requirements.

---

## 6. Evidence

Evidence MUST preserve provenance.

Useful evidence properties:

```text
source
timestamp
freshness
trust
relevance
confidence
verification_state
```

Evidence may be:

```text
FACT
OBSERVATION
CLAIM
INFERENCE
OPINION
PREDICTION
```

These categories MUST NOT be silently treated as equivalent.

---

## 7. Constraints

Constraints are separated into:

### Hard Constraints
Must not be violated.

Examples:

- governance policy;
- legal/compliance requirement;
- explicit owner prohibition;
- safety boundary;
- authority boundary.

### Soft Constraints
Preferences that may be traded off when authorized.

Examples:

- speed;
- cost;
- convenience;
- quality target.

---

## 8. Option Evaluation

Options SHOULD be evaluated across relevant dimensions:

- objective alignment;
- expected outcome;
- evidence quality;
- cost;
- time;
- risk;
- reversibility;
- blast radius;
- dependencies;
- opportunity cost;
- resource requirements;
- uncertainty.

The system MUST make material tradeoffs visible.

---

## 9. Uncertainty

Decision Engine MUST represent uncertainty explicitly.

Examples:

```text
HIGH CONFIDENCE
MEDIUM CONFIDENCE
LOW CONFIDENCE
UNKNOWN
```

Unknown information MUST NOT be converted into false certainty.

---

## 10. Value of Information

When missing information could materially change a decision, the engine SHOULD consider whether acquiring that information is worth its cost and delay.

```text
DECIDE NOW
vs
GATHER INFORMATION
vs
ESCALATE
```

---

## 11. Abstention

Decision Engine MUST support:

```text
INSUFFICIENT_BASIS
```

Abstention is appropriate when:

- evidence is inadequate;
- uncertainty is too high;
- authority is missing;
- objective is ambiguous;
- constraints conflict;
- external state is unknown;
- consequences are too significant for autonomous action.

---

## 12. Autonomous Decisions

An autonomous decision is allowed only when all relevant conditions are satisfied:

```text
AUTHORIZED
+
WITHIN POLICY
+
WITHIN SCOPE
+
OBJECTIVE KNOWN
+
EVIDENCE SUFFICIENT
+
RISK ACCEPTABLE
+
REVERSIBILITY / BLAST RADIUS ACCEPTABLE
```

Autonomy does not override hard constraints.

---

## 13. Emergency Decisions

Emergency conditions may change speed and escalation paths, but MUST NOT automatically disable hard governance or safety constraints.

Emergency handling:

```text
DETECT
 ↓
CLASSIFY
 ↓
CONTAIN
 ↓
DECIDE WITH AVAILABLE EVIDENCE
 ↓
EXECUTE WITHIN EMERGENCY AUTHORITY
 ↓
VERIFY
 ↓
AUDIT
 ↓
REVIEW
```

---

## 14. Recommendation vs Decision

The engine MUST explicitly distinguish:

### Recommendation
What the system believes should happen based on available evidence.

### Decision
The selected course of action by an authorized decision-maker or authorized autonomous mechanism.

### Authorization
Permission to execute.

### Execution
Actual action.

These states MUST be independently auditable.

---

## 15. Decision Provenance

A decision should be reconstructable as:

```text
DECISION
 ↓
RECOMMENDATION
 ↓
OPTIONS
 ↓
EVIDENCE
 ↓
OBJECTIVES
 ↓
CONSTRAINTS
 ↓
AUTHORITY
 ↓
EXECUTION
 ↓
OUTCOME
```

---

## 16. Objective Alignment

Every consequential decision SHOULD reference at least one objective or explicitly state why it is allowed without one.

The engine SHOULD detect:

- local optimization harming strategic objectives;
- decisions conflicting with higher objectives;
- scope expansion;
- proxy metric abuse;
- objective drift.

---

## 17. Risk and Reversibility

Risk evaluation SHOULD consider:

```text
LIKELIHOOD
IMPACT
BLAST_RADIUS
REVERSIBILITY
DETECTION_DELAY
RECOVERY_COST
DEPENDENCIES
```

Irreversible or high-blast-radius actions require stronger authorization.

---

## 18. Outcome Evaluation

After execution:

```text
EXPECTED OUTCOME
vs
ACTUAL OUTCOME
```

The difference feeds:

- objective evaluation;
- memory;
- knowledge;
- observability;
- future decision quality;
- replanning.

---

## 19. Governance

Decision Engine MUST defer to Governance for:

- authority;
- policy;
- approval;
- scope;
- risk limits;
- external actions;
- irreversible operations.

Decision quality never grants authority.

---

## 20. Integration

### Executive
Requests decisions and consumes recommendations.

### Objective Engine
Provides WHAT, WHY, success criteria, and objective relationships.

### Planner
Uses authorized decisions to construct executable plans.

### Workflow
Executes plans.

### Agent Runtime
Provides agent-level reasoning and execution within assigned authority.

### Memory / Knowledge
Provide evidence and context.

### Attention
Receives escalation signals.

### Governance
Determines whether action is allowed.

### Observability
Records decision traces and outcomes.

---

## 21. Invariants

1. Recommendation is not authorization.
2. Decision is not execution.
3. Evidence is provenance-aware.
4. Uncertainty is explicit.
5. Hard constraints cannot be traded away.
6. Decision Engine cannot redefine objectives.
7. Decision Engine cannot self-authorize.
8. Missing information may cause abstention.
9. High-impact decisions receive stronger controls.
10. Outcomes feed future evaluation.

---

## 22. Locked Principle

> **The Decision Engine determines what the available evidence supports, what tradeoffs exist, and when NEXUS should act, abstain, or escalate. It does not grant itself authority to execute.**

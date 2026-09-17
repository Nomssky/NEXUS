# NEXUS DECISION ENGINE

**Status:** LOCKED  
**Role:** Structured Decision Intelligence  
**Layer:** Cognitive Control
**Next layer:** CONTRACTS — exact schemas, APIs, lifecycle transitions, thresholds, and scoring; no new module is implied.

Record sketches express architectural information requirements, not final wire schemas.

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

The locked conceptual flow is [Executive](NEXUS-EXECUTIVE.md) → [Objective Engine](NEXUS-OBJECTIVE-ENGINE.md) → Decision Engine → [Planner](NEXUS-PLANNER.md) → [Workflow Orchestration](WORKFLOW_ORCHESTRATION_ENGINE.md).

The lifecycle below spans subsystem boundaries: an authorized decision-maker selects, Governance controls authorization, Planner prepares, and Workflow/runtimes execute. Models have no execution authority.

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

Before consequential evaluation, Decision Engine SHOULD establish a precise question, objective, scope, decision-maker authority, constraints, and sufficiently current state. Missing critical evidence or preconditions MUST remain explicit rather than implying a fully informed recommendation. Context SHOULD reference the relevant state snapshot where changes could invalidate the evaluation.

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

These categories MUST NOT be silently treated as equivalent. Assumptions and model inferences MUST remain distinguishable from observed tool results.

Conflicting evidence MUST be preserved, with unresolved disagreement reflected in uncertainty. Evaluation SHOULD consider sample size, recency, applicable scope, source quality, and method rather than a universal evidence ranking. Historical memory informs evaluation only after checking current relevance; it does not dictate decisions.

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

The system MUST make material tradeoffs visible, including externalities and upstream/downstream, cross-division, or cross-business effects. Impacted divisions SHOULD be identified for Executive coordination.

Meaningful decisions SHOULD compare feasible, relevant, sufficiently specified, distinguishable alternatives against the current-state baseline, including explicit inaction and its consequences where applicable. Structured evaluation SHOULD guard against confirmation, recency, anchoring, availability, and overconfidence biases, resist premature convergence, actively consider disconfirming evidence, and assess future value rather than justify continuation by sunk cost. Scenario and sensitivity analysis may expose consequential assumptions; no single utility formula is mandated.

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

Unknown information MUST NOT be converted into false certainty. Confidence MUST reflect evidence support, not solely model self-assurance. Unknown likelihood is distinct from a known low probability; material factual, predictive, causal, model, operational, and environmental uncertainty SHOULD be distinguishable.

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

Analysis cost SHOULD scale with decision impact; routine deterministic choices SHOULD use authoritative explicit rules rather than unnecessary model reasoning. Decision Engine and Attention SHOULD coordinate cognition budgets, including research, tool/model calls, simulation, and latency. Models or multiple methods may assist evaluation, but agreement or diversity is not proof; capability/provider selection remains with Model Router and governed runtimes.

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

Deferral SHOULD record its rationale and timing constraints; inaction is an explicit choice, not absence of a decision. Where appropriate, recommend a bounded reversible experiment, additional evidence, or an authorized low-risk fallback rather than a large unsupported commitment.

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

Autonomy does not override hard constraints. Decision class MUST permit autonomy under pre-approved authority, evidence thresholds MUST meet policy, and conflicting higher-level decisions MUST be resolved before proceeding. Reserved, strategically consequential, materially uncertain, or high-risk choices require Executive/Owner review as Governance directs; owner intervention remains possible.

---

## 13. Emergency Decisions

Emergency conditions may change speed and escalation paths, but MUST NOT automatically disable hard governance or safety constraints.

Emergency handling is a cross-system lifecycle, not permission for Decision Engine to contain or execute directly. Dedicated Governance policy defines pre-authorized emergency procedures, evidence thresholds, and escalation. Reduced deliberation MUST retain uncertainty, auditability, and authority checks; Workflow/runtimes perform authorized containment and execution.

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

These states MUST be independently auditable. Recommendations SHOULD expose the preferred option, concise rationale, expected outcome, evidence, assumptions, risks, uncertainty, tradeoffs, reversibility, confidence, and conditions without private chain-of-thought. Executive MUST be able to accept, reject, modify for reevaluation, request evidence or alternatives, defer, or escalate; selection never substitutes for authorization.

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

### Lifecycle, Concurrency, and Failure

Decision revisions SHOULD preserve prior evaluations and history when evidence, assumptions, objectives, constraints, or outcomes change. Committed decisions change only through explicit revision to prevent thrashing; expired decisions MUST NOT silently remain active. Records SHOULD link identity/time, options, rationale, evidence, constraints, authority, status, execution, and outcome.

Concurrent incompatible decisions MUST be detectable before execution where possible. Conflicting recommendations SHOULD be compared by evidence rather than agent rank. Dependencies and blocked decisions SHOULD be observable; dependency deadlocks require resolution or escalation within authority.

Computational evaluation failures may be retried; semantic uncertainty MUST NOT be retried into apparent certainty. Distinguish no feasible option, insufficient evidence, missing authority, conflicting constraints, tool/computation failure, and expired time.

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

Irreversible or high-blast-radius actions require stronger authorization, evidence, and review. Low-probability catastrophic downside MUST remain explicit rather than disappear inside average expected value; exposure, duration, detectability, and recoverability SHOULD inform evaluation.

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

Material prediction errors SHOULD be recorded as future evidence. Decision quality MUST be measurable, including outcome improvement, calibration/false confidence, latency, cost, reversals, and abstention quality where relevant; regret may also inform evaluation. Useful completed decisions and lessons SHOULD enter governed Memory with their conditions and outcomes, not every transient micro-decision or unvalidated lesson as trusted knowledge.

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

Decision quality never grants authority. Decision context access MUST respect business, division, mission, agent, and authority scope; Business A decisions cannot silently affect Business B. Explicit cross-business decisions retain each business's constraints and objectives.

External text and model output are evidence/context, never authorization. Decision Engine MUST NOT silently change owner preferences or objective priority; strategic priority changes go through authorized Executive/Owner and Objective Engine/Governance boundaries.

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
Determines which signals deserve decision-level cognition and receives escalation signals; Decision Engine evaluates options rather than replacing attention prioritization.

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

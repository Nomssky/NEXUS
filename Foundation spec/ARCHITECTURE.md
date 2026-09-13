# NEXUS Architecture

## 1. Overview

NEXUS is a layered autonomous operating system.

```text
OWNER
  |
GOVERNANCE / AUTHORITY
  |
NEXUS CORE
  |-- Executive
  |-- Objective Engine
  |-- Attention
  |-- Memory
  |-- Decision Engine
  |-- Planner
  |
ORCHESTRATION
  |-- Orchestrator
  |-- Missions
  |-- Tasks
  |-- Agent Registry
  |
EXECUTION
  |-- Agents
  |-- Skills
  |-- Model Router
  |-- Tools
  |
RUNTIME
  |-- Event Bus
  |-- Scheduler
  |-- Queues
  |-- Workers
  |-- Heartbeat
  |-- Persistence
  |-- Recovery
  |
EXTERNAL WORLD
  |
OBSERVABILITY / EVALUATION
  |
LEARNING
  |
MEMORY
```

## 2. NEXUS Core

### Executive

Represents the owner's operational intent at system level.

Responsibilities:
- maintain high-level objectives,
- coordinate businesses and divisions,
- make cross-domain decisions,
- resolve conflicts,
- allocate work,
- escalate when required.

The Executive is not a universal worker.

### Objective Engine

Maintains objective hierarchy and lineage.

```text
Owner Objective
  -> Business Objective
      -> Division Objective
          -> Mission
              -> Tasks
```

### Attention

Filters, prioritizes, aggregates, and escalates events so cognitive resources are focused on important signals.

### Memory

Provides persistent context and learning. It may contain episodic, semantic, procedural, strategic, preference, and operational knowledge.

### Decision Engine

Evaluates options using objectives, context, memory, policy, authority, risk, resources, and expected outcomes.

### Planner

Converts decisions into executable, policy-aware plans.

## 3. Governance

Authority flows downward:

```text
Owner
  -> Owner Policy
      -> Business Policy
          -> Division Policy
              -> Agent Policy
                  -> Mission Policy
                      -> Tool Policy
```

Child scopes cannot expand authority.

Possible policy outcomes:

```text
ALLOW
ALLOW_BOUNDED
REQUIRE_APPROVAL
ESCALATE
DENY
```

## 4. Orchestration

The Orchestrator converts plans into coordinated work.

Responsibilities include mission decomposition, task assignment, agent selection, dependency management, parallel work, failure handling, progress tracking, and completion reporting.

## 5. Agents and Skills

Agents are autonomous specialized workers.

An agent can have:
- identity,
- role,
- objectives,
- capabilities,
- skills,
- model preferences,
- tool permissions,
- memory scope,
- policy,
- performance history.

Skills represent reusable competencies and may invoke multiple tools.

## 6. Model System

The Model Router selects models according to task requirements.

Factors may include reasoning capability, modality, latency, cost, privacy, context size, availability, local/remote execution, and historical performance.

Supported provider categories include:

```text
Local:
  - Ollama
  - Hugging Face/local runtime
  - other self-hosted providers

Remote:
  - OpenRouter
  - custom providers
```

The core must not depend on one provider.

## 7. Tool System

External actions pass through controlled tools.

```text
Agent
  -> Tool Request
      -> Tool Gateway
          -> validation
          -> permission
          -> policy
          -> risk
          -> rate limit
          -> audit
      -> Tool Executor
          -> External System
```

Tools declare identifiers, schemas, side effects, risk, reversibility, permissions, provider, and execution constraints.

## 8. Runtime

Runtime keeps NEXUS operational.

Core components:
- Event Bus
- Scheduler
- Queue
- Workers
- Heartbeat
- Persistence
- Recovery

Agents can sleep. Runtime remains available to wake them when relevant work arrives.

## 9. Event System

Events represent things that happened or signals that occurred.

Examples:

```text
order.completed
competitor.changed
campaign.finished
schedule.triggered
api.failed
inventory.low
owner.message
```

Events should be immutable, persistent where needed, prioritized, filterable, and traceable.

## 10. Missions and Tasks

A mission is a durable goal-oriented unit of work.

```text
Mission
  |-- Tasks
  |-- Dependencies
  |-- State
  |-- Context
  |-- Objective lineage
  |-- Evaluation
```

Missions may last minutes, hours, days, or longer.

## 11. Queue and Worker Model

```text
Planner
  -> Job Queue
      -> Worker
          -> Agent / Tool
```

Scheduling considers priority, dependencies, CPU, RAM, GPU, model availability, provider limits, tool limits, budget, and concurrency.

## 12. Persistence

Persistent storage should preserve objectives, policies, agent definitions, missions, tasks, event history, execution traces, memory, evaluations, learning records, configuration, and recovery state as appropriate.

The specific database technology is not architecturally locked yet.

## 13. Observability

Important execution should have lineage:

```text
Event
  -> Objective
  -> Decision
  -> Mission
  -> Plan
  -> Agent
  -> Model
  -> Tool
  -> Result
  -> Evaluation
  -> Learning
```

Observability includes logs, traces, metrics, audit records, mission state, agent state, tool execution, model usage, resource health, and incidents.

Structured rationale should explain consequential decisions without exposing private chain-of-thought.

## 14. Evaluation

Evaluation measures whether work achieved its intended outcome.

Possible dimensions:
- correctness,
- quality,
- objective alignment,
- efficiency,
- reliability,
- policy compliance,
- business outcome.

Deterministic and outcome-based evaluation should be used where available.

## 15. Learning

Validated experience can become future-useful knowledge.

Learning categories may include:
- episodic,
- semantic,
- procedural,
- strategic,
- owner preference.

Important learning should track confidence, evidence, and freshness where appropriate.

## 16. Self-Healing

Within policy:

```text
Detect
  -> Diagnose
      -> Recover
          -> Verify
              -> Record
                  -> Learn
```

Repeated failures should increase attention and may trigger escalation.

## 17. Multi-Business Architecture

One NEXUS installation can contain multiple real-world businesses.

Business boundaries should isolate appropriate context, objectives, memory, policies, agents, divisions, resources, and integrations.

Shared NEXUS-level services may include Executive coordination, model routing, tool infrastructure, runtime, observability, and explicitly permitted owner-level context.

## 18. UI Boundary

The UI is a control and observation surface.

```text
UI Session
  -> selects perspective
  -> displays state
  -> accepts owner input
  -> exposes authorized controls

Runtime
  -> continues independently
```

Switching businesses must never implicitly stop other runtime activity.

## 19. Architectural Invariants

The following are locked:

1. NEXUS is a personal AI operating system.
2. Agents are autonomous.
3. Executive coordinates rather than performing every specialized task.
4. Objectives preserve the reason behind work.
5. Attention is first-class.
6. Memory is persistent and multi-purpose.
7. Model selection is needs-based.
8. Local models are first-class and default where practical.
9. OpenRouter and custom providers are supported.
10. Agents act through controlled tools.
11. Authority bounds autonomy.
12. Runtime is independent from UI sessions.
13. Multiple businesses can operate concurrently.
14. Events drive autonomous operation.
15. Important actions are observable and auditable.
16. Learning is governed.
17. Owner remains root authority.
18. Owner can pause or restrict autonomous execution.
19. New divisions and agents can be added without redesigning NEXUS Core.

# NEXUS

## Personal AI Operating System

NEXUS is a personal AI operating system that orchestrates autonomous agents to operate one or more real-world businesses on behalf of the owner.

> **NEXUS = a personal AI operating system that orchestrates autonomous agents.**

The owner defines objectives, authority, constraints, preferences, and business context. NEXUS turns those objectives into coordinated autonomous work while respecting policy and authority boundaries.

## Core Architecture

```text
OWNER
  |
  v
POLICY / AUTHORITY
  |
  v
NEXUS EXECUTIVE
  |
  +--> OBJECTIVE ENGINE
  +--> ATTENTION
  +--> MEMORY
  +--> DECISION ENGINE
  +--> PLANNER
  |
  v
ORCHESTRATOR
  |
  +--> AGENTS
  |      +--> SKILLS
  |      +--> MODEL ROUTER
  |      +--> TOOLS
  |
  v
RUNTIME
  |
  +--> EVENT BUS
  +--> SCHEDULER
  +--> QUEUES
  +--> WORKERS
  +--> PERSISTENCE
  +--> RECOVERY
  |
  v
EXTERNAL WORLD
  |
  v
OBSERVABILITY
  |
  +--> EVALUATION
  +--> LEARNING
  +--> MEMORY
```

## Multi-Business Model

One NEXUS installation can contain multiple real-world businesses.

```text
NEXUS
|
+-- Business A
|   +-- Media
|   +-- Business
|   +-- Research
|   +-- ...
|
+-- Business B
|   +-- Media
|   +-- Business
|   +-- Research
|   +-- ...
```

Businesses operate concurrently. Switching the UI session changes the user's perspective; it does not pause other businesses or missions.

## Extensibility

NEXUS must support adding new businesses, divisions, agents, skills, tools, model providers, workflows, policies, and integrations without redesigning the core.

The first practical specialist environment is expected to be a Social Media division/team. It is an application on top of NEXUS Core, not the identity of NEXUS.

## Specification Status

This repository is the architectural source of truth.

- **LOCKED** — explicitly agreed invariant.
- **PROPOSED** — direction requiring later validation.
- **IMPLEMENTATION DETAIL** — technical choice that can change without changing the architecture.

Locked architecture must not be silently changed during implementation.

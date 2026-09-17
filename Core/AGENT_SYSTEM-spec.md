# NEXUS Agent System / Agent Runtime

> **HISTORICAL / NONCANONICAL — superseded source specification.** Replacements: [Agent Runtime & Lifecycle](NEXUS-AGENT-RUNTIME-LIFECYCLE.md), [Identity, Access & Trust](NEXUS-IDENTITY-ACCESS-TRUST-SYSTEM.md), and [Tool Runtime & Capability Execution](NEXUS-TOOL-RUNTIME-CAPABILITY.md).
> The canonical owners govern; this source grants no competing authority. Historical lifecycle alternatives, examples, schemas, API/interface drafts, acceptance sketches, and open questions are nonbinding candidates for the next CONTRACTS layer, not approved contracts or a mandate for new modules. Historical approval/lock wording below is not current status. The source body is preserved for traceability.

**Status:** HISTORICAL / NONCANONICAL (original proposal retained below)

## 1. Definition

An **Agent** is an autonomous software worker operating inside NEXUS.

An agent is **not a model**. A model is one replaceable reasoning component used by an agent.

Conceptually:

```text
AGENT
├── Identity
├── Role
├── Capabilities
├── Authority
├── Model Policy
├── Tools
├── Memory Access
├── Context
├── Task
├── Runtime State
├── Communication
├── Verification
└── Learning Signals
```

## 2. Core Principle

NEXUS must be **model-agnostic**.

Agents may use:

```text
Ollama
Hugging Face
OpenRouter
other compatible providers
custom/local inference
deterministic software
specialized models
```

The same agent identity and role should survive model replacement.

## 3. Agent vs Model

```text
Agent = worker identity + capability + authority + state + runtime
Model = reasoning/generation engine
```

Example:

```text
Social Copywriter Agent
        ↓
Model Router
        ↓
local model / OpenRouter model / fallback model
```

The agent does not become a different agent simply because its model changes.

## 4. Agent Identity

Every persistent agent should have:

```text
agent_id
name
description
role
business_id
division_id
status
created_at
updated_at
version
```

Identity must be stable enough for audit and memory association.

## 5. Agent Scope

Agents belong to an explicit scope.

Possible scopes:

```text
SYSTEM
BUSINESS
DIVISION
MISSION
TEMPORARY_TASK
```

Default autonomous workers should be scoped to the smallest necessary authority.

## 6. Business Isolation

An agent belonging to Business A must not automatically access:

- Business B memory;
- Business B credentials;
- Business B tools;
- Business B data;
- Business B objectives.

Cross-business access requires explicit authorization.

## 7. Division Affiliation

A business can contain multiple divisions:

```text
Business
├── Media
├── Research
├── Operations
├── Sales
└── Finance
```

Agents may belong to one division or be explicitly designated cross-division.

## 8. Role

Role describes what an agent is responsible for.

Examples:

```text
Researcher
Copywriter
Designer
Content Strategist
Social Media Analyst
QA Agent
Data Analyst
```

Role does not itself grant permissions.

## 9. Capabilities

Capabilities describe what an agent can do.

Examples:

```text
web_research
copywriting
image_generation
data_analysis
social_drafting
social_publishing
analytics
code_execution
```

Capability is distinct from permission.

## 10. Permissions

Permissions define what an agent is allowed to do.

Examples:

```text
read_research
write_draft
publish_social
modify_campaign
access_analytics
send_external_message
```

An agent may possess a capability without being authorized to exercise it in a particular context.

## 11. Least Privilege

Agents should receive only the permissions required for their current responsibilities.

Do not grant broad permissions merely because an agent might need them later.

## 12. Authority

Authority answers:

> "Who/what allows this agent to perform this action?"

Possible authority sources:

```text
Owner
Executive
Governance
Division Policy
Approved Plan
Approved Task
System Policy
```

## 13. Task-Bound Authority

When possible, authority should be scoped to the current task.

Example:

```text
Task:
draft Instagram post

Permission:
write social draft

Not automatically:
publish Instagram post
```

## 14. Agent Configuration

An agent configuration should include:

```text
identity
role
capabilities
permissions
model_policy
tool_policy
memory_policy
autonomy_policy
communication_policy
verification_policy
resource_policy
```

## 15. Model Policy

Model selection should be declarative.

Example:

```text
preferred:
local_reasoning_model

fallback:
OpenRouter reasoning model

specialized:
vision_model for image tasks
```

The runtime resolves concrete models.

## 16. Model Routing Factors

Routing may consider:

- task type;
- required reasoning depth;
- latency;
- cost;
- privacy;
- availability;
- context length;
- modality;
- historical reliability;
- local/cloud policy.

## 17. Local-First Default

NEXUS may default to local inference where practical.

Benefits can include:

```text
privacy
cost control
offline capability
lower external dependency
```

Cloud routing remains available when justified.

## 18. Model Fallback

A model failure should not necessarily kill the agent.

Possible fallback:

```text
local model
   ↓ failure
alternate local model
   ↓ failure
approved cloud model
   ↓ failure
recovery/escalation
```

Fallback must respect privacy and authorization.

## 19. Tool System

Tools are external capabilities an agent can invoke.

Examples:

```text
web
filesystem
database
browser automation
social APIs
image generation
code execution
analytics
```

Tool availability does not equal permission.

## 20. Tool Invocation

Every tool call should be attributable to:

```text
agent_id
task_id
mission_id
business_id
tool
arguments/intent
timestamp
authorization context
result
```

## 21. Tool Sandboxing

High-risk tools should run inside appropriate boundaries.

Examples:

```text
filesystem sandbox
network allowlist
credential isolation
API scope restriction
execution sandbox
```

## 22. External Actions

External side effects require stricter controls than internal reasoning.

Examples:

```text
publish post
send message
change price
spend money
delete data
modify production system
```

These actions must pass authorization and policy checks.

## 23. Agent Context

Agent context should contain:

```text
current task
task inputs
relevant objective
decision/plan context
required memory
constraints
tool information
output contract
verification criteria
```

Do not inject the entire NEXUS state by default.

## 24. Context Budget

Context should be bounded.

The runtime should prioritize:

```text
current task
critical constraints
relevant memory
recent observations
required references
```

## 25. Context Provenance

Agent-visible information should retain provenance where practical:

```text
owner instruction
objective
decision
plan
memory
research
tool result
agent inference
assumption
```

## 26. Memory Access

Agents should not have unrestricted access to all memory.

Memory access should follow:

```text
business
division
agent role
task relevance
permission
```

## 27. Memory Write

Agents may propose memories, but durable memory should pass the Memory subsystem's rules.

The agent should not silently rewrite core facts or owner preferences.

## 28. Working Memory

Agents may maintain temporary working state:

```text
current hypotheses
intermediate outputs
scratch context
temporary observations
```

Working memory can expire after the task/mission.

## 29. Long-Term Memory

Durable memories may include:

```text
validated facts
successful procedures
failure patterns
business knowledge
preferences
learned constraints
```

Persistence is governed by Memory.

## 30. Agent Lifecycle

Possible states:

```text
DEFINED
INITIALIZING
READY
ASSIGNED
RUNNING
WAITING
BLOCKED
DEGRADED
PAUSED
STOPPING
STOPPED
FAILED
RETIRED
```

## 31. Lifecycle

```text
DEFINED
  ↓
INITIALIZING
  ↓
READY
  ↓
ASSIGNED
  ↓
RUNNING
  ↓
WAITING / BLOCKED / DEGRADED
  ↓
RUNNING
  ↓
STOPPING
  ↓
STOPPED
```

## 32. Persistent vs Ephemeral Agents

NEXUS should support:

### Persistent
Long-lived specialist with identity and accumulated operational history.

### Ephemeral
Created for a bounded task/mission and discarded or archived afterward.

## 33. Agent Creation

Agents may be created from:

```text
owner configuration
division template
system template
approved autonomous generation
```

Agent creation must not bypass governance.

## 34. Agent Factory

A future Agent Factory may generate:

```text
role
capability set
model policy
tool policy
memory policy
autonomy boundary
verification policy
```

based on an approved requirement.

## 35. Generated Agent Safety

Automatically generated agents must start with conservative permissions.

Expansion of authority requires explicit policy/authorization.

## 36. Agent Versioning

Persistent agents should support configuration versions.

```text
Agent v1
Agent v2
Agent v3
```

Changes must be auditable.

## 37. Agent Retirement

Retirement should preserve:

```text
identity
history
outputs
decisions
failure records
memory lineage
```

unless retention policy says otherwise.

## 38. Agent Autonomy

Autonomy is bounded.

An agent may act autonomously only within:

```text
assigned task
approved plan
objective context
permissions
governance
resource limits
risk limits
```

## 39. Autonomy Levels

Conceptual levels:

```text
L0 OBSERVE
L1 SUGGEST
L2 EXECUTE_PREAUTHORIZED
L3 ADAPT_WITHIN_SCOPE
L4 AUTONOMOUS_WORKFLOW
L5 HIGH_IMPACT_AUTONOMY
```

Higher levels require stronger governance.

## 40. Autonomous Loop

A mature agent may operate:

```text
OBSERVE
  ↓
INTERPRET
  ↓
PLAN MICRO-ACTION
  ↓
ACT
  ↓
VERIFY
  ↓
LEARN
  ↺
```

The agent's micro-planning cannot override NEXUS Planner decisions.

## 41. Agent Heartbeat

Long-running agents may expose heartbeat state:

```text
alive
idle
working
blocked
degraded
```

Heartbeat is not proof of useful progress.

## 42. Progress Reporting

Agents should report evidence-based progress:

```text
started
completed subtask
waiting for dependency
tool failure
verification failed
```

Avoid fabricated percentage progress.

## 43. Agent Communication

Agents communicate through structured messages/events.

Conceptual message:

```text
from_agent
to_agent
business_id
mission_id
task_id
message_type
payload
provenance
timestamp
```

## 44. Communication Types

Examples:

```text
REQUEST
RESPONSE
HANDOFF
STATUS
BLOCKED
ESCALATION
VERIFICATION
OBSERVATION
```

## 45. Agent Handoff

A handoff should contain:

```text
completed work
artifacts
assumptions
known limitations
verification state
next required action
```

## 46. Agent-to-Agent Authority

One agent cannot grant itself or another agent additional authority merely through a message.

Authority comes from the governing system.

## 47. Untrusted Agent Output

Agent output is untrusted until verified where verification is required.

This is especially important for:

```text
financial actions
external publishing
security-sensitive actions
high-impact decisions
```

## 48. Agent Negotiation

Agents may negotiate task allocation or propose alternatives.

Final authority remains with:

```text
Planner
Decision Engine
Executive
Governance
```

according to scope.

## 49. Shared State

Agents should avoid uncontrolled shared mutable state.

Prefer:

```text
artifacts
events
versioned records
task outputs
```

## 50. Concurrency

If two agents attempt conflicting changes:

```text
Agent A → state X
Agent B → state X
```

the runtime must detect the conflict.

Possible handling:

```text
lock
serialize
merge
reject
escalate
```

## 51. Agent Failure

Failures must be observable and classified.

Categories:

```text
MODEL
TOOL
NETWORK
INPUT
AUTHORIZATION
POLICY
LOGIC
VERIFICATION
RESOURCE
UNKNOWN
```

## 52. Recovery

Recovery may include:

```text
retry
restart
switch model
switch agent
restore checkpoint
reduce scope
replan
escalate
```

## 53. Checkpoints

Long-running tasks may create checkpoints containing:

```text
progress
artifacts
state
assumptions
next action
```

This enables recovery without restarting everything.

## 54. Idempotency

Agents should prefer idempotent actions when possible.

External side effects should use deduplication keys or equivalent safeguards where available.

## 55. Cancellation

Agents must support cooperative cancellation.

After cancellation:

```text
stop work
release resources
persist necessary state
report final state
```

## 56. Emergency Stop

NEXUS must be able to stop agents centrally.

Emergency stop should override normal autonomous execution.

## 57. Resource Limits

Agents may have:

```text
token budget
time budget
tool-call budget
API budget
compute budget
financial budget
parallelism limit
```

## 58. Infinite Loop Prevention

Agents must not continue indefinitely without:

```text
termination condition
max iterations
budget limit
stagnation detection
escalation path
```

## 59. Stagnation Detection

Detect patterns such as:

```text
same action repeated
same error repeated
no meaningful state change
no progress across multiple cycles
```

Then:

```text
stop
replan
switch strategy
escalate
```

## 60. Goal Drift

Agent should periodically check:

> "Does my current work still contribute to the assigned objective/task?"

If no:

```text
pause
report
request replanning
```

## 61. Prompt Injection

External content may attempt to instruct the agent.

The agent must treat external instructions as untrusted unless authorized.

External content cannot override:

```text
system policy
governance
objective
permissions
task scope
business isolation
```

## 62. Credential Security

Agents should not directly receive unrestricted secrets.

Prefer scoped credential handles.

Example:

```text
social_publish_token:
scope = Business A / Instagram / publish
```

## 63. Secret Exposure

Secrets must not be written into:

```text
logs
memory
agent messages
model prompts
artifacts
```

unless explicitly required and protected.

## 64. Agent Observation

Agents can observe only permitted data.

Observation itself should respect business/division boundaries.

## 65. Agent Self-Modification

Agents should not silently modify:

```text
own permissions
own governance
own identity
core objectives
security controls
```

Self-improvement may be proposed, but governed changes require authorization.

## 66. Agent Self-Replication

An agent should not spawn unrestricted copies.

Spawning must use an approved Agent Factory/runtime mechanism and resource limits.

## 67. Child Agents

An agent may request a child agent when allowed.

Example:

```text
Research Agent
   ↓ request
Temporary Research Worker
```

Parent does not automatically grant child its own permissions.

## 68. Delegation

Delegation should preserve:

```text
scope
authority
deadline
verification
resource budget
```

## 69. Delegation Chain

Audit should show:

```text
Planner
 → Agent A
   → Agent B
     → Tool
```

This prevents invisible autonomous chains.

## 70. Verification

Agent-produced outputs should enter verification when required by task policy.

Agent should not mark its own output verified merely by assertion.

## 71. Self-Critique

Agents may perform self-review, but self-review does not replace independent verification for high-risk work.

## 72. Agent Reputation

NEXUS may track operational reliability:

```text
success rate
verification pass rate
failure rate
latency
resource efficiency
rework rate
```

Reputation should influence routing, not become unquestionable authority.

## 73. Learning

Agents can produce learning signals:

```text
what worked
what failed
why
under which conditions
```

Durable learning belongs in governed Memory.

## 74. Model Learning vs Agent Learning

Changing model weights is not required for agent learning.

Agent learning may instead mean:

```text
better routing
better prompts
better workflow templates
better tool selection
better memory
better failure handling
```

## 75. Model Independence

An agent's persistent identity should not be tied to a single provider.

Migration:

```text
Ollama → OpenRouter
OpenRouter → local
Model A → Model B
```

should preserve agent identity and operational history.

## 76. Multimodal Agents

Agents may support:

```text
text
image
audio
video
structured data
```

Capabilities and model routing determine modality support.

## 77. Social Media Specialist Example

A Social Media division could contain:

```text
Social Strategist
Content Researcher
Copywriter
Creative Agent
Publisher
Community Agent
Analytics Agent
QA Agent
```

Each has different capabilities and permissions.

## 78. Example Social Workflow

```text
Objective
  ↓
Social Strategist
  ↓
Researcher
  ↓
Copywriter + Creative
  ↓
QA
  ↓
Publisher
  ↓
Analytics
  ↓
Learning
```

No single agent must perform every role.

## 79. Agent Specialization

Specialization should exist where it creates measurable benefit.

Do not create dozens of agents merely to appear "multi-agent."

## 80. Agent Collaboration

Collaboration should be driven by task structure.

Agents should not communicate continuously without purpose.

## 81. Agent Context Compression

For long workflows, runtime should summarize/archive old context while preserving critical facts and provenance.

## 82. Artifact Ownership

Every artifact should have:

```text
creator_agent
task_id
mission_id
business_id
version
verification_state
```

## 83. Artifact Versioning

Agents should avoid destructive overwrites where history matters.

Use:

```text
draft v1
draft v2
approved v1
published v1
```

## 84. External Side-Effect Confirmation

For important external actions, the runtime should verify the actual external state.

Example:

```text
Agent says:
"post published"

Runtime checks:
platform API/state confirms publication
```

## 85. Agent Truthfulness

An agent must distinguish:

```text
I intended to do X
I attempted X
X succeeded
X was verified
```

These are different states.

## 86. Observability

Runtime should expose:

```text
agent state
current task
model
tool activity
resource use
failures
verification
last heartbeat
```

Sensitive data must remain protected.

## 87. Agent Event Log

Useful events:

```text
agent.created
agent.started
agent.assigned
agent.model_selected
agent.tool_called
agent.output_created
agent.blocked
agent.failed
agent.recovered
agent.escalated
agent.stopped
agent.retired
```

## 88. Agent Trace

Important operations should support traceability:

```text
objective
→ decision
→ plan
→ task
→ agent
→ model
→ tool
→ artifact
→ verification
→ outcome
```

## 89. Deterministic Operations

Use deterministic logic for operations that do not require generative reasoning.

Examples:

```text
permission checks
budget checks
state transitions
schema validation
deadline calculations
```

## 90. Model-Assisted Operations

Use models where judgment/generation is required:

```text
research synthesis
copywriting
classification
strategy suggestions
creative generation
semantic analysis
```

## 91. Agent Runtime Boundary

Runtime is responsible for:

```text
loading agent
loading task
resolving model
enforcing tools
enforcing permissions
executing loop
capturing state
handling failures
reporting output
```

## 92. Runtime Must Not

Runtime must not:

- redefine objectives;
- bypass Governance;
- grant arbitrary permissions;
- silently alter plans;
- fabricate verification;
- erase failures.

## 93. Agent Contract

Conceptual runtime contract:

```text
Agent receives:
  task
  authorized context
  tools
  model
  policies

Agent returns:
  output
  status
  evidence
  tool trace
  assumptions
  verification request
  resource usage
```

## 94. Agent Stop Conditions

Stop when:

```text
task completed
verification passed
task cancelled
budget exhausted
deadline exceeded
authorization revoked
critical failure
objective invalidated
```

## 95. Agent Escalation

Escalate when:

- outside authority;
- insufficient information;
- conflicting instructions;
- repeated failure;
- high-impact uncertainty;
- unsafe action;
- policy conflict.

## 96. Escalation Payload

Include:

```text
task
problem
attempts
evidence
risk
available options
recommended next step
required authority
```

## 97. Agent Quality Metrics

Potential metrics:

```text
task success rate
verification pass rate
rework rate
failure rate
mean recovery time
latency
cost
tool efficiency
hallucination/error rate
outcome contribution
```

## 98. Agent Health

Possible health:

```text
HEALTHY
DEGRADED
BLOCKED
FAILING
STOPPED
```

Health should combine runtime signals, not simply heartbeat.

## 99. Agent Registry

NEXUS should maintain an Agent Registry containing:

```text
identity
scope
role
capabilities
permissions
model policy
tool policy
status
version
health
```

## 100. Capability Registry

Capabilities should be machine-readable so Planner can route tasks.

Example:

```text
capability:
  social_copywriting

requirements:
  text_generation
  brand_context
```

## 101. Tool Registry

Tools should expose:

```text
tool_id
capabilities
risk level
permissions
input schema
output schema
availability
cost
```

## 102. Model Registry

Models should expose:

```text
model_id
provider
modality
capabilities
context limits
cost
latency
availability
privacy class
```

## 103. Compatibility Resolution

Runtime should resolve:

```text
task requirements
→ capable agent
→ compatible model
→ authorized tools
```

## 104. Agent Scheduling

Planner/runtime may consider:

```text
agent availability
current load
capability
priority
deadline
health
cost
```

## 105. Agent Pool

A division may have a pool of equivalent workers.

Example:

```text
Research Worker 01
Research Worker 02
Research Worker 03
```

This supports parallelism and fault tolerance.

## 106. Persistent Specialist

Some agents should remain stable because they accumulate domain context.

Example:

```text
Brand Strategist
```

## 107. Temporary Specialist

Some agents should exist only for a mission.

Example:

```text
Competitor Research Worker
```

## 108. Agent Templates

Templates can define reusable specialists:

```text
researcher.template
copywriter.template
analyst.template
qa.template
```

Templates are instantiated into actual agents with business-specific configuration.

## 109. Business Customization

Two businesses can use the same role but different:

```text
brand voice
tools
permissions
objectives
memory
model policy
workflow
```

## 110. No Global Business Context Leakage

A model prompt must never accidentally contain another business's private context.

Context assembly must be business-aware.

## 111. Division Collaboration Boundary

Cross-division collaboration should be explicit.

Example:

```text
Research → Media
```

Research data can be handed over without granting Media unrestricted Research memory.

## 112. Agent Objective Awareness

An agent should know enough of the parent objective to understand:

> "Why am I doing this?"

But it should not receive authority to redefine that objective.

## 113. Agent Decision Boundary

Agents may make local decisions within their task.

Example:

```text
choose headline A vs B
```

They should escalate decisions outside their authorized boundary.

## 114. Local Planning

An agent may perform micro-planning for its assigned task.

Example:

```text
choose research order
choose search queries
choose draft structure
```

It cannot silently create a new strategic mission.

## 115. Agent Memory Boundary

Memory access must follow relevance and authority.

An agent should not query:

```text
"everything NEXUS knows"
```

as a default operation.

## 116. Runtime Security Boundary

The runtime is the final enforcement point for:

```text
permissions
tool access
resource limits
business isolation
autonomy level
```

## 117. Defense in Depth

Security should exist at multiple layers:

```text
Planner
Governance
Runtime
Tool
External service
```

No single agent prompt is considered a security boundary.

## 118. Agent Sandbox

Untrusted/generated agents should operate in stricter sandboxes until trust is established.

## 119. Agent Trust

Trust can be evidence-based:

```text
verified history
capability certification
reliability
scope
```

Trust never overrides hard permissions.

## 120. Agent Certification

Future NEXUS may certify agents for capabilities:

```text
social publishing certified
financial analysis certified
research certified
```

Certification is separate from identity.

## 121. Agent Testing

Before production use, an agent can be evaluated on:

```text
capability tests
tool tests
permission tests
prompt-injection tests
failure recovery
verification accuracy
```

## 122. Agent Simulation

Agents should be testable in a simulated environment before high-impact autonomy.

## 123. Agent Promotion

Possible lifecycle:

```text
sandbox
 ↓
test
 ↓
limited production
 ↓
trusted production
```

Authority should increase only through governed promotion.

## 124. Agent Rollback

Configuration/model changes should be reversible.

If performance degrades:

```text
rollback agent version
rollback model policy
rollback prompt/config
```

## 125. Agent Runtime Determinism

State transitions should be deterministic where practical.

LLM outputs should not directly control security-critical state transitions.

## 126. Policy Enforcement

Policy should be evaluated outside the model whenever practical.

Do not rely solely on:

```text
"please behave safely"
```

inside prompts.

## 127. Human Override

Owner/authorized operators must be able to:

```text
pause
stop
revoke
reassign
modify
approve
```

agents according to governance.

## 128. Owner Replacement Model

NEXUS is intended to operate as the owner's autonomous organizational layer.

Agents therefore operate as workers under NEXUS authority—not as independent owners.

## 129. 24/7 Operation

The runtime should support continuous operation:

```text
event
 ↓
Planner
 ↓
task
 ↓
agent
 ↓
tool
 ↓
verification
 ↓
next task
 ↺
```

No human interaction is required for pre-authorized work.

## 130. Safe Autonomy

24/7 autonomy must include:

```text
budgets
termination
health checks
failure handling
permission enforcement
business isolation
emergency stop
```

## 131. Agent Invariants

The following are intended to become locked invariants:

1. Agent is not synonymous with model.
2. Model is replaceable.
3. Agent identity is persistent when the agent is persistent.
4. Capabilities do not automatically grant permissions.
5. Permissions do not automatically grant strategic authority.
6. Business boundaries are isolated by default.
7. External side effects require stronger controls.
8. Agent output is not automatically verified.
9. Agents cannot silently redefine objectives.
10. Agents cannot silently expand task scope.
11. Agents cannot grant themselves permissions.
12. Agents cannot bypass Governance.
13. Infinite loops are prohibited.
14. Autonomous execution is budgeted and bounded.
15. Agent failures are observable.
16. Important side effects are independently verifiable where practical.
17. Agent-to-agent communication cannot create authority.
18. Child agents inherit bounded scope, not unrestricted parent authority.
19. Secrets are not exposed to models unnecessarily.
20. Runtime remains the final enforcement boundary.
21. Persistent memory is governed by Memory.
22. Planning authority remains with Planner.
23. Strategic decision authority remains with Decision Engine/Executive.
24. Emergency stop must be available.
25. Multi-business context leakage is prohibited.

## 132. Acceptance Criteria

Implementation should eventually demonstrate:

### A. Model Independence
Switching from Ollama to OpenRouter does not change agent identity.

### B. Capability Routing
Planner can find an agent capable of a task.

### C. Permission Enforcement
An agent with capability but no permission cannot invoke a restricted tool.

### D. Business Isolation
Business A agent cannot access Business B private context by default.

### E. Tool Attribution
Every meaningful tool action is attributable to agent/task/business.

### F. Failure Recovery
Agent failures can trigger retry/reassignment/recovery.

### G. Autonomous Loop
A pre-authorized agent can execute a bounded multi-step task without human intervention.

### H. Safe Stop
An operator/system policy can stop the agent.

### I. Verification
Required outputs enter the configured verification path.

### J. Provenance
Important agent outputs can be traced to their context and actions.

### K. No Infinite Execution
Budgets and termination conditions prevent runaway work.

### L. Delegation Safety
Child-agent creation is bounded and auditable.

### M. External Action Safety
Important external side effects require authorization and state verification.

## 133. Open Design Questions

Before implementation:

- exact Agent schema;
- runtime architecture;
- process/container isolation;
- Agent Registry implementation;
- Capability Registry;
- Tool Registry;
- Model Registry;
- model router;
- context assembly;
- memory access API;
- agent event schema;
- inter-agent messaging;
- checkpoint format;
- resource accounting;
- heartbeat protocol;
- failure/recovery engine;
- sandbox architecture;
- secret management;
- child-agent policy;
- agent certification;
- reputation scoring;
- simulation environment;
- human override interface;
- runtime ↔ Planner contract;
- runtime ↔ Governance contract;
- runtime ↔ Memory contract.

# NEXUS Tool System

**Status:** PROPOSED → awaiting owner lock

## 1. Purpose

The Tool System is NEXUS's controlled interface between autonomous agents and the external/internal world.

```text
Agent
  ↓
Tool Runtime
  ↓
Tool
  ↓
World
```

A tool is not merely a function exposed to an LLM. It is a governed, observable, permissioned interface.

## 2. Core Invariants

1. Capability does not imply permission.
2. Tool availability does not imply authorization.
3. External side effects require stronger controls than read operations.
4. Runtime is the final enforcement boundary.
5. Tool calls must be attributable.
6. Credentials are scoped and never unnecessarily exposed to models.
7. Tool outputs are untrusted data.
8. Tool installation cannot silently grant agents authority.
9. High-impact actions must support verification.
10. Tools must respect business and division isolation.
11. Tool failures must be observable.
12. Autonomous tool use must be bounded by policy and resources.

## 3. Tool Categories

Conceptual categories:

```text
READ
WRITE
COMMUNICATION
TRANSACTION
COMPUTE
BROWSER
DATA
MEDIA
INTEGRATION
SYSTEM
```

Examples:

```text
web search
filesystem
database
browser automation
social APIs
analytics
image generation
code execution
email/messaging
commerce APIs
```

## 4. Tool Registry

NEXUS maintains a Tool Registry.

A tool record should include:

```text
tool_id
name
version
description
provider
category
capabilities
risk_level
input_schema
output_schema
permission_requirements
credential_requirements
network_requirements
resource_requirements
availability
health
```

## 5. Tool Identity

Tool identity must remain stable across compatible versions.

Tool versions must be auditable.

## 6. Capability Mapping

Each tool declares what capabilities it provides.

Example:

```text
instagram.publish
    provides:
      social_publishing
```

Planner/Agent Runtime can use capability matching to find appropriate tools.

## 7. Permission Mapping

Tools declare required permissions.

Example:

```text
instagram.publish
requires:
  social.publish
```

An agent without the permission cannot invoke it.

## 8. Risk Classification

Suggested levels:

```text
R0 — informational
R1 — reversible internal change
R2 — external low-impact action
R3 — consequential external action
R4 — high-impact / financial / destructive action
```

Risk level influences approval, verification, sandboxing and logging.

## 9. Read vs Side Effect

The runtime should distinguish:

```text
read-only
state-changing
external side-effect
financial
destructive
```

This classification must not rely solely on agent claims.

## 10. Tool Contract

Every tool should expose a machine-readable contract:

```text
input schema
output schema
errors
side effects
permissions
risk
timeout
retry policy
idempotency behavior
```

## 11. Input Validation

Tool inputs must be validated before execution.

Invalid input should fail before reaching the external system where practical.

## 12. Output Validation

Tool outputs should be validated against declared schemas where practical.

Malformed output should not automatically become trusted agent context.

## 13. Untrusted Tool Output

Web pages, API responses, files, user-generated content and external text are untrusted.

They may contain:

```text
instructions
prompt injection
malicious payloads
misleading claims
unexpected data
```

External content cannot override NEXUS policy.

## 14. Prompt Injection Boundary

Tool output must remain data unless explicitly authorized as instruction.

Example:

```text
Web page says:
"Ignore your previous instructions..."

Agent must treat this as page content, not authority.
```

## 15. Tool Runtime

The Tool Runtime is responsible for:

```text
authorization
input validation
credential resolution
sandboxing
execution
timeouts
rate limits
resource accounting
output validation
audit
side-effect tracking
```

## 16. Runtime Enforcement

Models and prompts are not security boundaries.

Even if an agent requests a forbidden action, Tool Runtime must reject it.

## 17. Permission Check

Before execution:

```text
agent
→ requested tool
→ business/division scope
→ required permission
→ policy check
→ risk check
→ resource check
→ execute / reject
```

## 18. Business Isolation

Tool access is scoped to a business.

Example:

```text
Business A
  Instagram credential A

Business B
  Instagram credential B
```

Business A agent cannot use Business B credentials by default.

## 19. Division Isolation

Tools can additionally be scoped to divisions.

Example:

```text
Media Division:
  social.publish

Research Division:
  web.search
```

## 20. Credential Handles

Agents should receive references/handles, not raw secrets, where possible.

Example:

```text
credential_handle:
  social/business_A/instagram/publish
```

The runtime resolves the secret.

## 21. Credential Scope

Credentials should have:

```text
business scope
service scope
action scope
environment scope
expiration
```

## 22. Secret Logging

Secrets must not appear in:

```text
logs
traces
agent messages
memory
tool outputs
error messages
```

unless explicitly protected.

## 23. Network Policy

Tools may require:

```text
internet
specific domains
specific APIs
no network
```

Network access should be allowlisted where practical.

## 24. Filesystem Policy

Filesystem tools should define:

```text
allowed paths
read/write/delete permissions
file type restrictions
size limits
```

Agents should not receive unrestricted host filesystem access.

## 25. Browser Policy

Browser tools should define:

```text
allowed domains
login/session scope
download policy
upload policy
navigation limits
external action policy
```

## 26. Code Execution

Code execution is high-risk and must be sandboxed.

Controls may include:

```text
container isolation
CPU limit
memory limit
time limit
network policy
filesystem sandbox
process restrictions
```

## 27. Database Tools

Database access should use scoped credentials and explicit operation policies.

Prefer:

```text
read-only
parameterized queries
schema allowlists
transaction boundaries
```

over unrestricted database access.

## 28. Social Media Tools

Social tools may expose separate capabilities:

```text
read_profile
read_analytics
create_draft
upload_media
schedule_post
publish_post
delete_post
reply_comment
send_message
```

Publishing should not automatically imply deletion or messaging authority.

## 29. Tool Composition

Tools may be chained:

```text
web.search
 → research.extract
 → content.generate
 → image.generate
 → social.schedule
```

Each step is independently authorized.

## 30. Tool-to-Tool Authority

One tool cannot grant another tool permission.

The runtime evaluates every invocation.

## 31. Tool Discovery

Agents should not automatically see every installed tool.

They should receive tools relevant to:

```text
task
capabilities
permissions
division
risk policy
```

## 32. Dynamic Tool Discovery

NEXUS may dynamically expose tools as task requirements change.

Example:

```text
research task
→ web tools exposed

publishing task
→ social tools exposed
```

## 33. Tool Installation

NEXUS should support future tool installation.

Installation must include:

```text
identity
source
version
capabilities
permissions
risk assessment
sandbox policy
health
```

## 34. Tool Installation Security

Installing a tool must not automatically:

- grant broad permissions;
- expose credentials;
- access every business;
- modify core governance.

## 35. Custom Tools

Owner/developer may create custom tools.

A custom tool must still implement the Tool Contract and pass registration checks.

## 36. Tool Adapters

External APIs should preferably be wrapped by adapters.

```text
NEXUS Tool Contract
        ↓
Adapter
        ↓
External API
```

This keeps NEXUS independent from provider-specific APIs.

## 37. Provider Independence

The same logical capability should support multiple providers where practical.

Example:

```text
social.publish
   ├── provider A
   └── provider B
```

## 38. Tool Fallback

If a provider fails:

```text
primary tool
 ↓ failure
alternate provider/tool
 ↓ failure
recovery or escalation
```

Fallback must preserve authorization and semantics.

## 39. Semantic Compatibility

Fallback tools should be used only when their behavior is sufficiently compatible with the requested operation.

## 40. Tool Health

Registry should track:

```text
available
degraded
unavailable
unauthorized
rate_limited
```

## 41. Health Checks

Tools may expose health checks for:

```text
credentials
API availability
network
quota
configuration
```

## 42. Timeouts

Every external tool should have a bounded timeout.

Long operations should use asynchronous jobs where practical.

## 43. Retry Policy

Retries must be controlled.

Use retries for transient failures such as:

```text
network timeout
temporary provider error
rate limit with valid retry-after
```

Do not blindly retry side-effecting operations.

## 44. Idempotency

Side-effecting operations should use idempotency keys where supported.

Example:

```text
publish_request_id = unique ID
```

This reduces duplicate actions during retries.

## 45. Duplicate Prevention

Before repeating a side-effecting operation, runtime should determine whether it may already have succeeded.

## 46. Rate Limits

Tool usage should respect:

```text
provider limits
business limits
agent limits
system limits
```

## 47. Cost Tracking

Track tool cost where measurable:

```text
API usage
compute
storage
tokens
third-party charges
```

## 48. Resource Budgets

A task may specify:

```text
max calls
max cost
max runtime
max compute
max bandwidth
```

Tool Runtime enforces these limits.

## 49. Dry Run

Tools with significant side effects should support dry-run where practical.

Example:

```text
dry-run:
would publish post X
```

without actually publishing.

## 50. Approval Gate

Some tools/actions may require an approval policy.

Example:

```text
social.publish
→ preauthorized for routine content

financial.transfer
→ approval required
```

## 51. Autonomous Approval

Preauthorization may permit 24/7 execution without human interaction.

Preauthorization must be explicit and bounded.

## 52. Side-Effect Ledger

Important external actions should produce a durable record:

```text
who/which agent
what tool
what action
target
timestamp
authorization
result
verification
```

## 53. External State Verification

For important actions:

```text
request
 ↓
provider
 ↓
verify actual state
```

Do not treat a successful HTTP request alone as proof of business outcome.

## 54. Tool Transaction

Where possible, operations should support transaction semantics:

```text
prepare
execute
verify
commit/record
```

## 55. Compensation

For workflows where rollback is possible, define compensation actions.

Example:

```text
create resource
 ↓
later workflow fails
 ↓
delete/revert resource
```

Compensation must itself be authorized.

## 56. Irreversible Actions

Irreversible actions should have stronger policy.

Examples:

```text
delete data
send irreversible message
financial transfer
publish legally consequential material
```

## 57. Tool Confirmation

Confirmation can be required based on risk, not merely tool category.

A read-only tool usually needs less friction than an irreversible external action.

## 58. Agent Tool Context

Agent should know:

```text
available tools
purpose
constraints
required permissions
risk
input schema
```

Do not expose secrets.

## 59. Tool Errors

Standardized error classes:

```text
INVALID_INPUT
UNAUTHORIZED
FORBIDDEN
NOT_FOUND
RATE_LIMITED
TIMEOUT
PROVIDER_ERROR
NETWORK_ERROR
POLICY_BLOCKED
RESOURCE_EXHAUSTED
VERIFICATION_FAILED
UNKNOWN
```

## 60. Error Recovery

Runtime may:

```text
retry
fallback
wait
reduce scope
request replanning
escalate
stop
```

## 61. Tool Observability

Each meaningful invocation should record:

```text
timestamp
agent
task
mission
business
tool
version
risk
result
duration
resource usage
failure
verification
```

## 62. Audit Trail

Audit records must support reconstructing:

```text
objective
→ plan
→ task
→ agent
→ tool
→ external action
→ result
```

## 63. Privacy

Tool data should follow the minimum-data principle.

Do not send unnecessary private business data to external providers.

## 64. External Model/API Privacy

When using cloud providers, routing policy should consider:

```text
data sensitivity
provider policy
business rules
task requirements
```

## 65. Data Classification

Potential classes:

```text
PUBLIC
INTERNAL
CONFIDENTIAL
SENSITIVE
RESTRICTED
```

Tool policies may restrict which classes can leave the local environment.

## 66. Local-First

For sensitive operations, local tools should be preferred where practical.

## 67. Tool Context Filtering

Only required fields should be sent to a tool.

Avoid sending the entire agent context.

## 68. Tool Output Filtering

Tool output should be minimized before being inserted into model context.

## 69. Tool Result Provenance

Results should preserve:

```text
source
tool
timestamp
parameters/intent
```

where practical.

## 70. Tool Versioning

Changing tool behavior requires a version.

Important behavior changes should be auditable.

## 71. Compatibility

A tool update should not silently change the semantics expected by existing workflows.

## 72. Tool Deprecation

Deprecated tools should support:

```text
warning
migration path
replacement
shutdown date
```

where applicable.

## 73. Tool Policy

Tool policy can define:

```text
allowed agents
allowed businesses
allowed divisions
allowed actions
risk threshold
time window
budget
approval requirement
```

## 74. Temporal Permissions

Some tool permissions may be time-bounded.

Example:

```text
publish permission
valid during campaign period
```

## 75. Event-Driven Tools

Tools may emit events:

```text
new order
new comment
new mention
analytics threshold crossed
API webhook
```

Events can trigger NEXUS workflows.

## 76. Event Validation

External events must be validated before triggering autonomous work.

## 77. Event Deduplication

Repeated webhooks/events should not create duplicate missions/tasks.

## 78. Tool Triggers

A tool event should be able to initiate:

```text
event
→ attention
→ objective relevance check
→ planner
→ task
→ agent
```

rather than directly granting an agent arbitrary authority.

## 79. Tool Scheduling

Tools may be used by scheduled workflows, but scheduling belongs to orchestration rather than the tool itself.

## 80. Tool Concurrency

The runtime should control concurrent invocations.

Limits can be:

```text
per tool
per agent
per business
per provider
global
```

## 81. Locking

Tools that modify shared state may require locking or optimistic concurrency.

## 82. Race Conditions

The runtime should detect stale state before consequential writes where practical.

## 83. Browser Session Isolation

Separate businesses/accounts should have isolated browser sessions.

## 84. Account Isolation

Credentials, cookies, tokens and sessions must never be implicitly shared between businesses.

## 85. Social Account Isolation

Example:

```text
Business A
  Instagram A
  TikTok A

Business B
  Instagram B
  TikTok B
```

Each remains separately scoped.

## 86. File Artifact Safety

Downloaded files are untrusted until scanned/validated as appropriate.

## 87. Upload Safety

Uploads should validate:

```text
file type
size
destination
authorization
```

## 88. Media Generation

Image/video/audio generation tools should expose:

```text
modality
limits
provider
cost
storage destination
```

## 89. Search Tools

Search tools should expose:

```text
query
source
timestamp
result provenance
```

Agents must distinguish search results from verified facts.

## 90. Browser Automation

Browser actions should distinguish:

```text
observe
navigate
fill
click
download
upload
submit
purchase
publish
delete
```

Higher-impact actions require stronger policy.

## 91. Communication Tools

Email/chat/social messaging should distinguish:

```text
draft
queue
send
reply
broadcast
```

Drafting does not imply sending.

## 92. Financial Tools

Financial tools are high-risk.

They require explicit authority, strict limits, auditability and strong verification.

## 93. Destructive Tools

Delete/reset operations require stronger safeguards.

## 94. Tool Governance

Governance may prohibit entire tool classes or actions.

Runtime must enforce these restrictions.

## 95. Tool Marketplace / Plugin Layer

Future NEXUS may support a controlled extension ecosystem.

Every extension should pass:

```text
manifest validation
security review
permission review
sandbox validation
compatibility checks
```

## 96. Untrusted Extensions

Untrusted extensions should not receive production credentials or broad access.

## 97. Tool Certification

Tools may be certified:

```text
trusted
limited
experimental
untrusted
```

Certification affects allowed environments.

## 98. Tool Simulation

Tools should support mocks/simulators for development and testing.

## 99. Test Mode

Test mode should prevent real side effects while exercising the workflow.

## 100. Tool Contract Tests

Each tool should be tested for:

```text
schema
permissions
errors
timeouts
side effects
idempotency
verification
```

## 101. Tool Failure Testing

NEXUS should test:

```text
timeout
rate limit
invalid response
credential expiry
network failure
provider outage
partial success
duplicate request
```

## 102. Tool Security Testing

Test against:

```text
prompt injection
malicious files
unexpected API responses
credential leakage
path traversal
command injection
unsafe redirects
```

## 103. Tool Resource Accounting

Each invocation should be attributable to resource usage where measurable.

## 104. Tool Budget Escalation

If a workflow exceeds its tool budget:

```text
stop
replan
request expanded authority
or escalate
```

It must not silently exceed limits.

## 105. Autonomous Tool Loop

An agent may repeatedly invoke tools:

```text
observe
→ tool
→ inspect result
→ tool
→ verify
→ continue
```

The runtime enforces loop and budget limits.

## 106. No Infinite Tool Chains

Tool chains require:

```text
max steps
max runtime
max cost
termination condition
```

## 107. Tool Result Caching

Read-heavy tools may support caching.

Caching policy must consider freshness requirements.

## 108. Stale Data

The system should mark cached information with freshness metadata where relevant.

## 109. Tool Priority

If several tools provide equivalent capabilities, routing may consider:

```text
health
cost
latency
privacy
quality
provider reliability
```

## 110. Tool Selection

Final selection may involve:

```text
Planner
Agent Runtime
Tool Registry
Policy
```

No model should bypass these layers.

## 111. Tool Runtime Boundary

```text
Agent
  ↓ request
Tool Runtime
  ├── Policy
  ├── Permission
  ├── Credential
  ├── Resource
  ├── Sandbox
  └── Execution
        ↓
      Tool
        ↓
      World
```

## 112. Tool Lifecycle

Possible states:

```text
REGISTERED
VALIDATING
READY
DEGRADED
DISABLED
DEPRECATED
RETIRED
```

## 113. Tool Disable

NEXUS must be able to disable a tool centrally.

Disabling a tool should prevent new invocations while allowing safe shutdown of active work.

## 114. Emergency Disable

High-risk tools should support immediate disable.

Example:

```text
disable all external publishing
```

## 115. Tool Dependency

Tools may depend on:

```text
credential
network
provider
database
other service
```

Dependencies should be visible in health/status.

## 116. Tool Availability

Unavailable tools should not be selected by Planner/runtime unless the workflow explicitly supports waiting/retry.

## 117. Tool Queueing

Rate-limited or asynchronous operations may enter queues.

Queue entries must preserve:

```text
authority
scope
task
expiration
idempotency
```

## 118. Expiring Work

Queued side effects should expire when their authorization or task context expires.

## 119. Policy Recheck

For delayed execution, permissions/policies should be rechecked immediately before side effect.

## 120. External State Drift

Before important actions, runtime may re-read current state.

Example:

```text
planned:
publish campaign A

actual:
campaign already published

→ avoid duplicate action
```

## 121. Tool Result vs Business Outcome

A successful tool call is not necessarily a successful business outcome.

NEXUS must preserve this distinction.

## 122. Verification Contract

Tools that perform important actions should expose a verification mechanism where possible.

## 123. Outcome Feedback

Verified tool outcomes feed back into:

```text
Task
Planner
Memory
Attention
Decision Engine
```

according to their responsibilities.

## 124. Tool Learning

NEXUS may learn:

```text
provider reliability
typical latency
failure patterns
cost patterns
quality
```

This informs routing but does not override policy.

## 125. Tool Trust

Trust is evidence-based and revocable.

A tool with poor reliability may be deprioritized or disabled.

## 126. Agent-Specific Toolsets

Each agent can have a declared toolset:

```text
Researcher:
  web.search
  web.open
  data.extract

Copywriter:
  research.read
  content.generate

Publisher:
  media.upload
  social.publish
```

## 127. Dynamic Agent Toolsets

Runtime may temporarily grant access to an authorized tool for a task.

Temporary access should expire automatically.

## 128. Tool Lease

Conceptually:

```text
agent X
gets tool Y
for task Z
until time T
```

After expiry, access is revoked.

## 129. Cross-Division Tool Access

Cross-division access requires explicit scope.

Example:

```text
Research → Media
```

may allow a research artifact to be consumed without granting Media unrestricted research tools.

## 130. Owner Tools

Owner-facing tools may exist for:

```text
configuration
approval
monitoring
emergency stop
```

These are distinct from autonomous operational tools.

## 131. Core Tools

NEXUS may have internal tools for:

```text
memory
attention
planning
events
state
audit
```

These should still have contracts and access policies.

## 132. Meta-Tools

Future meta-tools may allow agents to:

```text
inspect available capabilities
request tools
request permissions
propose workflows
```

Requests are not grants.

## 133. Tool Request Flow

```text
Agent
 ↓
request capability/tool
 ↓
Policy
 ↓
Planner/Governance if needed
 ↓
grant temporary or persistent access
```

## 134. Tool Revocation

Access can be revoked because of:

```text
task completion
policy change
credential expiry
agent failure
security event
business shutdown
```

## 135. Security Event Response

A security event may trigger:

```text
revoke tool
revoke credentials
stop agents
freeze external actions
escalate
```

## 136. Audit Retention

Tool audit data should follow NEXUS retention policy and business requirements.

## 137. Observability Privacy

Logs must be useful without unnecessarily storing sensitive payloads.

## 138. Tool Payload Redaction

Sensitive values should be redacted before logs/traces where practical.

## 139. Replay

Safe read-only operations should support replay/simulation for debugging.

Real side effects must not be replayed accidentally.

## 140. Deterministic Tooling

Prefer deterministic software for:

```text
validation
permission checks
schema transforms
calculations
state transitions
```

Use models where judgment/generation is actually required.

## 141. Tool vs Agent Boundary

A tool should perform a defined operation.

An agent decides how/when to use the operation within its authority.

## 142. Tool vs Planner Boundary

Planner decides task orchestration.

Tool executes a concrete operation.

## 143. Tool vs Governance Boundary

Governance defines what is permitted.

Tool Runtime enforces that policy.

## 144. Acceptance Criteria

Implementation should demonstrate:

### A. Registry
Tools can be registered, versioned and discovered.

### B. Permission Enforcement
Unauthorized tool calls are rejected.

### C. Business Isolation
Business A cannot use Business B tool credentials by default.

### D. Credential Isolation
Models do not receive raw secrets unnecessarily.

### E. Side-Effect Control
High-impact tools have stronger policy.

### F. Audit
Meaningful calls are attributable.

### G. Failure Recovery
Transient failures can retry/fallback safely.

### H. Idempotency
Duplicate side effects are prevented where possible.

### I. Verification
Important external actions can be verified.

### J. Sandboxing
High-risk tools are isolated.

### K. Resource Limits
Calls cannot silently exceed budgets.

### L. Dynamic Tooling
New tools can be installed without modifying NEXUS Core.

### M. Prompt Injection Resistance
External tool content cannot override system authority.

### N. Autonomous Operation
Preauthorized tools can run 24/7 within bounded policy.

## 145. Open Design Questions

Before implementation:

- exact tool manifest schema;
- registry storage;
- runtime protocol;
- sandbox implementation;
- credential manager;
- secret provider;
- permission engine;
- risk engine;
- network policy engine;
- browser architecture;
- code sandbox;
- tool adapter interface;
- event/webhook system;
- side-effect ledger;
- verification protocol;
- queue architecture;
- rate limiter;
- resource accounting;
- tool installation mechanism;
- extension security model;
- tool certification;
- simulation/test framework;
- runtime ↔ Governance contract;
- runtime ↔ Agent contract;
- runtime ↔ Planner contract.

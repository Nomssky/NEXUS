# NEXUS Tool & Integration Runtime

**Status:** PROPOSED → awaiting owner lock

## 1. Purpose

The Tool & Integration Runtime is the controlled execution layer between NEXUS agents and external capabilities.

Agents decide what they need. The runtime decides whether and how that action may execute.

```text
Agent
  ↓
Tool Intent
  ↓
Policy / Authority Check
  ↓
Credential Check
  ↓
Tool Runtime
  ↓
External System
  ↓
Result
  ↓
Verification
  ↓
Memory / Event / Workflow
```

## 2. Core Principle

A tool is not simply a function exposed to a model.

A tool is:

```text
Capability
+ Input Contract
+ Permission Policy
+ Credential Policy
+ Execution Policy
+ Safety Policy
+ Result Contract
+ Audit Metadata
```

## 3. Agent Boundary

Agents should request tool execution through Tool Runtime.

Agents should not receive unrestricted infrastructure access.

## 4. Tool Definition

Every tool should define:

```text
tool_id
name
description
version
input_schema
output_schema
required_capabilities
risk_level
side_effect_level
credential_requirements
network_requirements
timeout
retry_policy
```

## 5. Tool Categories

Initial categories:

```text
WEB
SEARCH
BROWSER
HTTP_API
DATABASE
FILESYSTEM
CODE_EXECUTION
SHELL
MESSAGING
SOCIAL_MEDIA
EMAIL
CALENDAR
SCHEDULER
ANALYTICS
MEDIA
BUSINESS_SYSTEM
INTERNAL_NEXUS
```

## 6. Read vs Write Tools

Tools should be classified as:

```text
READ
WRITE
DELETE
EXECUTE
TRANSACT
```

Write/delete/transactional tools require stronger controls.

## 7. Side-Effect Classification

Suggested:

```text
NONE
LOW
MEDIUM
HIGH
CRITICAL
```

## 8. Tool Risk

Risk should be evaluated independently from model confidence.

A highly confident model can still request a dangerous action.

## 9. Tool Authority

Tool access requires:

```text
agent identity
business scope
division scope
task scope
capability
policy approval
```

## 10. Least Privilege

Agents receive only the tools required for their responsibilities.

## 11. Dynamic Tool Selection

Agents may discover tools through the Tool Registry.

Tool discovery does not automatically grant permission.

## 12. Tool Registry

Registry supports:

```text
register
discover
inspect
enable
disable
version
deprecate
```

## 13. Tool Versioning

Tool definitions must be versioned.

Existing workflows should not unexpectedly break when a tool changes.

## 14. Tool Adapter

External integrations should be hidden behind adapters.

```text
Agent
 ↓
NEXUS Tool Interface
 ↓
Provider Adapter
 ↓
External API
```

## 15. Provider Independence

NEXUS should avoid hard-coding a single external provider.

## 16. API Tools

Support generic authenticated HTTP APIs through controlled adapters.

## 17. Web Search

Search should return structured results:

```text
query
source
title
url/reference
snippet
timestamp
```

## 18. Browser Tool

Browser automation should expose bounded actions rather than unrestricted browser control where practical.

## 19. Browser Sessions

Browser sessions should have:

```text
session_id
scope
allowed domains
credential context
expiration
```

## 20. Domain Allowlist

High-risk browser/API agents may operate only on approved domains.

## 21. Network Policy

Network access should be explicitly controlled.

Possible policies:

```text
NO_NETWORK
ALLOWLIST
RESTRICTED
FULL
```

Full network access should be exceptional.

## 22. Filesystem Tool

Agents should access only authorized directories.

## 23. Filesystem Scope

Example:

```text
Business A
└── Media
    └── workspace/
```

An agent should not automatically access the entire host filesystem.

## 24. Shell / Code Execution

Shell and arbitrary code execution are high-risk capabilities.

They require stronger isolation and policy.

## 25. Sandbox

Risky code/tools should execute in an isolated environment when practical.

Possible isolation:

```text
container
sandbox process
restricted VM
```

## 26. Resource Limits

Sandboxed tools should have:

```text
CPU limit
memory limit
disk limit
process limit
runtime limit
network limit
```

## 27. Credential Isolation

Credentials should never be treated as ordinary agent memory.

## 28. Credential Broker

Agents request access through a credential subsystem.

The runtime supplies only the minimum required credential context.

## 29. Credential Lifetime

Prefer:

```text
short-lived
scoped
revocable
```

credentials.

## 30. Credential Exposure

Tool execution should minimize credential exposure to the model.

## 31. Secret Redaction

Logs/results should redact secrets.

## 32. Credential Rotation

Runtime must support credential rotation without changing agent definitions.

## 33. OAuth

OAuth-based integrations should support delegated/scoped access.

## 34. Tool Authentication

Authentication belongs to integration infrastructure, not prompts.

## 35. Tool Input Schema

Every tool must validate inputs before execution.

## 36. Tool Output Schema

Every tool should return a predictable structured result.

## 37. Untrusted Tool Output

External output must be treated as untrusted data.

It cannot redefine:

```text
system policy
agent authority
objective
tool permissions
```

## 38. Prompt Injection

Web pages, emails, documents, API responses, and other external content may contain malicious instructions.

Those instructions must remain data.

## 39. Output Sanitization

Tool Runtime may sanitize:

```text
secrets
malicious payloads
oversized content
unsupported markup
```

## 40. Result Size Limits

Tools should have bounded result sizes.

Large results should be stored as artifacts and referenced.

## 41. Artifact Handling

Instead of injecting large files into model context:

```text
Tool
 ↓
Artifact Store
 ↓
artifact_id
 ↓
Agent retrieves relevant portion
```

## 42. Tool Timeout

Every external call should have a timeout.

## 43. Retry Policy

Retries depend on failure type.

```text
transient → retry
validation → do not retry
permission → do not retry
rate limit → backoff
unknown → bounded retry
```

## 44. Exponential Backoff

Transient external failures should use bounded backoff.

## 45. Retry Budget

Retries consume a budget.

## 46. Idempotency

Write operations should support idempotency where possible.

## 47. Duplicate Action Protection

If a tool call times out after an external system may have completed it, NEXUS must reconcile state before retrying.

## 48. Transaction Safety

Critical actions should use transactional or compensating mechanisms where supported.

## 49. Two-Phase Intent

For high-impact actions:

```text
prepare
→ verify
→ execute
```

## 50. Dry Run

Tools may support dry-run mode.

## 51. Approval Gate

Certain actions may require owner/governance approval before execution.

## 52. Approval Classification

Example:

```text
AUTO
AUTO_WITH_LIMIT
REVIEW_REQUIRED
OWNER_REQUIRED
BLOCKED
```

## 53. Autonomy Does Not Mean Unlimited Access

NEXUS can operate autonomously while remaining policy-bounded.

## 54. Tool Budget

Tools may have:

```text
calls/minute
calls/task
calls/workflow
cost/task
daily limit
```

## 55. Rate Limits

Respect external provider rate limits.

## 56. Concurrency Limits

Avoid uncontrolled parallel calls.

## 57. Tool Queue

Calls may enter a queue when capacity is unavailable.

## 58. Priority

Tool requests inherit task/workflow priority where appropriate.

## 59. Fairness

One agent must not monopolize external resources.

## 60. Tool Circuit Breaker

Repeated failures may temporarily disable a provider/tool.

```text
healthy
→ failing
→ open
→ recovery
→ healthy
```

## 61. Provider Fallback

If multiple providers offer equivalent capabilities, runtime may route to a fallback.

## 62. Provider Health

Track:

```text
latency
success rate
error rate
availability
rate limits
cost
```

## 63. Tool Health

Track health independently from provider health.

## 64. Tool Observability

Every call should produce operational telemetry.

## 65. Tool Trace

Trace:

```text
agent
→ task
→ tool
→ provider
→ request
→ result
→ verification
```

## 66. Audit Log

High-impact tool actions require durable audit records.

## 67. Audit Record

Conceptual:

```text
event_id
agent_id
business_id
workflow_id
task_id
tool_id
action
timestamp
authorization
result_status
artifact_refs
```

## 68. Do Not Log Secrets

Request/response logs must be redacted or minimized.

## 69. Tool Result Verification

Tool success should be based on runtime evidence, not model assumptions.

## 70. External State Verification

For important write operations:

```text
execute
→ query/reconcile
→ confirm state
```

## 71. Honest Tool Claims

An agent must not claim:

```text
email sent
post published
file uploaded
payment completed
```

without tool/runtime evidence.

## 72. Partial Failure

A multi-step tool workflow may partially succeed.

Runtime must report completed and incomplete operations separately.

## 73. Compensation

Where supported, failed workflows may execute compensating actions.

## 74. Tool Composition

Tools may be composed into workflows.

## 75. Tool Chaining

Example:

```text
search
→ analyze
→ generate
→ review
→ publish
```

Each step remains independently governed.

## 76. Tool Loop Protection

Prevent:

```text
tool A
→ tool B
→ tool A
→ ...
```

without bounded workflow logic.

## 77. Tool Call Budget

Every autonomous loop should have a maximum tool-call budget.

## 78. Tool Decision Record

Record:

```text
requested tool
purpose
scope
authorization result
execution result
```

Do not require storing private chain-of-thought.

## 79. Tool Errors

Standardize error classes:

```text
AUTHENTICATION_ERROR
AUTHORIZATION_ERROR
VALIDATION_ERROR
RATE_LIMIT
TIMEOUT
NETWORK_ERROR
PROVIDER_ERROR
NOT_FOUND
CONFLICT
SIDE_EFFECT_UNKNOWN
SANDBOX_ERROR
POLICY_BLOCK
```

## 80. Side Effect Unknown

This is critical.

If execution outcome is uncertain:

```text
DO NOT blindly retry
→ reconcile external state
```

## 81. Tool Failure Escalation

Repeated failures may trigger:

```text
retry
→ fallback
→ replan
→ Attention
→ owner escalation
```

## 82. Tool Discovery

Agents can query:

```text
what tools exist?
what does this tool do?
what permissions does it require?
```

## 83. Tool Documentation

Every tool should have machine-readable and human-readable documentation.

## 84. Tool Examples

Tool schemas should include safe examples where useful.

## 85. Tool Capability Matching

Planner can select tools based on required capability.

## 86. Tool Capability vs Permission

Capability says what a tool can do.

Permission says whether this agent may use it.

## 87. Integration Lifecycle

```text
DISCOVERED
→ CONFIGURED
→ AUTHENTICATED
→ ENABLED
→ HEALTHY
→ DEGRADED
→ DISABLED
→ DEPRECATED
```

## 88. Integration Installation

Installing an integration should not automatically grant all agents access.

## 89. Integration Scope

An integration can be scoped to:

```text
NEXUS
business
division
agent
workflow
```

## 90. Multi-Business Isolation

Business A's social media credentials must never become available to Business B by default.

## 91. Social Media Integration

Social integrations may expose:

```text
read profile
read analytics
create draft
upload media
schedule post
publish
read comments
reply
```

Each capability should be independently permissioned.

## 92. Publishing Safety

Publishing is a consequential external side effect.

Possible controls:

```text
draft-only
approval-required
auto-publish-with-policy
```

## 93. Content Review

Automated publishing may require:

```text
content validation
brand policy
platform policy
objective alignment
```

## 94. Email Integration

Separate:

```text
read
draft
send
delete
```

permissions.

## 95. Messaging Integration

Separate:

```text
read
draft
send
moderate
```

permissions.

## 96. Calendar Integration

Separate:

```text
read
create
modify
cancel
```

permissions.

## 97. Database Integration

Prefer scoped queries/views.

Avoid giving agents unrestricted production database credentials.

## 98. Database Writes

High-impact writes should use controlled APIs where possible.

## 99. Internal NEXUS Tools

NEXUS itself should expose internal capabilities through the same governed tool interface when practical.

Examples:

```text
memory.search
workflow.create
attention.create
agent.create
artifact.store
```

## 100. Internal Tool Governance

Internal tools are not exempt from authorization.

## 101. Scheduling

Tools may be triggered by:

```text
time
event
condition
objective
workflow
```

Scheduling belongs to orchestration/scheduler infrastructure.

## 102. Webhooks

External systems may trigger NEXUS through authenticated webhooks.

## 103. Webhook Validation

Validate:

```text
signature
source
timestamp
replay protection
payload schema
```

## 104. Replay Protection

Webhook events should support idempotency/event IDs.

## 105. External Event Normalization

Normalize external events into NEXUS event format.

## 106. API Keys

API keys should be stored in credential infrastructure.

## 107. Secrets in Environment

Secrets should not be embedded in agent definitions or source code.

## 108. Secret Rotation

Rotation should not require redeploying agent logic.

## 109. Integration Configuration

Separate:

```text
tool code
provider config
credentials
agent permission
```

## 110. Tool Installation

Installing a tool should be reversible.

## 111. Tool Disable

Disabled tools should fail closed.

## 112. Fail Closed

When authorization state is uncertain:

```text
do not execute
```

## 113. Network Failure

Network failure must not be interpreted as successful external action.

## 114. Timeout Failure

Timeout must be classified as:

```text
outcome unknown
```

when external completion cannot be established.

## 115. Provider Maintenance

Provider maintenance should trigger health/degraded state and fallback where possible.

## 116. Tool Compatibility

Tools should declare supported runtime versions and dependencies.

## 117. Dependency Management

Tool dependencies should be isolated where possible.

## 118. Tool Sandbox Profiles

Examples:

```text
SAFE_READ
WEB_RESEARCH
CODE_SANDBOX
SOCIAL_PUBLISH
SYSTEM_ADMIN
```

Each profile defines allowed resources.

## 119. System Administration

System-admin capabilities should be extremely restricted.

## 120. Dangerous Tools

Examples:

```text
arbitrary shell
production DB write
credential management
financial transaction
mass messaging
account deletion
```

require elevated governance.

## 121. Human Approval

Owner approval can be represented as an explicit authorization event.

## 122. Approval Expiration

Approvals should expire.

## 123. Approval Scope

Approval should specify:

```text
who
what
which business
which tool
which action
which limit
until when
```

## 124. No Blanket Approval

Approval for one action should not silently authorize unrelated actions.

## 125. Tool Policy Engine

Policy evaluates:

```text
agent
tool
action
scope
risk
context
approval
limits
```

## 126. Policy Decision

Possible:

```text
ALLOW
ALLOW_WITH_LIMIT
REQUIRE_APPROVAL
DENY
```

## 127. Policy Explainability

Runtime should record a concise policy decision reason.

## 128. Policy Precedence

Higher-level security/governance policies override agent preferences.

## 129. Tool Runtime Independence

Tool Runtime must remain functional even if a particular agent/model fails.

## 130. Model Independence

Tool execution must not depend on a specific model provider.

## 131. Tool Call Translation

Model-specific tool-call formats should be normalized into NEXUS tool requests.

## 132. Structured Tool Request

Conceptual:

```text
request_id
agent_id
task_id
tool_id
action
arguments
scope
priority
deadline
```

## 133. Structured Tool Result

Conceptual:

```text
request_id
status
result
artifacts
external_reference
verification_state
error
metadata
```

## 134. External Reference

External systems may return IDs.

Store them for reconciliation.

## 135. Reconciliation

Runtime should support:

```text
get_external_state
compare_expected_state
resolve_unknown
```

## 136. Idempotency Key

Generate deterministic or persisted idempotency keys for supported actions.

## 137. Tool Transaction Record

Persist enough state to determine whether an action was already attempted.

## 138. Long-Running Tools

Long operations should become jobs:

```text
submit
→ RUNNING
→ progress
→ COMPLETE/FAILED
```

## 139. Tool Job Polling

Runtime can poll long-running external jobs.

## 140. Tool Job Cancellation

Where supported, jobs may be cancelled.

## 141. Tool Progress

Long operations may report progress to Workflow/Attention.

## 142. Background Tools

Tools may continue after the owner disconnects.

## 143. Owner Visibility

Owner can inspect active tool jobs.

## 144. Tool Notifications

Important failures/completions may generate Attention items.

## 145. Artifact Download

External files should be downloaded through controlled artifact tooling.

## 146. Artifact Upload

Uploads require scope and permission.

## 147. Media Tools

Media generation/editing tools should use artifact references.

## 148. Content Safety

Tool outputs should pass applicable safety/policy validation before consequential use.

## 149. Data Validation

External structured data should be schema-validated before being trusted by workflows.

## 150. External System Drift

Integration schemas can change.

Runtime should detect compatibility failures.

## 151. Integration Health Checks

Periodic health checks should validate configured integrations.

## 152. Health Check Scope

Health checks must avoid unintended side effects.

## 153. Read-Only Health Check

Prefer non-mutating health checks.

## 154. Rate Limit Awareness

Health checks must not create excessive external traffic.

## 155. Provider Cost

Track external API costs where available.

## 156. Cost Attribution

Attribute cost to:

```text
tool
agent
task
workflow
division
business
```

## 157. Tool Quotas

Business-level and division-level quotas may be configured.

## 158. Tool Budget Exhaustion

When budget is exhausted:

```text
stop
→ fallback/replan
→ Attention
```

## 159. Tool Cache

Safe read results may be cached.

## 160. Cache Freshness

Cached external data must have TTL/freshness metadata.

## 161. Cache Invalidation

Critical updates should invalidate dependent caches.

## 162. Sensitive Cache

Sensitive external data requires stricter handling.

## 163. Tool Result Memory

Useful verified results may be proposed to Memory.

## 164. Tool Result ≠ Memory

Not every tool result should become durable memory.

## 165. Tool Result ≠ Truth

External systems can be wrong or stale.

## 166. Verification

Verification should depend on the action's consequence.

## 167. Low-Risk Verification

Simple read actions may require basic schema validation.

## 168. High-Risk Verification

Critical writes should require external state confirmation.

## 169. Multi-System Actions

Actions across systems should be coordinated through workflows.

## 170. Distributed Transaction Reality

Do not assume external APIs support atomic distributed transactions.

Use compensation/reconciliation.

## 171. Failure Recovery

Recovery should be deterministic where possible.

## 172. Tool Runtime Restart

Runtime restart must preserve in-flight request state.

## 173. In-Flight Requests

Track:

```text
submitted
running
unknown
completed
failed
```

## 174. Unknown In-Flight State

On restart:

```text
reconcile
→ then retry or complete
```

## 175. Tool Runtime Queue Durability

Important queued actions should survive process restart.

## 176. Dead Letter Queue

Repeatedly failing tool requests may enter a dead-letter queue.

## 177. Dead Letter Handling

Dead-letter items should be inspectable and reprocessable.

## 178. Tool Metrics

Track:

```text
calls
success
failure
latency
timeouts
retries
cost
policy blocks
```

## 179. Integration Metrics

Track:

```text
provider health
authentication failures
rate limits
schema errors
availability
```

## 180. Security Metrics

Track:

```text
denied tool calls
credential failures
scope violations
sandbox violations
suspicious activity
```

## 181. Anomaly Detection

Unusual tool usage may trigger Security/Attention.

## 182. Tool Abuse

Detect patterns such as:

```text
mass publishing
mass downloading
rapid API calls
unexpected domains
credential probing
```

## 183. Emergency Disable

NEXUS should be able to disable:

```text
single tool
integration
provider
agent tool access
entire category
```

## 184. Kill Switch

Critical tool categories should have emergency shutdown controls.

## 185. Tool Recovery

After emergency disable:

```text
investigate
→ verify safety
→ re-enable explicitly
```

## 186. Integration Migration

Providers can be replaced without changing agent business logic.

## 187. Provider Abstraction Example

```text
SocialPublisher
 ├── Provider A
 ├── Provider B
 └── Provider C
```

Agents depend on `SocialPublisher`, not provider-specific implementation.

## 188. Tool Marketplace / Plugins

Future NEXUS versions may support installable tools/plugins.

Installed plugins must enter the same registry/governance system.

## 189. Plugin Trust

Third-party tools should have trust metadata and restricted capabilities.

## 190. Plugin Sandbox

Untrusted plugins should run isolated.

## 191. Plugin Permissions

Plugins request explicit permissions.

## 192. Plugin Review

High-risk plugins may require owner approval before activation.

## 193. Tool Development SDK

Future SDK should allow developers to define:

```text
schema
handler
permissions
sandbox profile
health check
version
```

## 194. Tool Testing

Each tool should have:

```text
unit tests
schema tests
authorization tests
failure tests
timeout tests
idempotency tests
security tests
```

## 195. Integration Testing

Test against provider sandboxes/mocks where possible.

## 196. Contract Testing

Validate provider response schemas.

## 197. Chaos Testing

Simulate:

```text
timeout
rate limit
invalid response
partial response
provider outage
credential expiration
network failure
```

## 198. Acceptance Criteria

### A. Registry
Tools can be registered, discovered, versioned, disabled, and deprecated.

### B. Permission
Agents cannot use unauthorized tools.

### C. Scope
Tool access respects business/division/task scope.

### D. Credential Isolation
Secrets are managed outside ordinary agent memory.

### E. Sandboxing
High-risk tools have resource/network isolation.

### F. Structured Calls
Tool requests/results use stable schemas.

### G. Reliability
Timeout, retry, fallback, and circuit-breaker behavior works.

### H. Idempotency
Duplicate external side effects are prevented/reconciled.

### I. Verification
Important actions can be externally verified.

### J. Audit
Consequential tool actions are traceable.

### K. Cost
Tool/API usage is attributable and bounded.

### L. Autonomy
Agents can operate tools unattended within policy.

### M. Multi-Business
Credentials and data remain business-scoped.

### N. Background Execution
Tool jobs continue without owner UI connection.

### O. Injection Resistance
External tool content cannot redefine NEXUS authority/policy.

### P. Recovery
In-flight/unknown tool operations can be reconciled after restart.

### Q. Emergency Control
Tools/integrations can be disabled immediately.

## 199. Open Design Questions

Before implementation:

- tool registry schema;
- capability model;
- tool permission model;
- policy engine;
- credential broker;
- secret storage;
- provider adapter interface;
- HTTP client;
- browser runtime;
- filesystem sandbox;
- code execution sandbox;
- shell restrictions;
- network policy;
- tool request protocol;
- tool result protocol;
- idempotency system;
- external state reconciliation;
- retry/backoff;
- circuit breaker;
- durable job queue;
- dead-letter queue;
- artifact integration;
- webhook gateway;
- OAuth management;
- API-key management;
- rate limiting;
- cost accounting;
- tool health;
- audit logging;
- emergency kill switch;
- plugin SDK;
- plugin sandbox;
- tool testing framework.

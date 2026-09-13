# NEXUS Principles

These principles define stable architectural rules.

## Status

- **LOCKED**: invariant unless explicitly re-decided.
- **GUIDING**: strong design principle.
- **PROPOSED**: direction to validate.

## Locked Principles

1. **NEXUS is an operating system.** It is not merely a chatbot or agent framework.
2. **Agents are autonomous.** They are autonomous workers rather than passive functions.
3. **Executive coordinates; specialists execute.** NEXUS Executive must not perform every specialized task itself.
4. **Objective context must survive.** Consequential work retains its relationship to the objective and, where applicable, higher-level objectives.
5. **Attention is first-class.** Not every event deserves cognitive processing.
6. **Memory is more than storage.** It preserves useful context, experience, knowledge, preferences, and learning.
7. **Agents do not need the same model.** Model selection is needs-based.
8. **Local models are first-class.** Ollama, Hugging Face/local inference, and other self-hosted runtimes are supported.
9. **Agents reason; tools act.** External side effects happen through controlled tools.
10. **Autonomy is bounded by authority.** Intelligence does not grant unlimited permission.
11. **Lower layers cannot expand authority.** Child policies cannot exceed parent authority.
12. **Important side effects are observable and auditable.**
13. **Important actions are verifiable.** A successful transport response is not automatically proof of external success.
14. **Runtime is independent from UI.** UI sessions represent perspective, not execution ownership.
15. **Runtime is event-driven.**
16. **Deterministic logic first.** Simple deterministic conditions should not require an LLM call.
17. **Failures must be recoverable.** Persistence, retry, recovery, heartbeat, cancellation, and crash recovery are required architectural concerns.
18. **Learning must be governed.** Observations do not automatically become trusted knowledge.
19. **Knowledge has freshness.** Time-sensitive knowledge tracks confidence, evidence, and verification.
20. **Owner is root authority.**
21. **Owner can stop the system.** Pause/restrict controls must exist at appropriate scopes.
22. **Escalation is a valid autonomous decision.** Knowing when to ask the owner is part of autonomy.
23. **Providers are replaceable.** Models, tools, and integrations use replaceable interfaces/adapters.
24. **NEXUS is extensible.** Businesses, divisions, agents, skills, tools, and providers can be added without redesigning Core.
25. **No silent architecture drift.** A locked principle requires an explicit architecture decision before being changed.

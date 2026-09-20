# NEXUS — Development (M8 Memory + Knowledge)

This document covers **only** M8: memory and knowledge systems.
It does not duplicate architecture or contract docs.

> M8 extends M0–M7. It adds **Memory Store, Context Assembly,
> and Knowledge Ingestion** — the systems that let NEXUS remember,
> retrieve, and learn from information in a scoped, governed way.
> It does **not** implement full attention dedup or autonomous
> workflow (M9).

---

## Scope (C09)

| Component | What M8 adds |
|---|---|
| **Memory Store** | scoped admission, retrieval, archive, forget |
| **Context Assembly** | build context from memory for agents |
| **Knowledge Ingestion** | discovery → validation → classification → store pipeline |

### Invariants preserved by M8

- **Scoped retrieval** — no "everything NEXUS knows" default
- **Observation ≠ permanent memory** — working memory can be forgotten
- **Memory/knowledge can never authorize** — no authority fields
- **Knowledge is untrusted until validated** — pipeline enforces validation
- **Provenance always tracked** — source, confidence, verification
- **Business isolation** — memories scoped to business

---

## Layout

```
internal/foundation/memory/
  memory.go        Memory store: admission, scoped retrieval, archive, forget
  context.go       Context assembly: build context from memory
  knowledge.go     Knowledge ingestion: discovery → validation → classification → store
  memory_test.go   20 tests (TEST-M8-001..020)
```

---

## Usage

```go
import "github.com/Nomssky/NEXUS/internal/foundation/memory"

// 1. Memory store with scoped retrieval
ms := memory.NewMemoryStore()
ms.Admit(&memory.MemoryEntry{
    Type:       memory.MemoryTypeEpisodic,
    BusinessID: "biz-1",
    Content:    "Workflow completed",
    Provenance: memory.Provenance{Source: "workflow", Confidence: 0.9},
})

// 2. Scoped retrieval
results := ms.Retrieve(&memory.MemoryQuery{
    BusinessID: "biz-1",
    Types:      []memory.MemoryType{memory.MemoryTypeEpisodic},
})

// 3. Context assembly
ca := memory.NewContextAssembler(ms)
ctx, _ := ca.Assemble(&memory.ContextRequest{
    BusinessID: "biz-1",
    MaxTokens:  2000,
})

// 4. Knowledge ingestion pipeline
ki := memory.NewKnowledgeIngestion(ms)
entry, _ := ki.Discover(memory.SourceWeb, "https://example.com", "biz-1", "Knowledge")
ki.Validate(entry.ID, 0.85)
ki.Classify(entry.ID, "fact")
ki.Store(entry.ID)
```

---

## Testing

- 20 new tests covering TEST-M8-001..020
- M0–M7 tests unchanged and passing
- Race detector clean
- Locked-layer guard passes (no `contracts/` or `Core/` changes)

# Graph Report - internal/ports  (2026-07-26)

## Corpus Check
- Corpus is ~3,811 words - fits in a single context window. You may not need a graph.

## Summary
- 86 nodes · 131 edges · 8 communities (6 shown, 2 thin omitted)
- Extraction: 99% EXTRACTED · 1% INFERRED · 0% AMBIGUOUS · INFERRED: 1 edges (avg confidence: 0.8)
- Token cost: 0 input · 0 output

## Community Hubs (Navigation)
- Port Interfaces
- Core Model Types
- Port Tests
- Test Fakes
- Tool Fakes
- Test Helpers
- Leak Guard Tests
- Checkpoint Fakes

## God Nodes (most connected - your core abstractions)
1. `Event` - 7 edges
2. `TestNoBannedDirectDeps()` - 6 edges
3. `Message` - 5 edges
4. `Request` - 5 edges
5. `FakeTool` - 5 edges
6. `Fake` - 5 edges
7. `findModuleRoot()` - 4 edges
8. `FakeCheckpointer` - 4 edges
9. `TestNoCoreImportsProviderPackage()` - 3 edges
10. `TestCmdUsesOnlySelectAdapter()` - 3 edges

## Surprising Connections (you probably didn't know these)
- `TestAllTenPortsAreDeclared()` --calls--> `PortNames()`  [INFERRED]
  ports_test.go → ports.go
- `Fake` --references--> `Event`  [EXTRACTED]
  providerfake/fake.go → model.go

## Import Cycles
- None detected.

## Communities (8 total, 2 thin omitted)

### Community 0 - "Port Interfaces"
Cohesion: 0.11
Nodes (13): Budgeter, Checkpointer, Embedder, FakeBudgeter, FakeSkillIndex, FakeTokenizer, MemoryStore, PolicyGate (+5 more)

### Community 1 - "Core Model Types"
Cohesion: 0.20
Nodes (14): Int32, Block, BlockKind, Event, EventKind, FakeProvider, Message, Provider (+6 more)

### Community 2 - "Port Tests"
Cohesion: 0.22
Nodes (16): T, TestAllTenPortsAreDeclared(), TestFakeBudgeterPassesThrough(), TestFakeCheckpointerRoundTrips(), TestFakeEmbedderReturnsPinnedDim(), TestFakeMemoryStoreRoundTrips(), TestFakeMemoryStoreSearchMiss(), TestFakePolicyGateAllows() (+8 more)

### Community 3 - "Test Fakes"
Cohesion: 0.17
Nodes (6): FakeEmbedder, FakeMemoryStore, FakePolicyGate, FakeWorkflow, Context, ToolCall

### Community 5 - "Test Helpers"
Cohesion: 0.33
Nodes (6): depTokenInLine(), findRepoRoot(), isInsideRequireBlock(), lineAtOffset(), readFile(), TestNoBannedDirectDeps()

### Community 6 - "Leak Guard Tests"
Cohesion: 0.80
Nodes (4): findModuleRoot(), T, TestCmdUsesOnlySelectAdapter(), TestNoCoreImportsProviderPackage()

## Knowledge Gaps
- **10 isolated node(s):** `Provider`, `Embedder`, `MemoryStore`, `Tool`, `SkillIndex` (+5 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **2 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `PortNames()` connect `Port Interfaces` to `Port Tests`?**
  _High betweenness centrality (0.367) - this node is a cross-community bridge._
- **Why does `TestAllTenPortsAreDeclared()` connect `Port Tests` to `Port Interfaces`?**
  _High betweenness centrality (0.357) - this node is a cross-community bridge._
- **Why does `FakeProvider` connect `Core Model Types` to `Port Interfaces`?**
  _High betweenness centrality (0.154) - this node is a cross-community bridge._
- **What connects `Provider`, `Embedder`, `MemoryStore` to the rest of the system?**
  _10 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Port Interfaces` be split into smaller, more focused modules?**
  _Cohesion score 0.1111111111111111 - nodes in this community are weakly interconnected._
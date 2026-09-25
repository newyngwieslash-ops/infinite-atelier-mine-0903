# Architecture Decision Records

编码 Agent 在需要作出长期、跨模块或难以回滚的技术决策时，在本目录新增 ADR。

命名：

```text
0001-short-title.md
```

模板：

```markdown
# ADR-0001 Title

- Status: Proposed / Accepted / Superseded / Rejected
- Date: YYYY-MM-DD
- Deciders:
- Related work package:

## Context

## Decision drivers

## Options considered

## Decision

## Consequences

## Verification

## References
```

已发布 Accepted ADR 不直接改写结论；新建 ADR supersede。

## 目录

| 编号 | 标题 | 状态 |
|---|---|---|
| 0001 | Desktop framework (Wails v2) | Accepted |
| 0002 | SQLite driver and migrations | Accepted |
| 0003 | Windows Credential Manager as the secret backend | Accepted |
| 0004 | Job lifecycle names, recovery semantics, outbound download policy | Accepted |
| 0005 | Entity identifiers (UUIDv7) and the physical file table name | Accepted |
| 0006 | Legacy import pipeline, idempotency, ordinary backup v1 | Accepted |
| 0007 | Drama schema and vocabulary rulings | Accepted |
| 0008 | Staleness propagation, projection commands, and version approval | Accepted |
| 0009 | Domain event stream and approval semantics | Accepted |
| 0010 | Document import, the event extraction contract, and the story graph | Accepted |
| 0011 | The agent runtime, its tool table, and the stage-key rulings | Accepted |
| 0012 | The script pipeline: one payload, field locks, derived duration, and the stated stage map | Accepted |
| 0013 | The production pipeline: stage vocabulary, a shared mechanism, the gap report, the batch, and the MONOFORM envelope | Accepted |
| 0014 | Persistent memory: the store, the summary chain, the vector index, the embedding port, and the deterministic checks | Accepted |
| 0015 | Media: the one audited subprocess, the export recipe, and the mock video adapter | Accepted |
| 0016 | Encrypted sensitive backups are not in v1 | Accepted |
| 0017 | Asset-production UI: the panel-image chain, its approve control, and the AC-E2E-002 walk | Accepted |
| 0018 | Per-provider concurrency limits, and the rate limit this record does not build | Accepted |
| 0019 | Explicit stage dependencies: a declared graph, checked before the attempt exists | Accepted |
| 0020 | The complete asset ruleset, and the two categories that had no emitter | Accepted |
| 0021 | The event graph drawing: what is a node, what is an edge, and what is not drawn | Accepted |
| 0022 | The summary ladder's third rung, the defect it exposed, and the recall metrics | Accepted |
| 0023 | PDF import: the library that cannot read Chinese, and the one that panics | Accepted |
| 0024 | The audio mix, the silent film it closed, and why the measurement moved | Accepted |
| 0025 | MONOFORM deep integration: the snapshot that was being dropped | Accepted |
| 0026 | Local ONNX embeddings, and the model that cannot read the corpus | Accepted |
| 0027 | The asynchronous video adapter, the protocol shape it assumes, and two defects a boundary test found | Accepted |
| 0028 | Multi-character voice casting, effect suggestions, and two rules no code could reach | Accepted |
| 0029 | The first/last-frame picker, the empty data URL behind it, and the shot batch | Accepted |
| 0030 | Background music import, the bed that started eight seconds late, and the link that could never succeed | Accepted |
| 0031 | The speech adapter, the chain that had a hole in the middle, and style references | Accepted |

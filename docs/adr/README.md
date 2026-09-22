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

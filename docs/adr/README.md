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

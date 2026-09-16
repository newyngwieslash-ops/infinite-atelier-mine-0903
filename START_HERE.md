# START HERE

这是用于把 `infinite-atelier-mine-0903` 演进为“通用 AI 视觉工作台 + AI 短剧生产平台”的完整开发规格包。

---

# 1. 文件说明

| 文件 | 用途 |
|---|---|
| `PRD.md` | 完整产品范围、功能、MVP、验收和发布阻断条件 |
| `AGENTS.md` | 所有编码 Agent 必须遵守的持久工程规则 |
| `CODEX_MASTER_PROMPT.md` | 可直接粘贴给 Codex 的首次完整提示词 |
| `docs/ARCHITECTURE.md` | Go/Wails/React/SQLite 目标架构与迁移边界 |
| `docs/DOMAIN_MODEL.md` | 小说、剧本、资产、分镜、工作流、任务、记忆等正式数据模型 |
| `docs/AGENT_CONTRACTS.md` | Decision/Execution/Supervision、Skill、Tool、Memory 和 Schema 契约 |
| `docs/SECURITY.md` | 密钥、SSRF、动态脚本、Prompt Injection、导入与备份安全 |
| `docs/ACCEPTANCE.md` | 自动化测试和端到端验收场景 |
| `docs/ROADMAP.md` | WP-00～WP-12 分阶段实施路线 |
| `docs/implementation/STATUS.md` | 当前只允许执行的工作包及真实完成状态 |
| `docs/reference/*` | 本规格包的分析参考，优先级低于批准规格 |

---

# 2. 放置方式

将本目录中的全部文件和目录复制到目标仓库根目录，保持路径不变：

```text
<repo>/
├─ PRD.md
├─ AGENTS.md
├─ CODEX_MASTER_PROMPT.md
├─ START_HERE.md
└─ docs/
```

不要只复制 PRD 和提示词而遗漏 Domain、Security 或 Acceptance；Codex 的首轮提示词会引用这些文件。

---

# 3. 第一次提交给 Codex

1. 打开 `CODEX_MASTER_PROMPT.md`；
2. 复制“可直接提交给 Codex 的提示词”整个代码块；
3. 在目标仓库的 Codex 会话中粘贴；
4. 让 Codex只执行 WP-00；
5. 检查其 `STATUS.md`、基线命令和审计证据；
6. 未通过验收时使用同文件中的“修复当前工作包”模板；
7. 通过后再明确批准 WP-01。

首轮不要要求 Codex“完整实现 PRD”。WP-00 的目的就是先确认仓库事实、用户修改、构建状态和安全风险。

---

# 4. 后续开发节奏

推荐严格按以下顺序：

```text
WP-00 基线审计
→ WP-01 Wails/Go/SQLite/FileStore
→ WP-02 Secret/安全 Provider
→ WP-03 Persistent Job/多模态 Provider
→ WP-04 旧项目迁移/画布后端
→ WP-05 短剧领域模型/UI Shell
→ WP-06 文档/章节/事件图谱
→ WP-07 Agent Runtime/Workflow/Quality Gate
→ WP-08 ScriptAgent
→ WP-09 ProductionAgent/资产/分镜
→ WP-10 Memory/一致性/质量中心
→ WP-11 视频/音频/时间线/导出
→ WP-12 硬化/打包/RC
```

每个工作包：

```text
批准
→ 实施
→ 测试
→ 验收
→ 更新 STATUS
→ 停止
→ 人工批准下一包
```

---

# 5. 必须守住的边界

- 不直接复制 Toonflow 源码、Skill、品牌或界面；
- 不机械合并两套后端；
- 不允许浏览器保存或直传 API Key；
- 不允许任意 JavaScript 模型脚本；
- 不让 LLM 成为唯一工作流状态机；
- 不让 Supervisor 相信 Execution 自述；
- 不把剧本、镜头和资产只塞进画布 metadata；
- 不一次性重写现有画布；
- 不删除用户本地数据或 Git 修改；
- 不用占位实现、静态假数据或 TODO 冒充完成。

---

# 6. 规格的核心产品决策

```text
数据库 = 事实来源
画布 = 可视化投影

Decision = 协调
Execution = 执行
Supervision = 独立检查
Memory = 连续性
Workflow DB = 真实状态
User Gate = 最终批准
```

产品保留两个模式：

```text
自由画布
+
短剧工作室
```

首发采用本地桌面架构：

```text
React/Vite
→ Wails v2
→ Go Core
→ SQLite / Local Files / OS Keychain
```

---

# 7. 下载包校验

解压后至少应看到以下 12 个文件：

```text
PRD.md
AGENTS.md
CODEX_MASTER_PROMPT.md
START_HERE.md
docs/ARCHITECTURE.md
docs/DOMAIN_MODEL.md
docs/AGENT_CONTRACTS.md
docs/SECURITY.md
docs/ACCEPTANCE.md
docs/ROADMAP.md
docs/implementation/STATUS.md
docs/adr/README.md
```

还应包含两份 `docs/reference/` 参考文档。


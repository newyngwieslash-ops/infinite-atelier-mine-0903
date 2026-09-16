# Infinite Atelier Drama Studio 技术架构

> 版本：v1.0  
> 状态：已批准  
> 适用范围：Foundation、MVP、Desktop v1  
> 关联：`PRD.md`、`docs/DOMAIN_MODEL.md`、`docs/AGENT_CONTRACTS.md`、`docs/SECURITY.md`

---

# 1. 架构目标

本架构用于将现有 React/Vite 浏览器工作台逐步升级为本地优先桌面应用，同时保留自由画布并新增短剧领域、三层 Agent、可恢复工作流、持久记忆和安全 Provider 网关。

目标：

1. 保留现有 Infinite Atelier 前端价值；
2. 把密钥、网络、任务、业务事实和媒体处理迁入 Go Core；
3. 保证数据可恢复、任务可续跑、结果可追溯；
4. 通过明确模块边界避免把新能力继续堆入大型 React 页面；
5. 为未来 Server/SaaS 形态保留 Ports，但不在 MVP 引入微服务；
6. 允许按工作包渐进迁移，不要求一次性重写。

非目标：

- 在 MVP 引入分布式消息队列、Kubernetes 或微服务；
- 用通用 Agent 框架替代本项目可控运行时；
- 让前端直接访问 SQLite、文件系统或 Provider；
- 让 LLM 成为工作流状态机或安全决策者；
- 复制 Toonflow 的后端结构或源码。

---

# 2. 已批准技术决策

## ADR-BASE-001：桌面框架

MVP 使用 **Wails v2 稳定线**，原因：

- 产品要求稳定桌面基线；
- Wails v3 在本规格制定时仍为 beta；
- v2 足以完成单主窗口、Go Binding、文件对话框和桌面打包；
- 未来 v3 GA 后通过正式 ADR 评估迁移。

禁止在无 ADR 的情况下切换 Electron、Tauri、Wails v3 beta 或自建 WebView。

## ADR-BASE-002：后端语言

核心服务统一使用 Go：

- 领域模型；
- SQLite；
- 文件与备份；
- Provider；
- Job；
- Agent Runtime；
- Workflow；
- Memory；
- Secret；
- 媒体处理。

前端继续使用 React/Vite/TypeScript。

## ADR-BASE-003：持久化

- 元数据：SQLite；
- SQL 访问：`database/sql` + 显式 Repository；
- 迁移：版本化 SQL 文件，由成熟迁移工具执行；
- 不使用自动建表作为正式迁移；
- 不在领域层依赖 SQLite 特性；
- 媒体：应用管理的本地文件根目录；
- 文件寻址：SHA-256 或稳定内容 ID；
- 向量：Float32 BLOB/原生 Vector，通过 `VectorIndex` 抽象。

具体 SQLite Driver、迁移工具和 sqlite-vec 打包方式在 WP-01 形成 ADR。

## ADR-BASE-004：前端状态

- Zustand 只保留临时 UI 状态、画布选择、视口和本地交互状态；
- 项目、剧本、资产、任务、工作流等事实由 Go Core 持有；
- 前端通过 Wails Binding 查询和命令调用；
- 服务端事实缓存优先使用 TanStack Query 或薄 Repository Client；
- 不允许把新领域数据继续持久化到 localStorage/localForage；
- 旧 localForage/IndexedDB 仅用于一次性兼容导入与迁移期只读回退。

## ADR-BASE-005：Agent 架构

不引入 LangChain、LangGraph、CrewAI 或 AutoGen 作为核心运行依赖。采用：

```text
Agent Runtime
+ Markdown Skill
+ Typed Tool Registry
+ Structured Output
+ Durable Workflow
+ Persistent Memory
```

Decision、Execution、Supervision 是独立调用。

## ADR-BASE-006：Provider 插件

- 内置 Adapter 使用受信任 Go 代码；
- 可扩展 Adapter 使用受限声明式 Manifest；
- 禁止任意 JavaScript、动态 Go 插件、Shell、`eval`、`new Function`；
- Secret 只在 Go Provider 调用边界解析。

## ADR-BASE-007：本地优先

- 应用默认不运行公网服务；
- 如需本地监听，只绑定 `127.0.0.1`/`::1` 并带鉴权；
- 默认无遥测；
- 用户作品和记忆仅在调用所选 Provider 时发送必要内容。

---

# 3. 总体架构

```text
┌──────────────────────────────────────────────────────────────┐
│ Wails Desktop Application                                    │
│                                                              │
│  React / Vite / TypeScript                                   │
│  ┌────────────────────────────────────────────────────────┐  │
│  │ Home / Free Canvas / Drama Studio / Asset Library      │  │
│  │ Agent Center / Quality Center / Job Center / Settings  │  │
│  │ MONOFORM Adapter                                       │  │
│  └──────────────────────────┬─────────────────────────────┘  │
│                             │ generated bindings + events    │
│  ┌──────────────────────────▼─────────────────────────────┐  │
│  │ Go Application Layer                                  │  │
│  │ Commands / Queries / Transactions / Authorization     │  │
│  └─────────────┬─────────────┬─────────────┬──────────────┘  │
│                │             │             │                 │
│  ┌─────────────▼─────┐ ┌─────▼────────┐ ┌──▼──────────────┐ │
│  │ Domain Services   │ │ Agent System │ │ Runtime Services │ │
│  │ Story/Script/...  │ │ Workflow     │ │ Provider/Job/... │ │
│  └─────────────┬─────┘ │ Memory/Skill │ └──┬──────────────┘ │
│                │       └─────┬────────┘    │                │
│  ┌─────────────▼─────────────▼─────────────▼──────────────┐ │
│  │ Ports                                                  │ │
│  │ Repositories / FileStore / SecretStore / Providers     │ │
│  │ VectorIndex / Clock / EventBus / MediaEngine            │ │
│  └─────────────┬─────────────┬─────────────┬──────────────┘ │
│                │             │             │                │
│  ┌─────────────▼────┐ ┌──────▼───────┐ ┌──▼──────────────┐│
│  │ SQLite           │ │ Local Files  │ │ OS Keychain      ││
│  │ migrations       │ │ thumbnails   │ │ Credentials      ││
│  └──────────────────┘ └──────────────┘ └──────────────────┘│
└──────────────────────────────────────────────────────────────┘
                         │ HTTPS / explicit local providers
                         ▼
              OpenAI-compatible / Gemini / Video / TTS
```

---

# 4. 分层与依赖规则

## 4.1 Domain

包含纯业务类型、值对象、状态机、不变量和领域事件。

允许依赖：

- Go 标准库；
- 极少数无基础设施语义的通用库。

禁止依赖：

- Wails；
- SQLite Driver；
- HTTP Client；
- Provider SDK；
- 操作系统 API；
- React 或前端类型。

## 4.2 Application

负责 Use Case：

- 命令；
- 查询；
- 事务；
- 幂等；
- 权限和状态检查；
- 调用 Domain 与 Ports；
- 发布领域事件。

Application 不实现外部协议细节。

## 4.3 Ports

定义可替换接口：

- Repository；
- UnitOfWork；
- FileStore；
- SecretStore；
- Provider；
- VectorIndex；
- EventPublisher；
- Clock；
- IDGenerator；
- MediaEngine；
- ArchiveService。

## 4.4 Infrastructure

实现 Ports：

- SQLite Repository；
- 本地文件系统；
- OS Keychain；
- HTTP Provider Adapter；
- Wails Event Publisher；
- FFmpeg Adapter；
- ZIP/加密备份；
- 本地向量索引。

## 4.5 Desktop Adapter

- Wails Binding；
- 文件选择；
- 窗口生命周期；
- 原生菜单；
- 应用目录；
- 事件桥。

Desktop Adapter 不包含领域规则。

## 4.6 Web Frontend

- 视图；
- 画布交互；
- 表单；
- 本地临时状态；
- Binding Client；
- Event Subscription；
- i18n；
- 可访问性。

前端不得：

- 读取 Secret；
- 直接执行 SQL；
- 直接访问任意文件路径；
- 直连外部 Provider；
- 自行决定工作流状态转换。

---

# 5. 建议仓库结构

Codex 在 WP-00 审计后可对路径作小幅调整，但必须保持依赖方向和职责。

```text
/
├─ PRD.md
├─ AGENTS.md
├─ CODEX_MASTER_PROMPT.md
├─ go.mod
├─ main.go
├─ app.go
├─ wails.json
│
├─ cmd/
│  ├─ desktop/
│  └─ worker/                    # P2，可无窗口运行
│
├─ internal/
│  ├─ domain/
│  │  ├─ project/
│  │  ├─ story/
│  │  ├─ script/
│  │  ├─ asset/
│  │  ├─ storyboard/
│  │  ├─ workflow/
│  │  ├─ job/
│  │  ├─ memory/
│  │  └─ provider/
│  │
│  ├─ application/
│  │  ├─ project/
│  │  ├─ story/
│  │  ├─ script/
│  │  ├─ asset/
│  │  ├─ storyboard/
│  │  ├─ workflow/
│  │  ├─ agent/
│  │  ├─ job/
│  │  ├─ provider/
│  │  └─ backup/
│  │
│  ├─ agent/
│  │  ├─ runtime/
│  │  ├─ decision/
│  │  ├─ execution/
│  │  ├─ supervision/
│  │  ├─ skills/
│  │  ├─ tools/
│  │  ├─ memory/
│  │  └─ eval/
│  │
│  ├─ ports/
│  │  ├─ repositories.go
│  │  ├─ providers.go
│  │  ├─ filestore.go
│  │  ├─ secretstore.go
│  │  ├─ vectorindex.go
│  │  ├─ media.go
│  │  └─ events.go
│  │
│  ├─ infrastructure/
│  │  ├─ sqlite/
│  │  ├─ filestore/
│  │  ├─ keyring/
│  │  ├─ providers/
│  │  │  ├─ openaicompat/
│  │  │  ├─ geminicompat/
│  │  │  ├─ video/
│  │  │  └─ mock/
│  │  ├─ vector/
│  │  ├─ media/
│  │  ├─ archive/
│  │  └─ observability/
│  │
│  ├─ desktop/
│  │  ├─ bindings/
│  │  ├─ events/
│  │  └─ lifecycle/
│  │
│  └─ platform/
│     ├─ paths/
│     ├─ clock/
│     └─ id/
│
├─ migrations/
├─ schemas/
│  ├─ agent/
│  ├─ provider/
│  ├─ backup/
│  └─ events/
│
├─ skills/
│  ├─ script/
│  └─ production/
│
├─ web/                          # 现有 React/Vite 前端
│  ├─ src/
│  │  ├─ app/
│  │  ├─ features/
│  │  │  ├─ canvas/
│  │  │  ├─ drama/
│  │  │  ├─ story/
│  │  │  ├─ script/
│  │  │  ├─ assets/
│  │  │  ├─ storyboard/
│  │  │  ├─ agents/
│  │  │  ├─ quality/
│  │  │  ├─ jobs/
│  │  │  └─ settings/
│  │  ├─ entities/
│  │  ├─ shared/
│  │  └─ generated/              # Wails 生成，按工具约定管理
│  └─ monoform-studio/
│
├─ testdata/
│  ├─ canary-drama/
│  ├─ old-projects/
│  ├─ provider-fixtures/
│  └─ malicious-imports/
│
└─ docs/
   ├─ ARCHITECTURE.md
   ├─ DOMAIN_MODEL.md
   ├─ AGENT_CONTRACTS.md
   ├─ SECURITY.md
   ├─ ACCEPTANCE.md
   ├─ ROADMAP.md
   ├─ adr/
   ├─ implementation/
   └─ reference/
```

---

# 6. 核心运行时

## 6.1 启动顺序

```text
Resolve app directories
→ Init structured logger and redaction
→ Acquire single-instance/write lock
→ Open SQLite
→ Integrity check
→ Apply migrations with pre-migration backup
→ Init FileStore
→ Init SecretStore
→ Register Provider adapters
→ Init EventBus
→ Init Job Manager
→ Recover jobs
→ Init Agent/Workflow/Memory
→ Start Wails UI
```

失败原则：

- 数据库不可安全打开：进入只读恢复模式；
- SecretStore 不可用：允许打开项目，但禁用外部 Provider；
- 某 Provider 失败：不影响其他 Provider；
- 任务恢复失败：标记并展示，不静默删除；
- 媒体引擎不可用：禁用相关能力并显示诊断。

## 6.2 关闭顺序

```text
Stop accepting new jobs
→ Request cancellable operations to stop
→ Persist scheduler state
→ Flush events/logs
→ Checkpoint database
→ Release locks
```

不能等待不可控远程任务无限结束。

---

# 7. 领域命令与查询

前端对 Go Core 使用“命令改变状态、查询读取状态”的模式。

命令示例：

```text
CreateProject
ImportSourceDocument
ConfirmChapterBoundaries
AcceptStoryFact
LockProjectRule
StartWorkflow
ApproveStage
RequestStageFix
RedoStage
CancelWorkflow
CreateAssetVersion
ApproveAssetVersion
CreateCanvasProjection
SubmitGenerationJob
CancelJob
ExportBackup
RestoreBackup
```

查询示例：

```text
GetProject
ListProjects
GetStoryGraph
GetEpisode
GetScriptVersion
ListAssetUsages
GetWorkflowRun
ListStageRuns
GetReviewReport
ListJobs
GetAgentRunTrace
BuildMemoryContextPreview
```

每个命令要求：

- 输入 DTO Schema；
- Context；
- 状态和权限校验；
- 幂等策略；
- 事务边界；
- 领域事件；
- 稳定错误码；
- 测试。

---

# 8. 数据持久化

## 8.1 SQLite

配置：

- WAL 模式；
- foreign_keys=ON；
- busy_timeout；
- 明确同步策略；
- 单写多读约束；
- 事务短小；
- 不在事务中调用外部 Provider；
- migrations 只前进，破坏性变更使用新表迁移和验证；
- schema version 与应用版本独立。

## 8.2 Unit of Work

涉及多个聚合写入时：

```text
Application Command
→ Begin Tx
→ Load aggregates
→ Validate invariants
→ Write domain state
→ Write outbox/events
→ Commit
→ Publish events
```

若本地事件发布失败，可从 Outbox 重放。MVP 可采用 SQLite Outbox，禁止在事务提交前向前端宣布成功。

## 8.3 文件存储

建议结构：

```text
<AppData>/InfiniteAtelier/
├─ app.db
├─ files/
│  ├─ sha256/ab/cd/<hash>
│  └─ temp/
├─ thumbnails/
├─ backups/
├─ logs/
├─ models/
└─ recovery/
```

规则：

- 用户原始文件先复制到临时位置；
- 计算哈希、Magic、大小；
- 验证后原子移入；
- 数据库保存相对路径；
- 物理文件可被多个资产引用；
- 删除必须依据引用计数和回收策略；
- 临时文件有 TTL 和启动清理。

## 8.4 旧数据迁移

采用双阶段导入：

```text
Scan legacy IndexedDB/localForage
→ Export legacy snapshot
→ Transform to import manifest
→ Validate counts and hashes
→ Import into Go Core transactionally
→ Compare node/edge/media counts
→ Mark migrated
→ Keep legacy data read-only until user confirms
```

迁移必须幂等。失败时可重新执行，不覆盖已成功迁移数据。

---

# 9. Canvas 架构

## 9.1 Source of Truth

Canvas Document 保存布局和投影引用；领域实体保存在领域表。

```text
Domain entity changes
→ Domain Event
→ Projection Synchronizer
→ Canvas Node display update
```

Canvas 操作：

- 仅移动、缩放、折叠：只改 Canvas；
- 编辑实体字段：发送领域命令；
- 删除节点：默认只移除投影；
- 删除实体：走领域删除和影响检查；
- 创建语义连线：校验关系注册表后写领域关系/投影关系。

## 9.2 前端拆分

禁止继续把所有功能集中在单个 Project 页面。至少拆分：

```text
CanvasViewportController
CanvasSelectionController
CanvasDragController
CanvasConnectionController
CanvasHistoryController
CanvasProjectionStore
CanvasGenerationController
CanvasPersistenceAdapter
```

## 9.3 性能

- Node Map/normalized store；
- 视口裁剪；
- Edge 裁剪或分区；
- 空间索引；
- 局部 selector；
- 批量事件合并；
- 大媒体只用缩略图；
- 操作日志/命令历史替代全项目深拷贝。

---

# 10. Workflow Engine

## 10.1 状态来源

状态来自数据库，不来自 LLM 文本。

状态转移由代码表驱动，例如：

```text
ready -> running
running -> under_review | waiting_user | failed | cancelled
under_review -> waiting_user | passed | failed
waiting_user -> running | passed | cancelled
passed -> completed | next-stage-ready | superseded
failed -> ready | cancelled
```

非法转移返回稳定错误码。

## 10.2 Stage Execution

```text
StartStage command
→ Create StageRun(attempt N)
→ Commit running state
→ Run Execution Agent/Service outside DB transaction
→ Validate structured result
→ Persist artifacts in transaction
→ Mark StageRun executed
→ If Quality Gate: create Supervision run
→ Persist ReviewReport
→ Enter waiting_user/passed/failed
```

## 10.3 Retry

- 同一 Stage 的每次尝试独立保存；
- 默认自动修订最多 2 次；
- FIX 输入包含明确 issue IDs 与锁定约束；
- REDO 创建新版本；
- 超过最大次数转人工；
- 不能覆盖历史输出。

## 10.4 Stale Propagation

上游批准版本变化时：

```text
Find dependent artifacts
→ mark stale with reason and source version
→ prevent use in new final output by default
→ show impact graph
→ user selects regenerate / retain with waiver
```

Stale 不等于删除。

---

# 11. Agent Runtime

详见 `docs/AGENT_CONTRACTS.md`。

组件：

```text
AgentRuntime
├─ SkillLoader
├─ PromptAssembler
├─ ToolRegistry
├─ StructuredOutputValidator
├─ DecisionRunner
├─ SubAgentRunner
├─ SupervisorRunner
├─ MemoryContextBuilder
├─ RunRecorder
└─ EvaluationHooks
```

原则：

- 每个 Agent 调用都可取消；
- 每个 Tool 有输入/输出 Schema、超时和权限；
- Agent 不执行 SQL；
- Agent 不读 Secret；
- Agent 不持久化自由文本作为业务成功；
- raw output 只用于诊断；
- 业务结果需 Schema 验证和 Tool 写入；
- Supervisor 重新读 Workspace。

---

# 12. Memory 架构

```text
Current Query
├─ Recent messages
├─ Recent summaries
├─ Semantic message candidates
├─ Semantic summary candidates
└─ Structured project facts
        ↓
Score fusion + threshold + token budget
        ↓
Agent Context
```

## 12.1 分层

- Episodic：发生过什么；
- Semantic：用户批准的偏好/经验事实；
- Procedural：用户工作方式；
- Artifact：资产和运行引用；
- Event Graph 不属于 Memory。

## 12.2 召回顺序

建议先召回再写当前用户消息，避免 self-hit：

```text
Build scope
→ Recall previous memory excluding current turn
→ Persist user message
→ Run Decision
→ Persist Decision/Execution/Supervision summaries
```

## 12.3 VectorIndex

```go
type VectorIndex interface {
    Upsert(ctx context.Context, items []VectorItem) error
    Search(ctx context.Context, scope Scope, vector []float32, opts SearchOptions) ([]VectorHit, error)
    Delete(ctx context.Context, ids []string) error
    Rebuild(ctx context.Context, scope Scope) error
}
```

MVP：

- Float32 BLOB；
- 小规模归一化点积；
- Scope 过滤；
- Threshold + TopK；
- 可测试排序。

V1：

- sqlite-vec Adapter；
- 层级摘要；
- 模型版本迁移。

---

# 13. Provider Gateway

## 13.1 能力接口

```go
type TextProvider interface {
    Generate(context.Context, TextRequest) (TextResult, error)
    Stream(context.Context, TextRequest, TextEventSink) error
}

type ImageProvider interface {
    Generate(context.Context, ImageRequest) (ImageResult, error)
    Edit(context.Context, ImageEditRequest) (ImageResult, error)
}

type VideoProvider interface {
    Submit(context.Context, VideoRequest) (RemoteJob, error)
    Poll(context.Context, RemoteJob) (RemoteJobStatus, error)
    Fetch(context.Context, RemoteJob) (MediaResult, error)
    Cancel(context.Context, RemoteJob) error
}

type AudioProvider interface {
    Generate(context.Context, AudioRequest) (AudioResult, error)
}

type EmbeddingProvider interface {
    Embed(context.Context, []string) ([][]float32, error)
}
```

## 13.2 Error Taxonomy

```text
configuration
unauthorized
forbidden
rate_limited
invalid_input
content_policy
network
timeout
remote_transient
remote_permanent
cancelled
unsupported
response_invalid
storage
```

每个错误包含：

- stable code；
- retriable；
- safe message；
- retry-after；
- provider request ID；
- redacted diagnostic details。

## 13.3 SSRF 防护

详见 `docs/SECURITY.md`。每次请求和每次重定向均：

1. 只允许 HTTPS，显式批准的本地 Provider 可允许 HTTP；
2. 规范化主机；
3. DNS 解析；
4. 拒绝回环、私网、链路本地、ULA、元数据 IP；
5. 校验允许域名和端口；
6. 禁止用户控制代理；
7. 限制重定向次数；
8. 重定向目标重新完整校验。

---

# 14. Persistent Job Manager

组件：

```text
JobService
├─ JobRepository
├─ Scheduler
├─ WorkerPool
├─ Lease/Revision Guard
├─ RetryPolicy
├─ Provider Poller
├─ Download Manager
├─ Result Committer
└─ Recovery Scanner
```

## 14.1 执行原则

- Job 状态先持久化再执行；
- Worker 通过短租约或 revision 防止重复执行；
- 外部请求使用幂等键；
- 结果下载到临时文件；
- 校验 MIME/大小/哈希；
- 原子提交文件和资产版本；
- 成功事件在事务提交后发布。

## 14.2 恢复

启动扫描：

- `running` 且无租约：根据类型重新排队；
- `waiting_remote`：恢复 Poll；
- `downloading`：检查临时文件或重新下载；
- 不可恢复：`orphaned`/`failed`，保留诊断；
- 不自动重复提交未知是否成功的高成本远程任务，先尝试按 remote ID 查询。

---

# 15. Secret Store

```go
type SecretStore interface {
    Put(context.Context, SecretMetadata, []byte) (SecretRef, error)
    Resolve(context.Context, SecretRef) ([]byte, error)
    Delete(context.Context, SecretRef) error
    Status(context.Context, SecretRef) (SecretStatus, error)
}
```

规则：

- `Resolve` 只允许 Provider Infrastructure 使用；
- Wails Binding 不暴露 `Resolve`；
- 数据库只保存 SecretRef；
- 前端只获取掩码和状态；
- 普通备份排除 Secret；
- 测试使用内存实现；
- Linux 无可用 Keyring 时 fail closed 或要求用户明确启用加密本地 Vault。

---

# 16. Backup/Restore

普通备份：

```text
manifest.json
app.sqlite snapshot
files/
thumbnails optional
skills/project overrides
provider metadata without secrets
checksums.txt
```

恢复：

```text
Open archive with limits
→ Validate paths, counts, sizes, ratio
→ Validate manifest/schema
→ Verify checksums
→ Extract to temp
→ Open copied DB read-only and integrity check
→ Run migration in temp
→ Validate referential/file integrity
→ Atomic import/replace after user confirmation
```

敏感备份是独立功能，不与普通备份复用默认按钮。

---

# 17. MONOFORM 集成

MONOFORM 作为受控子应用：

- 明确版本；
- 通过 Typed Message Bridge；
- 只接收 Shot、资产引用和已批准参数；
- 返回 Camera Pose、Lens、Framing、Movement、Snapshot、Notes；
- Message Schema 校验；
- Origin 校验；
- 最小 iframe 权限；
- 不能读取 Secret、数据库或任意文件；
- 未来可迁移至独立 Wails Window，但需 ADR。

---

# 18. 可观测性

## 18.1 Trace

关联链：

```text
User Command
→ WorkflowRun
→ StageRun
→ AgentRun
→ ToolCall
→ ProviderRequest/Job
→ AssetVersion
→ ReviewReport
```

每层保存 ID，UI 可跳转。

## 18.2 日志

- JSON 结构化；
- redaction middleware；
- 日志级别可配置；
- 默认不记录完整作品正文和模型原始响应；
- Agent 原始输出写受控诊断附件并有 TTL；
- 诊断包生成前预览。

---

# 19. 测试架构

```text
Domain unit tests
Application use-case tests with in-memory ports
SQLite repository integration tests
Provider contract tests with httptest
Job crash/recovery tests
Agent runtime tests with deterministic Mock LLM
Skill/schema tests
Frontend component tests
Playwright desktop/web E2E
Security regression corpus
Performance benchmarks
```

Mock LLM/Provider 必须支持：

- deterministic structured success；
- malformed JSON；
- tool call；
- rate limit；
- timeout；
- partial stream；
- duplicated remote result；
- supervisor failure；
- cancellation。

---

# 20. 渐进迁移策略

## 阶段 A：冻结基线

- 记录现有路由、节点、项目数据和关键行为；
- 建回归测试；
- 不改 UX。

## 阶段 B：Go Core 旁路

- Wails Shell；
- SQLite、FileStore、SecretStore；
- Provider Gateway 和 Job；
- 前端通过 Adapter 可在旧/新服务间切换。

## 阶段 C：事实迁移

- 项目、资产、生成历史迁入 Go；
- legacy 数据只读；
- 画布 UI 保持。

## 阶段 D：短剧领域

- 新实体、页面、Workflow、Agent、Memory；
- 画布投影。

## 阶段 E：移除危险旧路径

只有在迁移和回归通过后移除：

- 前端 API Key；
- 浏览器直连；
- Vite 任意代理；
- JavaScript 模型插件；
- 新业务 localForage 持久化。

---

# 21. 架构守卫

CI 或静态检查应逐步加入：

- Domain 不导入 Infrastructure/Wails；
- 前端不包含 API Key 类型字段；
- 禁止 `eval`/`new Function`；
- 禁止未经包装的 `os/exec`；
- Provider URL 只能通过安全 Client；
- 文件路径只能通过 FileStore；
- SQL 只在 Repository；
- Skill Manifest 和 Schema 全部可验证；
- Migration 不可重复编号；
- 生成文件和大模型权重不误提交。

---

# 22. 架构完成标准

架构不是只创建目录。至少满足：

1. 依赖方向有测试或静态检查；
2. 一个真实 Use Case 贯穿 React → Wails → Application → SQLite；
3. 一个真实 Provider 通过 Go Gateway 调用；
4. 一个 Job 可持久、取消、恢复；
5. 一个领域实体可投影到画布；
6. 一个 Decision → Execution → Supervision 流程使用结构化输出；
7. Secret 不进入前端或普通备份；
8. 文档、代码、测试一致。


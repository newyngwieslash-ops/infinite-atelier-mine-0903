# 项目开发进度、PRD 差距与完整剩余任务

> 核对日期：2026-09-08，Asia/Shanghai。  
> 恢复依据：`handoff-2026-09-08-153906+0800.md`、当前 STATUS、PRD、Roadmap、实施计划、执行证据及当前源码。  
> 本文是用户要求的进度分析与后续清单，不是工作包验收完成声明，也不授权启动后续工作包。  
> 配套提示词：`continue-wp01-codex-zcode-2026-09-08.md`，与本文位于同一目录。

## 1. 当前项目到底做到哪里

**现有自由画布产品已存在，Windows 桌面与 Go/SQLite/FileStore 底座已实现主要部分；短剧生产业务仍未开始实现。当前应继续 WP-01 Task 10，随后完成 Task 11 验收。**

| 层次 | 当前结论 | 不能据此宣称的能力 |
|---|---|---|
| 原有 Infinite Atelier | 自由画布、文本/图像/视频/音频/Group/Director 节点、素材/提示词/历史、配置和 MONOFORM 已有代码与历史使用证据 | 不代表已完成旧数据迁移、持久生成任务、安全 Provider 或全量回归 |
| WP-00 | COMPLETE：完成基线、审计、风险盘点、初始 ADR 与 verify 入口 | 审计完成不等于风险已修复 |
| WP-01 Task 1–9 | 最新交接记录为 APPROVED；当前源码与主要基础模块相符 | 不等于 WP-01 整包 COMPLETE |
| WP-01 Task 10 | NOT STARTED：verify、Windows CI、README 待完成 | 现有名为 Desktop Build 的 CI 实际只有 Ubuntu Web job |
| WP-01 Task 11 | PENDING：最终验收、traceability、ADR/STATUS 收尾 | 尚无 WP-01 最终关闭结论 |
| WP-02～WP-12 | 尚未开始 | Secret、Provider Gateway、Job、Drama、Agent、Workflow、Memory、成片链路均不能标完成 |
| 发布状态 | 尚不满足 PRD 发布门槛 | 桌面 exe 能启动不等于安全桌面 MVP 或 Release Candidate |

不使用“整体完成 70%”等百分比：工作包规模不同，9/11 仅代表 WP-01 的任务计数，无法换算全项目工时或功能完成率。

### 已实现的 WP-01 细节

| Task | 已有实现与记录 | 当前文件证据 |
|---|---|---|
| 1 | 修复 npm 锁文件及直接依赖中的平台包问题；交接记录已双审 | `web/package.json`、`web/package-lock.json` |
| 2 | Wails v2.15.0；Go 1.25.0；嵌入现有 Web 前端；单实例 | `main.go`、`app.go`、`go.mod`、`wails.json` |
| 3 | 应用目录、稳定错误/diagnostic、结构化日志和脱敏、关闭流程 | `internal/domain/apperror/`、`internal/infrastructure/appdirs/`、`logging/` |
| 4 | 唯一 SQLite driver：modernc.org/sqlite v1.58.0；WAL/FK/busy timeout；历史 CGO=0 与三平台测试包交叉编译证据 | `internal/infrastructure/database/driver.go` 及测试 |
| 5 | 迁移数字排序、校验和、事务、失败回滚、安全模式、迁移前快照、checkpoint | `database.go`、`migrate.go` 及对应测试 |
| 6 | SHA-256 内容寻址；流式临时写入、MIME/magic、原子提交、去重、路径防护、文件引用基础 | `internal/application/files/`、`filestore/`、`database/files.go` |
| 7 | 只读 HealthBinding.Get；core:event v1；安全启动失败返回 | `internal/application/health/`、`internal/desktop/`、生成 bindings |
| 8 | React Query health；浏览器模式兼容；按能力动态导入；中英文与可访问状态提示 | `web/src/services/desktop/health.ts`、hook、health status 组件 |
| 9 | 历史 Wails clean build、隔离原生启动/关闭/重启/单实例/日志和五条路由证据；最新交接记录双审通过 | `wp01-execution-2026-09-07.md` 后续条目及指定 handoff |

**数据库边界尤其重要：**当前正式 migration 仅有 `schema_migrations`、`file_objects`、`file_references`。尚无 Project、Episode、Script、Shot、Job、Workflow、AgentRun、Memory 等业务表。旧项目仍使用浏览器持久化；新底座数据库尚未接管它们。

### 本轮复核与历史证据分开记录

| 检查 | 本轮结果 |
|---|---|
| `git status --short --branch` / `git rev-parse HEAD` | 分支 `codex/wp-01-desktop-foundation`；HEAD `a243891455ec17687dd54b5ac90d3bd64478a1a1` |
| `git diff --cached --name-status` | 输出为空，索引无 staged changes |
| `git diff --check` | PASS；存在既有 LF/CRLF 与全局 ignore 读取权限提示 |
| `go test ./... -count=1` | 首次 sandbox 因 Go cache Access denied 整体 FAIL；获准重跑后 PASS，根包及所有含测试的子包通过 |
| `go vet ./...` | 首次 sandbox 同类 cache 权限 FAIL；获准重跑后 PASS |
| `npm.cmd run typecheck`（在 web） | PASS，tsc --noEmit |
| `.github/workflows/desktop-build.yml` | 源码核对：只有 Ubuntu Web 安装/typecheck/build，无 Windows Wails job |
| 前端 test/lint | 当前 package scripts 不存在；不能称测试已通过 |
| 当前 foundation migration | 源码核对：仅三个基础表，无短剧、Provider、Job、Memory 表 |
| `web/dist/.gitkeep` | 存在，0 字节 |
| `build/bin/InfiniteAtelier.exe` | 存在，26,524,672 字节；本轮未重新构建或启动 |

以下仅引用历史证据，本轮没有重跑：Wails production build、原生五路由 smoke、全量 verify、race、clean npm ci、跨平台测试包编译和格式检查。

- Task 9 历史二进制 SHA-256 为 `a52e7add9ea7959ee948466998400cffdc5ecdfe53b8d9c94c5c3e897519d1d5`；不能要求未来重建必然相同。
- 历史 `go test -race ./... -count=1` 为 **ENVIRONMENT FAIL**：宿主 32-bit MinGW GCC 不能编译 amd64 CGO。普通 Go 测试通过不能代替 race。
- 三平台 `go test -c` 是测试包交叉编译证据，不能替代 macOS/Linux 原生运行、Wails 打包或安装验收。
- Task 9 原生路由证据是 `/`、`/assets`、`/canvas`、`/director`、`/config`；不是全节点交互、旧项目导入、生成或 `/canvas/:id` 的完整 E2E。

## 2. 当前真实缺口与容易误读的文档

### 2.1 可由当前源码直接确认的缺口

1. `web/src/services/api/model-plugin.ts:120` 仍使用 `new Function`，参数含 apiKey 和网络助手。
2. `web/src/stores/use-config-store.ts` 仍有前端 apiKey 配置与持久化体系。
3. `web/src/services/backup-restore.ts` 仍把完整 config 放入普通备份；恢复流程不是目标 Go 临时空间验证后原子导入。
4. `web/vite.config.ts` 仍有接受任意 target 的开发代理；`web/package.json` 的 dev/start 默认使用 `0.0.0.0`。Wails watcher 已显式覆盖成 loopback，但不等于所有入口已安全。
5. 现有 Go 目录只有基础设施、health、file、error 等模块；尚无新业务运行链路。
6. verify.ps1 缺 vet/Wails gate；verify.sh 执行 Go 时仍停留在 web 目录，根 app 包可能被漏检；两者仍报告 WP-00 文案。
7. README 尚未说明完整桌面 prerequisites、Wails 开发/构建、数据目录和验证流程。

这些是 PRD 明确要求处理的遗留问题，不应在本次进度整理中顺手修改。WP-02 建安全边界，WP-03/04 迁移调用和数据，WP-12 验证危险旧路径已删除或在发布构建中完全不可达。

### 2.2 收尾时需要修正或明确的事实

- **TRACEABILITY 编号不可信。**现有文件多行使用旧映射，例如把 FR-020 写成 SQLite、FR-030 写成 FileStore，把 NFR-001/002 对调。正式修改由 Task 11 完成；本文第 3 节按当前 PRD 重建映射。
- **STATUS 的历史段落仍需统一标注。**其 WP-00 结果及旧命令表是历史；“无 Go”“锁文件未修”等不能当作当前结论。Task 11 应保留历史并补当前结果。
- **ADR-0002 仍 Proposed。**已有实现不等于 ADR 已接受；需要合并后续 migration/snapshot/Wails 证据，并诚实列出 race、平台与测量缺口。
- **计划示例过时。**Go 1.24 应按当前 go.mod 使用 Go 1.25.0；`wailsjsdir` 当前为 `web/src`，生成到 `web/src/wailsjs/`。不得照抄旧示例覆盖正确配置。
- **迁移工具选择有文档差异。**Architecture 提及成熟迁移工具，当前已批准实施计划使用有界自有 runner；Task 11 应通过 ADR 明确理由与验收证据，不能为对齐一句话重写现有实现。
- **审查协议来源要准确。**implementer → 独立 spec → 独立 quality 是本项目历史授权与交接协议；Ponytail 本身只规定最小实现等原则，不自带三角色审查工具。
- **Comet 状态是历史工具限制。**handoff 记录两个 active change、旧 state schema 解析失败；本轮未重跑 Comet。不得删除 state 或假修复，也不需为进度总结重建工作流。

## 3. PRD 全量功能映射

“部分”表示有相关基础或遗留功能，不表示已通过该 FR 的完整验收。

| PRD | 当前状态 | 剩余主责工作包 |
|---|---|---|
| FR-001 现有功能兼容 | 原有 UI 存在；迁移与完整回归未完成 | WP-04、WP-12；持续回归 |
| FR-010 桌面运行时与 Go Core | WP-01 Task 1–9 已实现；CI/最终验收待完成；Worker 等后续接入 | WP-01 收尾、WP-03/07、WP-12 |
| FR-020 项目、原著与章节导入 | 原有自由画布项目存在；Drama/原著导入未实现 | WP-04/05/06 |
| FR-030 章节事件图谱 | 未实现 | WP-05/06，消费端 WP-08/09/10 |
| FR-040 ScriptAgent 剧本流水线 | 未实现 | WP-07/08 |
| FR-050 资产圣经 | 通用素材能力存在；正式领域、版本和批准未实现 | WP-05/09/10 |
| FR-060 导演规划与 MONOFORM | 遗留预演存在；Shot 上下文和安全双向桥未实现 | WP-09；深度增强后续版本 |
| FR-070 分镜表、面板、分镜图 | 未实现 | WP-05/09 |
| FR-080 视频、音频、字幕与导出 | 有旧生成节点；持久镜头、字幕、时间线、FFmpeg 闭环未实现 | WP-03/11 |
| FR-090 Agent 中心 | 未实现 | WP-07，完善于 WP-08/09/10/12 |
| FR-100 Durable Workflow | 未实现 | WP-07，业务接入 WP-08～11 |
| FR-110 Supervisor 与质量中心 | 未实现 | WP-07/08/09/10/11 |
| FR-120 Persistent Memory | 未实现，聊天历史不等于 Memory | WP-07 Recent port、WP-10 核心；V1 增强 |
| FR-130 画布语义化 | 旧无类型连线与画布存在；领域投影/命令同步未实现 | WP-04/05/08/09/12 |
| FR-140 Provider Gateway 与模型策略 | 旧前端 Provider 存在；目标 Go 网关未实现 | WP-02/03；策略 WP-07～11；清退 WP-12 |
| FR-150 Persistent Job Manager | 旧内存轮询不满足要求 | WP-03，后续业务接入 |
| FR-160 素材存储、版本与血缘 | FileStore 和基础引用已实现；资产版本/血缘/缩略图/GC 未完成 | WP-01 收尾、WP-03/04/05/09/12 |
| FR-170 备份、恢复与迁移 | 基础 DB 快照已实现；普通无密钥备份、旧项目迁移和恢复未实现 | WP-04/12；Secret 配合 WP-02 |
| FR-180 设置、日志、诊断与隐私 | 基础日志/脱敏/health 和旧主题语言存在；完整设置/诊断/隐私流程未实现 | WP-02/03/07/10/11/12 |

## 4. 剩余完整任务清单：按依赖执行

以下复选框均为尚未完成的交付或验收工作；复合条目中的技术基础即使已存在，也仍需完成该条端到端集成。除 WP-01 剩余任务外，后续工作包均等待用户明确授权，不是本次开始实施。

### WP-01 剩余：Task 10 和 Task 11

#### Task 10 — verify、CI、README

- [ ] 更新 `scripts/verify.ps1` 与 `scripts/verify.sh`：在仓库根执行 `go test ./... -count=1`、`go vet ./...`；执行失败保持非零退出码。
- [ ] Wails CLI 可用时执行 build；不可用时给出安装固定 v2.15.0 的 SKIP 原因，不用 SKIP 替代本包必需的生产构建证据。
- [ ] 修正 POSIX 当前目录问题、过时 WP-00 结论；保留前端 test/lint 缺失的真实 SKIP，不引入无关框架。
- [ ] 保留 Ubuntu Web CI，验证修复后锁文件的 npm ci；增加 Windows job：Node 22、Go 1.25、固定 Wails v2.15.0、npm ci、Go tests/vet、Wails build。
- [ ] CI 正确处理 Go embed placeholder、生成 bindings 与构建产物；不上传 DB、log、secret 或用户素材。
- [ ] README 增加桌面 prerequisites、CLI 安装、wails dev/build、数据目录、verify、浏览器模式以及当前 Provider/Secret 迁移边界；浏览器示例使用 loopback。
- [ ] 两个 verify 入口实际运行、shell 语法检查、diff/artifact 检查；CI 本地审查与远端真实执行证据分别记录，未运行远端 CI 不称 PASS。
- [ ] Task 10 完成 implementer 检查 → 独立只读 spec review → 修复/复审 → 独立只读 quality review → 修复/复审。

#### Task 11 — 最终关闭

- [ ] Task 10 双审通过后，执行最终 Go tests/vet、可运行 verify、Wails clean build、必要隔离 native smoke；记录版本、退出码、限制和当前 artifact 信息。
- [ ] race 在合适环境复核；若宿主仍失败则记 ENVIRONMENT FAIL，不能用普通测试或 CGO=0 称 race 已通过；是否满足本包 DoD 须如实判断。
- [ ] 逐条验 AC-FOUND-001（production window/binding/embedded Web/无 Vite/进程退出）、002（SQLite/WAL/FK/migration/safe mode/snapshot）、003（FileStore 哈希/校验/原子/去重/路径/缺失文件）。
- [ ] 更新 STATUS、TRACEABILITY、ADR-0002；ADR-0001 仅在证据变化时修改；纠正 FR/NFR 编号、当前事实与历史事实；记录本文件第 2.2 节文档差异。
- [ ] 结合现有失败测试、并发检查、快照读回、平台边界与性能测量完成 ADR 接受条件评估；证据不足保持 Proposed/PARTIAL，禁止为了结案放宽条件。
- [ ] 最终检查 secret/临时/生成/大文件及迁移文件；恢复构建清空的零字节 `.gitkeep`；不删除未知文件。
- [ ] 按 AGENTS.md 报告 COMPLETE/PARTIAL/BLOCKED 与 AC 真实结果，停止。WP-02 仅推荐，不启动。

### WP-02 — Secret、网络安全、Provider Gateway

依赖 WP-01；验收 AC-FOUND-004、AC-FOUND-005 文本/安全子集、AC-SEC-001。

- [ ] SecretStore port、native 实现与测试 fake；只持久 SecretRef；无安全凭据后端时禁用外部 Provider，禁止明文回退。
- [ ] 安全录入/替换/删除和状态展示；Binding 不提供 Resolve；前端 Store、返回 DTO、日志、Agent 输入不得含原始密钥。
- [ ] Provider/Model/Capability 配置、Registry、健康检查、项目默认/阶段/单次策略基础。
- [ ] 安全 HTTP Client：严格 URL/scheme/host/port/allowlist，DNS A/AAAA 与 IPv4-mapped 检查、连接绑定已验证 IP、防 rebinding、重定向逐次复验、TLS。
- [ ] 精确本地 Provider 授权例外；限制代理/Header/connect/TLS/header/idle/total timeout、响应与下载大小；默认不放开私网。
- [ ] OpenAI-compatible Text Generate/Stream、取消、错误分类、429/5xx/无效 JSON；请求 ID、耗时与费用审计脱敏。
- [ ] httptest 契约与完整 SSRF corpus；Secret 前端/日志负面测试；禁止真实付费调用成为 CI 条件。
- [ ] 标记旧 key/script/直连路径、迁移警告与 feature flag；CI 增动态代码及 Secret 静态扫描，遗留例外有明确范围和清退归属。

### WP-03 — 持久任务与多模态 Provider

依赖 WP-02；验收 AC-FOUND-006、AC-MEDIA-001 Mock。

- [ ] GenerationJob/JobAttempt/ProviderRequest/JobDependency 表、状态机、幂等键、revision/lease、priority 与事件日志。
- [ ] SQLite 队列、scheduler、worker pool、Provider 并发/速率限制、进度、超时、指数退避、最大重试、依赖调度。
- [ ] 启动 recovery scanner；有 remote ID 优先查询，不重复提交未知计费请求；不可恢复安全失败，保留诊断。
- [ ] OpenAI-compatible Image/Edit、Gemini-compatible Text/Image、异步 Video submit/poll/fetch/cancel、基础 Audio/TTS；生产 Adapter 与测试 Mock 严格隔离。
- [ ] URL/Base64/Binary 结果进入 FileStore，限制大小/内容、临时文件校验、原子提交；下载未完成不得标完整成功，remote_only 有明确语义。
- [ ] 取消贯穿：停止后续产物提交，记录无法远端取消的 orphan candidate；安全保留此前已提交结果。
- [ ] Job Center 最小 UI，批量暂停/取消/恢复/仅重试失败；100 图片任务排队不冻结，供应商并发不超限，UI 进度合并。
- [ ] 现有前端生成接 Go Adapter，至少一条图像链路真实贯通；生产 flag 禁新增浏览器直连；重启、重复响应、失败不假成功测试。

### WP-04 — Legacy 迁移与画布后端适配

依赖 WP-03；验收 AC-LEGACY-001/002/003、AC-CANVAS-004、AC-BACKUP-001 基础、AC-E2E-001。

- [ ] 原创/脱敏 legacy fixtures：最小、全部节点、缺失媒体、畸形 metadata；版本识别、预检、迁移 manifest 与旧 ID mapping。
- [ ] Project/Canvas/Asset/File/History 基础 Repository；迁移前快照，Node/Edge/Viewport/Chat/History/媒体转换。
- [ ] 节点/连线数量、文字、媒体哈希、视口读回比对；未知插件节点、字段与 unsupported metadata 保留并报告，缺失文件显式告警。
- [ ] 幂等导入、重复提示、显式创建副本；临时空间验证、事务、失败回滚与清理；旧数据保留只读，不自动删除。
- [ ] Canvas persistence/repository adapter 接 Go；事实由后端持有，Zustand 仅 UI/交互；保留现有交互与视觉。
- [ ] 普通备份 v1：业务 snapshot、非敏感配置、素材、manifest/schema/checksum；不含 Secret；恢复原子性基础。
- [ ] 核心回归 E2E：新增/移动/删除、复制、框选/多选、缩放/平移、undo/redo、连接、小地图、裁剪/分割/mask、生成子节点、所有媒体预览。

### WP-05 — 短剧领域模型与工作室 UI

依赖 WP-04；验收领域不变量与 AC-CANVAS-001/002 基础子集。

- [ ] `free_canvas`/`drama` 项目；设置/规则/风格/模型策略、创建向导、语言/画幅/分辨率/集数/时长。
- [ ] SourceDocument/Version/Chapter；StoryEntity/Alias/Event/Relation/FactSource/Conflict/CharacterState；覆盖 PRD 所需人物、地点、组织、道具、时间与状态语义。
- [ ] Episode/Skeleton/Strategy/Script/Version/Scene/Dialogue/Shot；正确外键、顺序与来源关系。
- [ ] Asset/Version/File/Usage/Relation/Lineage 与 DirectorPlan/Storyboard/Item/Panel；领域类型覆盖角色、场景、服装、道具及 PRD 其他资产类型。
- [ ] Workflow/Stage/Review/Issue/UserGate 基础实体，为后续 Runtime 提供正式持久化基础。
- [ ] revision CAS、不可变内容版本、approved 唯一、锁定规则、软删除、stale 与影响传播基础；正式 SQL 迁移与负面测试。
- [ ] Domain Command/Query/Event 与 Binding；Canvas entity refs/Relation Registry、合法端点/required 引用/删除影响；旧无类型边可保留 generic。
- [ ] Studio 导航、项目总览及各业务页诚实空状态；不放静态假成功、不在本包接 LLM；数据完整性测试。

### WP-06 — 文档、章节与故事事实

依赖 WP-05；验收 AC-STORY-001/002。

- [ ] TXT/Markdown/DOCX/粘贴文本；编码检测、规范化、预览、SHA-256、不可变原始文件与文本版本、重复导入提示。
- [ ] 自动拆章、人工合并/拆分/确认；字符/段落范围定位、offset 不重叠；修改产生版本；10 万汉字处理与分块不冻结 UI。
- [ ] EventExtraction 输入输出 Schema、Application Service/受控 Provider；Runtime 未就绪时保持明确适配边界，Mock 仅用于测试。
- [ ] 人物/地点/组织/道具/事件/因果/时间/状态候选；接受/拒绝/编辑/锁定、合并别名、证据引用、冲突处理。
- [ ] Story Graph 列表与基础可视化；章节变更使下游事实待复核；为 Script/Supervisor 提供同一只读事实查询。
- [ ] 文档不可信边界、DOCX ZIP/XML/外部关系限制、大小/压缩比/取消/超时；恶意文档与跨项目 ID 测试。

### WP-07 — 三层 Agent、Skill、Workflow、Quality Gate

依赖 WP-05，按当前路线先完成 WP-06；验收 AC-AGENT-001～005。

- [ ] AgentRegistry/Spec、Skill Manifest/Loader/版本/hash、Schema/Tool 注册静态验证；项目覆盖有来源、限制与启用规则。
- [ ] 三个独立调用：Decision、Execution、Supervisor；Decision 无业务写，Execution 窄工具，Supervisor 只读且重新加载 DB。
- [ ] Tool 输入/输出 Schema、ACL、项目/剧集 scope、状态与 locked guard、超时、限长、审计；禁止 SQL/File/Network/Shell 原语。
- [ ] 结构化输出、一次 Schema repair、artifact ID/来源/版本 DB 读回；无效输出不半写，模型自述不能判成功。
- [ ] AgentRun/Message/ToolCall、精确 Skill/model/Provider/Workflow/Stage 关联、Token/费用/耗时/error；仅 reason summary，无私有思维链。
- [ ] Workflow/Stage 显式状态机、依赖、幂等、事务、输入输出快照、审计事件、暂停/取消/恢复；数据库控制合法动作。
- [ ] 用户 PASS/FIX/REDO/MANUAL_EDIT/SKIP/CANCEL 与原因记录；关键阶段门；max tool/time/token、最多自动修订 2 次、超限人工处理。
- [ ] Recent Memory port、召回早于写入当前消息的调用顺序；Script/Production Skill 骨架，不偷跑具体业务。
- [ ] Agent Center/Quality Gate 最小 UI；Skill 查看/新版本/回滚、启停与模型策略、调用详情/只读测试逐步接真实路径。
- [ ] deterministic Mock LLM Canary：Decision → Execution → Persist/Verify → Supervisor → User Gate；权限/幻觉/注入/修复失败/最大循环/取消/重启测试。

### WP-08 — ScriptAgent 三阶段

依赖 WP-06/07；验收 AC-SCRIPT-001/002/003。

- [ ] Script Decision Skill；故事骨架 execution/schema/tools 与独立 supervisor：命题、主支线、目标阻力代价、关键事件、集数、Hook/推进/反转/悬念及改编清单。
- [ ] 改编策略 execution/schema/tools/supervisor：受众、时长节奏、视角、场景/角色压缩、揭示顺序、可视化难点、成本风险红线。
- [ ] 结构化剧本 execution/schema/tools/supervisor：Episode/Scene/Dialogue/Shot 正式持久化；动作/对白/旁白、人物、时间/场景、前后状态、事件映射、时长与改编标记。
- [ ] 每阶段不可变版本、PASS/FIX/REDO/MANUAL_EDIT、锁定字段保护、Version Diff、旧版本保留和上下游追踪。
- [ ] Script UI、版本比较与审核定位、模型策略、Canvas 领域投影与双向命令更新。
- [ ] Canary 从原著/事件到 approved Script，验证修复缺失 Hook、重新审核、中断恢复及未满足依赖不得前进。

### WP-09 — ProductionAgent、资产、导演、分镜

依赖 WP-08 与 Job/Provider；验收 AC-ASSET-001/002、AC-BOARD-001/002/003、AC-E2E-002 生产部分。

- [ ] Production Decision Skill 与 director plan execution/schema/version：风格、摄影、构图、灯光色彩、运动、节奏、视线轴线、空间及模型限制。
- [ ] 资产缺口分析与用户清单/优先级确认；角色/场景/道具/服装/车辆/生物/风格参考/派生资产创建、导入、候选生成。
- [ ] 资产稳定 ID、属性/约束/负面约束、参考媒体、批准版本、适用集数/状态；角色多状态共存并按 Shot 引用。
- [ ] 候选比较、批准/淘汰/回滚、父子血缘、用途与影响分析；替换批准版本列出下游引用，旧版本保留。
- [ ] Storyboard Table Agent：Shot 编号/顺序、Scene、时长、景别机位运动、构图动作表情、资产引用、对白音效、首尾帧、视频运动、连续性与事件来源。
- [ ] Storyboard Supervisor 独立检查剧本覆盖、引用、镜头可执行性与连续性；问题定位 Shot/Panel/Asset，未通过或 required asset 缺失时阻止批量。
- [ ] Panel Agent、参考图/mask/参数、多候选图、Canonical Panel、Asset Registry 入库；批量 image jobs 暂停/取消/恢复/失败重试。
- [ ] Shot 重排正确更新顺序与关系；单 Shot 重做不改变其他 Shot；表格与画布双向同步、revision 冲突提示。
- [ ] MONOFORM typed bridge 基础：版本/source/origin/nonce/schema/大小限制；Shot 上下文传入，相机/预演图安全保存写回，子应用失败不破坏主数据。
- [ ] Production Canary：至少 2 角色、2 场景、12 Shots 到批准分镜图；每个结果可追溯 prompt/model/job/parent/source/agent/stage/review。

### WP-10 — Persistent Memory、一致性与质量中心

依赖 WP-07/08/09；验收 AC-MEM-001～005、AC-E2E-004/005。

- [ ] MemoryItem/SummarySource/EntityLink；episodic/semantic/procedural/artifact；role/agent/time/source provenance；与 Story Graph 分离。
- [ ] Scope Resolver：local-user/workspace/project/episode/agent/session；先过滤作用域再排序，跨项目泄露为零。
- [ ] Recent、Summary、Semantic recall、Pinned critical facts；先 recall 后写当前消息或明确排除 self-hit；阈值、TopK、去重、权重融合与 Token Budget。
- [ ] Float32 BLOB、VectorIndex 小规模精确搜索；Embedding provider 可换、版本切换/重建、关键词降级；无授权不外发项目文本。
- [ ] Deep Recall：summary 检索→threshold→rerank→source IDs→原始消息与实体版本→有界来源结果；明确历史问题才调用。
- [ ] Memory Inspector：查看/来源跳转/固定/编辑/降权/删除/重建；删源消息正确失效或更新摘要；locked 用户控制。
- [ ] 确定性一致性规则：角色/服装/伤势/道具所有权/场景/时间/引用/时长；语义 Supervisor 补充检查，区分证据来源。
- [ ] Quality Center：跨阶段问题、严重度、规则/版本、实体跳转、FIX/waiver/复审、stale 影响；统一执行与监督追踪。
- [ ] 召回与一致性评测：低相关空结果、隔离、自召回、来源删除、长期服装禁用设定恢复、局部修复及 token 上限。
- [ ] Embedding ADR；MVP Provider + 关键词降级即可，本地 ONNX/sqlite-vec 仅在对应许可、打包和产品范围获批后加入。

### WP-11 — 视频、音频、字幕、时间线、导出

依赖 WP-09/10；验收 AC-MEDIA-001/002/003、AC-E2E-003 媒体部分。

- [ ] Video Job UI 与受控 Adapter，支持分镜/首尾帧/参考资产/运动/时长/数量；远端状态、取消、下载、验证与恢复真实接入。
- [ ] Shot 多视频版本、预览/批准/替换；正式结果落本地并验证后成功；remote_only 与完整成功区分；重复响应不重复资产。
- [ ] 对白 TTS、旁白、音频导入与角色/台词关联；基础音量/起止时间、缺失台词检测。
- [ ] 字幕草稿、手动时间码/文字编辑、合法 SRT/VTT、外挂/烧录，镜头顺序和字幕同步。
- [ ] 基础 Timeline：有序镜头、音频字幕、clip 替换；不实现专业 NLE 全套。
- [ ] MediaEngine/FFmpeg ADR，探测/大小/时长/流限制、安全参数化、取消/超时、禁止 shell 字符串拼接。
- [ ] MP4 预览/最终质量，横/竖/自定义分辨率；剧本/分镜/字幕/导出 manifest 与版本信息。
- [ ] Final Supervisor：批准镜头/媒体/音频字幕/stale/waiver/时长/异常与资源元数据；人工质量门，导出可播放验证。
- [ ] 完整单集 E2E 与强制中断恢复；Mock 测试通过与真实供应商联调结果分开，未获授权不调用付费接口。

### WP-12 — 硬化、性能、打包、Release Candidate

依赖 WP-01～11；验收全部当前发布范围 AC，清零发布阻断。

- [ ] 两套完整 verify：format/vet/tests、lockfile install/typecheck/component/build、schema/skill、security、license/SBOM；失败/skip 明确。
- [ ] Unit/Application/Repository/Provider/Agent/Workflow/Memory/Frontend/E2E/Security 全层测试；原创 Canary 与恶意 fixtures 有来源许可证说明。
- [ ] 性能：1000 nodes/2000 edges、1 万 asset/Memory、500 job history、大文件/ZIP；项目打开 P95 <3s、普通 DB 命令 P95 <100ms、进度 UI ≤10 次/秒。
- [ ] Canvas 按 viewport/selection/drag/connection/history/projection/generation/persistence 拆分；normalized map、局部 selector、节点与边裁剪、分页/虚拟化、避免无界深拷贝。
- [ ] Backup/Restore 完整：schema/manifest、hash、snapshot、迁移临时副本、Zip Slip/Bomb/设备名/symlink/重复规范路径限制、原子提交与失败项目不变。
- [ ] 从前序已发布 schema 升级；迁移前快照、校验和、完整性、降级拒绝/前向修复、恢复模式；禁止改写已发布 migration。
- [ ] FileStore 全资产接入、缩略图、引用计数/删除保护、GC 预览和取消、缓存/临时 TTL、安全完整性检查；GC 不删其他项目引用和批准资产。
- [ ] 设置闭环：数据目录、缓存、Worker、timeout、模型/Memory 策略、日志级别、FFmpeg、自动备份、主题/语言；数据目录变更需安全迁移策略。
- [ ] 日志轮转/保留上限、Trace/Run/Job/Workflow 关联、原始响应 TTL、diagnostics 清单与脱敏预览/取消/导出；默认无遥测与自动正文上传。
- [ ] Provider 隐私/数据外发展示与授权、fallback 不绕过授权；缓存清理/记忆删除/项目导出与永久删除控制。
- [ ] 删除或使发布构建不可达：任意 JS 模型脚本、前端原始 key、browser direct Provider、任意 target proxy、未校验 iframe 与新事实 localForage 路径；所有入口有回归证据。
- [ ] Secret scan、npm/Go 依赖漏洞检查、MIT/版权、完整 THIRD_PARTY_NOTICES/SBOM、构建 artifact 哈希；测试和 CI 不打印密钥。
- [ ] Windows clean VM：无 Node 的安装/启动/关闭/重启、WebView prerequisites、安装/升级/恢复/卸载数据保护；installer/signing 策略和实际验证分开。
- [ ] 中文首发/英文结构、键盘/焦点/非颜色状态、Markdown/SVG/CSP/外链/剪贴板/拖放/WebView 权限与无障碍回归。
- [ ] 用户文档、故障恢复说明、release checklist；六项 E2E 全部记录证据；未通过发布阻断不标 RC。
- [ ] 加密敏感备份作为独立能力，仅在 ADR/版本范围明确后实现密码 KDF+AEAD、预览、错误密码与明文临时清理；不能混入普通备份默认按钮。

## 5. Roadmap 容易遗漏的 PRD 子项及归属建议

下表属于任务规划建议，正式写入工作包计划时需按事实优先级处理，不扩大当前 WP-01。

| 容易遗漏项 | 归属建议与验收重点 |
|---|---|
| 声明式 HTTP Manifest Adapter | WP-02 契约/安全，WP-03 能力映射，WP-12 清退前完整验证：endpoint/method/header/body/status/JSONPath/poll/allowlist，禁止脚本与任意网络 |
| Embedding Adapter/关键词降级 | WP-10；不能只写向量表而无可用查询与降级路径 |
| Provider Webhook（若支持） | WP-03/11 按适配器能力处理；本地桌面不能为此默认开放公网 listener，不支持则明确 polling 能力 |
| FR-090 Skill 新版本/回滚/只读测试/诊断导出 | WP-07 管理基础，WP-12 与诊断闭环；仅注册 Skill 不算 Agent 中心全部完成 |
| FR-150 非生成任务 | Job 基础不能只覆盖图片：Embedding/下载/缩略图/导入/导出/摘要/迁移等按各业务包接入，保留取消和恢复策略 |
| FR-160 资产血缘、引用与 GC | WP-01 仅物理对象基础；完整 parent/job/prompt/model/parameters/source/review/usage 在 WP-03/05/09，GC 与限额在 WP-12 |
| FR-180 全局设置与隐私/诊断 | 横跨各包；由 WP-12 最终逐项收口，不能因没有专门 WP 而漏做 |
| 旧项目 Prompt/History/未知节点 | WP-04 fixtures 与 read-back；旧 UI 可打开不能代替迁移完整性 |
| stale/waiver 与最终导出 | WP-05 不变量、WP-09/10 传播、WP-11 final gate/manifest；不能静默采用失效版本 |

## 6. 非功能要求和最终成功判据

| PRD NFR | 正确含义 | 验收归属 |
|---|---|---|
| NFR-001 | 性能 | WP-06 大文本、WP-03 queue/UI、WP-12 Canvas/asset/memory/DB P95 |
| NFR-002 | 可靠性 | WP-01 migration/snapshot、WP-03 job、WP-04 import、WP-07 workflow、WP-12 recovery |
| NFR-003 | 安全 | 各信任边界负面测试，WP-12 全量安全语料和发布门 |
| NFR-004 | 可维护性 | 分层、明确 port、Schema 版本、稳定错误、大组件拆分、不引入大型 Agent 框架 |
| NFR-005 | 测试 | Go/Repository/Mock Provider/状态机/Memory/React/Playwright/迁移/安全/性能 |
| NFR-006 | 可访问性与国际化 | 每个 UI 包实现，WP-12 整体键盘/焦点/中英文验证 |

最终必须完成的六个 E2E：

1. **AC-E2E-001**：旧项目完整导入，全部媒体预览，新图片任务，新备份不含 key。
2. **AC-E2E-002**：≥3 章、≥3 万汉字故事，3 集规划，第 1 集剧本通过监督，≥2 角色/2 场景/12 镜头，生成分镜并全链路可追溯。
3. **AC-E2E-003**：视频和 storyboard workflow 运行中强制关闭，重启显示真实阶段，恢复或安全失败，无重复资产。
4. **AC-E2E-004**：故意服装错引，监督定位，FIX 新版本，旧版保留，通过后才能推进。
5. **AC-E2E-005**：早期禁用红色服装，经大量后续消息后 Deep Recall 回到原始来源及资产，不串项目。
6. **AC-E2E-006**：backup/log/frontend 无 key，恶意 URL/ZIP 被阻断，Supervisor 无写工具。

PRD 指标同样需要受控评测而非猜测：导入到骨架成功率 ≥90%、恢复 ≥95%、连续性问题可定位 ≥90%、输出首次 Schema ≥90%/一次 repair 后 ≥98%、监督问题实体定位 ≥95%；可迁移节点/边不丢失 100%、任务与实际结果一致 100%、重复幂等产物 0、密钥泄露 0。当前未测得这些整体指标。

## 7. MVP 之后的完整规划边界

不能把这里所有未来项塞入当前工作包，也不能用未来版本名取消 FR 明确要求的基础能力。

| 版本/条件 | 后续待办 |
|---|---|
| MVP/v0.5，至 WP-11 + WP-12 发布硬化 | 完成原著→事件→剧本→资产→导演→分镜→视频→基础 TTS/字幕→单集 MP4，并具备安全/恢复/记忆/审核/迁移 |
| v1.0 | 许可与打包核验后的本地多语言 ONNX、可选 sqlite-vec、层级摘要/增强记忆中心和召回评测；更强事件图谱可视化、MONOFORM 深度集成、资产一致性、视频首尾帧/批量增强；音效建议/生成、背景音乐导入、简单混音、多角色声线、更完整时间线；Windows 稳定安装升级；PDF 导入增强 |
| v1.5 | macOS/Linux 打包、Workflow 模板、模型路由与成本预算、资产批量替换/影响分析增强、更大画布性能、外部剪辑格式研究与适配 |
| v2.0，当前范围外 | 可选团队/云同步/多租户 API/PostgreSQL-pgvector/Worker 集群/S3-MinIO/订阅 Credits 审计；尚不具备批准详细工作包 |

需要在对应包开始前记录的规格差异：

- FR-120 的“必要规则”要求层级摘要与管理操作，而 MVP/V1 段落将部分 UI/层级摘要放 V1；WP-10 明确包含 Inspector。至少按 WP-10 完成管理/来源/删除基础，层级深度通过 ADR/产品边界明确，不静默延期。
- FR-060/080 的基础 MONOFORM、首尾帧、MP4 已在 FR/MVP/当前 Roadmap 中出现；版本规划中的“深度/更完整”不能作为不实现基础闭环的理由。
- ACCEPTANCE 将 macOS/Linux 必须验证放在 v1，PRD 版本规划把打包列在 v1.5。以 PRD 优先，分别记录验证和打包范围；若影响承诺需产品决策。
- PRD、Domain、Architecture 中 Workflow/Stage/Job 状态名有差异（如 awaiting_remote/waiting_remote、recovering/verifying、executed/execution_succeeded）。WP-03/07 应以 PRD 行为为准建立统一规范或显式映射，写 ADR/Schema/测试，不凭模型文字推断。
- PRD 的资产类型与关系集合比部分 Domain 示例更广，WP-05 需逐一覆盖或明确映射，不能将示例枚举误当全部需求。
- 加密敏感备份在 FR-170 有要求、Roadmap 为 ADR 条件项；作为独立待办保留，正式发布时间归属需明确，不偷塞、不漏记。

## 8. 新会话应该怎样接手

**本机继续时，让新 Codex 或 zcode 打开当前这个目录，粘贴配套提示词全文。**本报告不创建新会话。

- 不要只 checkout 当前分支名或从 GitHub clone：HEAD 仍是旧提交，大量 WP-00/WP-01 文件为 untracked，新的 clone/worktree 不自动包含现场。
- 在另一机器继续时，必须另行准备经过检查的源代码/规格/未提交改动迁移包，排除真实密钥、数据库、素材、node_modules 和 build 输出；只传 handoff 文本无法恢复代码。
- 不要同时让两个会话写同一现场。可运行独立只读 reviewer；写任务按 Task 串行。
- zcode 若没有同名工具，使用等价 shell/file/test 能力；若没有独立审查能力，完成可验证实现后保留“待独立审查”，不要自称已获得两个独立批准。
- 下一会话的目标是 **Task 10 → 审查 → Task 11 → 报告停止**。后续 WP-02 需要在 WP-01 真正结束后明确授权。

## 9. 本次整理的安全与交付范围

- 本轮只新增本报告和配套提示词，未实施 Task 10/11，未改变 STATUS 当前工作包或验收状态。
- 保留现有 tracked/untracked 文件；未 commit/push/stash/reset/clean。
- 未访问真实用户数据库、浏览器/WebView profile、素材或 Provider；测试使用临时环境。
- 未发现或输出真实密钥值；本轮不是覆盖所有文件/运行态的完整 secret audit，不能据此宣称全仓库已安全。
- 当前发布阻断仍包括可达动态模型脚本、前端 key/备份路径、任意代理、持久业务链路缺失、未完成的迁移/E2E/许可证与发布矩阵。

权威来源：`PRD.md`；`AGENTS.md`；`docs/ROADMAP.md`；Architecture/Domain/Agent/Security/Acceptance；`docs/implementation/STATUS.md`；指定 handoff；WP-01 plan 与执行证据。本文的任务分配建议不覆盖它们。

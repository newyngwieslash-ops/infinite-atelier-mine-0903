# Infinite Atelier Drama Studio 实施路线图

> 版本：v1.0  
> 原则：一次只执行一个工作包；每包可验证、可回滚、不得偷跑后续范围  
> 当前工作包：见 `docs/implementation/STATUS.md`

---

# 1. 使用规则

1. 工作包必须按依赖执行，除非先更新本文件并说明理由；
2. Codex 每次只实现 `STATUS.md` 指定的当前工作包；
3. 工作包开始前读取 PRD、AGENTS、Architecture、Domain、Agent、Security、Acceptance；
4. 开始前记录基线，结束后记录真实命令和结果；
5. 未通过当前包验收不得自动进入下一包；
6. 不允许为了“提前展示”在生产路径加入静态假数据或假成功；
7. 发现阻塞时实现安全的最小部分、保留证据并停止在当前包；
8. 每包不得大规模无关重构；
9. 数据、安全和现有自由画布回归优先；
10. 每包完成后用户决定是否进入下一包。

---

# 2. 里程碑

```text
M0 Baseline
  WP-00

M1 Secure Desktop Foundation
  WP-01 → WP-02 → WP-03

M2 Compatible Core
  WP-04 → WP-05

M3 Story and Agent MVP
  WP-06 → WP-07 → WP-08

M4 Production MVP
  WP-09 → WP-10

M5 Media and Release
  WP-11 → WP-12
```

---

# WP-00 仓库审计、基线与实施契约

## 目标

在修改产品代码前完整理解当前仓库，建立可重复基线，验证规格包与代码现状，并生成第一批 ADR/差异清单。

## 依赖

无。

## 范围

1. 阅读全部规格文件；
2. 审计仓库目录、包管理器、路由、Store、节点、服务、备份、MONOFORM、脚本和 CI；
3. 执行现有安装、typecheck、build、测试；
4. 记录 Node/Go/Wails 等工具版本；
5. 建立 Git 工作树状态和现有失败清单；
6. 建立自由画布基线行为清单；
7. 查找并记录：
   - `new Function`/`eval`；
   - API Key 保存位置；
   - Vite proxy；
   - 浏览器 Provider 请求；
   - localStorage/localForage/IndexedDB；
   - 大型 God Component；
   - 未实现按钮；
   - 测试缺口；
   - 许可证文件；
8. 创建或更新：
   - `docs/implementation/BASELINE.md`；
   - `docs/implementation/REPO_AUDIT.md`；
   - `docs/implementation/TRACEABILITY.md`；
   - `docs/adr/0001-desktop-framework.md`；
   - `docs/adr/0002-sqlite-driver-and-migrations.md`（可先 Proposed）；
   - `scripts/verify.sh`/`.ps1` 的最小可用版；
9. 更新 `STATUS.md`。

## 不在范围

- 不引入 Wails；
- 不迁移数据；
- 不改模型调用；
- 不删除动态脚本；
- 不重构画布；
- 不实现短剧 UI；
- 不更新大量依赖，除非基线完全无法安装且记录原因。

## 交付物

```text
docs/implementation/BASELINE.md
docs/implementation/REPO_AUDIT.md
docs/implementation/TRACEABILITY.md
docs/adr/0001-desktop-framework.md
docs/adr/0002-sqlite-driver-and-migrations.md
scripts/verify.sh
scripts/verify.ps1
```

## 验收

- AC-BASE-001；
- AC-BASE-002；
- 所有当前命令和失败真实记录；
- 规格引用的文件全部存在；
- 没有无关产品代码 diff；
- `STATUS.md` 明确推荐下一包 WP-01，但不自行开始。

---

# WP-01 Wails v2、Go Core、SQLite 与文件底座

## 目标

建立稳定桌面壳、Go 分层骨架、数据库迁移、FileStore、事件桥和安全启动/关闭流程，同时继续承载现有前端。

## 依赖

WP-00 完成。

## 范围

1. 引入 Wails v2 稳定线并锁定版本；
2. 建立 `main.go/app.go/internal/...` 分层；
3. 建立应用目录解析和单实例写锁；
4. SQLite Driver/迁移 ADR 定稿；
5. 实现数据库初始化、WAL、foreign keys、busy timeout、migration；
6. 实现 pre-migration snapshot；
7. 建立 `HealthService` Binding；
8. 实现 FileStore：临时写入、SHA-256、MIME/Magic、原子提交、读取、引用基础；
9. 建立结构化日志和 Redaction 骨架；
10. 建立 Wails Event Envelope；
11. 增加 Go Unit/Integration Test；
12. 保持现有 Web 开发模式可用。

## 不在范围

- Secret/Provider；
- Legacy 数据迁移；
- Job；
- Agent；
- Drama 领域。

## 关键验收

- AC-FOUND-001；
- AC-FOUND-002；
- AC-FOUND-003；
- 桌面 production build；
- 一个 Binding 贯穿前后端；
- SQLite 和 FileStore 测试；
- 现有前端路由打开；
- 无现有画布数据破坏。

---

# WP-02 Secret、网络安全与 Provider Gateway 基础

## 目标

建立 SecretStore、受控 HTTP Client、Provider Registry 和首个 OpenAI-compatible 文本 Adapter，切断新增功能对前端密钥和任意代理的依赖。

## 依赖

WP-01。

## 范围

1. SecretStore Port 与 fake/native 实现；
2. Provider/Model/Capability 配置领域；
3. 前端只能读取 Secret 状态和掩码；
4. 安全 HTTP Client：URL、DNS/IP、重定向、TLS、超时、大小；
5. Provider Error Taxonomy；
6. OpenAI-compatible Text Generate/Stream Adapter；
7. Provider Health Check；
8. Provider Request 脱敏审计；
9. Mock HTTP Provider 和契约测试；
10. 识别现有前端 API Key/脚本路径，增加迁移警告和 Feature Flag；
11. CI 增加动态代码/Secret 静态扫描。

## 不在范围

- 立即删除所有旧调用；
- 图像/视频/音频完整迁移；
- Job；
- Agent。

## 关键验收

- AC-FOUND-004；
- AC-FOUND-005 的文本/安全子集；
- AC-SEC-001；
- 前端无法通过 Binding 读取 Secret；
- 普通日志无测试密钥；
- Provider SSRF corpus 通过。

---

# WP-03 Persistent Job Manager 与多模态 Provider

## 目标

实现可持久、可取消、可恢复的任务系统，并把图像、视频和基础音频 Provider 能力迁入 Go。

## 依赖

WP-02。

## 范围

1. GenerationJob/JobAttempt/ProviderRequest 表和领域状态机；
2. Scheduler、WorkerPool、Lease/Revision、Retry；
3. 启动 Recovery Scanner；
4. OpenAI-compatible Image；
5. Gemini-compatible Text/Image；
6. Async Video Provider Contract + Mock；
7. Audio/TTS Contract + Mock；
8. URL/Base64/Binary 结果统一进入 FileStore；
9. 临时文件、校验、原子提交；
10. Job Center 最小 UI；
11. 批量并发、暂停、取消、仅重试失败；
12. 前端现有生成服务增加 Go Adapter 路径；
13. 生产 Flag 下禁止新增 Browser Direct Provider。

## 不在范围

- Toonflow 类 Agent；
- Drama 实体；
- 完整视频时间线。

## 关键验收

- AC-FOUND-006；
- AC-MEDIA-001 Mock；
- 重启恢复；
- 幂等；
- 失败不生成成功资产；
- 大文件不进入 SQLite；
- 现有图像生成至少一条链路通过 Go。

---

# WP-04 Legacy 项目迁移与画布后端适配

## 目标

把现有 Project、Asset、Generation History 和媒体安全迁入 Go Core，保持画布视觉和交互，逐步停止新事实写入 localForage。

## 依赖

WP-03。

## 范围

1. Legacy 数据格式盘点和版本识别；
2. 一次性迁移 Manifest；
3. Project/Canvas/Asset/File 基础实体；
4. Legacy ID Mapping；
5. 迁移前快照；
6. Node/Edge/Viewport/Chat/History/Media 转换；
7. Unsupported metadata 保留；
8. 幂等导入；
9. 画布通过 Repository Adapter 读写 Go Core；
10. Zustand 只保留 UI/交互状态；
11. 自由画布回归 E2E；
12. 旧数据只读保留策略；
13. 普通项目备份 v1（无密钥）。

## 不在范围

- 画布性能大重构；
- 语义领域节点；
- Drama 页面。

## 关键验收

- AC-LEGACY-001/002/003；
- AC-CANVAS-004；
- AC-BACKUP-001 基础；
- 节点/边/媒体数量和哈希一致；
- 迁移失败不破坏旧数据。

---

# WP-05 短剧领域模型与工作室 UI Shell

## 目标

建立 PRD 定义的核心短剧领域、版本、不变量和空白 UI 导航，尚不调用 LLM。

## 依赖

WP-04。

## 范围

1. ProjectSettings/Rules/Style；
2. Source/Chapter；
3. StoryEntity/Event/Relation/Conflict；
4. Episode/Skeleton/Strategy/Script/Scene/Dialogue/Shot；
5. Asset/AssetVersion/Usage/Lineage；
6. DirectorPlan/Storyboard/Panel；
7. Workflow/Stage/Review/UserGate 基础；
8. Canvas entity refs 和 Relation Registry；
9. Drama Studio 导航和空状态；
10. Project 创建向导；
11. 领域命令、查询、事件；
12. Revision/Version/Approve/Stale 单元测试；
13. 数据模型迁移。

## 不在范围

- Agent；
- 文档解析 LLM；
- Memory；
- 真实 Storyboard 生成。

## 关键验收

- Domain Model MVP 表与约束；
- approved 唯一；
- locked rule；
- stale 传播基础；
- 创建 Drama Project/Episode/Asset；
- Canvas projection 基础 AC-CANVAS-001/002 子集。

---

# WP-06 原始文档、章节与事件图谱

## 目标

实现真实文档导入、章节确认、候选事件/实体工作流和故事事实 UI。

## 依赖

WP-05。

## 范围

1. TXT/Markdown/DOCX 导入；
2. 编码和规范化；
3. 章节检测/调整/确认；
4. 原文 offset 定位；
5. 大文本分块；
6. EventExtraction 输出 Schema 和 Mock Agent/Service；
7. StoryEntity/Event Candidate；
8. 接受/拒绝/修改/锁定；
9. Alias；
10. Conflict；
11. Story Graph 列表和基础可视化；
12. Prompt Injection 边界；
13. 测试恶意 DOCX/大文件。

说明：如果 Agent Runtime 尚未完成，Event Extraction 先通过明确的 Application Service + Mock/Provider 适配实现，WP-07 再接入统一 Runtime，不创建临时不可迁移架构。

## 关键验收

- AC-STORY-001；
- AC-STORY-002；
- 原文定位；
- 100k 中文字符不阻塞 UI；
- 不执行文档指令/外部关系。

---

# WP-07 Agent Runtime、Skill、Workflow 与 Quality Gate

## 目标

实现通用 Decision/Execution/Supervision Runtime、Skill Loader、Tool ACL、结构化输出、显式 Workflow 和质量门。

## 依赖

WP-05；建议 WP-06 完成。

## 范围

1. Agent Registry/Spec；
2. Skill Manifest/Loader/Version；
3. Tool Registry/Authorizer；
4. DecisionRunner；
5. ExecutionRunner；
6. SupervisorRunner；
7. JSON Schema 验证与一次 Repair；
8. AgentRun/Message/ToolCall；
9. Workflow State Machine；
10. StageRun/Review/UserGate；
11. 最大 Tool/Time/Retry；
12. Cancellation；
13. Deterministic Mock LLM；
14. Agent Center/Quality Gate 最小 UI；
15. 基础 Memory Port，暂可只 Recent；
16. Script/Production 空 Skill 骨架，不实现业务内容。

## 不在范围

- 完整 Script Agent；
- 完整 Production Agent；
- Semantic Memory。

## 关键验收

- AC-AGENT-001/002/003/004/005；
- Decision → Execution → Supervisor → User Gate 的 Canary；
- Workflow DB 状态；
- Supervisor read-only；
- CI 不调用真实模型。

---

# WP-08 ScriptAgent：骨架、策略与剧本

## 目标

实现小说/事件到批准结构化剧本的完整三阶段工作流。

## 依赖

WP-06、WP-07。

## 范围

1. Script Decision Skill；
2. Story Skeleton Execution/Schema/Tools；
3. Skeleton Supervisor Rules；
4. Adaptation Strategy Execution/Schema/Tools；
5. Strategy Supervisor；
6. Script Generation Execution/Schema/Tools；
7. Script Supervisor；
8. PASS/FIX/REDO/MANUAL_EDIT；
9. Version Diff；
10. 锁定字段；
11. Scene/Dialogue/Shot 正式持久化；
12. Script UI；
13. Canvas 投影；
14. 模型策略配置；
15. Canary Drama E2E 到 approved Script。

## 关键验收

- AC-SCRIPT-001/002/003；
- Script Agent 独立层；
- 原著事实来源；
- 锁定内容不被 FIX 改变；
- Duration/结构完整；
- 版本可追踪。

---

# WP-09 ProductionAgent：资产、导演与分镜

## 目标

实现批准剧本到批准分镜图的生产工作流，并与现有图片生成、素材库和 MONOFORM 建立正式关联。

## 依赖

WP-08；Job/Provider 已由 WP-03 提供。

## 范围

1. Production Decision Skill；
2. Director Plan Agent/Schema/Version；
3. Asset Gap Analysis；
4. Asset 创建/导入/候选生成；
5. Approved Asset/Usage/Lineage；
6. Storyboard Table Agent；
7. Storyboard Supervisor；
8. Storyboard Panel Agent；
9. Image Jobs 批量；
10. Candidate/Approve/Redo；
11. Shot/Asset/Panel Canvas 投影；
12. MONOFORM Typed Bridge 基础；
13. 影响分析；
14. Production E2E 到 approved Storyboard Images。

## 关键验收

- AC-ASSET-001/002；
- AC-BOARD-001/002/003；
- 必需资产缺失时阻止批量；
- 单 Shot 重做不改变其他 Shot；
- 所有结果可追溯。

---

# WP-10 Persistent Memory、Consistency 与 Quality Center

## 目标

实现 Recent/Summary/Semantic/Deep Recall、记忆检查器、跨阶段一致性和统一质量中心。

## 依赖

WP-07、WP-08、WP-09。

## 范围

1. MemoryItem/SummarySource/EntityLink；
2. Scope Resolver；
3. Recent；
4. Summary；
5. Float32 Embedding Storage；
6. VectorIndex MVP；
7. Threshold + TopK + Score Fusion；
8. Self-hit 排除；
9. Deep Recall；
10. 用户 Memory Inspector；
11. Episodic/Semantic/Procedural/Artifact；
12. Event Graph 分离；
13. Consistency deterministic checks；
14. Character/Costume/Prop/Location continuity；
15. Quality Center；
16. Memory/Consistency E2E；
17. Embedding 模型 ADR；可先 Provider/Fake，若本地模型许可证/打包已批准则加入本地 Adapter。

## 关键验收

- AC-MEM-001/002/003/004/005；
- AC-E2E-004/005；
- 跨项目泄露 0；
- Supervisor evidence；
- Memory 可查看/删除；
- 低相关候选不强制返回。

---

# WP-11 视频、音频、字幕、时间线与导出

## 目标

把批准分镜转化为可预览单集，完成基础媒体生产和 Final Supervisor。

## 依赖

WP-09、WP-10。

## 范围

1. Video Job UI/Provider 正式适配至少一个经批准接口或完整 Mock；
2. 首帧/尾帧/参考资产；
3. Shot Video Version；
4. TTS Voice/Dialogue mapping；
5. Narration/Audio assets；
6. Subtitle SRT/VTT；
7. 基础 Timeline；
8. MediaEngine/FFmpeg ADR；
9. 安全参数化媒体处理；
10. MP4 Export；
11. Script/Storyboard/Subtitle/Manifest Export；
12. Final Supervisor；
13. 中断恢复；
14. 单集 E2E。

## 关键验收

- AC-MEDIA-001/002/003；
- Final Review；
- output playable；
- 无 Shell 注入；
- 导出清单可追溯。

---

# WP-12 硬化、性能、打包与 Release Candidate

## 目标

完成安全、性能、迁移、备份、Windows 安装和发布阻断检查，形成可交付 RC。

## 依赖

WP-01 至 WP-11。

## 范围

1. 完整 verify scripts；
2. 全部 Unit/Integration/E2E/Security；
3. 1,000 node/2,000 edge 性能；
4. 10k asset/Memory benchmark；
5. Backup/Restore 原子性；
6. 加密敏感备份（若 ADR 批准进入 v1）；
7. Database migration from previous package；
8. Windows clean VM；
9. installer/signing strategy；
10. Third-party notices/SBOM；
11. Secret scan；
12. Dependency vulnerability；
13. Recovery/Safe Mode；
14. User documentation；
15. Release Checklist；
16. 删除或不可达危险 legacy path；
17. 最终 E2E。

## 关键验收

- PRD 发布阻断项全部清零；
- `docs/ACCEPTANCE.md` Foundation/MVP/Media 场景；
- Windows 安装/升级/恢复；
- 普通备份无 Secret；
- 动态脚本不可达；
- Provider SSRF 全部通过；
- 旧画布回归通过；
- 未解决问题明确，不伪装完成。

---

# 3. 依赖图

```text
WP-00
  ↓
WP-01
  ↓
WP-02
  ↓
WP-03
  ↓
WP-04
  ↓
WP-05
  ├────────→ WP-06
  └────────→ WP-07
               ↑
WP-06 ─────────┘
  ↓
WP-08
  ↓
WP-09
  ↓
WP-10
  ↓
WP-11
  ↓
WP-12
```

---

# 4. 每包结束动作

Codex 必须：

1. 运行当前包全部测试；
2. 运行完整可用回归；
3. 检查 Git diff；
4. 更新 `STATUS.md`；
5. 在本文件对应工作包下不直接勾选，验收证据放 `STATUS.md`；
6. 总结完成、未完成、风险、命令和下一包；
7. 停止，不自动开始下一工作包。

---

# 5. 版本映射

| 版本 | 工作包 |
|---|---|
| Baseline | WP-00 |
| Foundation Preview | WP-01～WP-03 |
| Compatible Core | WP-04～WP-05 |
| Story MVP | WP-06～WP-08 |
| Production MVP | WP-09～WP-10 |
| Desktop MVP/v0.5 | WP-11 |
| Release Candidate | WP-12 |


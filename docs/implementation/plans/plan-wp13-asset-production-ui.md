# WP-13 — 资产生产 UI：分镜图批量生成、候选批准与 AC-E2E-002 整场景串测

依据：交接文档 P0-1（`docs/implementation/project-progress-and-remaining-tasks-2026-09-23.md:79-105`，
用户原话裁定"P0-1 单独成包"）、STATUS §0n2（十个零调用绑定，"WP-12 最大未了项"）、
`PRD.md:1765-1778`（AC-E2E-002 七个子句）、`PRD.md:699-747`（FR-070「批量生成分镜图时可
暂停、取消和恢复」「Supervisor 问题可定位到具体 Shot/Panel/Asset」）、`PRD.md:566-612`
（FR-050「替换批准版本时列出受影响分镜」）、`docs/ACCEPTANCE.md:440-468`（AC-BOARD-003
Image Jobs、AC-ASSET-002 File Lineage）、`docs/ARCHITECTURE.md`（Wails Binding 层约定）。

## 0. 现状（已侦察核实，不重建）

| 事实 | 证据 |
|---|---|
| **十个绑定零前端调用**（逐个 grep 复核为 0） | `RunImageBatch`/`CheckStoryboardGate`/`CollectBatchResults`/`ApproveCandidate`/`ApprovePanelImage`（DramaBinding）、`AddVersion`/`AttachFile`/`AddUsage`/`AddRelation`/`AttachJobResult`（AssetsBinding） |
| 绑定层完整且有生成的 TS 客户端 | `drama_binding.go:2025-2366`、`assets_binding.go:291-640`、`wailsjs/go/desktop/*.d.ts` |
| 导出链依赖 `approved_image_asset_version_id`，该列无 UI 写入者 | `timeline.go:92,103`、`final_reader.go:192,204` |
| `RunImageBatch` 前置：已批准 board 版本 + gate 通过 + providerId+modelName 必填 | `batch.go:130-192`；每 shot 1-8 候选；幂等键含候选序号；半失败批次返回已成功提交 + 错误 |
| `CollectBatchResults` 需要 `assetByItem`（item→asset 映射，**调用方须先逐 item 建 image 资产**） | `candidate.go:73-100`；内部做 AttachJobResult + AddUsage(consumer='storyboard_panel') |
| `ApprovePanelImage` 前置：panelVersionId + 候选必须在 `candidateVersionIds` 内 + **父 item 的 revision** | `storyboard.go:329` `CanApproveImage`；`drama_binding.go:2348` |
| `CreatePanelVersion`/`ListPanels` 已存在但同样无 UI 调用（面板版本本身没人建） | `drama_binding.go:1858,1882,2329` |
| provider/model 解析先例：`decodeModelSelection` + `channelIdForModel` + config store | `video-view.tsx:144-179`（`config.videoModel` `"channelId::model"` 格式） |
| 轮询先例：节内手动刷新（video-view 注释 :105-118 说明为何不上定时器）+ `onJobChanged` 事件（`jobs.ts:192`） | 两条路都有先例 |
| **批准媒体结果是全仓第一个此类控件**（video/audio view 只显示不批准） | `video-view.tsx:32-36` 注释自认 |
| i18n：`zh-CN.ts:1914-1952` 已预埋 `studio.storyboardTable.panels`（"面板"）与 `approvedHint`（"批量生成图片针对它运行"）——**文案已承诺本功能** | `zh-CN.ts:1920,1932` |
| 测试基座：`batch_wiring_test.go` + `candidate_wiring_test.go`（root 层，真实栈）分别覆盖 batch 与 collect，但**没有任何测试把 batch→collect→ApprovePanelImage→导出读回串成一条** | 侦察确认；`storyboard_test.go:393` 只到仓储层 |
| AC-E2E-002 现状：两个 canary 各覆盖一半，缺「≥3 章 3 万字真实文本→分镜图」单场景 | `canary_script_test.go:66`、`canary_production_test.go:65`；缺 3-集规划、asset_generation 阶段（2 角色+2 场景）、面板图链、批准读回 |

## 1. 十项裁定（写入 ADR-0017，全部记为可反驳）

| # | 裁定 | 理由与代价 |
|---|---|---|
| **1** | **UI 落在分镜表视图内，不建新分区**：表头加「生成分镜图」批量入口（gate 前置读），行级动作列加「面板图」入口（候选画廊+批准） | i18n 已在 `studio.storyboardTable` 预埋承诺；AC-BOARD-001 的语境就是表；建新分区会重复列 shot。★ |
| **2** | **面板版本由 collect 前的显式步骤创建**：批量生成前对每个无面板版本的 item 调 `CreatePanelVersion`（visualPrompt 取 item 的 visualDescription） | §9.5 批准需要 panelVersionId，而它现在没有任何创建路径。替代方案（collect 内隐式建）改后端语义，超出本包 UI 范围。★ |
| **3** | **每 item 一个 image 资产，名字派生自 shot 序号**（`createAsset(type:'image')`），映射即 `assetByItem` | `candidate_wiring_test.go:83-95` 的既定模式；资产是永久实体，重跑批次复用同一资产追加版本。★ |
| **4** | **轮询用节内手动刷新 + `onJobChanged` 事件驱动重载**（混合），沿用 video-view 的"不定时器"原则：事件到了才刷，用户也可手动刷 | 批量 12 shot × N 候选的 job 数量大，纯手动刷新体验差；纯定时器违反既有原则。事件订阅必须清理（AGENTS §9）。★ |
| **5** | **`ApproveCandidate` 不接 UI**（binding 保留）：UI 走 `ApprovePanelImage`——两者语义相同（都委托 `service.ApprovePanelImage`），`ApproveCandidate` 的 DTO 还少带 ItemID | 零调用者从 10 个减到 9 个的是"有真实路径的方法"；重复入口只会让调用方困惑。ADR 里记录该裁定，绑定不删（公开 API 面）。★ |
| **6** | **AssetsBinding 五方法中只接 4 个进 UI**：`AttachJobResult`/`AddUsage`/`AddRelation`/`AddVersion` 留在服务层包装（collect 内部已用前两者；后两者给"外部图片入库"留路径）；`AttachFile` 接入（用户从磁盘选文件→FileStore→挂到版本） | 本包主旅程不需要全部五个；强行全接是凑数。每个未接的在 ADR 记录其解锁条件。★ |
| **7** | **AC-E2E-002 walk 用 Go 层写**（`internal/infrastructure/database/acceptance_e2e002_test.go`），输入用 canary 既有 32,736 字 5 章 fixture（`testdata/canary-drama/`），不新建文本 | canary fixture 本就满足"≥3 章 3 万字"；walk 要证明的是**链路**而非文本规模。规模断言读 fixture 元数据。★ |
| **8** | **walk 的 3-集规划**用 script 服务既有命令（`CreateEpisode`×3），**不引入新 agent 阶段** | PRD 说"创建 3 集规划"，集数规划是数据而非模型输出；为它造阶段是扩 PRD 范围。★ |
| **9** | **asset_generation 阶段进 walk**（2 角色+2 场景），走 production canary 的 `runProductionStage` 模式 + mock provider | AC-E2E-002 子句 9 的字面要求；`StageAssetGeneration` 已在层策略表里，只是没 canary。★ |
| **10** | **traceability 收口断言**：walk 末尾用 timeline 读（`GetTimeline`）证明每个 shot 的 `approvedImageAssetVersionId` 非空且指向存在的版本——这是"所有结果可追溯"的可执行形式 | `timeline.go:92` 正是导出的 join；读它就是读导出将要读的。★ |

★ 均因细节未经用户逐条确认而按推荐执行，记为可反驳。

## 2. 前端服务层（`web/src/services/desktop/drama.ts` 扩展 + 可能新文件）

按 drama.ts 既有三段式（探针 → 惰性 import → 查询返空/命令抛错）补包装：

- `checkStoryboardGate(episodeId, storyboardVersionId)` → boolean/错误文案（错误里带原因，UI 直接展示）
- `runImageBatch(request)` / `collectBatchResults(request)` / `createPanelVersion(request)` / `listPanels(storyboardItemID)` / `approvePanelImage(request)`
- assets 侧：`attachFile`（若裁定 6 接入）
- `attachJobResult` / `addUsage` / `addRelation` / `addVersion` 进服务层包装但暂无 UI 调用者——**与裁定 6 一致：包装≠接 UI，包装给测试和后续包用**
- `resetDramaClients` 与探针同步更新（测试用）

## 3. UI（`storyboard-table-view.tsx` 内）

**表头区**（在既有批准控件旁）：
- 「生成分镜图」按钮：`CheckStoryboardGate` 前置读，未过则禁用并显示原因；过则弹 Modal（候选数 1-8、provider/model 从 config store 解析、prompt 后缀可选、seed 可选），提交 `RunImageBatch`；结果（含 duplicate 计数）用 message 汇报
- 批次进行中显示提交的 job 概览（复用 video-view 的 job 表列模式）

**行级**（动作列扩展）：
- 「面板图」按钮 → Drawer：该 item 的面板版本列表（无则一键 `CreatePanelVersion`）、每版本的状态 + 候选画廊（`ListPanelVersions` + item 的候选 usage 读）、**批准按钮**（选一个候选 → `ApprovePanelImage`，带 `expectedRevision: item.revision`，冲突则提示重载）
- 新列「面板图」：已批准显示缩略 Tag/标记，未批准显示「—」——与导出的 join 列一一对应

**约定**：所有命令错误走 `message.error(String(failure))`（既有模式）；事件订阅在 useEffect 清理；键盘可达；文案 zh-CN + en-US 双语同步。

## 4. i18n

`studio.storyboardTable.*` 下新增：`generateImages`、`generateImagesTitle`、`candidatesPerShot`、`gateBlocked`、`batchSubmitted`（带 `{{count}}`/`{{duplicate}}` 插值）、`panelImage`、`createPanelVersion`、`approveImage`、`approvedImage`、`noPanelVersion`、`candidateGallery`、`approveConflict` 等；两份 locale 同步，`locales.spec.ts` 键集一致性会强制。

## 5. AC-E2E-002 整场景 walk（Go，验收核心）

`acceptance_e2e002_test.go`，单一 `TestE2E002FromNovelToApprovedPanelImages`：

1. **导入**：canary fixture 文本（32,736 字 5 章）走真实导入服务；断言章节数与字符规模（≥3 章、≥30k）
2. **事件候选**：extraction 走 runtime（mock provider），接受若干实体
3. **3 集规划**：`CreateEpisode`×3
4. **骨架→策略→第 1 集剧本**：复用 script canary 的驱动方式（`runStage`+supervision+PASS），断言 approved script
5. **资产清单**：`asset_gap_analysis` 阶段走完 → gap report approved
6. **2 角色 + 2 场景**：`asset_generation` 阶段（本包为它补 canary 级驱动），断言 ≥2 character + ≥2 scene 资产及其版本
7. **12+ 镜头分镜表**：`storyboard_table` 阶段 → approved board（既有断言模式）
8. **面板图**：逐 item `CreateAsset`+`CreatePanelVersion` → `RunImageBatch` → workers → `awaitJobs` → `CollectBatchResults` → 逐 panel `ApprovePanelImage`
9. **可追溯收口**：`GetTimeline(episodeId)` 断言每个 shot 的 `approvedImageAssetVersionId` 非空、指向的版本存在、且 `GenerationJobID` 可回查到 job 行——「所有结果可追溯到输入、模型、任务和版本」的可执行形式

walk 独立于既有 canary（不改动它们）；mock provider，不触真实付费 API。

## 6. 测试与验收

- **前端**：`drama.ts` 新包装的单测（mock binding 边界，不 mock 组件内部）；`storyboard-table-view` 增测：gate 阻断禁用、批量提交参数传递、批准冲突提示、事件订阅清理；`locales.spec.ts` 自动覆盖新键
- **Playwright**：studio.spec 增一例——分镜表分区渲染出「生成分镜图」入口（无 core 时正确禁用；有 core 的完整交互由 Go walk 保证，浏览器 e2e 不造 Go 栈）
- **Go**：`acceptance_e2e002_test.go`（上节）；`CreatePanelVersion` 服务路径补一条 wiring 测试（现在只有 binding 层 round-trip）
- **变异**：walk 的关键断言（批准列非空、job 可回查、章节规模）各配 1-2 个变异并报告存活项
- **验收对照**：AC-E2E-002 七子句逐条 PASS；FR-070「批量生成可暂停/取消」——批次提交后取消走既有 `cancelJobs`（job 层已有），UI 提供 job 表入口即视为满足，ADR 记录此解读；AC-BOARD-003 的 UI 半边从 PARTIAL 转 PASS

## 7. 明确不做

- 真实付费视频/图片 Provider（用户未授权）
- `ApproveCandidate` 的 UI（裁定 5）；AssetsBinding 五方法全接 UI（裁定 6）
- 时间线/导出改动（它们只读 `approved_image_asset_version_id`，本包把它填上即通）
- FR-150 每 Provider 并发上限（交接清单 P0-2，独立成包）
- 表-画布双向同步（P1-5）；别名合并 UI（P1-4）
- 新数据库迁移（预计零迁移：全部走既有表与命令）
- `assetByItem` 的"复用既有资产"选择器（v1：现在每 item 固定建/找一个派生名资产，ADR 记录）

## 8. 风险与诚实声明

- **最大风险：批次×候选的 job 量**。12 shot × 8 候选 = 96 job；mock 下无碍，真实 Provider 下必须有并发上限（P0-2）才安全。UI 的提交确认 Modal 会显示将创建的 job 总数；ADR 记录该依赖。
- **`CreatePanelVersion` 的语义是本包首次生产使用**：它的 `basedOnVersionID`/`referencePolicyJSON` 字段在 UI 路径上只填最小集（visualPrompt + changeReason），walk 断言读回一致。若服务端有未发现的校验，walk 会先于用户踩到。
- **AC-E2E-002 的"3 集规划"解读**（裁定 8）可能偏保守——若产品意图是"模型产出三集规划文档"，本包交付的是数据层三集 + 第 1 集全链；ADR 记录为可反驳裁定，改解读只动 walk 不动产品代码。
- 前端 e2e 不驱动完整批次（浏览器测试无 Go 栈），完整链路的证据来自 Go walk——与仓库既有分工一致（canary 皆 Go 层）。
- 本机 `-race` 仍不可用；并发证据同前。

## 9. 流程

基线全绿 → ADR-0017 → 前端服务层包装 + 单测 → UI（表头批量 + 行级面板抽屉）+ i18n → 前端测试 + Playwright → Go walk（acceptance_e2e002）+ asset_generation 驱动 + CreatePanelVersion wiring 测试 → 变异 → 全量门 → STATUS §0q + TRACEABILITY（AC-E2E-002/AC-BOARD-003/FR-070 行更新 + 六行文档债顺手修）→ §15 报告，停止。

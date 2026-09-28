# PRD Gap Closure Development Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.
>
> Codex 执行说明：使用本机 `executing-plans` 技能；上述技能模板中的名称不构成安装新工具的要求。实施必须服从本仓库 AGENTS.md：一次只执行一个已选工作包，不自动 commit/push，不调用未经授权的真实付费 Provider。

**Goal:** 修复当前工作树中已确认的功能断链，补齐 PRD 必需能力，并为 Windows 发布建立同一版本、可复现的开发与验收证据。

**Architecture:** 保持 Domain → Application/Ports → Infrastructure/Desktop → React Binding Client 的边界，数据库继续作为事实来源。优先接通已有服务和持久化结构；新增能力使用受限命令、不可变版本和可恢复任务，不引入另一套任务、Skill 或配置系统。配置、内容分析和 Provider 扩展必须复用现有安全边界。

**Tech Stack:** Go（遵循 go.mod/toolchain）、Wails v2.15.0、SQLite/database/sql、React/TypeScript/Vite、npm lockfile、Node 内置测试运行器、现有 Playwright、FFmpeg/ffprobe、可选 ONNX Runtime。

---

## 1. 文档状态与执行边界

- 日期：2026-09-28。
- 状态：**PLANNED；本文件没有启动任何实现工作包，也没有修改产品需求或宣告既有能力通过验收。**
- 规划基线：`codex/wp-01-desktop-foundation` 当前工作树；审计时领先远端 172 commits，存在 123 条 Git 状态记录。它不是干净发布基线。
- `RP-00`～`RP-12` 是本计划的新工作包标识，不覆盖历史 WP/T 编号；下文保留旧编号便于追踪。
- 事实顺序：PRD > AGENTS > Architecture/Domain/Agent/Security/Acceptance > 当前 Roadmap > 已验证 Status > 代码与测试 > 参考分析。
- 实施前按 AGENTS 的顺序完整阅读必读文档；本次规划已分工核对产品、架构、领域、Agent、安全、验收与两份参考材料。STATUS 核对覆盖当前 T 批次、相关历史和最新补充，不声称重新逐字审计全部 4,615 行历史证据；执行包仍须遵守完整阅读要求。参考材料中的旧架构、许可证或 Prompt 叙述不覆盖本仓库的 clean-room 规则。
- 不把 `STATUS.md` 的 `COMPLETE`、方法存在、源码字符串测试通过，单独作为用户功能完成证据。
- 本计划默认不新增第三方依赖。确需新依赖时先提交具体用途、替代方案、许可证和维护风险，取得对应授权后才实施。
- 所有路径均相对仓库根 `F:/AI_ai_movie_studio_Things_202604/new-toonflow/infinite-atelier-mine-0903`；“新增”路径是计划产物，不声称已经存在。
- 本轮只新增规划文件。历史脏文件、用户文档、数据库、密钥、素材、工件和 Git 历史不由本计划整理或改写。
- 不新建一个缺少未提交工作的新 checkout 来假装当前基线。将来需要隔离实施时，先检查可复用 managed worktree，并明确如何保留/纳入当前工作，禁止 reset/stash/覆盖用户修改。

### 1.1 本次审计证据，不等于未来提交的验证结果

| 门 | 2026-09-28 审计结果 | 使用限制 |
|---|---|---|
| `go test ./... -count=1` | PASS；database 包约 218 秒 | ONNX 包显示 `[no test files]`，不证明真实推理通过 |
| `npm --prefix web run typecheck` | PASS | 不证明 DTO 运行时字段存在 |
| `npm --prefix web test` | 136/136 PASS | 包含源码接线断言，不能替代真实 UI/Binding 行为 |
| `node scripts/security-scan.mjs` | PASS；732 files | 含已登记例外，不代表全部动态安全验收完成 |
| Canary/恶意输入/工具 Schema/Skill 检查 | PASS | 仅该工作树对应内容 |
| `git diff --check` | PASS | 不代表未跟踪文件都适合提交 |
| `node scripts/gen-sbom.mjs --check` | FAIL；两份 SBOM drift，正确工具链复核仍失败 | 先调查环境依赖，不能直接断言依赖被恶意更改，也不能直接重生成掩盖差异 |
| EXE/NSIS | 文件存在，匹配 STATUS 最新哈希 | 来自 dirty 工作树；不等于干净构建、签名、安装态验收 |
| race/原生完整走查/付费冒烟/VM | 本轮未复验 | 历史证据与本次证据分开存放 |

### 1.2 较上一轮总结新增的实施前事实

1. `AssetUsageDTO` 的 Go 定义及转换没有输出 `id`，音轨编辑器却按 `u.id` 查找；生成 TS 中出现字段不能证明真实 Binding 已返回字段。RP-01 先修读链，再修写链。
2. 修正 Gemini kind 后仍需验证文本端点；当前原生 Gemini 图片与文本支持不能合并宣称已完成。
3. 四类本地任务除入口缺失，还需补完整参数传递、项目归属、受限文件读取和执行幂等；提交幂等键不能防止崩溃后的重复副作用。
4. Skill 的 loader、hash 和版本表已有基础；缺的是用户版本管理与运行时选择闭环，不能重复造 Skill 存储。
5. SBOM 生成器、CI 环境和两套 verify 的严格语义需一起检查，不能把旧 `PASS` 复制到新版本。
6. PRD 与 ACCEPTANCE 对跨平台/外部剪辑格式版本安排存在差异；设置、加密备份的延后记录也不自动等于用户批准修改 PRD。

## 2. 工作包总表与推荐顺序

| 工作包 | 任务数 | 目的 | 前置依赖 | 初始状态 |
|---|---:|---|---|---|
| RP-00 | 2 | 基线、范围与验收映射 | 无 | NOT_STARTED |
| RP-01 | 3 | 音轨 ID、参数保留、校验与真实回归 | RP-00.1 | NOT_STARTED |
| RP-02 | 3 | Provider 类型、速率、保存反馈与 Gemini 路由 | RP-00.1 | NOT_STARTED |
| RP-03 | 1 | 视频时长/分辨率能力一致性 | RP-02 | NOT_STARTED |
| RP-04 | 4 | 本地持久任务完整闭环 | RP-00.1 | NOT_STARTED |
| RP-05 | 3 | 安全 Manifest 生产适配 | RP-02、RP-00.2 范围确认 | NOT_STARTED |
| RP-06 | 5 | Skill 管理、Agent 状态、模型策略与只读测试 | RP-00.2 | NOT_STARTED |
| RP-07 | 3 | 许可录入、媒体内容分析、最终审核 | RP-01；内容分析长任务复用 RP-04 | NOT_STARTED |
| RP-08 | 2 | 真实音效 Provider 与资产采用 | RP-02；供应商协议选择 | DECISION_REQUIRED |
| RP-09 | 3 | 设置清单、可配置备份与其余设置边界 | RP-00.2 | PARTLY_DECISION_REQUIRED |
| RP-10 | 3 | SBOM、verify/CI、ONNX/媒体严格验证 | 可先做 RP-10.1；最终接收 RP-01～09 | NOT_STARTED |
| RP-11 | 3 | 离线主旅程、授权冒烟、性能与产品指标 | 对应实现包、RP-10 | PARTLY_EXTERNAL |
| RP-12 | 4 | 干净基线、签名、原生/VM验收与文档封板 | RP-10、RP-11 | PARTLY_EXTERNAL |

总计 **13 个工作包、39 个可跟踪任务**，另有 O01～O06 后续版本工作流。

推荐实施顺序：RP-00.1 → RP-01 → RP-02 → RP-03 → RP-04 → RP-05/RP-06/RP-07 → RP-08/RP-09 → RP-10 → RP-11 → RP-12。

发布关键顺序固定为：对应软件修复完成 → RP-11.1 离线全链验证 → RP-11.2 授权真实冒烟 → RP-11.3 指标 → RP-12.1 授权提交/远程 CI/干净候选构建 → RP-12.2 签名与最终打包 → RP-12.3 最终字节的原生/VM验收 → RP-12.4 封板。早期开发构建的走查与性能数据只作预验收；最终工件变化后按影响范围重验，不能用旧 dirty 工件的报告替换最终安装包证据。

RP-00.2 的范围对账可提前进行；不阻塞明确的数据丢失和配置错误修复。RP-10.1 可提前排查 SBOM。表中的并列项表示依赖允许，不授权同时开始多个包；是否并行由后续明确的包级执行指令决定。

## 3. 所有代码任务共用的执行步骤

每个任务按下列检查点拆成小步，避免一次写完再猜原因；单步以 2–5 分钟可检视的编辑/验证为目标，耗时较长的命令单独运行并报告进度。

1. [ ] 记录 `git status --short --branch`，区分本任务与现有修改；定位实际调用链及现有测试。
2. [ ] 增加下文点名的失败用例；断言用户结果、数据库读回或 Provider 收到的协议，而非源码字符串。
3. [ ] 执行对应命令并保存 RED 证据。必须看到目标测试实际执行且因预期缺陷失败；`no tests to run`、构建环境失败不算 RED。
4. [ ] 修改一个最小逻辑单元；领域/端口/迁移先于 Binding/UI。每次改动后跑目标测试。
5. [ ] 运行范围回归与负面用例，得到 GREEN；涉及 Binding 时从 Go 源重新生成，不手改生成文件来掩盖契约缺失。
6. [ ] 更新当前包 STATUS、需求映射、必要 ADR、用户文档；写真实结果和剩余限制。
7. [ ] 包结束运行完整可用 verify、`git diff --check`，检查 secret/生成物/临时文件；停止在该包边界。

**提交检查点：** writing-plans 的“频繁提交”在本仓库解释为“形成可独立提交的逻辑检查点”。未得到明确 commit/push 授权时不执行 Git 写操作，不使用 `git add .`；获准后只提交当前范围，消息遵循 Lore protocol。

**绑定生成命令：** 在仓库根使用已安装的固定 Wails v2.15.0 执行 `wails generate module`，再运行 `npm --prefix web run typecheck`。先核对 CLI 帮助与仓库生成配置，确认实际生成目录是 `web/src/wailsjs`；若不同，修正配置而非复制手写声明。比较生成 diff，仅纳入对应契约变化。

**测试命令约定：** 下文 Go 测试名是本计划要求新建的测试，不声称已经存在。前端纯逻辑使用现有 `web/src/services/__tests__/*.spec.ts`，由 `npm --prefix web test` 自动发现；不默认引入 Vitest/RTL。真实组件交互复用 Playwright，以 Binding 边界 fixture 驱动，并明确其不等于原生 Wails 验收。

## 4. RP-00：基线与范围对账

### Task RP-00.1：建立可复用的审计基线

**文件：** 新增 `docs/implementation/gap-closure-baseline-2026-09-28.md`；只读 `PRD.md`、`AGENTS.md`、`docs/implementation/STATUS.md`、`go.mod`、`web/package-lock.json`、`web/monoform-studio/package-lock.json`、`scripts/verify.sh`、`scripts/verify.ps1`。

- [ ] 按 AGENTS 必读顺序读取当前文件，而不是沿用历史摘要。
- [ ] 记录 branch、HEAD、Git 状态清单、Node/npm/Go/Wails/CGO/FFmpeg/ONNX 环境；密钥只记录“配置/未配置”，不打印值。
- [ ] 将上文审计命令保存为历史基线；只有代码/工具链发生变化或结果未覆盖当前包时才重跑，不无意义重复全部测试。
- [ ] 核对 npm 双 lockfile、主应用和 MONOFORM 独立构建路径；禁止混入 yarn/pnpm lockfile。
- [ ] 为每项发现记录“重现步骤、实际结果、预期结果、证据路径、关联 PRD/AC”；不将未知当作失败或通过。

**验收：** 新执行者能区分用户脏改、审计发现与自己的新改动；没有修改用户数据或 Git 历史。

### Task RP-00.2：冻结验收口径并记录冲突

**文件：** 修改 `docs/implementation/TRACEABILITY.md`、`docs/ROADMAP.md`、`docs/implementation/STATUS.md`；新增 `docs/implementation/gap-closure-scope-decisions.md`；获对应产品决定后才修改 `PRD.md`、`docs/ACCEPTANCE.md`、`docs/SETTINGS_INVENTORY_T10_T11.md`。

- [ ] 建立 FR-001～180、NFR-001～006、G1～G6、AC-E2E-001～006 到 RP 任务的映射；沿用已完成证据，不重开全部历史 WP。
- [ ] 明列待决定事项：Windows 首发平台范围；外部剪辑格式版本；加密敏感备份版本；FR-180 设置裁定；真实音效协议/供应商；签名分发策略。
- [ ] 对照 PRD §16 与 ACCEPTANCE §15 的 macOS/Linux、外部格式冲突，记录高优先级依据及待同步文档；未解决时相关条款标 `DECISION_REQUIRED`。
- [ ] 核对 FR-090 模型策略/只读测试与 FR-140 项目默认、阶段覆盖、单次覆盖；历史“按阶段延后”的记录保留 provenance，不悄悄覆盖。
- [ ] 分别记“代码已实现”“用户可操作”“离线已测”“真实/原生已验收”“获准延后”，禁止用一个 DONE 合并这五种事实。

以下是**待覆盖核对的合同条款，不是本计划已证实的新缺陷**。RP-00.2 为它们定位已有测试/代码；发现缺口后归入相应包或记录新的独立待批准包，不静默扩大某个正在执行的任务：

| 条款 | 必查行为 |
|---|---|
| FR-020/030 | 原文不可变/hash、人工拆合章节、source offsets、章节修订→事实待复核、别名合并、Script/Supervisor 同源读取 |
| FR-070/130 | Shot 重排及关系、表格/画布业务字段双向同步、删除投影与实体分离、破坏性引用阻止、revision 冲突提示 |
| FR-080 | 远程未验证不算完整成功、首尾帧、供应商可选 Webhook 约束、SRT/VTT、烧录/外挂、角色声线、BGM 导入 |
| FR-090 | 调用耗时/Token/费用估算、结构化输入输出、Memory Scope、Workflow/Stage、诊断出口 |
| FR-120 | 编辑/删除后的摘要源关系、Embedding 重建、Token Budget、本地模式不外发 |
| FR-160/170 | GC 预览/取消/引用保护、Skills/Provider非敏感配置/缩略图/Schema/校验和完整备份往返 |
| FR-180/NFR-006 | 日志关联 IDs、保留时间和大小限制、诊断范围预览、键盘/焦点/i18n/非纯颜色状态 |

**验收：** 未批准的范围缩减不被记作实现；待决定项不阻塞 RP-01/02 等明确缺陷。

## 5. RP-01：音轨编辑完整性

### Task RP-01.1：修复 Usage ID 与参数的真实读回

**文件：** 修改 `internal/desktop/assets_binding.go`、`internal/desktop/assets_binding_test.go`；重新生成 `web/src/wailsjs/go/desktop/AssetsBinding.d.ts`、`web/src/wailsjs/go/models.ts` 等受影响绑定；检查 `web/src/services/desktop/drama.ts`。

- [ ] 增加 `TestRP01UsageDTOContainsIDAndParams`，从服务返回真实 `asset.Usage`，序列化 Binding DTO 并断言 `id` 和 `params`。
- [ ] 断言 `ListUsagesOfConsumer` 与审批影响等其他 usage 返回路径一致，避免只修其中一个构造点。
- [ ] 在 `AssetUsageDTO` 增加 `ID string`/`json:"id"`，让统一转换器携带 `ID` 与 `Params`；列表复用转换器。
- [ ] 重新生成 Binding，验证 UI 获取的是实际后端 ID，移除用类型断言补不存在字段的做法。

**运行：** `go test ./internal/desktop -run TestRP01UsageDTO -count=1`；`npm --prefix web run typecheck`。

**通过条件：** 两条相同 assetVersion、不同 usage 的记录仍可准确分别编辑；空参数不丢 ID。

### Task RP-01.2：保存音轨时保留未编辑字段

**文件：** 修改 `web/src/components/studio/audio-view.tsx`；新增 `web/src/services/desktop/audio-track-params.ts`、`web/src/services/__tests__/audio-track-params.spec.ts`；检查 `internal/application/assets/collect.go` 与 `internal/desktop/assets_binding.go` 的替换语义。

拟新增纯函数契约与完整最小测试：

```ts
// audio-track-params.ts
export type TrackParams = {
    offsetMs?: number | null;
    sourceStartMs?: number | null;
    sourceEndMs?: number | null;
    durationMs?: number | null;
    volume?: number | null;
    muted?: boolean | null;
    dialogueLineId?: string;
};
export type TrackEdit = Pick<TrackParams, "offsetMs" | "volume" | "muted">;
export function mergeTrackEdit(current: TrackParams, edit: TrackEdit): TrackParams {
    return { ...current, ...edit };
}
```

```ts
import assert from "node:assert/strict";
import { test } from "node:test";
import { mergeTrackEdit } from "../desktop/audio-track-params";

test("editing gain preserves source trim and dialogue identity", () => {
    const original = {
        offsetMs: 1200, sourceStartMs: 300, sourceEndMs: 2100,
        durationMs: 1800, volume: 1, muted: true, dialogueLineId: "line-A",
    };
    assert.deepEqual(mergeTrackEdit(original, {
        offsetMs: 1200, volume: 0, muted: false,
    }), { ...original, volume: 0, muted: false });
    assert.equal(original.volume, 1);
});
```

- [ ] 写上述 RED 用例，并增加显式 `0`、`false`、`null`、不相干 usage 的用例。
- [ ] 编辑器保存已读的完整参数快照，只覆盖用户编辑的三个字段，再调用现有“整体替换”命令；不把 `false` 用 `|| null` 抹掉。
- [ ] 参数读取或 JSON 校验失败时显示错误并禁用保存；禁止吞错后用默认值覆盖数据库。
- [ ] 切换 shot/usage 时取消或忽略过时的参数读取，避免前一个请求迟到后覆盖当前草稿。
- [ ] 移除 `as never`，使用生成请求类型；对未来未知字段采用明确版本/拒绝策略，不静默丢弃。
- [ ] 增加 Playwright 交互：打开既有轨道→改音量→保存→再次打开，验证裁剪及台词关联未变化。

**运行：** `npm --prefix web test`；`npm --prefix web run typecheck`；对应 RP-11 行为 spec。

**通过条件：** 不改变现有整文档替换 API 含义，界面局部编辑不丢其他参数。

### Task RP-01.3：参数写入校验与混音回归

**文件：** 修改 `internal/application/assets/collect.go`、`internal/application/media/audio.go`、`internal/infrastructure/database/usage_params_t05_test.go`、`internal/infrastructure/database/audio_track_params_t05_test.go`；必要时新增 `internal/domain/asset/track_params.go`。

- [ ] 新增 `TestRP01TrackParamsRoundTrip`：完整参数入库→Binding 读→局部改音量→写回→timeline→混音请求，逐字段断言。
- [ ] 新增负例：负 offset、end≤start、负/非有限音量、无效时长、未知 usage、错误所属项目；拒绝发生在数据库写入前。
- [ ] 复用现有有效范围；若规则需共享，将纯校验放到适当 Domain 类型，禁止 assets application 反向依赖 media infrastructure。
- [ ] 并发更新使用现有 revision 机制或同事务读改写；若缺少对应版本字段，先写迁移 fixture，再增加前向迁移，不覆盖同一轨道另一编辑者的结果。
- [ ] 用真实 FFmpeg 小音频 fixture 验证源裁剪、静音、音量 0、两台词隔离；缺 FFmpeg 时明确 SKIP，发布门按 RP-10.3 变 FAIL。

**运行：** `go test ./internal/application/assets ./internal/application/media ./internal/infrastructure/database -run 'TestRP01|Test.*Track|Test.*UsageParams' -count=1`。

**验收映射：** FR-080、AC-MEDIA-002/003；已有音频隔离 T01/T04 不退化。

## 6. RP-02：Provider 配置与路由

### Task RP-02.1：正确保存供应商类型和速率限制

**文件：** 修改 `web/src/stores/use-config-store.ts`、`web/src/services/desktop/provider-sync.ts`、`web/src/services/desktop/providers.ts`、`web/src/components/layout/channel-editor-drawer.tsx`、`internal/desktop/providers_binding.go`；新增 `web/src/services/__tests__/provider-config.spec.ts`；扩展 `internal/application/providers/service_test.go`。

- [ ] 用真实 `ModelChannel` fixture 测 Gemini/OpenAI 映射；从已有 apiFormat 映射到后端已支持 kind，未知类型拒绝而不是默认 OpenAI。
- [ ] 测速率 0、正整数、负值、小数、NaN/超界；明确 0=不限，空值编辑不得把已存限额变为 0。
- [ ] 前端类型、通道持久配置、同步输入、Go DTO、服务和数据库读回都携带 rateLimitPerMinute。
- [ ] 选择字段缺失兼容策略：新增配置默认 0；旧客户端更新未提供值时保留已有值。必要时 DTO 使用可选指针区分“缺失”和“显式 0”。
- [ ] 补双语输入标签和说明，再测保存→关闭→重开限额与 kind 不变。

**运行：** `npm --prefix web test`；`go test ./internal/application/providers ./internal/desktop -run 'TestRP02|Test.*Provider' -count=1`。

**通过条件：** UI 保存不会清除后台限额；Gemini 配置到达 registry 的 kind 正确。

### Task RP-02.2：配置保存失败可见，避免 UI 与后端分叉

**文件：** 修改 `web/src/services/desktop/provider-sync.ts`、`web/src/components/layout/channel-editor-drawer.tsx`、相关调用处；扩展 `web/src/services/__tests__/provider-config.spec.ts`；新增 `web/e2e/provider-config.spec.ts`。

- [ ] Binding fixture 返回安全错误时，断言抽屉保留草稿、不展示成功、不关闭。
- [ ] 将 fire-and-forget 保存改为显式 await；后端确认成功后才更新相应前端非敏感配置，或提供有测试的回滚策略。
- [ ] 批量同步返回逐通道结果，不吞全部错误；失败不抹掉已存在通道。
- [ ] Key 写入失败与配置写入失败分开呈现且可重试；禁止从后端把 Key 读回前端来“回滚”。
- [ ] 测重复点击、并发编辑、网络/Binding 暂不可用，确认无虚假成功或静默数据覆盖。

**运行：** `npm --prefix web test`；`npm --prefix web run test:e2e -- provider-config.spec.ts`。

### Task RP-02.3：分别验证 Gemini Text/Image 的真实协议路由

**文件：** 修改 `internal/infrastructure/providers/registry.go`、`provider_wiring.go`；复用 `internal/infrastructure/providers/gemini_image.go`；按缺口新增 `internal/infrastructure/providers/gemini_text.go`、`gemini_text_test.go`；扩展 `batch_wiring_test.go`。

- [ ] 用 `httptest` 断言 Gemini kind 的文本请求不会发送到 OpenAI chat 端点；测试生成、流式片段、终止、取消和错误。
- [ ] 实现前核对供应商当前官方协议并记录请求/响应 fixture 来源与日期；不得用兼容服务的偶然行为代表 Gemini 原生协议。
- [ ] 复用现有受控 HTTP、SecretStore、审计和响应大小限制；实现文本端口后按 kind/capability 显式路由。
- [ ] 图片路径回归保留；不支持的能力诚实返回 unsupported，不能偷偷回退另一协议。
- [ ] 从配置 Binding→registry→mock server→结果读回走一次组合根测试。

**运行：** `go test ./internal/infrastructure/providers ./internal/desktop -run 'TestRP02|Test.*Gemini' -count=1`；`go test . -run TestRP02 -count=1`。

**验收映射：** FR-140、AC-FOUND-005、AC-SEC-001；全程离线，不调用真实供应商。

## 7. RP-03：视频输入能力一致性

### Task RP-03.1：能力驱动的时长与分辨率控件

**文件：** 修改 `internal/desktop/media_jobs.go`、`web/src/components/studio/video-view.tsx`、`web/src/services/desktop/jobs.ts`、`internal/desktop/jobs_binding_test.go`；新增 `web/src/services/__tests__/video-capabilities.spec.ts`、`web/e2e/video-settings.spec.ts`；更新中英文 locale。

- [ ] 对当前实现测试合法 4/8/12 秒与非法 1/5/60 秒；UI 不可选后端必拒绝值。
- [ ] 将供应商约束作为可查询能力返回，不把 OpenAI 枚举推广到所有未来 Provider；请求仍在后端二次校验。
- [ ] 用选择器替代任意数值输入；切换模型后保留合法值，否则提示重新选择，不静默改计费参数。
- [ ] 同包处理 O02 的 size 控件：与项目画幅和支持枚举匹配，无法匹配时明确提示，不生成空/不支持分辨率。
- [ ] 单镜与批量复用同一校验；验证首尾帧字节已加载、引用数限制、批量六镜上限和逐项错误不退化。

**运行：** `go test ./internal/desktop -run 'TestRP03|Test.*Video' -count=1`；`npm --prefix web test`；`npm --prefix web run test:e2e -- video-settings.spec.ts`。

**通过条件：** UI 能提交的参数组合是后端当前通道接受的组合；不宣称真实 vendor 冒烟已通过。

## 8. RP-04：本地任务持久执行

### Task RP-04.1：定义完整、限域的本地任务输入

**文件：** 修改 `internal/infrastructure/jobs/local_handlers_t06.go`、`internal/application/jobs/service.go`、`internal/application/jobs/ports.go`、`local_job_handlers.go`；新增 `internal/application/jobs/local_input.go`、`local_input_test.go`。

- [ ] 明确四种有版本的输入：thumbnail=project/version/尺寸；import=project/staged source/格式/原名；export=project/episode/质量/FPS/批准版本快照；migration=受管 snapshot hash/fingerprint/导入策略。
- [ ] RED 用例覆盖未知字段、缺少主体、跨项目主体、过期版本、不支持格式、无效尺寸/FPS/质量；输入不接受任意 OS 路径或脚本。
- [ ] DTO→持久 InputJSON→runner→handler 的每个参数逐字段测试，修复当前 export/thumbnail 参数丢失。
- [ ] 结果使用真实语义字段：章节数不能冒充 story entity 数，导入不能固定宣称创建 1 个 episode。
- [ ] 归属校验放在 application/repository 查询边界；runner 不直接执行 SQL。

**范围边界：** 此处 migration 指用户发起的 legacy snapshot 导入任务。启动数据库 schema migration 仍须在数据库/worker 初始化安全顺序中完成，不能为了“四类都入队”把启动迁移延后到正常 worker。导入 snapshot 新建项目时，根据导入策略和结果建立作用域，不伪造已存在的目标 project ID。

**运行：** `go test ./internal/application/jobs ./internal/infrastructure/jobs -run TestRP04 -count=1`。

### Task RP-04.2：修复导入与迁移的真实内容读取

**文件：** 修改 `local_job_handlers.go`、`internal/application/importing/service.go`、`internal/application/legacy/service.go`；复用 `internal/desktop/import_upload.go`、`internal/desktop/import_store.go` 的受管上传契约；按存储端口需要新增 `internal/infrastructure/database/local_job_sources.go`；新增 `local_job_handlers_test.go`。

- [ ] 创建受管原文 fixture，从 job 输入读回 Content/Format，断言 `importing.Import` 收到非空内容并生成可定位章节。
- [ ] 使用 FileStore/repository ports 加载内容；将组合根中直接 SQL 移回存储适配器。
- [ ] 复用已有有界读取、ZIP 预检和迁移 service；移除无上界 `io.ReadAll` 的新路径，不另写一个更弱的 ZIP 解包器。
- [ ] 测 hash 不符、源丢失、超限、Zip Slip/高压缩比、错误 project、取消时无半导入。
- [ ] 用迁移前 fixture→当前 schema 的真实 DB 测试，不对用户数据库操作。

**运行：** `go test . -run TestRP04Local -count=1`；`go test ./internal/application/importing ./internal/infrastructure/database -run 'TestRP04|Test.*Import|Test.*Migration' -count=1`。

### Task RP-04.3：保障执行幂等与崩溃恢复

**文件：** 修改 `internal/application/media/export_service.go`、`internal/infrastructure/jobs/runner.go`、`internal/infrastructure/database/jobs.go` 及相应 repository；新增 `internal/infrastructure/database/local_job_recovery_test.go`。

- [ ] 在“业务结果已提交、Job 尚未 succeeded”之间注入崩溃，再重启执行同一 job；导入/导出/迁移/缩略图各写一个测试。
- [ ] 使用 job ID/operation ID 记录业务效果及结果引用，恢复先读回已提交效果；不仅依赖提交时 idempotency key。
- [ ] 新写入使用事务和唯一约束；文件原子提交后数据库记录具备清晰补偿/重试路径，禁止事务包外部媒体处理。
- [ ] 测 cancel/timeout 后不产生新成功资产；中间文件清理不影响已批准内容。
- [ ] 需要新列/表时先新增前向迁移测试，再分配下一个空闲编号，绝不修改 000027～000031 或其他已发布迁移。

**运行：** `go test ./internal/infrastructure/database -run TestRP04LocalJobRecovery -count=1`；并发相关范围测试使用 `-race`。

### Task RP-04.4：接入用户提交与 Job Center

**文件：** 修改 `internal/desktop/importing_binding.go`、`internal/desktop/media_binding.go`、`internal/desktop/jobs_binding.go`、`job_wiring.go`、`web/src/components/studio/import-flow.tsx`、`web/src/components/studio/timeline-view.tsx`、对应 desktop clients；新增 `web/e2e/local-jobs.spec.ts`。

- [ ] 提供各能力窄提交命令，不暴露一个允许任意 jobType/任意 JSON 的通用后门。
- [ ] 大文件先分块写入受管 staging，再提交引用；UI 显示 job ID、进度、取消和结果入口。
- [ ] 保持现有同步业务 service 可供内部调用，但用户长任务走持久队列；失败不会显示导入/导出成功。
- [ ] 测 UI 提交→查询→取消→重启恢复→读回结果；明确浏览器 fixture 与原生桌面证据类别。
- [ ] 验证暂停、恢复、仅重试失败、关闭窗口后 worker 生命周期和已拥有进程清理。

**运行：** `go test ./internal/desktop . -run TestRP04 -count=1`；`npm --prefix web run test:e2e -- local-jobs.spec.ts`。

**验收映射：** FR-150、AC-FOUND-006、AC-STORY-001、AC-MEDIA-003、AC-E2E-003。

## 9. RP-05：声明式 Manifest 生产能力

### Task RP-05.1：补强 Manifest 契约与有效负例

**文件：** 修改 `internal/domain/provider/manifest.go`、`internal/domain/provider/manifest_t08_test.go`；新增 `testdata/provider-fixtures/manifest/` 下原创 JSON fixtures。

- [ ] 先提供一份能成功解码的完整基础 Manifest；每个负例只修改一个属性，并断言具体错误码/字段，防止所有用例都因放错字段而提前失败。
- [ ] 严格一次 JSON 文档 EOF、未知字段、能力词汇、方法、封闭占位符、合法 JSON body 模板、受限结果路径。
- [ ] 明确 Method/非敏感 Header allowlist 与类型化占位符；使用 JSON AST/值编码，不直接把 prompt 的引号、换行或反斜杠插入原始 JSON 字符串。
- [ ] Endpoint 只能是批准 base URL 下的相对路径；拒绝 scheme/host 覆盖、`//host`、用户信息、非法 query/fragment 和路径编码绕过。
- [ ] 轮询间隔、最大次数/时长、响应/下载大小均有服务端上限；Manifest 只能收紧边界，不能扩大。
- [ ] Headers 禁止用户直接声明 credential/cookie 值，Secret 仅在 Go 请求时解析；模板不支持表达式、脚本、文件或任意 JSONPath 执行引擎。

**运行：** `go test ./internal/domain/provider -run 'TestRP05|TestManifest' -count=1`。

### Task RP-05.2：版本化保存与受控适配器

**文件：** 修改 `internal/application/providers/service.go`、`internal/infrastructure/providers/registry.go`、`internal/infrastructure/database/providers.go`；新增 `internal/application/providers/manifest.go`、`internal/infrastructure/providers/manifest_adapter.go`、`manifest_adapter_test.go`；按需前向迁移。

- [ ] 定义不可变 manifest version/hash；配置只引用版本，新编辑不改变已提交任务的协议快照。
- [ ] 持久化只保存声明、非敏感配置和 secret ref；不把原始 Key 写入模板。
- [ ] 实现封闭模板映射和有界响应字段读取，所有请求经过现有 guarded client、DNS/redirect SSRF 防护、审计与文件下载器。
- [ ] 实现声明所需同步/异步提交、轮询、下载、取消状态映射；未知远端状态转可诊断错误，不猜成功。
- [ ] `httptest` 覆盖 happy path、429/5xx/timeout、坏 JSON、恶意结果 URL、重复响应和恢复，完成前不把能力 advertised 为可用。

**运行：** `go test ./internal/infrastructure/providers ./internal/infrastructure/providerhttp ./internal/infrastructure/database -run 'TestRP05|Test.*SSRF' -count=1`。

### Task RP-05.3：Manifest 管理与完整生产链测试

**文件：** 修改 `internal/desktop/providers_binding.go`、`web/src/services/desktop/providers.ts`、`web/src/components/layout/channel-editor-drawer.tsx`；新增 `web/src/components/layout/provider-manifest-editor.tsx`、`web/e2e/provider-manifest.spec.ts`；修改 `docs/USER_GUIDE.md`。

- [ ] 先做导入/验证预览，展示能力、目标域名、限制和版本；禁止把编辑器当 JavaScript 插件入口。
- [ ] 保存/启用/切换版本走窄命令，显示校验字段错误；未经批准的本地目标不自动放行。
- [ ] 走 UI 配置→Binding→数据库→registry→mock server→FileStore→Job成功 的真实集成链。
- [ ] 验证普通备份包含非敏感 Manifest 及版本、恢复后可使用，运行中任务仍引用旧版本。
- [ ] 补恶意 Manifest 从 UI 进入后的全路径拒绝测试，不能只测 domain decoder。

**运行：** `npm --prefix web test`；`npm --prefix web run test:e2e -- provider-manifest.spec.ts`；对应 Go 组合根测试。

**验收映射：** FR-140、AC-SEC-001、AC-E2E-006。

## 10. RP-06：Agent、Skill 与模型策略

### Task RP-06.1：可恢复的 Skill 版本与运行快照

**文件：** 修改 `internal/application/skill/skill.go`、`internal/application/agentassembly/assembly.go`、`internal/infrastructure/database/agent_repository.go`；新增 `internal/application/skill/version_service.go`、`internal/infrastructure/database/skill_versions_test.go`。

- [ ] 盘点现有 skill_versions、agent_runs、FileStore 内容和 builtin pack hash，复用 loader 的 Schema/Tool ACL/路径校验。
- [ ] 建立可逆的版本内容表示：manifest + 按路径保存的文档清单；不再只保存不能可靠拆分的拼接文本。
- [ ] 版本单位继续是整个 Skill pack：修改一个 Agent 文档派生 pack 新版本。历史拼接内容不可恢复时保留审计信息、明确不可激活，不能捏造可执行文档。
- [ ] 创建版本写入不可变内容及 hash；active 指针切换与 revision 检查同事务完成，回滚是切换指针，不覆盖旧内容。
- [ ] 修复启动注册对“最新 active”与“精确 key/version”的混用；用户创建新版本后重启不会被 builtin 注册覆盖。
- [ ] 同一次 run 的文档、version ID、hash 原子取快照；并发切换不能使记录 hash 与实际发送内容不一致。

建议在使用方定义并一次性获取如下快照，不再分别读取文档和当前版本 ID：

```go
type SkillSnapshot struct {
    AgentKey    string
    VersionID   string
    ContentHash string
    Document    string
    Spec        agent.Spec
}
```

必测时序：调用 A 获取 v1 快照 → 用户激活 v2 → A 仍执行并记录 v1 → 调用 B 使用 v2 → 回滚 v1 只影响之后的调用。

**运行：** `go test ./internal/application/skill ./internal/application/agentassembly ./internal/infrastructure/database -run TestRP06Skill -count=1`；并发范围使用 `-race`。

### Task RP-06.2：Skill 查看、创建版本、回滚界面

**文件：** 修改 `internal/desktop/agent_binding.go`、`web/src/services/desktop/agents.ts`、`web/src/components/studio/agent-center.tsx`；新增 `web/src/components/studio/agent-skill-editor.tsx`、`web/e2e/agent-skills.spec.ts`；更新 i18n。

- [ ] 接通已有 AgentSkillDocument 的查看入口，显示实际版本/hash 与来源，不展示“当前文档”冒充历史 run 文档。
- [ ] 禁用 Agent 仍允许查看 Skill 和历史；文档查看与执行授权分别校验，不能复用执行 gate 把查看也关闭。
- [ ] 提供“基于版本创建新版本”和“切换到历史版本”，使用 RP-06.1 服务，不手写文件路径或替换 embedded 包。
- [ ] 编辑只允许获准的文本/声明变更；工具权限和模型能力仍由后台注册表限制，用户 Skill 不能升级 Supervisor 写权限。
- [ ] 测无效 schema、非法工具、重复版本、并发冲突、取消编辑、回滚后的新 run 与旧 run 分离。
- [ ] 创建→启用→运行→创建下一版→回滚→重启，确认历史引用和备份恢复可追溯。

**运行：** `go test ./internal/desktop -run TestRP06Skill -count=1`；`npm --prefix web test`；`npm --prefix web run test:e2e -- agent-skills.spec.ts`。

### Task RP-06.3：持久 Agent 启停与可见状态

**文件：** 修改 `internal/application/agentassembly/assembly.go`、`internal/desktop/agent_binding.go`、`web/src/components/studio/agent-center.tsx`、`internal/infrastructure/database/agent_repository.go`；扩展 `internal/application/agentassembly/agent_management_t09_test.go`。

- [ ] 为“禁用→重启→仍禁用”写 RED；明确全局或项目级作用域，由 RP-00.2 决策记录，不混用两者。
- [ ] 保存启停偏好并在装配时恢复；启动失败状态可见，不静默按 enabled 继续。
- [ ] 新 run 在读取 Skill/调度时检查禁用；运行中的任务默认继续既有快照，取消必须用已有明确取消命令。
- [ ] 多窗口更改使用 revision，UI 读真实状态而非仅翻转本地 checkbox。
- [ ] 保留历史 runs、Skill 内容和审计记录，禁用不删除任何版本。

**运行：** `go test ./internal/application/agentassembly ./internal/infrastructure/database ./internal/desktop -run TestRP06AgentEnabled -count=1`。

### Task RP-06.4：只读测试与诊断导出闭环

**文件：** 修改 `internal/application/agentassembly/assembly.go`、`internal/application/agentruntime` 对应 runner、`internal/desktop/agent_binding.go`、`web/src/components/studio/agent-center.tsx`；新增 `internal/application/agentassembly/readonly_probe_test.go`；复用 `internal/application/diagnostics`。

- [ ] 定义只读测试输入/结果 DTO，返回 validation、允许工具、版本/hash 和安全错误；不得返回私有推理链。
- [ ] 测试运行使用只读工具集合；即使 Skill 请求业务写操作，也必须在授权层拒绝并记录。
- [ ] 生产界面默认只做离线静态预检；真实 LLM 只读试跑必须由用户显式选择 Provider/模型并确认调用，不能随打开界面自动发生。deterministic Mock 只在测试进程注入，绝不作为产品的成功运行路径；静态预检不得标成模型运行通过。
- [ ] 测运行前后业务实体版本/文件/队列无变化；允许的测试审计记录与业务写入分开。
- [ ] 从 Agent 页接已有脱敏诊断预览/导出能力，不新建包含敏感原始请求的诊断包。

**运行：** `go test ./internal/application/agentassembly ./internal/application/agentruntime ./internal/application/diagnostics -run TestRP06ReadOnly -count=1`。

### Task RP-06.5：项目、阶段与单次模型策略解析

**文件：** 修改 `internal/application/agentassembly/assembly.go`、`internal/infrastructure/database/drama_settings.go`、`internal/desktop/drama_binding.go`、`internal/desktop/agent_binding.go`；新增 `internal/application/agentassembly/model_policy.go`、`internal/application/agentassembly/model_policy_test.go`；前端修改对应项目设置和 Agent 页。

- [ ] 对照已有按层模型策略，不重复实现；仅补未支持的阶段和单次覆盖。
- [ ] 解析顺序固定：单次覆盖 > 阶段覆盖 > 项目按能力/层默认；没有匹配时明确拒绝或使用已批准的显式默认，不能偷偷换供应商。
- [ ] 请求前验证 Provider enabled、模型能力、超时/预算上限；Decision/Execution/Supervision 独立取策略并独立调用。
- [ ] 把最终 provider/model/覆盖来源写入 run 快照；历史 run 不随设置变化重写。
- [ ] 增加三层不同模型、无效阶段、禁用 Provider、并发变更、清除覆盖回退的表驱动测试和 UI 读回。

**运行：** `go test ./internal/application/agentassembly ./internal/desktop ./internal/infrastructure/database -run TestRP06ModelPolicy -count=1`；`npm --prefix web test`。

**验收映射：** FR-090/100/140、AC-AGENT-001～005；不加入任意工具/脚本执行。

## 11. RP-07：资产许可与最终内容审核

### Task RP-07.1：许可元数据读写到用户界面

**文件：** 修改 `internal/domain/asset/asset.go`、`internal/application/assets/service.go`、`internal/infrastructure/database/assets.go`、`internal/desktop/assets_binding.go`、`web/src/services/desktop/drama.ts`、`web/src/pages/studio/sections.tsx` 的 AssetsSection/AssetVersionsDrawer；新增 `web/src/components/studio/asset-license-editor.tsx`；扩展 `internal/infrastructure/database/usage_params_t05_test.go`。

- [ ] 先验证 AssetDTO 返回 `license`、`licenseSource`、`allowsExportUse`；未知/null 与禁止/false 严格区分。
- [ ] Application 新增受限命令，参数包括 assetID、expectedRevision、许可说明、来源和 tri-state 使用许可；复用已有数据库方法并补并发检查。
- [ ] 校验长度、项目归属、revision、来源字段格式；不自动抓取用户输入 URL，也不让 Agent 冒充用户确认许可。
- [ ] UI 允许未知/允许/禁止，并保存后重新读取；许可变更写审计/失效标记，必要时使旧 final review 待复核。
- [ ] 测未知→允许→禁止→重启读回、旧 revision 拒绝、普通备份恢复和 Final 逐资产定位。

**运行：** `go test ./internal/application/assets ./internal/desktop ./internal/infrastructure/database -run TestRP07License -count=1`；`npm --prefix web test`。

### Task RP-07.2：通过应用端口接入真实媒体内容分析

**文件：** 修改 `internal/infrastructure/media/content_analysis_t16.go`、`internal/infrastructure/media/content_analysis_t16_test.go`、`internal/application/consistency/final.go`、`agent_wiring.go`（当前 WithFinalRuleset 装配处）、`media_wiring.go`；新增 `internal/application/consistency/content_analysis.go`、`internal/application/consistency/content_analysis_test.go` 与必要的 `final_content_analysis.go` 组合适配；检查 `internal/infrastructure/database/final_reader.go`。

- [ ] 定义 ContentAnalyzer 应用端口，由组合根注入 FFmpeg 适配器；数据库 reader 只提供受管媒体引用，不在 SQL repository 内运行外部进程。
- [ ] 返回带 asset/version/hash、规则版本、区间、分析状态的结果；未知/失败不能表示“无黑帧/无静音”。
- [ ] 针对批准版本调用分析：有界进程时长、输出大小、取消、临时文件清理；只使用受管文件路径和参数 argv。
- [ ] 先 probe 实际流，再选择视频/音频分析；纯音频、无音轨视频都不因不存在的流失败。多次子进程共享总 deadline，不把每次 timeout 相加成更长无界检查。
- [ ] 用黑色视频、有声普通视频、静音片段、坏文件、取消/timeout fixture 测试。黑场阈值和预期静音策略具名且版本化；黑帧不一概等于错误。
- [ ] 分析缓存如需要，以内容 hash + 分析器/规则版本键控；避免同一不变素材重复执行，版本变化不能复用陈旧结果。

**运行：** `go test ./internal/application/consistency ./internal/infrastructure/media -run TestRP07Content -count=1`。

### Task RP-07.3：Final Review 整合、定位与失效测试

**文件：** 修改 `internal/application/consistency/final.go`、`internal/infrastructure/database/final_reader.go`、质量中心现有展示处；新增 `internal/infrastructure/database/final_content_test.go`。

- [ ] 把分析区间转换为结构化 issue，带 Shot/AssetVersion/时间区间证据；保留 deterministic 来源，不伪装 LLM 结论。
- [ ] 未安装引擎时 UI 显示“未完成内容分析”，元数据检查仍可运行，但不能给内容质量全通过。
- [ ] 许可未知、禁止、对白缺行、仅 BGM、要求视频却为图片等既有规则与分析规则合并并去重。
- [ ] 换批准版本或更新许可后旧 final review 标陈旧；人工豁免必须有理由和审计，不能修改原报告冒充已修复。
- [ ] 真实导出→ffprobe→内容分析→Final Review→问题跳转→修订→再审核走通一次。

**运行：** `go test ./internal/infrastructure/database ./internal/application/consistency -run 'TestRP07|Test.*Final' -count=1`。

**验收映射：** FR-110、FR-160、AC-MEDIA-003、AC-E2E-004。

## 12. RP-08：真实音效生成

### Task RP-08.1：选择并固定一个可验证的音效协议

**文件：** 修改 `docs/VENDOR_PROTOCOL_CROSSCHECK.md`、`docs/ROADMAP.md`；新增 ADR 与 `testdata/provider-fixtures/effects/` 下原创协议 fixtures。

- [ ] 区分“音效建议/音频导入已可用”与“真实音效生成尚未交付”，保留现有 unsupported 行为。
- [ ] 核对选定供应商当前官方音效接口、输入字段、格式、限制、状态机、取消、鉴权和结果许可；未经选择不猜厂商协议。
- [ ] 写离线 fixture 与协议矩阵；授权、账户和费用条件只阻塞真实冒烟，不阻塞安全的离线适配实现。
- [ ] 优先标准 HTTP 和现有依赖；需要新 SDK 时另行走依赖授权。

**通过条件：** 有具名协议、版本/日期、可测试合同；否则任务保持 `DECISION_REQUIRED`，不宣称交付。

### Task RP-08.2：适配、收集、采用与混音

**文件：** 修改 `internal/infrastructure/providers/registry.go`、`internal/infrastructure/providers/openai_effect.go`（保留不支持家族的拒绝）、`provider_wiring.go`；新增 `internal/infrastructure/providers/effect_http.go`、`effect_http_test.go`；扩展 `internal/infrastructure/jobs/effect_generation_t03_test.go`、`web/src/components/studio/timeline-view.tsx`。

- [ ] 实现 RP-08.1 选定协议到 EffectPort 的映射；不能用 `/audio/speech` 朗读描述假装音效。
- [ ] 测鉴权、错误、取消、结果大小、MIME/magic、SSRF、审计脱敏；适配器未具备真实能力时不启用 UI 生成按钮。
- [ ] Job→本地文件→资产版本→shot usage `audio_effect`→调音→混音导出走通。
- [ ] 重试/重启不重复资产，重做某镜头不改变其他镜头；实际音效验证留给 RP-11.2。

**运行：** `go test ./internal/infrastructure/providers ./internal/infrastructure/jobs ./internal/infrastructure/database -run TestRP08 -count=1`；`npm --prefix web test`。

## 13. RP-09：设置能力与范围差异

### Task RP-09.1：逐项核对 FR-180 设置清单

**文件：** 修改 `docs/SETTINGS_INVENTORY_T10_T11.md`、`docs/implementation/gap-closure-scope-decisions.md`；读取 `job_wiring.go`、`backup_scheduler.go`、`embedder.go`、`embedder_local.go` 及设置 UI。

- [ ] 逐项登记数据目录、缓存上限、Worker 并发、Provider 超时、默认模型策略、记忆策略、日志级别、FFmpeg 路径、自动备份、主题、语言。
- [ ] 每项标注：实际存储、读入口、写入口、是否可由用户修改、重启生效规则、测试、需求差异；“常量”和“只读展示”不能记“可配置”。
- [ ] 将缺漏的默认模型策略映射 RP-06.5；补记忆策略（阈值/TopK/token budget/local/provider隐私）实际可配置边界。
- [ ] 超时允许在硬安全上限内收紧，不以“可配置必然不安全”为理由自动豁免；FFmpeg 路径由本机用户选择与 Agent 任意执行是不同权限边界。
- [ ] 将需要产品决定的目录迁移、FFmpeg 分发/路径、缓存管理、加密备份分别记录；未获决定仅阻塞相关子项。

**验收：** 11 类设置有完整证据，不再以一份裁定文档代替缺失能力。

### Task RP-09.2：应用设置与可配置自动备份

**文件：** 修改 `backup_scheduler.go`、`backup_scheduler_t12_test.go`、`app.go`；先复用现有适合的设置存储，若不存在再新增 `internal/application/settings/service.go`、`internal/infrastructure/database/app_settings.go`、`internal/desktop/settings_binding.go`、`web/src/components/settings/app-settings-panel.tsx` 及测试。

- [ ] 用假时钟测试停用/启用、频率修改、保留数量、重启后读回、失败不删旧备份和只清理自家前缀。
- [ ] 持久设置使用 revision；默认值保持当前每日/3份；参数范围按磁盘/负载策略在 ADR 中明确，不允许 0/负数造成无限循环或误删全部。
- [ ] 将 scheduler 依赖从常量切为已验证设置快照，变更重新调度，不遗留 goroutine；关闭应用先取消并等待自有任务。
- [ ] UI 提供最近一次结果、下次时间、存储位置、失败诊断；不在备份设置页面接触 Secret 内容。
- [ ] 应用设置普通备份恢复后可读，非法历史值使用有告警的安全兼容策略。

**运行：** `go test . -run 'TestRP09|Test.*BackupScheduler' -count=1`；新增 settings 服务/数据库测试；`npm --prefix web test`。

### Task RP-09.3：其余设置实现或经批准的范围同步

**文件：** 按 RP-09.1 决策修改 `job_wiring.go`、`internal/infrastructure/providerhttp`、`internal/infrastructure/logging`、`internal/application/memory`、对应设置 Binding/UI 和 `docs/USER_GUIDE.md`；新文件路径在该包选定分支后先补入本计划。

- [ ] Worker 并发变更有上限、取消与在途任务语义，不通过改常量重编译让用户配置。
- [ ] Provider 超时只能在系统上限内取值；日志级别不关闭脱敏和轮转；记忆策略禁止未授权外发本地文本。
- [ ] 本地 ONNX 配置如进入本轮：用户选择受管/明确路径、验证文件/版本/hash，健康页区分 local/provider/fallback，不静默上传。
- [ ] 数据目录迁移如获准：预检→备份→停止写入→复制校验→原子切换→失败回滚；绝不直接 Move 用户目录。否则保留为待办并同步 PRD 范围决定。
- [ ] FFmpeg 路径与缓存上限按明确决定实现或延后；缓存删除先预览、校验引用、支持取消，不删除批准资产/源文档/活跃任务文件。

**运行：** 对所选设置的边界、重启、恢复、失败回滚执行表驱动测试；`go test ./... -count=1`、`npm --prefix web test` 在包结束运行。

**说明：** 此任务不是允许“随意选做”；每个原 PRD 条款最终必须对应实现证据或具备来源的产品范围决定，未决项保持未完成。

## 14. RP-10：可复现验证与发布门

### Task RP-10.1：SBOM 差异调查与确定性检查

**文件：** 修改 `scripts/gen-sbom.mjs`、`.github/workflows/desktop-build.yml`、`sbom/cyclonedx.json`、`sbom/licences.json`、必要 `THIRD_PARTY_NOTICES.md`；新增 `scripts/gen-sbom.test.mjs`。

- [ ] 保存当前 drift，记录 Go 工具链、module cache、node_modules、lockfile hash，逐字段比较来源，而不是先覆盖两份文件。
- [ ] 修复生成器显式默认 `GOTOOLCHAIN=go1.25.0` 与 go.mod toolchain 的冲突；尊重项目工具链，缺环境时返回具体错误。
- [ ] 将“依赖解析/许可事实采集”和“确定性序列化”边界分清；按锁文件预备依赖的专用步骤与只读 `--check` 分开，禁止检查模式偷偷修文件。
- [ ] `--check` 不仅比较字节，还必须执行 unknown linked/restrictive license 策略；把目前早 return 绕过后续政策检查的情况写成负例。
- [ ] 测固定 metadata fixture 两次产物一致、缺缓存诊断、未知 linked license、限制性许可证和漂移均非零退出；不把未知许可编成 MIT。
- [ ] 在正确依赖环境重生成并审阅真实依赖差异，更新 notices，然后 CI 在同等环境检查。

**运行：** `node --test scripts/gen-sbom.test.mjs`；`node scripts/gen-sbom.mjs --check`。

**通过条件：** 可解释的生成输入；两次同输入一致；许可策略与 drift 均有失败证据；无新增未经批准依赖。

### Task RP-10.2：verify/CI 门一致及安全进程清理

**文件：** 修改 `scripts/verify.sh`、`scripts/verify.ps1`、`.github/workflows/desktop-build.yml`、`web/package.json`（仅确需命令修正）、`web/playwright.config.ts`；新增 `scripts/verify-contract.test.mjs`。

- [ ] 建矩阵对齐两脚本：gofmt、vet、Go tests、typecheck、前端 tests、build、Playwright、MONOFORM、fixtures、SBOM、安全扫描、Wails；严格模式下必要环境缺失变 FAIL。
- [ ] 当前 lint 已存在但实质是 tsc 重复门：如实命名，不宣称 ESLint 检查；不为追求标签新增未授权依赖或全仓格式化。
- [ ] 以 fake tool executables 测某门返回非零、工具缺失、无测试匹配、严格/开发模式，确认退出码没有被后续 echo 覆盖。
- [ ] verify 清理仅作用于本次启动并记录的 PID/进程树；移除按 5173 端口杀进程的逻辑，测试预先存在的用户服务不被关闭。
- [ ] Playwright 的 dev server 显式 loopback，不依赖 `dev` 的 0.0.0.0 默认；退出后无自建服务遗留。
- [ ] CI fixtures 准备 SBOM 所需 Go/npm metadata；单列浏览器 E2E、MONOFORM 构建、race、安装包与 artifact 上传，失败保留日志/trace。
- [ ] 签名身份策略明确后，在本包准备可选的签名集成配置和顺序测试；真实凭据/真实签名仍留 RP-12.2。这样发布冻结后不必为了接流水线再修改源码；若 RP-12.2 才发现必须改配置，返回 RP-12.1 重新提交、CI 和构建。
- [ ] 固定 SBOM 对应目标 GOOS/GOARCH/CGO；不同目标 linked closure 如不同，应有目标清单而不是强求跨平台字节一致。固定环境重复生成验证确定性。
- [ ] 纳入有说明的格式基线和依赖漏洞审计；已有大范围格式债不得靠全仓无关重写清零。漏洞工具未提供时先按仓库既有流程准备，失败/未知不能当 PASS。
- [ ] 检查安装模板来源是否受版本控制：当前 `build/windows/installer/project.nsi` 存在但未被 `git ls-files` 跟踪。把必须的 NSIS 定制保存在拟新增 `packaging/windows/project.nsi` 等受版本控制来源，并在构建前显式复制/校验；不能仅修改被忽略的生成模板。增加干净 checkout 含声明文件的安装包测试。
- [ ] 加 Binding 再生成差异检查，确保 Go DTO 与 TS 产物一致；构建前后保留原有 `web/dist/.gitkeep` 状态，不覆盖用户已有删除意图。

**运行：** `node --test scripts/verify-contract.test.mjs`；`bash -n scripts/verify.sh`；`STRICT_VERIFY=1 bash scripts/verify.sh`；PowerShell 中 `$env:STRICT_VERIFY='1'; ./scripts/verify.ps1`。

**通过条件：** 两平台入口具有相同必要门语义，跳过不是通过；本机全绿与远程 CI 全绿分别记录。

### Task RP-10.3：ONNX、FFmpeg、race 的严格配置矩阵

**文件：** 修改 `internal/infrastructure/onnxemb/embedder_test.go`、`embedder_local.go`、`embedder.go`、`internal/infrastructure/media` 对应集成测试、CI 与 verify；新增 `scripts/check-release-prerequisites.mjs`；更新 `docs/EMBEDDING_MODEL_DISTRIBUTION.md`。

- [ ] 检查 CGO=0 下 ONNX 测试文件因 build tag 完全不参与：严格模式必须在测试外验证 CGO/runtime/model/tokenizer 条件，不能让 `[no test files]` 变绿色推理证据。
- [ ] `IA_REQUIRE_ONNX=1` 覆盖模型加载失败、多语言模型缺失、中文 tokenizer 缺失等所有相关 skip；开发模式允许明确降级。
- [ ] 用固定中英文相关/无关文本测试实际推理、维度、有限值、排序与来源，不仅测试 runtime 能加载。
- [ ] 模型、tokenizer、运行时使用同一份固定 revision/hash 资源清单；测试读取生产同样的 `IA_ONNX_RUNTIME`/模型/词表配置，不依赖历史机器的 Python 固定路径。单语 MiniLM 不得替代中文/跨语言验收所需模型。
- [ ] 模型/运行时来源、许可证、SHA-256 可复核；不把大型模型或运行时二进制自动提交到 Git。
- [ ] 严格媒体门要求 ffmpeg/ffprobe 并验证真实输出可解码、有音轨/字幕；缺外部引擎不能算 PASS。
- [ ] CGO/race 与普通性能测试分开；正常构建跑性能上界，race 跑并发正确性；审核每个 skip 原因，不新加泛化 skip 掩盖失败。

**运行：** 正常 `go test ./... -count=1`；准备好受支持 CGO 工具链后 `go test -race ./... -count=1 -timeout 25m`；`IA_REQUIRE_ONNX=1` 对 ONNX 包运行 `go test -v -count=1`，检查测试数量与零必要 skip。

## 15. RP-11：产品验收与受控真实验证

### Task RP-11.1：补行为测试与完整离线主旅程

**文件：** 扩展 `web/e2e/`、`web/src/services/__tests__/`、`internal/infrastructure/database/e2e002_steps_test.go`、`internal/infrastructure/database/canary_production_test.go`、既有媒体/恢复验收测试；新增 `docs/implementation/gap-closure-test-matrix.md`。

- [ ] 把 RP-01～09 的每个断链映射到至少一个能通过真实边界重现的回归用例；保留有价值的源码扫描但不将其当唯一验收。
- [ ] 用现有原创 3万字/多章节 fixture 走导入→事实确认→3集规划→第1集骨架/策略/剧本→2角色2场景→12镜分镜→图/视频/音频→导出。
- [ ] 在每阶段查询真实数据库和文件 hash，验证模型/任务/版本/上游引用；Mock 只在测试边界，不能进入正式 registry 冒充生产。
- [ ] 故意插入服装错误、Schema 修复失败、非法 Tool、跨项目引用、取消/重启、重复提交，确认不越过质量门、不重复副作用。
- [ ] 回归旧画布、迁移、备份篡改拒绝、恢复原子性、普通备份无 Secret，以及键盘/焦点/i18n。

**运行：** 当前完整 verify；`npm --prefix web run test:e2e`；`go test ./... -count=1`。

**通过条件：** AC-E2E-001～006 各有对应测试/日志，不因某一包测试通过直接标全部 AC 通过。

### Task RP-11.2：受控真实 Provider 冒烟

**文件：** 修改 `docs/VENDOR_PROTOCOL_CROSSCHECK.md`；新增 `docs/implementation/provider-smoke-template.md` 及脱敏执行记录。

- [ ] 明确授权的 Provider/模型/能力、最大请求数、费用上限和测试素材；默认不外发真实用户原著或敏感项目。
- [ ] 使用最小输入分别验证文字/图片/Gemini、视频、TTS、已选音效能力；哪些未纳入本次声明逐项写明。
- [ ] 视频测试合法时长/尺寸/参考条件、提交/轮询/下载/取消/恢复；TTS/音效核对实际声音而非只看 HTTP 200。
- [ ] 将结果收集为候选、采用、导出、ffprobe和实际播放；确认来源与费用审计、失败与取消不会生成假成功。
- [ ] 出现协议差异先保存脱敏 fixture，再回离线修复；禁止反复无预算地尝试真实付费调用。

**外部条件：** 明确付费测试授权、可用账户/模型权限；未授权时保持 BLOCKED，不向用户索取明文 Key 到聊天。

### Task RP-11.3：性能及 PRD §14 产品指标

**文件：** 修改 `web/e2e/render-perf-t27.spec.ts`、既有 scale 测试；新增 `docs/implementation/product-metrics-protocol.md`、`docs/implementation/product-metrics-results.md`；必要新增 `scripts/summarize-acceptance-metrics.mjs`。

- [ ] 先写测量协议：硬件/版本/冷暖启动/样本数/超时/排除规则/分母/失败分类；阈值和样本策略预先固定，不看到结果后修改。
- [ ] 测 1000节点/2000边平移缩放、项目打开 P95<3s、普通 DB 命令 P95<100ms、100k汉字导入、10k素材/记忆、500任务和长运行内存。
- [ ] 注入高频 Job events，断言合并后的普通进度写入 UI 不超过每秒 10 次，且成功/失败/取消终态不被节流吞掉或长期延迟。
- [ ] 单次整面重渲染 737.9ms 的历史数字不能代替打开 P95 或原生 VM 流畅度；新增目标量的采样。
- [ ] PRD 产品指标逐项计算：新用户导入→骨架 ≥90%、恢复 ≥95%、连续性定位 ≥90%、迁移节点/边不丢失 100%、备份泄漏 0、有效操作步骤对照。
- [ ] PRD 质量指标逐项计算：首次 Schema ≥90%、一次修复后 ≥98%、审核实体定位 ≥95%、任务/文件一致 100%、幂等重复资产 0。
- [ ] Mock 指标只证明机制；真实模型质量样本需 RP-11.2 授权并独立标注，不能以 deterministic Mock 的 100% 宣称真实成功率。
- [ ] 产出 numerator/denominator、原始脱敏记录和局限；样本不足记未验收，不虚构统计把握度。

## 16. RP-12：干净发布、原生验收与封板

### Task RP-12.1：授权后的提交、远程 CI 和干净重建

**文件：** 审阅 `.github/workflows/desktop-build.yml`、`T32_COMMIT_STEPS.sh`、`.gitignore`、全部待提交范围；不得直接执行历史脚本的批量 staging。

- [ ] 用户明确授权 commit/push 后，逐包核对 diff、未跟踪文件、secret、生成物和第三方声明；只 stage 获准的精确路径。
- [ ] 使用 Lore 提交记录原因、约束、验证与未测试项；不 reset 历史、不用临时真实 commit 再回滚来“测试提交”。
- [ ] 按实际分支规则触发 PR/远程 CI 或获准的 workflow_dispatch；dry-run push 不等于远程 CI 已执行。
- [ ] 若创建 PR，使用 Codex attach_artifact 关联本聊天；不得把后台参考 PR 当本任务 PR。
- [ ] 在包含已提交变更的干净 checkout 重建，记录 commit/toolchain/依赖/hash与全部 job URL；无关 `.omx`、用户分析稿、数据库、媒体、Key和本机工具链脚本不混入发布。

**交接：** 本任务产出待签名候选；随后 RP-12.2 生成最终分发字节，RP-12.3 验收这些字节。任何修复导致源码/工件变化时，只重跑受影响门及必要完整发布门，并更新全部关联证据。

**外部条件：** Git 写操作和远端发布的明确授权；本文件本身不授予这些权限。

### Task RP-12.2：签名身份与流水线

**执行性质：** 优先使用 RP-10.2 已纳入干净基线的流水线完成真实签名和记录。下面列出的文件用于确认配置及必要修正，不授权在已验证候选上静默改源码；一旦修正影响构建，必须回 RP-12.1 形成新候选。

**文件：** 修改 `docs/T29_SIGNING_STRATEGY.md`、`docs/INSTALL_AND_SIGNING.md`、`.github/workflows/desktop-build.yml`、RP-10.2 建立的受控 NSIS 来源 `packaging/windows/project.nsi` 与生成步骤；`build/windows/installer/project.nsi` 仅作构建输入，不把未跟踪临时产物当唯一源码。

- [ ] 统一内部测试版/公开分发的签名要求及证书来源；不能混用硬件/HSM要求和不适用的明文 PFX 模板。
- [ ] 按选定证书服务接入签名及时间戳；凭据走受控 Secret，不写 Git/日志/普通备份。
- [ ] 检查 EXE、安装包以及实际安装后卸载器的签名；不假设外层安装包签名自动覆盖内部可执行文件。
- [ ] 验证签名主体、证书链和时间戳；错误证书/签名失败阻止受该政策约束的发布作业。
- [ ] 签名后重新计算 hash；VM 验收必须对应最终分发字节，必要时补装测。

**验证：** `signtool verify /pa /v <实际待发文件>`；逐文件 `Get-AuthenticodeSignature`；不在计划中嵌入证书口令或真实 Secret 名值。

### Task RP-12.3：原生 Wails 与干净 Windows VM 手动验收

**前置：** RP-12.1 的干净源码候选与 RP-12.2 中符合本次分发政策的最终工件；此前走查仅标预验收。内部未签名 RC 如获准可另验，但不得复用其结果宣称不同字节的已签名正式包通过。

**文件：** 修改 `docs/T25_VM_ACCEPTANCE_CHECKLIST.md`；新增 `docs/implementation/native-acceptance/README.md` 与对应候选版本记录；更新 `docs/USER_GUIDE.md`。

- [ ] 固定待测 commit、EXE/installer hash、Windows/WebView2/FFmpeg/ONNX 版本和独立测试数据目录；先备份，禁止真实用户项目做破坏性测试。
- [ ] 不安装 Node 的标准用户环境安装并启动；检查窗口、Binding、数据目录、退出与重启。
- [ ] 按 RP-11.1 的可见页面逐段手动操作；每段写“入口、输入、操作、预期、实际、截图/日志、AC”。浏览器 Mock 不能替代本条。
- [ ] 执行旧版本升级保留数据、卸载不误删用户数据、中文/空格路径、WebView2有无、断网、依赖缺失、备份恢复。
- [ ] 在视频轮询、工作流、导入/导出运行中强杀专用测试进程，再启动读回；只杀自己启动的 PID。
- [ ] 每个缺陷附最小复现；失败回流对应 RP 包修复，再重测受影响路径；不因 VM 未准备而伪装完成。
- [ ] 对最终构建重测 RP-11.3 中与构建、WebView2、安装路径相关的原生性能；质量样本如受模型/Skill变化影响也须重新评测。
- [ ] 不为手动验收降低生产 SSRF 或把 Mock 注册到正式 Provider；需要离线原生数据时使用独立受限测试 harness/验收构建，报告清楚它与正式版的差别。

**外部条件：** 干净 VM/快照与人工交互条件。缺失时仅本任务 BLOCKED，不阻塞纯代码包。

### Task RP-12.4：文档、版本、工件与验收封板

**文件：** 修改 `README.md`、`docs/USER_GUIDE.md`、`docs/RELEASE_CHECKLIST.md`、`docs/INSTALL_AND_SIGNING.md`、`docs/implementation/STATUS.md`、`docs/implementation/TRACEABILITY.md`、`docs/ROADMAP.md`、`docs/T25_VM_ACCEPTANCE_CHECKLIST.md`。

- [ ] 统一当前功能、限制、环境准备与操作入口；历史段明确日期和“已被替换”，不把当前行与历史结论混排。
- [ ] 处理缺失的 `project-progress-audit-2026-09-26.md` 引用：能找到原件则核对后恢复来源；找不到则标缺失并链接本次新证据，不伪造旧审计。
- [ ] 工件、签名、SBOM、安装包内声明、用户指南、VM和真实 Provider记录全部引用同一候选版本；不得用压缩后尺寸变化推断声明确实安装。
- [ ] 安装后逐一检查 LICENSE、THIRD_PARTY_NOTICES 和 SBOM 内容与原文件 hash；真实解析并播放导出成片。
- [ ] 将每个 RP/FR/AC 状态写为 PASS/FAIL/BLOCKED/获准延期，并附来源；必要项未闭合不得标全部 PRD 完成。
- [ ] 发布说明写清环境依赖、仍未支持能力和升级/恢复步骤；发布动作仅在获准时执行。

**证据版本：** 封板报告可以作为单独文档提交引用被验收的源码 commit 与最终工件 hash。若同时修改任何影响产品、依赖、打包或签名的文件，必须重新冻结并重跑相应发布门，不能仍引用旧工件为新代码背书。

## 17. O01～O06：后续版本清单（不混入当前包自动实施）

| ID | 细化任务与入口 | 进入条件/验收 |
|---|---|---|
| O01 大规模记忆 | 在 `internal/application/memory` 与对应 database 查询增加窗口外关键事实评测；比较扩大候选、分层召回、索引方案；保持 scope/阈值/token budget；需要 sqlite-vec 时先走依赖/安全 ADR | 先证明 500 候选窗口的召回缺口；含跨项目零泄漏和10k/更大规模 latency/recall 对照；不默认引入动态扩展 |
| O02 视频 size UI | 已并入 RP-03.1，避免重复创建一个任务 | 供应商枚举、画幅、请求参数与回读一致 |
| O03 macOS/Linux | 明确版本范围；实现 Keychain/Secret Service 安全适配；平台构建、签名/分发、文件路径、升级恢复与媒体依赖验收 | RP-00.2 先解决 PRD/Acceptance 版本冲突；若选为当前版本必须项，转入当前关键路径，不能继续标“可选” |
| O04 v1.5 | 工作流模板版本/导入校验；模型路由与成本预算；资产批量替换及影响预览/回滚；更大画布性能；外部剪辑格式可行性与适配 | 各自独立 PRD/测试规格；预算不得变成擅自付费调用；外部格式版本冲突先对账 |
| O05 加密敏感备份 | 明确范围、威胁模型、KDF/认证加密策略、Secret选择、密码不保存、错误密码/篡改/恢复失败测试 | ADR-0016 延后记录与 PRD FR-170 对齐；若纳入发布则独立安全工作包；不通过普通备份偷偷携带 Key |
| O06 v2 云与团队 | 团队/云同步/多租户/API/PostgreSQL/向量索引/对象存储/Worker集群/订阅Credits 分别建需求与迁移方案 | 不在本轮实现，不以 SaaS 绕过本地数据或工作流缺陷 |

## 18. 统一验收矩阵

| PRD/验收 | 主要任务 | 不能省略的证据 |
|---|---|---|
| G1、FR-001/130、AC-LEGACY/CANVAS | RP-11.1/RP-12.3 | 旧数据完整、画布行为、领域投影与删除边界 |
| G2、FR-020～070、AC-STORY/SCRIPT/ASSET/BOARD | RP-04、RP-06、RP-07、RP-11 | 数据库读回、版本和事实引用；已完成项回归 |
| G3、FR-090/100/150、AC-AGENT/FOUND-006/E2E-003 | RP-04、RP-06、RP-11 | 真重启、执行幂等、三层权限、历史 Skill 快照 |
| G4、FR-110/120、AC-MEM/E2E-004/005 | RP-06、RP-07、RP-09、RP-10.3、RP-11 | 真实媒体分析、记忆来源/隔离、ONNX明确测试配置 |
| G5、FR-140、AC-FOUND-005/SEC/E2E-006 | RP-02、RP-03、RP-05、RP-08、RP-11.2 | 真实路由、安全负例、离线与付费证据分离 |
| G6、FR-050/080/160、AC-MEDIA | RP-01、RP-04、RP-07、RP-08、RP-11 | Job→文件→版本→采用→成片清单；许可与参数不丢失 |
| FR-170/180、NFR-002/003 | RP-09、RP-10、RP-11/12 | 设置可配置性、备份恢复、缓存保护、脱敏 |
| NFR-001/005、PRD §14 | RP-10、RP-11.3、RP-11.2 | 有分母的样本结果、实际P95；无必要skip冒充通过 |
| PRD §18/§20、发布矩阵 | RP-00.2、RP-10～12 | 需求冲突闭合、全部必要门、同版本工件/安装/许可证据 |

## 19. 完成定义与执行交接

每个包只有同时满足以下条件才可改为 COMPLETE：

1. 本包每条任务已实现，或具备明确来源的产品范围决定；未决条款不是通过。
2. UI → Binding → Application → Repository/Provider → 读回的真实路径完成，没有用假成功、无调用方法或静态 JSON 代替。
3. 回归测试证明本次缺陷修复；安全/失败/取消/并发/恢复按相关性覆盖，不放宽旧断言。
4. 迁移前 fixture、备份、升级、约束与错误恢复有证据；不修改已发布迁移。
5. 包级门与完整可用 verify 真实运行，环境失败/跳过单列；执行过的失败必须修复或如实保持未完成。
6. 状态、需求映射、文档、Schema/Binding、许可证和工件证据一致。
7. 用户既有变更、数据与 Key 被保留；未授权 commit/push/付费调用/发布均未发生。
8. 当前包停止，不自动进入下一包。

**推荐首个执行包：RP-00.1 后紧接 RP-01（音轨完整性）。** 它修复用户参数可能丢失的真实问题，范围清晰，可独立验证。RP-02 随后处理 Gemini 和限流配置。

后续执行可在当前会话按包推进，或在保留当前工作树内容的独立执行环境使用 `executing-plans`；若采用 native subagents，先明确文件所有权，迁移、共享 Binding、模型策略和 i18n 由包负责人串行整合。不要并发改同一文件，不允许子任务自行扩展到下一个包。

本计划交付后等待具体实施指令；不把“制定计划”解释为已授权一次实施全部 39 项。

# RP-09.1 FR-180 设置清单核对（2026-09-28）

> 目的：按 `docs/plans/2026-09-28-prd-gap-closure-development-plan.md` RP-09.1 的要求，
> 对 FR-180 十一类设置逐项登记「实际存储、读入口、写入口、是否用户可改、重启生效规则、
> 测试、需求差异」。本文件不覆盖 `SETTINGS_INVENTORY_T10_T11.md` 的裁定——它在该裁定
> 之上补充 RP 批次（2026-09-28）落地的新事实，并在末尾登记差异。
>
> 「常量」与「只读展示」不记「可配置」（计划 §13 RP-09.1 的规则）。

## 十一类设置逐项登记

| # | 设置项 | 实际存储 | 读入口 | 写入口 | 用户可改 | 重启生效 | 测试 | 与 PRD 差异 |
|---|---|---|---|---|---|---|---|---|
| 1 | 数据目录 | 系统目录（appdirs 启动时确定） | health binding `dataDirectory`（只读展示） | 无 | 否（只读呈现） | — | health 快照测试 | T10 裁定：v1 不做迁移；备份/恢复替代 |
| 2 | 缓存上限 | 不适用（T11 判定） | — | — | 否 | — | garbage_collection.go GC 预览/收集测试 | T11 判定条款不适用 |
| 3 | Worker 并发 | 代码常量 `jobWorkerCount = 3`（job_wiring.go） | worker 构造 | 无 | 否（常量） | — | runner 并发测试（per-provider 限流另有 000022/000030） | T10 裁定：不入设置 |
| 4 | Provider 超时 | `phttp.Limits` 代码级（SECURITY §6.3） | 客户端构造 | 无 | 否（安全边界） | — | providerhttp 全套负例 | T10 裁定：不可配置化削弱 |
| 5 | 默认模型策略 | `project_provider_policies`（000006）+ RP-06.5 的 StageModelKeys 键表 | `textGenerator.choice`/`policyProvider`（agent_wiring.go）；`ResolveStageModelPolicy` | 项目设置 UI（既有）+ RP-06.5 键表供设置面板 | 部分（项目级已有；阶段级键表已建） | 立即（每次调用解析） | WP-08 policyProvider 测试 + RP-06.5 `TestRP06ModelPolicyResolutionOrder` | 键形状已由 RP-06.5 按 PRD 键名落地（ADR-0012 §5 的延后理由已答） |
| 6 | 记忆策略 | 代码级：阈值/TopK/fusion 权重为常量（`internal/application/memory`），local/provider/embedder 由构造注入 | memory service 构造 | 无 | 否 | — | AC-MEM-001..005 | 本地模式不外发由确定性 feature-hash 适配器保证（无网络路径） |
| 7 | 日志级别 | `slog.Default()` INFO（代码级） | logger 构造 | 无 | 否 | — | — | T10 裁定 defer；诊断导出已覆盖 FR-180 的诊断诉求 |
| 8 | FFmpeg 路径 | 代码常量 `ffmpegBinary`（拒绝设置化） | engine 构造 | 无 | 否（安全拒绝） | — | engine Available/Diagnostic 测试 | T10 裁定：明确拒绝（任意执行面） |
| 9 | 自动备份 | `app_settings` 表 key=`autobackup`（000033） | `desktop.ParseAutoBackupSettings`（启动读取）；`SettingsBinding.GetAutoBackup` | `SettingsBinding.SetAutoBackup`（revision 守卫） | **是（RP-09.2 落地）** | **下次启动生效（面板明示）** | `settings_binding_test.go` 5 测试 + `agent_enabled_persistence_rp06_test.go` 跨重启 | T10 记录的「常量待设置存储」已闭合 |
| 10 | 主题 | 前端 store/localStorage（use-config-store / i18n） | 前端 | 前端设置 | 是 | 立即 | locales.spec | T10 裁定：前端职责 |
| 11 | 语言 | i18next + 前端 store | 前端 | 语言切换 | 是 | 立即 | locales.spec | T10 裁定：前端职责 |

## 附带落地（RP 批次 2026-09-28）

- **Agent 启停开关持久化**（FR-090 的启用/禁用 + RP-06.3）：`app_settings` key=`agent.disabled_keys`，
  读入口 `LoadDisabledAgents`，写入口 `PersistDisabledAgents`（`SetSetting` revision 守卫）；
  跨两次 handle 生命周期测试证明「禁用→重启→仍禁用」（`agent_enabled_persistence_rp06_test.go`）。
  T09 的 in-memory 开关由此获得持久层，重启读回后装配时恢复。
- **应用设置存储**（T10 裁定 defer 的「应用级设置存储」）：迁移 000033 `app_settings`
  （key/value/revision），`GetSetting/SetSetting` 实现读-改-写丢失更新拒绝——T10 第 3/7 行
  defer 的前提条件已建成。

## 需求差异与待决定（未闭合项，不记 PASS）

1. **数据目录迁移**：T10 裁定 v1 不做，备份/恢复替代——维持，属 D8 待决定事项
   （scope-decisions §3）。
2. **FFmpeg 分发路径**：维持安全拒绝；分发（随包携带 ffmpeg）属 RP-12 发布决策。
3. **日志级别/保留时间与大小**：级别 defer 不变；**日志文件保留上限已实现
   （RP-09.3，2026-09-29）**——`internal/infrastructure/logging/rotation.go`：
   单文件 10 MiB 上限（`MaxLogFileSizeBytes`）、保留 5 份冻结快照
   （`MaxRotatedLogFiles`，最旧先删）、`shouldRotate/rotate/pruneRotatedLogs`
   三段契约测试全绿；轮转只动文件名与大小，不读日志行（脱敏由 redacting
   handler 负责），不可能泄漏其移动的内容。
4. **诊断范围预览**：diagnostics panel 已交付（T31），脱敏导出已有诊断绑定
   （`DiagnosticsBundleDTO`），键盘/焦点回归归 RP-10.2/RP-11.1。

## 结论

- FR-180 十一类设置：**10 类有实现或裁定证据，1 类如实 OPEN**（键盘/焦点
  自动化回归——归 RP-11.1 的 e2e 收口，canvas-regression 已覆盖工具栏键盘流）。
- RP-09.1 计划条款「将缺漏的默认模型策略映射 RP-06.5；补记忆策略边界」已闭合
  （第 5/6 行）。
- 未出现「以一份裁定文档代替缺失能力」的新增情形：RP-09.2 的设置存储与面板是
  实现证据，不是裁定。

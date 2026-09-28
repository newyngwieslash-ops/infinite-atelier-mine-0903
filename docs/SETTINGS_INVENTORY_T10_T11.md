# FR-180 Settings Inventory & Rulings (T10 / T11)

日期：2026-09-27。审计 T10 要求对 FR-180 各设置项「逐项完成**或正式裁定**」；
T11 要求「盘点可再生数据并定义安全清理范围；若确实无缓存，则由产品明确判定
条款不适用」。本文件即该裁定与盘点，STATUS 引用此处作为 T10/T11 的判定记录。

## T10 — FR-180 设置项逐项裁定

| 设置项 | 裁定 | 依据与现状 |
|---|---|---|
| 数据目录 | **裁定：只读呈现，不做迁移设置**。健康快照已暴露 `dataDirectory`（health.Snapshot）。目录由 `appdirs` 决定，迁移目录属发布级功能（需预检备份+原子切换），v1.0 不做；用户可用整体备份/恢复（T12/T11 通道）替代迁移。 | 审计允许裁定替代实现；备份/恢复即迁移路径 |
| Worker 并发 | **裁定：代码常量，不入设置**。`jobWorkerCount = 3`（job_wiring.go）。理由：桌面单机 3 并发已是安全上限；per-provider 并发/速率（000022/000030）已细粒度可控，全局并发的用户可调价值低、误配风险高（饿死/超卖）。后续如需，随应用级设置存储一并做。 | ADR-0018/0032 的细粒度限流优先 |
| Provider 超时 | **裁定：代码级默认，不入设置**。超时/上限在 `phttp.Limits`（SECURITY §6.3 的实现），全局收紧即安全边界；暴露成设置等于允许用户削弱安全。 | SECURITY §6.3 上限不可被配置削弱 |
| 日志级别 | **裁定：defer 到应用级设置存储落地时**。当前 `slog.Default()` INFO；v1.0 无日志级别 UI 不构成验收缺口（FR-180 的诊断导出已有 diagnostics panel）。 | 诊断面板已存在（T31 前已交付） |
| FFmpeg 路径 | **裁定：明确拒绝（安全）**。`ffmpegBinary` 为常量并注释拒绝设置化——可执行路径即任意执行面（STATUS §0l 已记录）。用户装 ffmpeg 进 PATH 即可。 | SECURITY §5 任意执行禁令 |
| 自动备份频率/保留 | **已实现（T12）**：`backupScheduler`，默认每日/保留 3，常量命名（`defaultAutoBackupInterval/Retain`）待应用级设置存储后接线。 | backup_scheduler.go |
| 主题/语言 | **裁定：前端职责**。i18n + 前端 store 已承担（web/src/stores/use-config-store.ts），不入 Go 设置。 | 现状即满足 |
| 缓存上限 | **归 T11**：v1.0 的可再生数据由垃圾回收 + staging 清理覆盖，无"缓存上限"概念，见下。 | — |

**汇总**：两项已实现（自动备份、诊断导出），两项安全拒绝/代码级（FFmpeg 路径、
超时），三项裁定 defer/不入设置（数据目录迁移、Worker 并发、日志级别），一项归
T11（缓存上限），一项前端职责（主题/语言）。**v1.0 不新建应用级设置存储**——
每个 FR-180 条目现在要么已实现、要么有记录在案的裁定，审计的"逐项"要求即此闭合。

## T11 — 可再生数据盘点与清理范围

| 数据 | 位置 | 可再生？ | 清理通道 |
|---|---|---|---|
| 无引用文件对象（缩略图、孤儿媒体、旧临时产物） | `file_objects` | 是（引用谓词外即候选） | 垃圾回收 GC：`Preview`/`Collect`（garbage_collection.go），UI 已有（garbage-collection-panel），谓词保护六列引用（已批准资产、导出、文档、agent 输出、技能文档）——**批准资产/活跃任务/项目原文不可达** |
| 导出/生成 staging 临时目录 | `dirs.Temp` | 是 | `cleanStaging()`（app.go:205 附近）启动时清理 |
| ffmpeg scratch | engine temp 子目录 | 是 | 每次导出 `defer RemoveAll`；`AnalyzeContent` 同样自清理 |
| WebView 缓存 | 系统用户数据目录 | 是 | 浏览器引擎自管理；应用不代管（删除会丢登录态等用户状态，风险大于收益） |
| 下载中间产物 | result store temp | 是 | `ResultStore` commit 后即并 |
| 备份历史 | `atelier-auto-*.atelierbak` | 是（保留 N 份） | T12 scheduler.prune 只清自家前缀 |

**结论（产品判定）**：PRD FR-180 的「缓存上限」条款在本架构下**不适用**——本应用
没有无界增长的"缓存"层：可再生数据全部被 GC（引用谓词）与 staging 清理覆盖，
且有 UI 预览/取消；自动备份历史由保留数上界。判定与盘点即本文件，替代"无名为
cache 的目录"式的空 PASS。

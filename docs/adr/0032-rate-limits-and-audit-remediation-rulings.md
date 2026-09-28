# ADR-0032: Per-Provider Rate Limits and the Audit Remediation Batch

日期：2026-09-27
状态：Accepted（更新 ADR-0018 的裁定 8，并记录 2026-09-26 审计整改批次的架构裁定）
上下文：`docs/implementation/project-progress-audit-2026-09-26.md`（T 编号来源）

## 裁定清单

### 裁定 1 — 速率限制已建（更新 ADR-0018 裁定 8）

ADR-0018 裁定 8 记录 FR-150 的速率限制半句"未建并命名"。T07 已交付：

- `provider_configs.rate_limit_per_minute`（迁移 000030，0 = 无限制，与
  max_concurrency 同一约定，升级行为不变）。
- `provider_request_windows` 持久台账：一行一（provider，UTC 分钟窗口），claim 时
  upsert 计数，写入方清理过期窗口。台账是**表**而非内存——桌面应用重启后同分钟
  的计数读回，这是审计要求的边界条件。
- 准入在 `dispatch`（ADR-0018 裁定 2 的同一位置）：并发检查旁加速率检查，同一
  skip 规则（保持队列位置）、同一 pass 内计数闭窗口、不满的窗口不饿死其他
  provider。不可读 = 本 pass 无限制（fail-open 于读故障，与并发裁定同向）。

Token-bucket 形态（ADR-0018 曾以此拒绝）被显式**分钟窗口计数**替代：桌面单机
场景下分钟粒度足够，且窗口数可直接从现有 claim 流记账，无需新时钟机制。

### 裁定 2 — 音效合成是独立能力（T03）

`effect_generation` 是独立任务类型与 `EffectPort`；OpenAI 兼容家族无音效端点，
真实适配器按 T03 诚实拒绝（`PROVIDER_UNSUPPORTED`）。拒绝即契约：把效果描述送进
语音端点读出来（T01 审计发现的缺陷）不再可能。未来接入有真实音效协议的供应商时
替换适配器实现，上游不变。

### 裁定 3 — 音频资产按台词实例键控（T01）

音频 asset 的身份是**使用它的台词/效果实例**（job 的 entity id），不是项目级角色名。
同一实例的重做是同 asset 的新版本；不同实例永不共享。存量共享资产由迁移 000027
按使用拆分（保留首次使用，后续使用复制到子 asset），只拆分不删除。

### 裁定 4 — 收集是原子命令（T04）

AttachJobResult→ApproveVersion→AddUsage 的三写合并为
`assets.CollectJobResult` 的一个事务（存储半 `CollectJobResultVersion`）。应用层
mint 全部 id 并传治理事件；重复收集先**修复**半途状态再报告 duplicate——重试
幂等但不空转。无事务 runner 的组合**拒绝**收集（fail closed），不回退三写。

### 裁定 5 — 视频采用走分镜面板批准（T02）

视频版本的"采用"复用 panel 批准开关（CreatePanelVersion 候选 + ApprovePanelImage），
不新增第二套镜头媒体批准。时间线/导出/final reader 对 asset_type 动态解析，视频
采用后自动流入导出段与 manifest，无需读侧改动。

### 裁定 6 — FIX 复用 attempt 行（T13，确认 ADR-0011 §4）

AC-SCRIPT-002 与 AGENT_CONTRACTS §10.2 的字面条文（"新 StageRun attempt"）按
ADR-0011 §4 修订：needs_fix 是活跃状态，单活跃部分唯一索引据此成立；修订 =
新 AgentRun + 新 Version，attempt 行复用。两份文档已按此改写。

### 裁定 7 — 分镜引用必须精确 approved（T14）

storyboard.create_storyboard_version 要求引用的 script/plan 版本
`status == versioning.StatusApproved`——不用 IsContentFrozen（它放行 superseded，
那是历史而非现行）。

### 裁定 8 — Final Ruleset 的视频收紧落地（T15）

ADR-0015 §2 预留的收紧条件实现：`video_motion_description` 非空的镜头其批准媒体
必须 `media_kind == "video"`。对白完整性按行核对（经 job-entity 链路的 coverage 读），
未覆盖行逐行出 finding。

### 裁定 9 — 性能上界与 race 分离（T24）

WP-12 的五个性能上界测试在 race 构建下 SKIP（`race` 构建标签检测）。依据：race
插桩对 DB 计时放大约一个数量级，同一上界在两种构建下测量不同对象；STATUS §0t
历史两次 6000ms 失败即此。race 作业的价值是零 data race 报告；性能数字由正常
构建测量。不是删除测试，是分离测量。

### 裁定 10 — 本地嵌入模型用户自选下载（T19）

模型/词表/运行时不随包分发、不入 Git、不静默下载；用户按
`docs/EMBEDDING_MODEL_DISTRIBUTION.md` 自取，环境变量指向。诊断可见性由
health snapshot 的 embedding 状态承载（T18）。

## 后果

- FR-150 的两半（并发 + 速率）均 PASS；TRACEABILITY 相应行已更新。
- FR-140 的清单加载为域层能力（closed-set 校验），registry 接线留给后续包。
- 审计批次其余 BLOCKED 项（T25 VM、T29 证书、T32 发布）依赖外部前提，不在
  本 ADR 范围。

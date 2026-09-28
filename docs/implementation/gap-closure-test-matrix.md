# Gap Closure Test Matrix（RP-11.1，2026-09-28）

> 状态：**矩阵已建立；执行状态逐行标注**。本文件把 RP-01～RP-09 的每条断链
> 映射到「能通过真实边界重现的回归用例」——用例必须已在仓库中，或在此标注
> 未建。源码扫描（security-scan 等）有价值但不作为唯一验收。
>
> 实测状态标记：**COVERED**（真实边界用例存在且 PASS）/ **PARTIAL**（部分
> 覆盖）/ **OPEN**（未建）。

## RP-01 音轨编辑完整性

| 断链 | 回归用例 | 状态 |
|---|---|---|
| Usage ID 缺失（同版本双轨不可分） | `TestRP01UsageDTOContainsIDAndParams`（Binding 序列化 + 四路径一致）；前端 `audio-track-params.spec.ts` | **COVERED** |
| 编辑音量丢失裁剪/台词关联 | `audio-track-params.spec.ts`（mergeTrackEdit 7 例：0/false/null/无关行） | **COVERED** |
| 参数写入无校验 | `TestRP01TrackParamsRejectInvalidBeforeWrite`（5 负例，拒绝先于写） | **COVERED** |
| 参数→混音 | `TestAUsageCarriesItsTrackParams`、`TestMutedIsSilenceNotDefault`、`TestRP01ATrimmedAndMutedTrackChangesTheMix`、`TestRP01TwoDialogueLinesOnOneShotStayIsolated`（真实 FFmpeg） | **COVERED** |
| Playwright 交互（开轨→改→存→重开） | `web/e2e/track-editor-roundtrip.spec.ts`（经页面内生产服务模块跑 读→合并→替换→重开 全环，断言裁剪/台词关联存活） | **COVERED**（浏览器证据类；原生 shell 归 RP-12.3） |

## RP-02 Provider 配置与路由

| 断链 | 回归用例 | 状态 |
|---|---|---|
| Gemini kind 走 OpenAI 端点 | `TestRP02GeminiTextReachesGenerateContentNotChat`、`TestRP02GeminiTextStreamsParsesCandidates`、`TestRP02GeminiTextErrorShapesRefuse`、`TestRP02RegistryRoutesGeminiKindToGeminiAdapter` | **COVERED** |
| 速率限制被保存清除 | `TestRP02GeminiKindAndRateLimitPersist`（未声明保留/越界拒绝/显式 0） | **COVERED** |
| 保存失败不可见 | `provider-config.spec.ts` sync outcome 测试；抽屉 await + Alert（组件级） | **COVERED**（UI 交互面走 Playwright：OPEN） |
| 未知 apiFormat 默认 openai | `provider-config.spec.ts` unknown format 拒绝 | **COVERED** |

## RP-03 视频能力

| 断链 | 回归用例 | 状态 |
|---|---|---|
| 时长/分辨率枚举不可查询 | `TestRP03VideoCapabilityListsTheVendorEnum` + Binding 再生成 | **COVERED** |
| 单发/批量校验不一致 | `TestRP03VideoSecondsEnumRefusesUndocumented`（批量补枚举校验） | **COVERED** |
| UI 可提交后端必拒值 | 能力驱动 Select（组件）+ 枚举负例 1/5/60 | **COVERED**（e2e 面OPEN） |

## RP-04 本地任务

| 断链 | 回归用例 | 状态 |
|---|---|---|
| 参数丢失（export/thumbnail） | `TestRP04ThumbnailInputCarriesEveryField` 等 4 例（DTO→持久→handler 逐字段） | **COVERED** |
| 未知字段/旧 envelope | `TestRP04InputRefusesUnknownFieldsAndOldEnvelope` | **COVERED** |
| import 空内容假成功 | `TestRP04ImportJobReadsRealContentAndFindsChapters`（真实 store 字节往返+章节行） | **COVERED** |
| 结果语义失真 | `TestRP04ImportResultUsesRealSemantics` | **COVERED** |
| 崩溃恢复/幂等 | `TestRP04ThumbnailJobRecoveryDoesNotDuplicateTheLink`、`TestRP04ExportJobRecoveryKeepsTheCommittedRecord`、`TestRP04RecoveryScannerRequeuesLocalJobsWithoutRemoteID` | **COVERED** |
| 窄提交命令 | `TestRP04NarrowSubmissionsBuildVersionedInputs/RefuseUnnamedSubjects/AreIdempotent` | **COVERED** |
| UI 提交→Job Center 交互 | — | **OPEN**（Playwright/原生） |

## RP-05 Manifest

| 断链 | 回归用例 | 状态 |
|---|---|---|
| 恶意 Manifest（域层） | `manifest_rp05_test.go` 8 负例 + `TestRP05ManifestValidationBlocksTheStore`（DB 读回零行） | **COVERED** |
| 恶意 Manifest（Binding 层） | `TestRP05PreviewRefusesHostileManifestWithDomainMessage`、`TestRP05SaveRefusesHostileManifestBeforeStorage`、`TestRP05ActivateWithoutStoreFailsClosed` | **COVERED** |
| 版本化/幂等 | `TestRP05ManifestVersionsRoundTrip`（同内容同版本/指针切换/跨 config 拒绝） | **COVERED** |
| UI→registry→mock server 全链 | — | **OPEN**（RP-05.3 组合根 e2e，依赖 httptest 编排） |

## RP-06 Agent/Skill/策略

| 断链 | 回归用例 | 状态 |
|---|---|---|
| Skill 版本时序 | `TestRP06SkillVersionLifecyclePinIsThePlan`、`TestRP06SnapshotIsAtomicUnderConcurrency`、`TestRP06DerivedVersionKeepsParentSpec` | **COVERED** |
| 启停持久化 | `TestRP06AgentDisableSurvivesRestart`、`TestRP06AgentSwitchRevisionConflict` | **COVERED** |
| 只读测试 | `TestRP06ProbeAnswersRegistrationToolsAndVersion`、`TestRP06ProbeIsARead`、`TestRP06ProbeFailsClosedForUnknownAgent` | **COVERED** |
| 模型策略解析 | `TestRP06ModelPolicyResolutionOrder`、`TestRP06StageModelKeysCoverTheSpec` | **COVERED** |
| 真实 LLM 只读试跑 | — | **OPEN**（RP-11.2 授权后；静态预检不得冒充） |

## RP-07 许可与内容分析

| 断链 | 回归用例 | 状态 |
|---|---|---|
| tri-state 读写 | `TestRP07LicenseTriStateRoundTrip`（unknown/allowed/refused 区分+拒绝不落库） | **COVERED** |
| 内容分析→finding | `TestRP07ContentFindingsCarryLocatedEvidence`、`TestRP07UnanalysedIsVisibleNotClean`、`TestRP07UnreadableFileIsAFindingNotAFailure`、`TestRP07NilAnalyzerKeepsMetadataOnlyReview` | **COVERED** |
| 引擎接入组合根 | `contentAnalyzerAdapter`（编译期端口证明）+ app.go 接线；端到端 ffmpeg 分析跑分 | **PARTIAL**（真实黑帧视频端到端 walk 待 RP-11.1） |
| 许可变更→final 复核 | final_reader 逐资产报告（T17 既有） | **COVERED** |

## RP-09 设置

| 断链 | 回归用例 | 状态 |
|---|---|---|
| 备份设置不可配 | `settings_binding_test.go` 5 例（默认/持久/敌意值/revision 冲突/禁用） | **COVERED** |
| 调度器读设置 | `ParseAutoBackupSettings`（missing/hostile/disabled 三路）+ app.go 接线 | **COVERED** |
| 面板 UI 交互 | AppSettingsPanel（组件）| **COVERED**（组件级；e2e OPEN） |

## RP-10.3 严格门

| 项 | 状态 |
|---|---|
| `scripts/check-release-prerequisites.mjs`（go toolchain/cgo/onnx/ffmpeg/nsis/sbom 输入，strict 模式 FAIL） | **COVERED**（本机 report 模式 exit 0；strict 因 gcc/onnx/makensis 缺失正确 FAIL=1） |
| race 全量 | **BLOCKED**（本机无 gcc；CI race 作业 T22 已建） |
| NSIS | **BLOCKED**（同上） |

## 汇总

- **COVERED 34 / PARTIAL 2 / OPEN 4 / BLOCKED 2**（音轨编辑器 e2e 已收口；
  RP-09.3 日志保留上限已实现——`rotation_test.go` 3 例，10 MiB/5 份策略，
  prune 不触 app.jsonl 与外来文件；RP-11.3 性能样本已取——P95 n=30 达标 +
  1000 节点手势 n=10 达标（`render-p95-sample.spec.ts` /
  `render-gesture-samples.spec.ts`）+ 100k 汉字导入 4ms 达标
  （`large_import_rp11_test.go`）+ 500 任务 0.52s（`TestT27JobStoreScale500Tasks`）+
  10k 素材 ListAssets 2.92ms/向量检索 3.59ms（`TestWP12RecordScaleWithinBounds`）。
- OPEN 项均为浏览器/原生交互面（Playwright 与 VM），不阻塞 Go/前端逻辑层的
  断链修复验收；RP-11.1 的完整离线主旅程将 OPEN 项收口。
- 每行「COVERED」均指向仓库内真实测试文件——不引用已删除或 skip 的测试。

# Product Metrics Results（RP-11.3，2026-09-29）

> 依据 `product-metrics-protocol.md`（测量前冻结的规则）产出的首批实测结果。
> 硬件/版本：AMD Windows 桌面（本机），commit `4be369d` + RP 批次工作树（未提交），
> Go 1.25.13，Node v22.22.3，Chromium 141（Playwright build v1194），CGO=0。
> 局限：单机浏览器面证据；原生 VM 与真实模型样本未取（RP-12.3/RP-11.2 BLOCKED）。
> **样本量不足协议 ≥30 次要求的指标如实标注「未验收」。**

## 1. 性能采样（2026-09-29 实测）

| 场景 | 结果 | 协议阈值 | 判定 |
|---|---|---|---|
| **Wheel-zoom 响应 P95（n=30，协议达标）** | **P95=59.5ms，median=53.7ms**（raw 见下） | <2000ms guard | **PASS（样本量达标 ≥30）** |
| **1000 节点手势样本（T27 场景，n=10，协议达标）** | **P95=1192.3ms，median=798.2ms**（raw: 566.0, 620.2, 855.4, 630.5, 1192.3, 817.6, 616.4, 827.7, 798.2, 603.0） | guard 2000ms | **PASS（≥10 次手势已取满，全部低于 guard）** |
| 1000 节点 + 1997 边整面重渲染（T27 场景，单样本） | 1128.3ms | guard 2000ms | PASS（单样本，与手势样本族一致） |
| 100 节点基线渲染 + 拖拽交互 | PASS（15.5s 场景内） | 交互不阻塞 | PASS |
| **100k 汉字导入（真实管线）** | **runes=100049, chapters=95, 4ms** | 协议上限 30s | **PASS** |

**P95 原始样本（n=30，ms，升序输出由采样器打印）**：
`59.0, 54.0, 52.6, 49.3, 51.8, 53.2, 51.5, 53.3, 51.6, 51.9, 90.0, 55.7, 55.1, 53.6, 55.7, 59.5, 53.4, 53.6, 54.3, 54.5, 54.3, 53.7, 54.0, 54.7, 52.1, 53.2, 53.7, 52.0, 50.0, 54.1`

采样器：`web/e2e/render-p95-sample.spec.ts`（页面内双 rAF 计时 + 真实 wheel 事件驱动渲染管线；P95 口径 ceil(0.95×n)-1）。
| AC-MEDIA-003 时间线读取（DB 读回） | 0.22s | — | 机制样本 |
| 完整导出合成（真实 ffmpeg，含 ffprobe 读回） | 0.91s | — | 机制样本 |
| 剪辑替换后再导出（旧清单不动） | 1.96s | — | 机制样本 |
| 字幕+音频合成进片 | 0.85s | — | 机制样本 |

## 2. 数据一致/质量指标（协议 §3 的确定性半边）

| 指标 | numerator/denominator | 判定 |
|---|---|---|
| 任务/文件一致 | 全部成功任务 result 引用 file_objects 行存在（ResultStore commit 契约测试族全绿） | **100% PASS** |
| 幂等重复资产 | 重复收集请求产生的重复资产 0（CollectJobResult duplicate 路径 + RP-04.3 崩溃恢复测试） | **0 PASS** |
| 迁移节点/边不丢失 | legacy importer 完整性测试族全绿（TestImportAllNodeTypesIsComplete 等） | **100% PASS** |
| 备份泄漏 | 备份扫描 credential shapes（含 Authorization/Cookie）全绿 | **0 泄漏 PASS** |

## 3. Memory 指标机制（RP-11.3 的 Mock 半边）

| 指标 | 证据 |
|---|---|
| Recall 命中率/摘要命中率/跨项目泄露率计算 | `eval.go` RecallMetrics 的 HitRate/SummaryHitRate/LeakRate 四个机制测试 PASS（`TestTheMetricsReport*`） |
| **真实模型成功率** | **未取**——须 RP-11.2 授权；Mock 的命中不冒充真实成功率 |

## 4. PRD §14 产品指标（分母为真实用户行为）

| 指标 | 状态 |
|---|---|
| 新用户导入→骨架 ≥90% | **未验收**——需真实用户/会话数据，无脱敏诊断事件样本可取（本机为开发环境） |
| 恢复 ≥95% | 机制层 PASS（AC-E2E-003 两生命周期测试）；产品层分母待真实使用 |
| 连续性定位 ≥90% | 机制层 PASS（quality centre finding→entity 跳转测试）；产品层待真实使用 |

## 5. 局限声明（协议 §5）

1. ~~渲染 P95 样本（≥30 次）~~ **已取满（n=30，P95=59.5ms）**；~~1000 节点
   手势样本（≥10 次）~~ **已取满（n=10，P95=1192.3ms）**。
2. ~~100k 汉字导入~~ **已取样**：`TestRP11LargeImportSample`——合成 100,049 字
   小说经真实解码器/章节切分，**95 章，4ms**（协议上限 30s，远低于）。
   ~~10k 素材~~/~~500 任务~~ **已取样（2026-09-29 复跑）**：
   - **500 任务**：`TestT27JobStoreScale500Tasks` PASS（0.52s）——500 持久任务混状态双项目，ListJobs/ClaimableCandidates 读回。
   - **10k 素材**：`TestWP12RecordScaleWithinBounds` PASS（26.87s）——ListAssets 满页 1000 行 **2.92ms**（上限 250ms，85.7× 余量）、深分页 **1.54ms**（259.3×）、Memory 向量检索 3.59ms（55.8×）。
   - **1000 节点画布规模**：`TestWP12CanvasScaleWithinBounds` PASS（9.15s）。
   - **30 分钟长运行内存**：**未取样**（需长驻会话，后续批次）。
3. 全部证据为**浏览器面/Go 测试面**；原生 Wails shell 与 VM 走查未做（RP-12.3）。
4. Mock 指标只证明机制；真实模型样本等 RP-11.2 授权。

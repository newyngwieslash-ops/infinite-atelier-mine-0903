# WP-18 — 层级摘要（第三级）与召回评测集（P3 第 22 项）

依据：PRD §16 的 v1.0 清单第 22 项「层级摘要和记忆中心」，PRD FR-120 的「必要规则」
（`PRD.md:976-1000`：「支持层级摘要：message → episode/session → project」），
AGENT_CONTRACTS §18.1/18.2（`docs/AGENT_CONTRACTS.md:1083-1107`：fixture 清单与**指标**清单，
其中「Memory 跨项目泄露率」与「Deep Recall 命中率」两条至今没有实现），
DOMAIN_MODEL §14.5，STATUS §0k（记录的三处限制）、§0j（记忆中心 UI 已交付）。

## 0. 侦察结论（每条有证据，决定了这个包的形状）

| 事实 | 证据 | 后果 |
|---|---|---|
| **第三级被声明但没有实现** | `Scope.ProjectOnly()`（`domain/memory/memory.go:199`）文档说它是「how a PROJECT-level summary is scoped」，而**生产代码零调用者**；`SummaryRulesetVersion` 上方注释（`application/memory/summary.go:30-36`）明说「There is no third level in this build」 | 第三级是要建的东西，不是要修的 bug |
| **第二级从未被任何测试执行** | 全仓 `Level:` 的六处**全是 `Level: 1`**（`acceptance_wp10_test.go:329/420/481/613/627`、`canary_memory_wp10_test.go:183`） | 第二级的正确性**没有任何证据**，而第三级要建在它上面 |
| **第二级不标记其子项，与自己的注释矛盾** | `summary.go:155` 是 `CreateSummaryWithSources(ctx, summary, links, level == 1)`，而 `summarySources` 的注释（`:195-199`）说「The same `summarized` flag is reused at this level」 | **第二次跑第二级会把同一批子摘要再浓缩一次**：`UnsummarisedItems` 按 `summarized = 0` 选，而它们从未被标记 |
| **召回评测只有 fixture 与一个走查，没有指标** | `testdata/canary-drama/memory-recall.json` 存在且被 `canary_memory_wp10_test.go` 走通；但「命中率」「泄露率」在全仓**零实现**（`grep 命中率\|hitRate\|precision` 无命中）；§18.2 的两条指标因此无法被任何人回答 | 评测集是要建的东西 |
| **fixture 已经携带评测所需的一切** | `mustRecall{viaSummary, returnsSource}`、两个 decoy 各带 `reason`、`otherProjectId`、`maxRawMessages` | 指标**不需要新 fixture**，需要的是**衡量它的代码** |

**结论**：这个包有两个交付物，且第二个依赖第一个的正确性——「Deep Recall 命中率」只有在梯子是对的
时候才有意义。所以顺序是：先修第二级 + 建第三级 + 断言梯子的性质，再把评测集建在**已经能跑通的梯子**上。

## 1. 第二级的缺陷（先修，因为第三级建在同一条规则上）

**规则应该是**：一个摘要被某个父级覆盖后，就不该再被另一个父级覆盖。`summarized = 0` 是这条
规则的存储表达，而第二级没有写它。

- `CreateSummaryWithSources(..., markSummarized)` 的第三个参数改为 **`level != 3`**：一级摘要标记
  消息（已有），二级摘要标记子摘要（新），三级摘要标记二级摘要（新）。
  **为什么不是 `level == 1`**：那样第二级就永远不标记，而 `UnsummarisedItems` 会一遍遍返回同一批行。
- 这不是「换个值」而是一条**可反驳的设计决定**，理由写进 ADR：标记的语义是「已被覆盖」，而不是
  「已被一级摘要覆盖」——名字里的 1 是历史上的。

## 2. 第三级（project rung）

- `SummarizeRequest.Level` 接受 **3**：`summarySources` 在第三级读**项目作用域下未被覆盖的摘要**，
  窗口上限复用 `MaxSummaryParents`（八个父级已经是「一次浓缩多少条」的上限，第三级没有理由不同）。
- **读取作用域**：第三级读 `scope.ProjectOnly()`，写也用 `ProjectOnly()`（**清空 episode/agent/session**），
  理由与第二级清空 agent/session 相同：一个项目级摘要如果还带着某集的 id，它就不是项目级的，
  而且别的集会读不到它。
- **至少两个子项**的规则对第三级同样适用（一条摘要的摘要是多一跳的副本）。
- 文本分隔符新增 `SummaryLevel3Separator = "Earlier in this project:"`，使读者无需查链接就知道层级。
- `renderSummary`、`summaryImportance`、`summaryConfidence` **不改**：它们对来源集合的运算与层级无关，
  这是第三级能复用第二级全部机制的原因。

### 2.1 第三级必须真的可达（否则又是「没有调用者的接口」）

这是本仓反复出现的缺陷形态，所以要逐个确认并写下：

- **写入**：`Summarize` 的 level 3 分支（binding 的 `SummarizeMemoryRequest.Level` 直接透传，已经是任意 int）。
- **读取（语义通道）**：`DeepRecall` 与 `BuildMemoryContext` 走 `scoredCandidates(scope)`，
  而 `scopeClauses` **跳过空的部分**（`database/memory.go:649-669`），所以用 `ProjectOnly()` 读
  **会**匹配到 episode 为空的项目级行。第二级用 `EpisodeOnly()` 读，因此**读不到**第三级的行——
  这正是「项目级问题是项目级通道回答的」。但 `DeepRecall` 目前硬编码 `EpisodeOnly()`（`service.go:764`），
  所以**项目级摘要永远无法被深召回找到**——这是第三级必须一起修的点。
  修法：`DeepRecall` 的语义检索用**调用者自己的 scope**（含 episode 时匹配该集，空时匹配项目级），
  而不是强制 `EpisodeOnly()`。理由：调用者给的 scope 就是它想问的范围。
- **读取（recall preview UI）**：`PreviewMemoryRecall` 传 `ScopeFor(project, episode, agent)`，
  空 episode 时即项目级，天然可达。
- **层级可见性**：`ListSummarySources` 已经能走任意层级的来源；UI 的「摘要来源」抽屉对第三级同样工作。

## 3. 召回评测集（§18.2 的两条指标）

新建 `internal/application/memory/eval.go`（**纯函数 + 一个读取端口**），对外两个函数：

- `EvalRecall(result DeepRecallResult, expectations Expectations) RecallScore` —— 对**一次深召回**打分：
  - `Hit`：期望的消息是否出现在 `result.Messages`（即 AC-MEM-005 的「恢复原始消息」）；
  - `ViaSummary`：是否经由期望的摘要（`mustRecall.viaSummary`）；
  - `ReturnedSource`：返回的记忆是否携带来源（`mustRecall.returnsSource`）；
  - `ForeignCitations`：结果里**任何**引用指向另一个项目（`decoys[other-project]` 的 id / 另一个 project id）
    的条数——这是「跨项目泄露率」的分子；
  - `DecoyHits`：同项目 decoy 被当作答案返回的条数。
- `Aggregate(scores []RecallScore) RecallMetrics` —— 把多次打分汇总成 §18.2 的两条比率：
  `HitRate`（Deep Recall 命中率）、`LeakRate`（跨项目泄露率）。

**为什么是纯函数而不是一个新服务**：指标的全部输入是「一次召回的返回值 + 期望」，两者都已经是值；
一个需要数据库和 provider 的指标会变成第二个评测实现，而它要衡量的正是第一个的行为。

**为什么 fixture 不变**：`memory-recall.json` 已经携带 `mustRecall` 与两个带 `reason` 的 decoy，
所以指标是**读取现有 fixture** 得到的，不是新造场景。

## 4. 测试与验收

- **第二级**：新增 `TestASecondLevelTwoRunDoesNotRecondenseTheSameChildren` —— 跑两次第二级，
  第二次必须 `found=false`。**这一条会先失败**（证明缺陷真实存在），修完再通过。
- **第三级**：
  - `TestTheThirdLevelSummarisesEpisodeSummaries`：message → episode → project 一整条，
    逐级断言 scope（project 级行的 episode 必须为空）、来源链接、分隔符、`found` 语义；
  - `TestAProjectSummaryIsStillReachableFromAnEpisodeQuestion`：**可达性**——项目级摘要在
    带 episode 的 scope 下也能被深召回找到（否则它就是一个没有读取路径的写入）；
  - `TestTheThirdLevelNeedsTwoChildren`、`TestTheThirdLevelIsRefusedWithoutAProject`。
- **评测集**：`eval_test.go` 用**真实的 canary fixture** 走一次 DeepRecall 并断言指标形状与值
  （命中率 1.0、泄露率 0.0），再用构造的 `DeepRecallResult` 断言每条**反向**情形：
  - 期望消息没被恢复 → `Hit=false`；
  - 结果引用了另一个项目 → `LeakRate > 0`（**这条是防「指标永远报 0」的**）；
  - 同项目 decoy 被返回 → `DecoyHits > 0`。
- 迁移：**不需要**。schema 已有 Third 级所需的一切（`memory_items.scope_episode` 可空、
  `memory_summary_sources`、`memory_entity_links`）。
- 前端：记忆中心的「生成摘要」按钮目前固定发 `{ projectId, embed: true }`，即永远第一级。
  加一个**层级选择**（一级/二级/三级），并把每条摘要的层级**显示出来**（由 scope 推导：
  episode 空 = 项目级）。这是「层级摘要和记忆中心」里「记忆中心」那一半。
- `locales.spec.ts` 强制两语言键集一致，所以新文案两处都加。

## 5. 明确不做

- **不新增记忆类型**（`memory_type` 的 CHECK 是发布过的迁移，且「摘要」已经是一种类型）；
- **不建第四级**：FR-120 的梯子到 project 为止；
- **不实现模型驱动的摘要**（仍是 extractive/v1）：那是另一个产品决定，STATUS §0k 已记为限制；
- **不把指标做成 Dashboard**：§18.2 要的是**指标可被回答**，不是一个新的 UI 分面；
- **不动 500 候选窗**：那是第 17 项（ONNX/sqlite-vec）的范围，本项目不扩大。

## 6. 风险与诚实声明

- **第三级会在真实项目里几乎没有输入**：它需要 ≥2 条第二级摘要，而第二级需要 ≥2 条一级摘要。
  所以第三级在小项目里长期 `found=false`——这是**正确行为**（没有可浓缩的东西），测试与文档都这么写，
  不制造假输入。
- **指标只覆盖 Deep Recall 一条路径**：§18.2 的另外七条（Schema 通过率、Tool 选择正确率……）
  各有各的归口，本包只做与记忆有关的两条，其余在 ADR 里点名不假装。
- 变异验证：第二级的标记、第三级的作用域、评测的两个度量是主要变异面。

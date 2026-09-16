# 结论

**可以集成，而且非常值得。**

但最优方案不是把 Toonflow 的代码直接复制进 Infinite Atelier，也不是把两个仓库机械合并，而是：

> **以 Infinite Atelier 作为通用视觉创作前端和无限画布，以 Toonflow 的短剧业务模型、三层 Agent、工作流、质量审核、事件图谱和持久化记忆作为后端生产能力。**

最终产品可以形成：

```text
Infinite Atelier Core
├─ 通用无限画布
├─ 多模型生成
├─ 本地素材库
├─ MONOFORM 导演预演
│
├─ Agent Runtime
├─ Workflow Engine
├─ Persistent Memory
│
└─ Drama Production Pack
   ├─ 小说解析
   ├─ 章节事件图谱
   ├─ 剧本 Agent
   ├─ 角色/场景/道具资产
   ├─ 导演规划
   ├─ 分镜表
   ├─ 分镜画面
   ├─ 视频镜头生成
   └─ 监督审核与修订闭环
```

我的综合判断：

| 判断项结论        |              |
| ------------ | ------------ |
| 产品能力互补度      | **9/10**     |
| 技术上能否集成      | **8.5/10**   |
| 集成后的产品提升     | **9/10**     |
| 直接合并两个仓库     | **3/10，不推荐** |
| 按能力重新实现并集成   | **9/10，推荐**  |
| 是否值得作为下一阶段方向 | **非常值得**     |

---

# 一、为什么这两个项目高度互补

## 1. Infinite Atelier 强在“视觉工作台”

Infinite Atelier 当前已经具备：

- 无限画布；
- 图片、文字、视频、音频、导演节点；
- 多模型通道配置；
- 文生图、图生图和多参考图；
- 本地素材库；
- 生成历史；
- MONOFORM 导演预演；
- 浏览器本地项目保存。

它的定位本质上是一个通用的多模态视觉工作台。([GitHub](https://github.com/newyngwieslash-ops/infinite-atelier-mine-0903 "GitHub - newyngwieslash-ops/infinite-atelier-mine-0903: AI visual workspace for multi-provider model configuration, image generation, prompts, canvas and local assets · GitHub"))

但它的项目数据目前主要只有：

```text
Project
├─ nodes
├─ connections
├─ chatSessions
├─ viewport
└─ UI settings
```

没有原生的：

```text
Novel
Chapter
Episode
Event
Script
Scene
Shot
Character
Location
Prop
Storyboard
WorkflowRun
ReviewReport
```

当前项目也主要通过 Zustand 和 localForage 保存到浏览器本地。

所以 Infinite Atelier 更像：

> **一个很好的视觉操作台，但还缺少“小说到短剧”的业务生产引擎。**

---

## 2. Toonflow 强在“结构化短剧生产”

Toonflow 的主要价值并不只是另一个无限画布，而是围绕：

```text
策划
→ 编剧
→ 资产
→ 导演
→ 分镜
→ 图像
→ 视频
→ 审核
→ 修订
```

建立了一个短剧生产闭环。

其当前项目公开说明中明确包含：

- 三层 Agent 协作；
- 本地持久化 Agent 记忆；
- 章节事件图谱；
- ScriptAgent；
- ProductionAgent；
- Skill 文件化；
- 小说、剧本、分镜、任务、素材等后端模块；
- SQLite、Socket.IO 和 Electron 桌面端。([GitHub](https://github.com/HBAI-Ltd/Toonflow-app "GitHub - HBAI-Ltd/Toonflow-app: Toonflow 是开源一站式 AI 短剧创作工具，将小说、剧本快速转化为动画短剧。集成 AI 编剧、智能分镜、角色与视频生成，跨平台桌面端轻量部署，助力创作者低成本批量产出视觉内容。Toonflow is an open-source AI tool that turns stories and scripts into animated short dramas. Features AI scriptwriting, storyboarding, character and video generation. A cross-platform desktop app for efficient content creation. · GitHub"))

你上传的技术文档对这一体系的概括非常准确：

```text
Decision Agent
→ Execution Agent
→ Supervision Agent
→ Shared Persistent Memory
```

Decision 负责调度，Execution 负责专门任务，Supervisor 负责独立检查，三者共享 SQLite 和本地向量记忆。

因此二者正好形成：

| 项目强项当前不足         |                             |                      |
| ---------------- | --------------------------- | -------------------- |
| Infinite Atelier | 通用视觉画布、多模态模型、素材与导演预演        | 缺少结构化短剧领域模型和自动化生产工作流 |
| Toonflow         | 小说改编、剧本、生产 Agent、审核、记忆、事件图谱 | 工作台通用性和自由视觉编排不是其唯一重点 |
| 集成后              | 通用视觉创作平台 + 专业短剧生产系统         | 架构复杂度明显提高，需要正式后端     |

---

# 二、最值得集成的 Toonflow 能力

## 1. 短剧领域数据模型

这是第一优先级，比先做 Agent 更重要。

Infinite Atelier 目前的画布节点只能表达“一个图片节点”“一个文本节点”，但不知道该图片究竟是：

- 主角定妆图；
- 场景概念图；
- 道具图；
- 第 3 集第 5 场第 2 镜；
- 某个分镜的第二版；
- 某个视频镜头的首帧；
- 某个资产的派生版本。

建议增加正式领域实体：

```text
Project
├─ Novel
│  ├─ Chapter
│  └─ StoryEvent
│
├─ Episode
│  ├─ ScriptVersion
│  ├─ Scene
│  └─ Shot
│
├─ ProductionAsset
│  ├─ Character
│  ├─ Location
│  ├─ Prop
│  ├─ Costume
│  └─ DerivedAsset
│
├─ Storyboard
│  ├─ StoryboardTable
│  └─ StoryboardPanel
│
├─ RenderTask
├─ WorkflowRun
└─ ReviewReport
```

画布节点只保存引用：

```json
{
  "entityType": "storyboard_panel",
  "entityId": "panel_123",
  "versionId": "version_3",
  "workflowRunId": "workflow_456"
}
```

而不应把所有真实业务数据都塞入节点的 `metadata`。

### 一个关键设计原则

> **数据库是事实来源，画布是这些事实的视觉投影。**

否则未来 Agent 修改剧本、重新生成分镜、批量替换角色、恢复任务时，很容易出现画布状态和真实项目状态不一致。

---

## 2. ScriptAgent：小说到结构化剧本

Toonflow 的 ScriptAgent 可抽象为三个阶段：

```text
Stage 1：故事骨架
Stage 2：改编策略
Stage 3：剧本生成
```

每个阶段不是一次生成结束，而是：

```text
Decision
→ Execution
→ Supervision
→ 用户通过 / 修改 / 重做
```

你上传的文档明确记录了这个阶段推进和用户质量门机制。

集成后，Infinite Atelier 可以新增：

```text
原著节点
→ 章节节点
→ 故事骨架节点
→ 改编策略节点
→ 剧本节点
→ 场次节点
```

用户不再需要手动把小说复制进普通文本节点，再自行组织提示词，而是可以：

1. 导入小说；
2. 自动拆章；
3. 提取人物、地点、事件；
4. 选择目标集数和单集时长；
5. 生成故事骨架；
6. 审核改编策略；
7. 生成结构化剧本；
8. 将每场戏自动投射到画布。

这会让 Infinite Atelier 从“生成图片的画布”升级成“真正理解作品结构的创作系统”。

---

## 3. ProductionAgent：自动组织整个生产链

Toonflow 的 ProductionAgent 将生产拆成六个阶段：

```text
1. Director Planning
2. Derived Asset Analysis
3. Derived Asset Generation
4. Storyboard Table
5. Storyboard Panel
6. Storyboard Image Generation
```

每个阶段又对应独立的执行 Agent，而不是让一个超级 Agent 同时承担所有工作。

这与 Infinite Atelier 的节点体系非常适合。

可以映射为：

| Toonflow 阶段Infinite Atelier 中的集成方式 |                           |
| ---------------------------------- | ------------------------- |
| Director Planning                  | 创建导演规划节点，关联 MONOFORM 镜头预演 |
| Derived Asset Analysis             | 分析角色、场景、服装、道具缺口           |
| Derived Asset Generation           | 自动创建角色、场景、道具生成节点          |
| Storyboard Table                   | 创建结构化分镜表节点                |
| Storyboard Panel                   | 每个镜头创建分镜节点                |
| Storyboard Image Generation        | 调用现有图片模型批量生成分镜            |
| Video Generation 扩展                | 将通过审核的分镜接入现有视频节点          |
| Audio 扩展                           | 接入对白、旁白、音效和配音节点           |

Infinite Atelier 已经有图片、视频、音频、文本和 Director 类型节点，而且节点类型允许使用开放字符串扩展插件节点，因此从 UI 扩展角度并不困难。

真正的难点不是“画出新节点”，而是后端工作流、版本、任务和状态管理。

---

## 4. 三层 Agent 协作

这是最值得集成的核心能力之一。

### Decision Agent

只负责：

- 判断用户意图；
- 识别当前生产阶段；
- 决定调用哪个 Execution Agent；
- 决定是否需要监督；
- 处理修改、重做、通过和进入下一阶段。

它不直接完成剧本、分镜或角色生成。

### Execution Agent

一个 Agent 尽量只做一个窄任务，例如：

```text
StorySkeletonAgent
AdaptationStrategyAgent
ScriptWritingAgent
DirectorPlanAgent
AssetAnalysisAgent
StoryboardTableAgent
StoryboardPanelAgent
StoryboardGenerationAgent
```

这种 Context Isolation 可以减少：

- Tool 选择错误；
- 提示词互相干扰；
- 工作流跳步；
- 状态幻觉；
- 输出结构不稳定。

你上传的技术文档对此有完整说明。

### Supervision Agent

Supervisor 必须：

1. 独立读取数据库或实际画布状态；
2. 检查最终产物；
3. 输出规则化审核报告；
4. 不相信 Execution Agent 的“我已经完成”。

Toonflow 的监督思路包括：

- 资产引用是否合法；
- 是否忠实于剧本；
- 镜头描述是否具体；
- 父资产与派生资产是否正确；
- 节奏、Hook、反转和情绪布局；
- 红线规则；
- A/B/C/D 等级评价。

这项能力对 Infinite Atelier 的提升很大，因为目前它可以生成图，但不真正知道：

- 角色脸是否前后统一；
- 服装是否突然变化；
- 分镜有没有漏掉剧本信息；
- 镜头轴线是否异常；
- 视频镜头是否与分镜一致；
- 某个资产是否引用了错误版本。

---

# 三、持久化记忆集成后会带来什么

Infinite Atelier 当前有画布聊天会话，但这不等于 Agent 长期记忆。其项目结构主要保存 `chatSessions`，没有短期记忆、摘要记忆和向量召回的独立系统。

Toonflow 的记忆系统包含：

```text
Recent Memory
+
Summary Memory
+
Semantic RAG
```

还提供二阶段的 Deep Retrieve：

```text
用户查询
→ Summary 向量搜索
→ LLM 筛选相关摘要
→ relatedMessageIds
→ 恢复原始消息
```

集成后可以解决长期短剧项目最常见的问题：

### 角色一致性

```text
女主：
银色短发
左眼下方有痣
外冷内热
不穿高跟鞋
第一集受伤后左臂有绷带
```

这些约束不会只存在于一段很早的聊天中，而会成为可召回的项目事实。

### 生产决策连续性

Agent 可以记住：

- 用户否决过哪些画风；
- 为什么重做某场戏；
- 哪个视频模型在本项目中效果不好；
- 某个角色的参考图最终选了哪一版；
- Supervisor 之前发现过什么问题；
- 用户习惯优先保留哪些镜头。

Toonflow 的 Decision、Execution 和 Supervision 输出都会进入共享记忆，因此后续决策不仅知道用户说了什么，也知道系统此前做过什么、审核发现了什么。

---

## 但不应 1:1 照搬 Toonflow 的记忆实现

你上传的文档已经指出原实现的一些局限：

- 向量以 JSON 保存；
- 查询采用全表暴力扫描；
- 没有稳定的相似度阈值；
- 当前用户消息可能自己召回自己；
- 摘要可能混合不同 Agent 角色；
- 缺少多层摘要；
- 单纯 Top-K 容易召回无关内容。

建议在 Infinite Atelier 中改进为：

```text
桌面本地版：
SQLite + sqlite-vec + FLOAT32/BLOB

SaaS 版：
PostgreSQL + pgvector
```

记忆应拆成：

```text
Episodic Memory
发生过什么

Semantic Memory
角色、世界观、项目事实

Procedural Memory
用户偏好的工作方式

Artifact Memory
素材、镜头、版本和文件 ID
```

此外，“章节事件图谱”应与聊天记忆分开：

```text
Event Graph = 原著和剧本事实
Agent Memory = 用户与系统的交互历史
Workspace DB = 当前真实生产状态
```

三者不能混成一个向量库。

---

# 四、集成后最理想的完整工作流

```text
导入小说 / 故事 / 剧本
        │
        ▼
章节拆分与事件图谱
        │
        ▼
Script Decision Agent
        │
        ├─ 故事骨架 Execution
        ├─ 改编策略 Execution
        └─ 剧本 Execution
        │
        ▼
Script Supervisor
        │
        ├─ PASS
        ├─ FIX
        └─ REDO
        │
        ▼
结构化剧本进入画布
        │
        ▼
Production Decision Agent
        │
        ├─ 导演规划
        ├─ 资产分析
        ├─ 角色/场景/道具生成
        ├─ 分镜表
        ├─ 分镜画面
        ├─ 分镜图生成
        └─ 视频镜头生成
        │
        ▼
Production Supervisor
        │
        ├─ 剧本忠实度
        ├─ 角色一致性
        ├─ 资产引用
        ├─ 镜头连贯性
        └─ 生成质量
        │
        ▼
用户通过 / 修改 / 重做
        │
        ▼
视频、配音、音效、字幕、导出
```

在这个体系中，Infinite Atelier 原有的能力不会被浪费：

- 原有图片节点成为角色图、场景图和分镜图生成器；
- 视频节点成为 Shot Renderer；
- 音频节点成为对白和旁白生成器；
- MONOFORM 成为 Director Planning 的镜头预演工具；
- 素材库成为项目 Asset Registry；
- 无限画布成为 Production Workspace；
- 多模型通道成为 Agent 和生成任务的模型网关。

---

# 五、推荐的产品形态

不建议将整个应用强制改造成只能做短剧的 Toonflow 类产品。

应保留两个模式。

## 模式一：自由画布

保持 Infinite Atelier 当前体验：

```text
自由添加节点
自由配置模型
自由生成图片/视频/音频
自由组织素材
```

适用于：

- 概念设计；
- 广告创意；
- 单张图片；
- 短视频；
- 视觉探索；
- 任意 AI 工作流。

## 模式二：短剧工作室

新增结构化入口：

```text
小说改编
剧本生成
角色圣经
资产圣经
导演规划
分镜生产
镜头生成
质量审核
成片导出
```

用户既可以让 Agent 自动运行，也可以随时进入画布手工修改。

推荐产品结构：

```text
首页
├─ 自由画布
├─ 短剧工作室
├─ 素材库
├─ 提示词库
├─ Agent 中心
├─ 工作流运行记录
├─ 导演台
└─ 模型配置
```

这样既获得 Toonflow 的垂直生产能力，又不会破坏 Infinite Atelier 的通用价值。

---

# 六、推荐技术架构

考虑到 Infinite Atelier 当前只是浏览器前端，而 Toonflow 是 TypeScript、Express、SQLite、Socket.IO 和 Electron 的完整客户端架构，直接合并会形成两个状态系统、两个模型层和两个桌面运行方式。([GitHub](https://github.com/HBAI-Ltd/Toonflow-app "GitHub - HBAI-Ltd/Toonflow-app: Toonflow 是开源一站式 AI 短剧创作工具，将小说、剧本快速转化为动画短剧。集成 AI 编剧、智能分镜、角色与视频生成，跨平台桌面端轻量部署，助力创作者低成本批量产出视觉内容。Toonflow is an open-source AI tool that turns stories and scripts into animated short dramas. Features AI scriptwriting, storyboarding, character and video generation. A cross-platform desktop app for efficient content creation. · GitHub"))

更适合你的方案是：

```text
┌──────────────────────────────────────────┐
│ React / Vite Frontend                    │
│                                          │
│ ├─ Free Canvas                           │
│ ├─ Drama Studio                          │
│ ├─ Asset Library                         │
│ ├─ Agent Console                         │
│ └─ MONOFORM                              │
└─────────────────────┬────────────────────┘
                      │ Wails Bindings / HTTP / SSE
                      ▼
┌──────────────────────────────────────────┐
│ Go Core Service                          │
│                                          │
│ ├─ Domain Services                       │
│ │  ├─ Novel                              │
│ │  ├─ Script                             │
│ │  ├─ Production                         │
│ │  ├─ Storyboard                         │
│ │  └─ Asset                              │
│ │                                        │
│ ├─ Agent Runtime                         │
│ │  ├─ Decision                           │
│ │  ├─ Execution                          │
│ │  └─ Supervision                        │
│ │                                        │
│ ├─ Workflow Engine                       │
│ ├─ Memory Service                        │
│ ├─ Provider Gateway                      │
│ ├─ Job Manager                           │
│ ├─ Secret Manager                        │
│ └─ Backup / Migration                    │
└─────────────────────┬────────────────────┘
                      ▼
┌──────────────────────────────────────────┐
│ SQLite / sqlite-vec / Local Files        │
│                                          │
│ projects                                 │
│ novels / chapters / events               │
│ scripts / scenes / shots                 │
│ assets / versions / lineage              │
│ workflow_runs / stage_runs               │
│ review_reports / jobs                    │
│ memories / summaries / embeddings        │
└──────────────────────────────────────────┘
```

本地桌面版建议：

```text
Wails + Go + React + SQLite + 本地文件系统
```

未来 SaaS 版可以替换为：

```text
React
→ Go API
→ PostgreSQL / pgvector
→ Redis 或 NATS
→ Worker
→ S3 / MinIO
```

而 Agent、Workflow 和 Provider 的核心接口可以共用。

---

# 七、工作流不能只写在 Prompt 里

Toonflow 很多工作流阶段由 Decision Skill 判断，这种设计适合原型，但不应成为正式产品唯一的状态来源。

建议增加：

```sql
workflow_runs
stage_runs
review_reports
generation_jobs
artifact_versions
```

你上传的技术文档也建议由数据库记录真实状态，让 Prompt 负责智能决策，而不是让 LLM 成为唯一状态机。

例如：

```text
workflow_run
├─ current_stage
├─ status
├─ episode_id
├─ retry_count
└─ active_stage_run_id

stage_run
├─ stage
├─ execution_agent
├─ input_json
├─ output_json
├─ status
├─ attempt
└─ timestamps

review_report
├─ score
├─ passed
├─ severity
├─ issues_json
└─ supervisor
```

运行时应是：

```text
Prompt 决定“应该做什么”
数据库决定“现在真实处于什么状态”
Tool 决定“允许 Agent 做什么”
Supervisor 决定“结果是否达标”
用户决定“通过、修改还是重做”
```

---

# 八、Agent 输出必须结构化

Execution Agent 不应该只返回：

```text
“已经完成分镜生成。”
```

而应返回：

```json
{
  "status": "success",
  "stage": "storyboard_panel",
  "artifacts": [
    {
      "type": "storyboard_panel",
      "id": "panel_001",
      "version": 2
    }
  ],
  "warnings": [],
  "nextAction": "review"
}
```

Supervisor 应返回：

```json
{
  "passed": false,
  "score": 76,
  "severity": "major",
  "issues": [
    {
      "rule": "CHARACTER_CONSISTENCY",
      "entityId": "shot_013",
      "problem": "角色服装与前一镜头不一致",
      "suggestion": "重新引用 character_asset_v4"
    }
  ]
}
```

最后再由 Decision Agent 将结果转换为自然语言给用户。

这也是你上传的复现文档推荐的正式产品实现方式。

---

# 九、画布需要扩展的不只是节点，还包括连线语义

Infinite Atelier 当前连接数据只有：

```text
id
fromNodeId
toNodeId
```

没有连接类型。

集成后需要：

```json
{
  "id": "edge_1",
  "fromNodeId": "character_1",
  "toNodeId": "shot_12",
  "relationType": "referenced_by",
  "port": "character_reference",
  "required": true
}
```

建议支持的连接类型：

```text
contains
adapts_to
references
derived_from
continues_from
generated_by
reviewed_by
supersedes
first_frame_of
last_frame_of
```

这样 Agent 才能真正理解节点关系，而不是只看到任意两点之间有一根线。

---

# 十、集成后会增强哪些关键指标

## 1. 从“手工工作台”变成“自动生产系统”

当前用户需要：

```text
自己创建节点
自己组织提示词
自己决定下一步
自己检查结果
自己维护上下文
```

集成后可以：

```text
目标输入
→ Agent 拆解
→ 自动生成节点
→ 自动调用模型
→ 自动检查
→ 用户只处理质量门
```

## 2. 长项目一致性明显增强

通过事件图谱、资产引用、版本和记忆，可以控制：

- 角色造型；
- 场景空间；
- 道具状态；
- 时间线；
- 服装变化；
- 剧情因果；
- 镜头连续性；
- 用户长期偏好。

## 3. 生成结果可追溯

每个资产可以知道：

```text
由哪个 Agent 生成
使用哪个模型
来自哪个剧本版本
引用哪些角色和场景
使用什么提示词
经过哪次审核
被哪个后续镜头采用
```

## 4. 更适合批量生产

可以按：

```text
项目
→ 剧集
→ 场次
→ 镜头
→ 任务
```

批量调度，而不是逐个节点手动点击。

## 5. 商业产品差异化更强

仅有“无限画布 + 多模型生成”的产品较容易同质化。

加入：

```text
领域模型
+ Agent 工作流
+ 质量审核
+ 事件图谱
+ 长期记忆
+ 人机协同质量门
```

之后，产品价值会从“模型 UI”提升为“短剧生产操作系统”。

---

# 十一、不能直接照搬的部分

## 1. 不建议直接移植 Toonflow 的 Node/Express 后端

Toonflow 当前使用 TypeScript、Express、SQLite、Socket.IO 和 Electron；Infinite Atelier 当前主要是 React/Vite 浏览器应用。([GitHub](https://github.com/HBAI-Ltd/Toonflow-app "GitHub - HBAI-Ltd/Toonflow-app: Toonflow 是开源一站式 AI 短剧创作工具，将小说、剧本快速转化为动画短剧。集成 AI 编剧、智能分镜、角色与视频生成，跨平台桌面端轻量部署，助力创作者低成本批量产出视觉内容。Toonflow is an open-source AI tool that turns stories and scripts into animated short dramas. Features AI scriptwriting, storyboarding, character and video generation. A cross-platform desktop app for efficient content creation. · GitHub"))

直接移植会产生：

- 双重状态管理；
- 双重 Provider 配置；
- 两套素材存储；
- 两套项目模型；
- 两套桌面启动方式；
- 难以维护的前后端边界。

更合理的是提炼其行为、数据模型、Skill 和工作流，再用 Go 重建核心服务。

---

## 2. 不要复制 Prompt-only 状态机

可以保留 Decision Skill，但必须增加显式工作流状态。

推荐：

```text
Decision Skill
+
Workflow Engine
+
Database State
```

而不是：

```text
只靠 LLM 回忆自己进行到了哪一步
```

---

## 3. 不要把所有阶段都强制审核

三层 Agent 会增加模型调用、延迟和费用。

应设置 Quality Gate：

```yaml
stages:
  event_extraction:
    supervision: false

  story_skeleton:
    supervision: true

  adaptation_strategy:
    supervision: true

  asset_analysis:
    supervision: false

  storyboard_table:
    supervision: true

  storyboard_image:
    supervision: conditional

  final_episode:
    supervision: true
```

你上传的文档也建议只在关键阶段启用监督，并限制 `FIX/REDO` 最大重试次数。

---

## 4. 不要延续任意 JavaScript 模型脚本机制

Infinite Atelier 当前允许用 `new Function` 运行用户模型脚本，并将：

- API Key；
- Base URL；
- HTTP 请求函数；
- Prompt；
- 图片；
  -消息；

直接传入脚本。

如果再把 Agent 自动工具调用接入这个机制，风险会明显放大：

```text
Agent
→ 自动选择 Tool
→ Tool 调用动态脚本
→ 脚本拥有 API Key 和网络权限
```

推荐替换为：

```text
Go 编译期 Provider Adapter
+
声明式 Provider Manifest
+
严格域名白名单
+
每个 Agent 的 Tool 白名单
```

Supervisor 原则上只应拥有只读工具，你上传的文档也明确推荐这一安全边界。

---

## 5. 先修复密钥和备份问题

Infinite Atelier 当前配置对象包含各模型通道的 `apiKey`，而完整备份会把整个 `config` 写入 `backup.json`。

正式集成前应改为：

```text
普通备份：不包含密钥
加密备份：用户单独启用
桌面密钥：保存到操作系统凭据库
日志：自动脱敏
Agent：永远不能读取原始密钥
Provider Adapter：只接收密钥句柄
```

---

# 十二、许可证问题必须单独处理

Infinite Atelier 当前是 MIT License。([GitHub](https://github.com/newyngwieslash-ops/infinite-atelier-mine-0903/blob/main/LICENSE "infinite-atelier-mine-0903/LICENSE at main · newyngwieslash-ops/infinite-atelier-mine-0903 · GitHub"))

但 Toonflow 当前并不是实践意义上的“只有标准 Apache-2.0”。仓库 LICENSE 在 Apache-2.0 后增加了补充商业协议，其中规定：

- 将软件或衍生版本作为产品提供给两个及以上独立第三方，需要书面商业授权；
- 部分内部使用场景免费；
- 不得删除或修改相关标识和版权信息。

你上传的技术文档许可证部分只记录了 Apache-2.0，按当前仓库状态看，这一部分已经不完整，需要更新。

因此：

### 内部项目

按仓库当前补充条款，二次开发供自己团队内部使用属于其列出的免费场景，但仍应保留必要声明并核对具体边界。

### 对外销售、SaaS 或客户端发行

不能简单认为：

```text
Toonflow = Apache-2.0
所以可以任意复制后商用
```

如果直接复制其源码并形成对外产品，应事先确认商业授权。

### 推荐路线

从风险控制角度，更合适的是：

1. 保留 Infinite Atelier 的 MIT 前端代码；
2. 依据公开产品行为、工作流和你已有技术文档重新设计；
3. 用 Go 独立实现 Agent Runtime、Workflow、Memory 和领域模型；
4. 不直接复制 Toonflow 的受约束源码、品牌和界面；
5. 正式商业发布前由专业律师审核许可证边界。

这不是法律意见，但在工程和商业风险上明显更稳妥。

---

# 十三、推荐实施顺序

## Phase 0：先建立安全后端

```text
Go Core
SQLite
文件存储
Secret Store
Provider Gateway
Persistent Job Manager
SSE/WebSocket
```

同时移除前端任意脚本直接获得 API Key 的机制。

## Phase 1：建立短剧领域模型

实现：

```text
Novel
Chapter
StoryEvent
Episode
Script
Scene
Shot
Character
Location
Prop
Storyboard
AssetVersion
WorkflowRun
ReviewReport
```

并让画布节点引用这些实体。

## Phase 2：实现 ScriptAgent

实现：

```text
故事骨架
→ 改编策略
→ 剧本生成
→ Supervisor
→ 用户质量门
```

## Phase 3：实现 ProductionAgent

实现：

```text
导演规划
→ 资产分析
→ 资产生成
→ 分镜表
→ 分镜画面
→ 分镜图
→ 视频镜头
```

## Phase 4：实现持久化记忆

实现：

```text
Recent
Summary
Semantic
Deep Retrieve
Memory Scope
Importance
Hierarchical Summary
```

记忆隔离键建议：

```text
tenant/project/episode/agent/session
```

Toonflow 原有按项目、Agent 和剧集隔离记忆的思想值得保留。

## Phase 5：事件图谱和一致性引擎

建立：

```text
人物
地点
事件
因果
时间线
角色状态
道具状态
关系变化
```

并让 ScriptAgent、StoryboardAgent 和 Supervisor 使用同一事实层。

## Phase 6：成片生产能力

继续增加：

```text
镜头批量生成
首尾帧控制
配音
对白
音效
字幕
时间线
镜头替换
视频拼接
导出
```

---

# 最终建议

**集成 Toonflow 的关键能力，确实会让 Infinite Atelier 显著变强。**

但应该明确产品边界：

```text
Infinite Atelier
不是变成 Toonflow 的另一个 Fork

而是：

通用视觉工作台
+
短剧领域模型
+
三层 Agent Runtime
+
持久化语义记忆
+
可恢复 Workflow
+
质量控制闭环
```

最推荐的目标产品形态是：

> **Infinite Atelier Core + Drama Production Pack**

其中：

- Infinite Atelier 保留自由、通用、视觉化的优势；
- Toonflow 的思想负责自动化、结构化和长期一致性；
- Go 后端负责安全、状态、任务、记忆和模型网关；
- 画布负责把小说、角色、场景、分镜和视频生产过程可视化；
- MONOFORM 负责导演和镜头预演；
- Supervisor 负责质量门；
- 用户保留最终的通过、修改和重做权。

这条路线不是简单“增加几个功能”，而是会把 Infinite Atelier 从一个优秀的本地 AI 视觉工作台，提升为一个具有明显产品壁垒的 **AI 影视与短剧生产平台**。
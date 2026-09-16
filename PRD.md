# Infinite Atelier Drama Studio 产品需求文档（PRD）

> 项目代号：Infinite Atelier Core + Drama Production Pack  
> 文档版本：v1.0  
> 文档状态：已批准，作为实现事实源  
> 首发形态：本地优先的跨平台桌面应用  
> 首发平台优先级：Windows 优先，随后 macOS、Linux  
> 核心技术方向：React/Vite 现有前端 + Go 核心服务 + Wails 桌面容器 + SQLite + 本地文件存储  
> 目标仓库：`newyngwieslash-ops/infinite-atelier-mine-0903` 的演进版本

---

## 0. 文档使用规则

1. 本 PRD 是产品范围、行为和验收标准的最高事实源。
2. 技术边界以 `docs/ARCHITECTURE.md` 为准，数据定义以 `docs/DOMAIN_MODEL.md` 为准，安全边界以 `docs/SECURITY.md` 为准。
3. 实施顺序以 `docs/ROADMAP.md` 为准；编码 Agent 每次只执行一个工作包。
4. 若代码现状与本文冲突：先记录差异，不得静默改变需求；优先通过兼容迁移实现本文目标。
5. 本项目只复现公开可观察的产品能力和架构思想，不直接复制 Toonflow 的受约束源码、品牌、界面文案或素材。
6. 现有 Infinite Atelier 的 MIT 代码、版权声明及第三方许可证必须保留并建立依赖清单。

---

# 1. 执行摘要

Infinite Atelier 已具备自由无限画布、多模型配置、图像/视频/音频节点、本地素材库、生成历史和 MONOFORM 导演预演，但主要仍是由用户手工组织节点和模型调用的视觉工作台。

本项目将在不破坏原有自由画布能力的前提下，新增一个结构化的“短剧工作室”，把小说、故事或剧本逐步转化为：

```text
原著/创意
→ 章节与事件图谱
→ 故事骨架
→ 改编策略
→ 结构化剧本
→ 角色/场景/道具资产
→ 导演规划
→ 分镜表
→ 分镜面板与分镜图
→ 视频镜头
→ 配音/字幕/音效
→ 成片导出
```

系统核心由五部分组成：

```text
Infinite Atelier Core
+ Drama Production Pack
+ Three-layer Agent Runtime
+ Durable Workflow Engine
+ Persistent Semantic Memory
```

三层 Agent 的职责必须严格分离：

- Decision Agent：理解意图、读取真实工作流状态、选择执行 Agent、触发审核、处理通过/修订/重做。
- Execution Agent：每次只完成一个窄领域步骤，并通过受限工具读写业务状态。
- Supervision Agent：使用独立模型调用和只读工具重新读取工作区事实，生成结构化质量报告。

数据库是项目事实来源，画布是事实的视觉投影。Prompt 负责智能决策，数据库负责真实状态，工具权限负责安全边界，用户负责最终质量门。

---

# 2. 背景与问题

## 2.1 当前产品优势

现有 Infinite Atelier 已形成较好的视觉创作基础：

- 无限画布与节点连接；
- 图片、文本、视频、音频、导演节点；
- OpenAI/Gemini 风格及自定义通道配置；
- 文生图、图生图、多参考图与基础图片编辑；
- 本地素材管理；
- 生成历史；
- MONOFORM 镜头预演；
- 本地项目备份。

## 2.2 当前产品缺口

现有项目主要面向“单次生成”和“手工编排”，缺少长周期短剧生产所需的结构化能力：

- 不理解 Novel、Episode、Scene、Shot 等领域实体；
- 没有小说到剧本的阶段化工作流；
- 没有角色、场景、服装、道具的稳定资产圣经；
- 没有事件图谱、人物状态和因果连续性；
- 没有持久任务、失败恢复和跨会话续跑；
- 没有 Decision/Execution/Supervision 的质量闭环；
- 没有长期 Agent 记忆与深层历史召回；
- API Key、浏览器代理和任意 JavaScript 模型脚本不适合产品化；
- 画布状态承担过多业务事实，难以保证一致性和可追溯性。

## 2.3 市场与产品机会

大量 AI 视频工具只解决某一个生成环节，用户仍需在文档、图片工具、视频模型、素材文件夹和剪辑软件之间反复搬运。真正影响短剧批量生产的不是单一模型质量，而是：

- 结构化内容生产；
- 角色与资产一致性；
- 阶段化审批；
- 生成任务恢复；
- 可追溯版本；
- 模型供应商切换；
- 人机协同质量控制。

本项目的差异化目标不是成为“又一个模型调用界面”，而是成为本地优先的 AI 影视与短剧生产操作系统。

---

# 3. 产品定位

## 3.1 一句话定位

面向个人创作者和小型制作团队的本地优先 AI 视觉与短剧生产工作台，将小说、故事或剧本通过可审阅、可恢复、可追溯的 Agent 工作流转化为分镜和视频资产。

## 3.2 产品模式

产品必须同时保留两个入口，二者共享项目、素材、模型和任务基础设施。

### 模式 A：自由画布

用于：

- 概念设计；
- 广告创意；
- 单图和短视频生成；
- 任意多模态节点编排；
- 快速视觉探索；
- 非短剧工作流。

### 模式 B：短剧工作室

用于：

- 小说/故事导入；
- 改编规划；
- 剧本生成；
- 角色、场景、服装、道具资产管理；
- 导演规划与预演；
- 分镜表、分镜图、视频镜头生产；
- 质量审核、修订和版本追踪；
- 配音、字幕、音效和成片导出。

## 3.3 产品形态

MVP 是本地桌面应用，而不是公网 SaaS：

- 桌面容器：Wails v2 稳定线；
- 前端：保留现有 React/Vite/TypeScript；
- 核心服务：Go；
- 数据库：SQLite；
- 素材：本地文件系统；
- 密钥：操作系统凭据存储；
- 模型请求：只经 Go Provider Gateway；
- 运行状态：持久化到数据库；
- 前端不持有原始 API Key。

系统边界需为未来 SaaS 化保留接口，但 MVP 不实现多租户、订阅、Credits 或云协作。

---

# 4. 产品目标与非目标

## 4.1 核心目标

### G1：保留并增强现有自由画布

现有项目、节点、生成和素材能力在迁移后可继续使用，核心工作流不得因短剧模块加入而退化。

### G2：建立完整的短剧领域模型

系统能原生表达并关联：

```text
Novel / Chapter / StoryEvent
Episode / ScriptVersion / Scene / Shot
Character / Location / Prop / Costume / DerivedAsset
Storyboard / StoryboardPanel
WorkflowRun / StageRun / ReviewReport / GenerationJob
```

### G3：实现可恢复的三层 Agent 工作流

用户关闭程序、重启电脑或模型请求失败后，工作流仍可从数据库恢复，不依赖 LLM 自行回忆阶段。

### G4：提升长项目一致性

通过事件图谱、资产版本、语义连接、持久记忆和 Supervisor 检查，减少角色外观、服装、道具、场景、时间线与剧情因果的不一致。

### G5：实现供应商无关的安全模型网关

图像、视频、文本、音频和 Embedding 统一通过后端适配器调用；禁止前端执行任意供应商 JavaScript。

### G6：形成可追溯生产链

任一生成资产可以追溯：

- 来源实体与版本；
- Prompt 与参数；
- 使用模型与供应商；
- 生成任务；
- 上游参考资产；
- Agent 与工作流阶段；
- 审核报告；
- 后续采用关系。

## 4.2 非目标

MVP 不包括：

- 公网多租户 SaaS；
- 在线团队实时协作；
- 订阅、支付、Credits 账本；
- 自建 GPU 调度平台；
- 完整专业非线性剪辑器；
- 与 Premiere/After Effects 完全等价的时间线；
- 无人值守的一键全自动发布；
- 任意第三方 JavaScript 插件执行；
- 复制 Toonflow 的源码、品牌或受限制的 UI；
- 对所有模型服务的无限兼容承诺。

---

# 5. 固定产品决策

以下决策已经确定，编码阶段不得自行推翻：

1. **数据库是事实来源，画布是视觉投影。**
2. **保留自由画布，短剧工作室作为独立业务包接入。**
3. **Go 负责模型、任务、Agent、记忆、数据、密钥和备份；桌面 MVP 使用 Wails v2 稳定线。**
4. **React 负责交互和可视化，不直接持有供应商密钥。**
5. **桌面端优先；MVP 不做 SaaS。**
6. **工作流状态显式持久化，不能只写在 Prompt 中。**
7. **Decision、Execution、Supervision 必须是独立 LLM 调用。**
8. **Supervisor 默认只读，并重新加载真实业务状态。**
9. **Execution 与 Supervisor 输出必须通过 JSON Schema 校验。**
10. **每个阶段最多自动修订 2 次；超过后进入用户处理。**
11. **只有关键阶段默认启用 Supervisor，避免无控制成本。**
12. **禁止 `new Function`、`eval` 和等价的任意模型脚本。**
13. **普通备份不得包含 API Key。**
14. **生成任务必须可取消、可重试、可恢复、可审计。**
15. **素材采用内容哈希和版本血缘，不依赖临时 Blob URL。**
16. **不直接复制 Toonflow 受约束实现，采用独立实现。**

---

# 6. 用户与角色

## 6.1 目标用户

### U1：个人 AI 短剧创作者

需要从故事快速生成角色、场景、分镜和镜头，缺少大型制作团队，希望本地管理素材与模型密钥。

### U2：漫画/动画分镜创作者

关注角色一致性、镜头构图、视觉风格和批量分镜，不一定需要最终视频。

### U3：广告与短视频制作人员

需要保留自由画布，并复用结构化导演、资产和审核能力完成广告分镜与商单短片。

### U4：小型制作团队负责人

需要看到生产阶段、失败任务、成本估算、版本和审核问题，但 MVP 暂不提供实时多人协作。

### U5：模型与工作流高级用户

需要接入多个兼容供应商、配置不同任务模型、观察请求状态，并通过声明式 Manifest 扩展兼容接口。

## 6.2 系统角色

MVP 为单机单用户，不实现 RBAC；但内部权限仍按组件角色区分：

- User：最终批准、修订、重做和跳过阶段；
- Decision Agent：高层编排；
- Execution Agent：受限写入；
- Supervisor Agent：默认只读；
- Provider Gateway：唯一持有密钥使用权；
- Job Worker：执行持久任务；
- Workspace Repository：事实状态读写。

---

# 7. 核心用户旅程

## 7.1 首次启动与迁移

1. 用户安装并启动桌面应用。
2. 系统创建本地应用目录、SQLite 数据库和素材目录。
3. 用户可导入旧 Infinite Atelier 的 JSON/ZIP 备份。
4. 系统执行预检、展示可迁移项目/素材/模型配置。
5. API Key 不从普通备份自动导入；用户需在密钥设置页重新录入或从受支持的加密备份导入。
6. 迁移完成后，原自由画布项目可打开、编辑、生成和再次备份。

## 7.2 从小说创建短剧项目

1. 用户选择“短剧工作室 → 新建项目”。
2. 输入项目名、目标风格、语言、画幅、目标集数、单集时长。
3. 导入 TXT、Markdown、DOCX 或粘贴文本；PDF 可作为后续增强，不是 MVP 阻塞项。
4. 系统生成导入预览并按章节拆分。
5. 用户确认章节边界。
6. 系统创建 Novel、Chapter，并启动事件抽取任务。
7. 用户查看人物、地点、事件和因果关系，修正后锁定为当前事实版本。

## 7.3 小说到剧本

1. 用户选择一组章节或故事事件并创建 Episode。
2. Script Decision Agent 根据工作流状态选择 StorySkeletonAgent。
3. Execution Agent 生成结构化故事骨架并写入数据库。
4. Supervisor 独立读取章节、事件和骨架，输出 ReviewReport。
5. 用户选择通过、按问题修订或重做。
6. 通过后依次进入改编策略和剧本阶段。
7. 最终脚本拆分为 Scene 与 Shot 草案，并投射到画布。

## 7.4 资产与分镜生产

1. Production Decision Agent 读取剧本和现有资产。
2. 资产分析 Agent 输出缺失角色、场景、服装、道具和派生资产。
3. 用户确认资产清单和优先级。
4. 生成任务批量创建对应图像节点和资产版本。
5. 导演规划 Agent 创建镜头风格、摄影、光线、运动和节奏约束。
6. StoryboardTableAgent 将场景拆分成结构化镜头表。
7. Supervisor 检查剧本忠实度、资产引用和镜头可执行性。
8. 通过后批量创建 StoryboardPanel 并生成分镜图。
9. 用户可在自由画布手动修改任一节点；修改同步回领域实体并产生新版本。

## 7.5 视频与成片

1. 用户从通过审核的分镜创建视频任务。
2. 选择视频模型、时长、首尾帧、参考图、运动指令和生成数量。
3. 任务后台执行；应用关闭后可在下次启动恢复轮询或查询。
4. 用户选定镜头版本。
5. 可为对白和旁白创建 TTS 任务，并录入字幕。
6. 时间线按 Shot 顺序组织素材。
7. 系统调用本地 FFmpeg 生成预览或最终导出。

## 7.6 历史记忆与回忆

1. 每轮 Agent 对话自动召回近期、摘要和语义相关记忆。
2. 当用户提出“上次为什么重做”“之前定的角色规则”等请求时，Decision Agent 调用 Deep Recall。
3. Deep Recall 从摘要候选中筛选，再恢复原始消息和相关业务实体。
4. 用户可查看、编辑、固定、降权或删除记忆。

---

# 8. 信息架构与主要页面

```text
首页
├─ 最近项目
├─ 新建自由画布
├─ 新建短剧项目
└─ 导入旧项目/备份

自由画布
├─ 节点画布
├─ 生成面板
├─ 素材抽屉
├─ Agent 助手
└─ 运行历史

短剧工作室
├─ 项目总览
├─ 原著与章节
├─ 事件图谱
├─ 剧本工作台
├─ 角色圣经
├─ 场景圣经
├─ 道具/服装
├─ 导演规划
├─ 分镜表
├─ 分镜画布
├─ 视频镜头
├─ 音频与字幕
├─ 时间线与导出
└─ 质量中心

全局模块
├─ 素材库
├─ Agent 中心
├─ 工作流运行记录
├─ 任务中心
├─ MONOFORM 导演台
├─ 模型与供应商
├─ 记忆中心
├─ 备份与迁移
└─ 设置与诊断
```

---

# 9. 功能需求

## FR-001 现有功能兼容

### 需求

- 保留现有首页、自由画布、节点拖动、缩放、框选、连接、小地图、节点复制、图片裁剪与分割。
- 保留现有文本、图片、视频、音频、Group、Director 节点的可用能力。
- 保留素材库、提示词库、生成历史、主题和国际化框架。
- 保留 MONOFORM 源码及构建流程。
- 导入旧项目后，节点位置、连接、媒体引用、Prompt、模型设置和视口尽可能保持一致。

### 验收

- 现有基准项目导入后，节点和连接数量完全一致。
- 所有已支持的本地图片、视频和音频可以正常预览。
- 自由画布的核心交互通过 Playwright 回归测试。
- 迁移前后不存在静默丢失；不能迁移的字段必须生成报告。

---

## FR-010 桌面运行时与 Go Core

### 需求

- 使用 Wails 将现有 React 应用封装为真正桌面应用。
- Go Core 启动时创建数据目录、数据库、迁移、日志、任务 Worker 和事件总线。
- 前端通过 Wails Bindings 调用后端，通过事件订阅接收任务进度。
- 生产构建不得依赖浏览器直接访问供应商接口。
- 开发模式可使用 Wails Dev；不再保留“任意 target 的 Vite 代理”作为正式能力。

### 验收

- Windows 可构建单一安装包或可分发程序。
- 无 Node 运行时的目标机器可启动已构建应用。
- 断网时仍可打开项目、浏览素材、编辑文本和管理任务。
- Go Core 崩溃或异常退出后，不损坏已提交事务。

---

## FR-020 项目、原著与章节导入

### 需求

- 支持创建 `free_canvas` 与 `drama` 两类项目。
- Drama 项目设置包括：语言、画幅、目标分辨率、目标集数、单集时长、风格说明、默认模型策略。
- MVP 支持 TXT、Markdown 和粘贴文本；DOCX 支持应在 MVP 内完成；PDF 作为 V1。
- 导入流程必须提供：编码检测、文本预览、章节自动识别、手动合并/拆分、重复导入提示。
- 原始文件计算 SHA-256，保存导入来源和不可变原文副本。
- 章节文本修改产生新版本，不覆盖原始导入版本。

### 验收

- 10 万汉字文本可导入且 UI 不冻结。
- 章节拆分可人工修正并保存。
- 同一文件重复导入时给出明确提示。
- 每个 Chapter 可定位回原文字符范围或段落来源。

---

## FR-030 章节事件图谱

### 需求

事件图谱必须独立于聊天记忆和画布状态，作为故事事实层。

支持实体：

- Character；
- Location；
- Organization；
- Object/Prop；
- StoryEvent；
- Relationship；
- CharacterState；
- PropState；
- TimelineMarker。

支持关系：

- `participates_in`；
- `occurs_at`；
- `causes`；
- `precedes`；
- `reveals`；
- `conflicts_with`；
- `owns`；
- `transfers_to`；
- `changes_state`；
- `knows`；
- `related_to`。

系统应允许 Agent 提取候选事实，但候选事实在进入“已确认事实”前需通过用户或规则校验。

### 验收

- 可从章节生成结构化候选人物、地点和事件。
- 用户可合并重复实体并保留别名。
- 事件可以关联原文证据范围。
- 删除或修改章节后，受影响事实被标记为待复核而不是静默删除。
- ScriptAgent 和 Supervisor 可通过只读工具查询同一事实层。

---

## FR-040 ScriptAgent 剧本流水线

### 阶段

```text
S1 故事骨架
S2 改编策略
S3 结构化剧本
```

### S1 故事骨架

输出至少包括：

- 核心命题；
- 主线与支线；
- 主角目标、阻力和代价；
- 关键事件；
- 集数分配建议；
- 每集 Hook、推进、反转和结尾悬念；
- 忠实保留、合并、删减和新增项。

### S2 改编策略

输出至少包括：

- 目标受众；
- 单集时长与节奏；
- 视角；
- 场景压缩；
- 角色合并/调整；
- 信息揭示顺序；
- 视觉化难点；
- 成本与生成可行性；
- 风险和红线。

### S3 结构化剧本

每集包含：

- 标题与一句话梗概；
- 场次顺序；
- 场景、时间、内外景；
- 出场角色；
- 动作、对白、旁白；
- 情绪目标；
- 前置状态与结果状态；
- 与 StoryEvent 的映射；
- 预计时长；
- 可视化提示。

### 行为规则

- 每个阶段都创建不可变版本。
- 执行成功后根据 Quality Gate 决定是否触发 Supervisor。
- 用户可选择 PASS、FIX、REDO、MANUAL_EDIT、SKIP。
- FIX 使用 ReviewReport 的结构化问题作为输入。
- REDO 必须保留旧版本和比较信息。
- 阶段自动重试最多 2 次。

### 验收

- 工作流中断后可从当前阶段恢复。
- Agent 输出无法通过 Schema 时自动修复一次，仍失败则进入人工处理。
- 通过后的剧本可稳定拆成 Episode、Scene、ShotDraft。
- 每个结果可追溯输入章节、事件版本和模型调用。

---

## FR-050 角色、场景、服装、道具资产圣经

### 需求

ProductionAsset 类型：

- Character；
- Location；
- Prop；
- Costume；
- Vehicle；
- Creature；
- StyleReference；
- DerivedAsset。

每个资产包含：

- 稳定 ID；
- 名称、别名、描述；
- 结构化属性；
- 不可违反约束；
- 负面约束；
- 参考媒体；
- 当前已批准版本；
- 历史版本；
- 来源事件/剧本；
- 生成 Prompt 模板；
- 适用集数、场次或状态区间。

角色需支持状态版本，例如：

```text
角色基础形象
→ 第 1 集常服
→ 第 3 集受伤状态
→ 第 5 集战斗服
```

### 验收

- 同一角色多个状态可共存并按 Shot 引用。
- 替换批准版本时，系统列出受影响的分镜和镜头。
- 资产生成结果可比较、批准、淘汰和回滚。
- 任何派生资产都能追溯父资产及变换原因。

---

## FR-060 导演规划与 MONOFORM

### 需求

DirectorPlan 至少包括：

- 视觉风格；
- 摄影语言；
- 镜头尺寸分布；
- 构图原则；
- 光线与色彩；
- 镜头运动；
- 节奏与剪辑原则；
- 角色视线与轴线；
- 场景空间关系；
- 模型可执行限制。

MONOFORM 不得继续只是孤立 iframe。应建立受版本控制的双向消息协议，支持：

- 从 Shot/StoryboardPanel 打开预演；
- 发送角色站位、相机、镜头参数和场景参考；
- 保存预演快照和摄像机参数；
- 将结果写回 DirectorPlan 或 ShotVersion；
- 失败时不影响主项目数据。

### 验收

- 从一个 Shot 可打开导演预演并带入上下文。
- 保存后可在 Shot 中看到摄像机参数和预览图。
- 所有跨 iframe 消息校验来源、类型和 Schema。
- 不授予与功能无关的浏览器权限。

---

## FR-070 分镜表、分镜面板与分镜图

### Storyboard Table

每个镜头至少包含：

- Shot 编号；
- Scene；
- 预计时长；
- 景别；
- 机位；
- 镜头运动；
- 构图；
- 角色与动作；
- 表情；
- 场景/道具/服装引用；
- 对白/旁白；
- 音效建议；
- 首帧描述；
- 尾帧描述；
- 视频运动描述；
- 连续性备注；
- 上游 StoryEvent。

### Storyboard Panel

- 每个 Shot 可有多个面板版本；
- 面板可关联参考图、遮罩和生成参数；
- 面板生成应复用现有图像节点能力；
- 生成后自动进入 Asset Registry；
- 用户批准一个版本作为当前 Canonical Panel。

### Supervisor 检查

- 剧本信息是否遗漏；
- 角色和服装引用是否合法；
- 场景和道具状态是否一致；
- 景别和镜头变化是否可读；
- 轴线、视线和动作连续性；
- Prompt 是否足够具体；
- 视频模型是否可执行。

### 验收

- 可从一个 Episode 批量生成完整 Storyboard Table。
- 表格编辑与画布节点双向同步。
- 重新排序 Shot 后编号和上下游关系正确更新。
- 批量生成分镜时可暂停、取消和恢复。
- Supervisor 问题可定位到具体 Shot/Panel/Asset。

---

## FR-080 视频、音频、字幕与导出

### 视频

- 支持从 StoryboardPanel、首帧/尾帧或参考资产创建视频任务。
- Provider 适配器支持提交、轮询、Webhook（若供应商支持）、取消和结果下载。
- 任务状态必须持久化。
- 远程结果下载到本地资产存储后才能标记为完整成功；若仅保留远程 URL，状态为 `remote_only`。
- 同一 Shot 可保留多个视频版本并批准其中一个。

### 音频

MVP 支持：

- TTS 对白；
- 旁白；
- 音频文件导入；
- 基础音量与起止时间。

V1 支持：

- 音效建议与生成适配；
- 背景音乐导入；
- 简单混音；
- 多角色声线映射。

### 字幕

- 从剧本对白生成字幕草稿；
- 支持手动编辑时间码和文本；
- 支持 SRT/VTT 导出；
- 最终导出可选择烧录字幕或外挂字幕。

### 成片导出

- 使用本地 FFmpeg；
- 支持预览质量和最终质量；
- 支持横屏、竖屏和自定义分辨率；
- 输出包含导出清单和版本信息。

### 验收

- 应用重启后可继续查询未完成视频任务。
- 失败任务保留供应商错误、请求 ID 和可重试信息，日志中不包含密钥。
- 可将一集已批准镜头按顺序导出为 MP4。
- 字幕可独立导出并与镜头顺序一致。

---

## FR-090 Agent 中心

### 需求

显示：

- 当前 Decision Agent；
- 已注册 Execution Agent；
- Supervisor；
- 每个 Agent 的 Skill 版本、模型策略、允许工具；
- 最近调用、耗时、Token/费用估算、状态；
- 结构化输入与输出；
- 失败原因；
- Memory Scope；
- Workflow/Stage 关联。

用户可以：

- 启用/禁用特定 Agent；
- 修改模型策略；
- 查看 Skill；
- 创建 Skill 新版本；
- 回滚 Skill；
- 运行只读测试；
- 导出诊断包。

MVP 不允许普通用户为 Agent 注入任意可执行代码。

### 验收

- 每次 Agent 调用都有唯一 Run ID。
- 可以从 ReviewReport 追溯到执行和监督调用。
- 修改 Skill 后只影响新调用，历史记录保留旧版本哈希。

---

## FR-100 Durable Workflow Engine

### 状态

WorkflowRun 状态：

```text
pending
running
waiting_user
paused
completed
failed
cancelled
```

StageRun 状态：

```text
pending
running
execution_succeeded
reviewing
passed
needs_fix
needs_redo
waiting_user
failed
cancelled
```

### 必要能力

- 显式阶段依赖；
- 原子状态迁移；
- 幂等命令；
- 乐观并发或版本号；
- 失败重试；
- 用户暂停/恢复；
- 应用重启恢复；
- 事件日志；
- 阶段输入/输出快照；
- 用户质量门；
- 最大自动修订次数；
- 跳过阶段需记录原因。

### 默认质量门

```yaml
chapter_event_extraction:
  supervision: conditional
story_skeleton:
  supervision: true
adaptation_strategy:
  supervision: true
script_generation:
  supervision: true
asset_gap_analysis:
  supervision: false
asset_generation:
  supervision: conditional
storyboard_table:
  supervision: true
storyboard_panel_generation:
  supervision: conditional
video_generation:
  supervision: conditional
final_episode:
  supervision: true
```

### 验收

- 重复提交同一个幂等命令不会创建重复版本或重复任务。
- 应用强制关闭后，运行中任务在重启时进入可恢复状态。
- 状态迁移不允许跳过未满足依赖的阶段。
- 每次状态变化写入审计事件。

---

## FR-110 Supervisor 与质量中心

### 结构化 ReviewReport

```json
{
  "passed": false,
  "score": 76,
  "grade": "C",
  "severity": "major",
  "summary": "角色服装连续性与镜头信息完整度不达标",
  "issues": [
    {
      "rule": "CHARACTER_CONTINUITY",
      "entityType": "shot",
      "entityId": "shot_013",
      "field": "costumeVersionId",
      "problem": "角色服装与前一镜头不一致",
      "evidence": ["shot_012", "asset_version_004"],
      "suggestion": "重新引用 costume_version_004",
      "autoFixable": true
    }
  ],
  "nextAction": "fix"
}
```

### 质量规则分类

- Narrative：Hook、节奏、因果、反转、情绪；
- Fidelity：原著/剧本忠实度；
- Character：人物身份、外观、状态、行为；
- Asset：引用、血缘、版本；
- Spatial：空间、轴线、视线、位置；
- Temporal：时间线、动作和道具状态；
- Visual：构图、清晰度、风格、可生成性；
- Technical：参数完整、格式、模型限制；
- Safety：内容与供应商规则；
- Cost：不必要的高成本重试。

### 验收

- Supervisor 只能使用显式允许的只读工具。
- 审核前必须从 Repository 重新读取当前版本。
- 报告问题可以在 UI 中跳转到实体。
- 自动修复需生成新版本，不得覆盖被审核版本。

---

## FR-120 Persistent Memory

### 记忆类型

```text
Episodic Memory
- 用户、Decision、Execution、Supervisor 发生过什么

Semantic Memory
- 人物、世界观、规则、项目事实

Procedural Memory
- 用户偏好的工作方式、模型选择、审批习惯

Artifact Memory
- 资产、文件、版本、工作流和生成记录
```

### 召回通道

每轮自动构建：

```text
Recent Memory
+ Hierarchical Summaries
+ Semantic Candidates
+ Pinned Critical Facts
```

显式历史问题可调用：

```text
Query
→ Summary Vector Search
→ Threshold
→ Rerank
→ Summary Source IDs
→ Original Messages/Entities
```

### 隔离范围

```text
tenant(local-user)
/project
/episode(optional)
/agent-type
/session(optional)
```

### 必要规则

- 先召回，再写入当前用户消息，避免当前消息自召回。
- 向量使用 FLOAT32/BLOB 或向量索引，不使用 JSON 数组长期存储。
- 有相似度阈值和 Top-K；不得无条件返回低相关结果。
- 摘要保留来源消息关系和角色 provenance。
- 支持层级摘要：message → episode/session → project。
- 支持 importance、recency、semantic、role 权重融合。
- 用户可查看、固定、编辑、删除和重建 Embedding。
- Embedding Provider 可替换；本地模式不得在未授权时上传项目文本。

### MVP 与 V1

MVP：

- Recent；
- Summary；
- Semantic Recall；
- Deep Recall；
- BLOB 向量；
- 小规模精确搜索；
- Provider Embedding 与关键词降级。

V1：

- 许可核验后的本地多语言 ONNX Embedding；
- 可选 sqlite-vec；
- 层级摘要；
- 记忆管理 UI；
- 召回评测集。

### 验收

- 不同项目、集数和 Agent 的记忆不串扰。
- “之前确定的女主服装”能恢复相关原始消息和资产版本。
- 低于阈值的候选不会进入上下文。
- 删除记忆后，召回和摘要来源关系正确更新。
- 记忆构建符合 Token Budget。

---

## FR-130 画布语义化

### 节点引用

画布节点不再保存完整领域事实，只保存引用和显示状态：

```json
{
  "entityType": "storyboard_panel",
  "entityId": "panel_123",
  "versionId": "version_3",
  "workflowRunId": "workflow_456"
}
```

### 语义连线

连接必须扩展为：

```json
{
  "id": "edge_1",
  "fromNodeId": "character_1",
  "toNodeId": "shot_12",
  "relationType": "references",
  "port": "character_reference",
  "required": true
}
```

支持：

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
uses_character
uses_location
uses_prop
```

### 双向同步

- 领域实体变化后更新节点显示；
- 画布编辑业务字段时走 Domain Command，不直接写本地 Store；
- 仅位置、选中、折叠等 UI 状态留在画布层；
- 冲突必须提示，不允许静默覆盖。

### 验收

- Agent 能通过语义关系查询 Shot 使用的角色、地点和参考资产。
- 删除节点不默认删除领域实体；需要明确选择。
- 删除领域实体时列出所有引用并阻止破坏性删除。
- 旧无类型连线可迁移为 `generic`，不丢失。

---

## FR-140 Provider Gateway 与模型策略

### Provider Adapter

内置适配器：

- OpenAI-compatible Text/Image/Audio；
- Gemini-compatible Text/Image；
- 通用异步 Video Adapter；
- Embedding Adapter；
- 本地或自定义 HTTP Manifest Adapter。

### 声明式 Manifest

Manifest 只允许声明：

- Base URL；
- Endpoint；
- Method；
- Header 模板；
- Body 字段映射；
- 状态字段映射；
- 结果 JSONPath；
- 轮询规则；
- 能力和限制；
- 允许域名。

禁止脚本、动态代码、任意文件访问和任意网络访问。

### 模型策略

工作流可按能力配置：

```text
script_decision_model
script_execution_model
script_supervision_model
image_generation_model
video_generation_model
tts_model
embedding_model
```

支持项目默认、阶段覆盖和单次覆盖。

### 安全

- API Key 保存到 OS Keyring；
- 数据库只保存 Secret Reference；
- 前端只看到掩码；
- Provider 只在请求执行时解析密钥；
- 日志、错误和导出自动脱敏；
- 域名白名单和私网地址阻断；
- 超时、重试、最大响应和下载限制。

### 验收

- 前端运行时无法读取原始 API Key。
- 普通项目备份不包含密钥。
- 恶意 Manifest 无法请求回环地址、局域网地址或非允许域名。
- 每次调用记录模型、请求 ID、耗时、状态和估算成本，不记录敏感头。

---

## FR-150 Persistent Job Manager

### 任务类型

- LLM Agent；
- Embedding；
- Image Generation；
- Video Generation；
- Audio Generation；
- Asset Download；
- Thumbnail；
- Import；
- Export；
- Memory Summary；
- Migration。

### 能力

- SQLite 持久队列；
- Worker Pool；
- 优先级；
- 幂等键；
- 进度；
- 取消；
- 指数退避；
- 最大重试；
- 超时；
- 失败分类；
- 应用重启恢复；
- Provider 并发和速率限制；
- 依赖任务；
- 任务事件推送。

### 验收

- 100 个图片任务可排队，UI 不冻结。
- 同供应商并发不超过配置上限。
- 取消后不再创建新资产；已产生远程资源记录为 orphan candidate。
- 重启后运行中任务进入 `recovering`，按 Provider 能力恢复或安全失败。

---

## FR-160 素材存储、版本与血缘

### 存储

- 本地应用数据目录；
- Content-Addressed Storage，SHA-256 命名；
- 数据库记录逻辑资产和文件版本；
- 缩略图单独生成；
- 远程 URL 不是唯一事实来源；
- 支持引用计数和垃圾回收预览。

### 血缘

每个 AssetVersion 记录：

- parentVersionIds；
- generationJobId；
- promptSnapshot；
- modelSnapshot；
- parameters；
- sourceEntityIds；
- createdBy；
- reviewStatus；
- contentHash。

### 验收

- 相同文件只保存一份物理内容。
- 删除一个引用不会删除仍被其他项目使用的文件。
- 可查看任一资产的父版本、子版本和采用位置。
- 垃圾回收执行前显示将删除内容并支持取消。

---

## FR-170 备份、恢复与迁移

### 普通备份

包括：

- 数据库业务快照；
- 项目设置；
- Skills；
- Provider 非敏感配置；
- 素材和缩略图；
- Manifest、Schema 版本、校验和。

默认不包括：

- API Key；
- OS Keyring 内容；
- 敏感日志；
- 临时文件。

### 加密敏感备份

用户显式启用后：

- 使用密码派生密钥；
- 使用认证加密；
- 明确列出敏感内容；
- 不保存密码；
- 导入前展示风险。

### 迁移

- 数据库迁移可重复、可回滚或有可靠前向修复；
- 旧 Infinite Atelier 备份提供导入器；
- 导入前先验证 Schema、文件大小、压缩比、路径和校验和；
- 导入到临时空间，全部成功后原子提交。

### 验收

- 普通备份中搜索不到已配置 API Key。
- 修改任一媒体字节后校验失败并阻止静默导入。
- 导入失败不会破坏当前项目。
- 备份格式包含版本，旧版本有迁移测试。

---

## FR-180 设置、日志、诊断与隐私

### 设置

- 数据目录；
- 缓存上限；
- Worker 并发；
- Provider 超时；
- 默认模型策略；
- 记忆策略；
- 日志级别；
- FFmpeg 路径；
- 自动备份；
- 主题和语言。

### 日志

- 结构化日志；
- Trace/Run/Job/Workflow ID；
- 自动脱敏；
- 默认保留时间和大小上限；
- 一键生成诊断包；
- 诊断包生成前显示包含项目元数据的范围。

### 隐私

- 默认不启用远程遥测；
- 若以后加入崩溃上报，必须显式 Opt-in；
- 用户项目文本只发送给用户选择的模型供应商；
- 本地 Embedding 模式不外发文本。

### 验收

- 日志中不存在完整 Authorization、API Key 和本地敏感文件内容。
- 用户可清理缓存而不删除已批准资产。
- 诊断包可在脱敏预览后导出。

---

# 10. Agent Runtime 需求

## 10.1 Skill 文件化

目录：

```text
skills/
├─ script/
│  ├─ decision.md
│  ├─ supervision.md
│  └─ execution/
│     ├─ story_skeleton.md
│     ├─ adaptation_strategy.md
│     └─ script_generation.md
│
└─ production/
   ├─ decision.md
   ├─ supervision.md
   └─ execution/
      ├─ director_plan.md
      ├─ asset_analysis.md
      ├─ asset_generation.md
      ├─ storyboard_table.md
      ├─ storyboard_panel.md
      └─ storyboard_generation.md
```

每个 Skill 必须包含：

```text
Role
Goal
Input Contract
Workflow
Allowed Tools
Constraints
Output Contract
Failure Conditions
Quality Rules
Examples
Version
```

## 10.2 工具白名单

示例：

```text
Decision
├─ read_workflow_state
├─ invoke_execution_agent
├─ invoke_supervisor
├─ deep_recall
├─ request_user_gate
└─ pause_workflow

Execution:Storyboard
├─ read_script
├─ read_story_events
├─ read_assets
├─ create_storyboard_version
└─ write_storyboard_items

Supervisor:Storyboard
├─ read_script
├─ read_story_events
├─ read_assets
└─ read_storyboard_version
```

禁止将完整数据库、SQL、文件系统或全部 Tool 暴露给 LLM。

## 10.3 AgentResult

```json
{
  "status": "success",
  "stage": "storyboard_table",
  "artifacts": [
    {
      "entityType": "storyboard",
      "entityId": "sb_001",
      "versionId": "sbv_002"
    }
  ],
  "warnings": [],
  "nextAction": "review",
  "summary": "已创建 42 个镜头"
}
```

## 10.4 错误处理

- Tool Call 参数必须 Schema 校验；
- LLM 输出必须 Schema 校验；
- 可执行一次结构修复请求；
- 业务写入在事务中完成；
- Tool 超时和错误转为结构化 AgentError；
- 不能把“自然语言声称成功”当作执行成功；
- 成功必须以数据库写入和读取验证为准。

---

# 11. 数据模型摘要

详细字段见 `docs/DOMAIN_MODEL.md`。核心聚合：

```text
ProjectAggregate
├─ Project
├─ ProjectSettings
├─ Canvas
└─ ProviderPolicy

StoryAggregate
├─ Novel
├─ ChapterVersion
├─ StoryEntity
├─ StoryEvent
└─ StoryRelation

ScriptAggregate
├─ Episode
├─ StorySkeletonVersion
├─ AdaptationStrategyVersion
├─ ScriptVersion
├─ Scene
└─ Shot

AssetAggregate
├─ ProductionAsset
├─ AssetVersion
├─ AssetReference
└─ AssetLineage

StoryboardAggregate
├─ DirectorPlanVersion
├─ StoryboardVersion
├─ StoryboardItem
└─ StoryboardPanelVersion

WorkflowAggregate
├─ WorkflowRun
├─ StageRun
├─ ReviewReport
├─ WorkflowEvent
└─ UserGateDecision

RuntimeAggregate
├─ GenerationJob
├─ ProviderCall
├─ AgentRun
├─ MemoryItem
├─ MemorySummarySource
└─ SkillVersion
```

所有可修改核心实体必须包含：

```text
id
project_id
version/revision
created_at
updated_at
created_by
```

不可变内容版本不得原地覆盖。

---

# 12. 关键状态机

## 12.1 质量闭环

```text
Decision
  ↓
Execution
  ├─ failed → Stage Failed / User Action
  ↓
Persist + Verify
  ↓
Supervisor（按 Quality Gate）
  ├─ PASS → Waiting User or Next Stage
  ├─ FIX → New Execution Attempt
  ├─ REDO → New Version + New Attempt
  └─ MAJOR BLOCK → Waiting User
```

## 12.2 用户质量门

```text
waiting_user
├─ approve → passed / next
├─ fix → needs_fix
├─ redo → needs_redo
├─ manual_edit → new manual version
├─ skip → skipped with reason
└─ cancel → cancelled
```

## 12.3 Generation Job

```text
queued
→ running
→ awaiting_remote
→ downloading
→ verifying
→ succeeded

任意非终态：
→ retry_wait
→ failed
→ cancelled
→ recovering
```

---

# 13. 非功能需求

## NFR-001 性能

- 10 万汉字导入不冻结主线程；
- 1000 个可见/不可见节点的基准项目可平移和缩放；
- 节点渲染继续采用视口裁剪；
- 连接也需支持裁剪或分层优化；
- 项目打开 P95 小于 3 秒（不含首次缩略图生成）；
- 普通数据库命令 P95 小于 100ms；
- 任务进度更新不超过每秒 10 次写入 UI；
- 资产列表使用分页/虚拟化。

## NFR-002 可靠性

- 数据库启用 WAL、外键和合理 busy timeout；
- 所有多实体写入使用事务；
- 自动备份可配置；
- 崩溃后项目可重新打开；
- 运行中任务具有恢复策略；
- 任何迁移先备份；
- 不得用前端内存作为唯一任务事实源。

## NFR-003 安全

见 `docs/SECURITY.md`。最低要求：

- Secret 不进入前端、数据库明文、日志或普通备份；
- 阻断 SSRF；
- 禁止动态代码执行；
- 导入防 Zip Slip、Zip Bomb 和路径穿越；
- Supervisor 默认只读；
- Tool 最小权限；
- 文件类型、大小和内容验证；
- CSP 和 iframe 消息验证。

## NFR-004 可维护性

- Go 模块依赖方向清晰；
- `project.tsx` 等大型组件分解；
- 前端 Store 只保存 UI 和查询缓存，不保存唯一业务事实；
- Repository、Provider、VectorIndex、SecretStore 具备接口；
- 不引入不必要的大型 Agent 框架；
- 所有 Schema 有版本；
- 所有公共接口有错误语义。

## NFR-005 测试

- Go 核心单元测试和 Repository 集成测试；
- Provider 使用 Mock Server；
- Workflow 状态机属性/表驱动测试；
- Memory 召回评测；
- React 组件测试；
- Playwright E2E；
- 迁移与备份恢复测试；
- 安全回归测试；
- 大项目性能基准。

## NFR-006 可访问性与国际化

- 保留 i18n；
- 新文案不硬编码在组件；
- 关键操作支持键盘；
- 对话框焦点管理；
- 状态不只依靠颜色表达；
- 中文为首发主语言，英文结构预留。

---

# 14. 指标与成功标准

MVP 本地默认不上传遥测。通过本地可选统计和人工测试评估：

## 14.1 产品指标

- 新用户完成“导入文本 → 生成故事骨架”的成功率 ≥ 90%；
- 从批准剧本到首张分镜图的有效操作步骤明显少于纯自由画布；
- 中断后恢复工作流成功率 ≥ 95%；
- 通过资产版本和引用发现的连续性问题可定位率 ≥ 90%；
- 普通备份密钥泄露测试为 0；
- 旧项目迁移的可迁移节点/连接不丢失率为 100%。

## 14.2 质量指标

- Agent 结构化输出首次 Schema 通过率 ≥ 90%；
- 一次修复后 Schema 通过率 ≥ 98%；
- Supervisor 报告中问题实体定位率 ≥ 95%；
- 任务状态与实际文件结果一致率为 100%；
- 重复幂等命令产生重复资产率为 0。

---

# 15. MVP 范围

MVP 必须完成以下闭环：

```text
安装桌面应用
→ 导入旧自由画布项目
→ 创建短剧项目
→ 导入小说/故事
→ 章节拆分
→ 候选事件提取与确认
→ 故事骨架
→ 改编策略
→ 结构化剧本
→ 资产缺口分析
→ 创建并生成角色/场景资产
→ 导演规划
→ 分镜表
→ 分镜图
→ 视频镜头任务
→ 基础 TTS/字幕
→ 单集 MP4 导出
```

同时必须具备：

- Go Core 和 Wails；
- SQLite 领域模型；
- Provider Gateway；
- OS Secret Store；
- Persistent Job Manager；
- Decision/Execution/Supervision；
- Durable Workflow；
- Recent/Summary/Semantic/Deep Recall；
- Quality Center；
- 普通无密钥备份；
- 旧项目导入；
- 基础自动化测试和发布构建。

---

# 16. 版本规划

## MVP / v0.5

- 安全桌面后端；
- 领域模型与旧项目导入；
- ScriptAgent；
- ProductionAgent 至分镜图；
- 持久任务；
- 基础视频/TTS/字幕/导出；
- 基础持久记忆；
- Supervisor 和质量门。

## v1.0

- 本地多语言 ONNX Embedding；
- 事件图谱可视化；
- MONOFORM 深度双向集成；
- 完整资产一致性检查；
- 视频首尾帧与批量镜头生成；
- 层级摘要和记忆中心；
- 更完整的时间线、音效和混音；
- Windows 稳定安装与升级。

## v1.5

- macOS/Linux 打包；
- 工作流模板；
- 模型路由与成本预算；
- 资产批量替换和影响分析；
- 更大画布性能优化；
- 导出到外部剪辑格式的研究与适配。

## v2.0（不在当前实现范围）

- 可选团队工作区；
- 云同步；
- 多租户 Go API；
- PostgreSQL/pgvector；
- Worker 集群；
- S3/MinIO；
- 订阅、Credits 和审计。

---

# 17. 风险与缓解

## R1：两个项目机械合并导致架构混乱

缓解：不移植 Toonflow Node/Express 后端；按领域行为独立用 Go 实现。

## R2：动态模型脚本放大密钥泄露

缓解：移除任意脚本；使用编译期 Adapter 和声明式 Manifest。

## R3：一次性重写导致现有画布退化

缓解：建立基准回归；按工作包渐进迁移；先兼容再替换。

## R4：Agent 工作流依赖 Prompt 导致状态漂移

缓解：显式 Workflow/Stage 状态、事务和幂等命令。

## R5：三层 Agent 成本和延迟过高

缓解：关键阶段 Quality Gate；不同层使用不同模型策略；最大重试；缓存和预算。

## R6：本地 ONNX 跨平台打包复杂

缓解：Embedding 接口化；MVP 支持 Provider 与关键词降级；本地 ONNX 在许可和打包验证后启用。

## R7：SQLite 在大量向量下性能下降

缓解：BLOB 存储、阈值和限域；VectorIndex 接口；后续 sqlite-vec；SaaS 迁移 pgvector。

## R8：视频供应商异步协议差异大

缓解：统一 Job/Provider Contract；内置适配器逐个验证；未知协议通过声明式有限 Manifest 接入。

## R9：许可证风险

缓解：保留 Infinite Atelier MIT 声明；Toonflow 只作为能力研究参考，禁止复制受约束源码；建立 THIRD_PARTY_NOTICES；商业发布前单独审查。

---

# 18. 发布阻断条件

出现以下任一情况不得发布：

- 前端或普通备份可获取完整 API Key；
- 仍存在任意模型 JavaScript 执行路径；
- API 代理可访问回环、私网或任意地址；
- 工作流运行状态只存在内存；
- 视频结果未验证即标记成功；
- 数据库迁移无备份或回滚/修复路径；
- Supervisor 可使用未授权写工具；
- 导入可路径穿越或 Zip Bomb；
- 旧项目迁移存在静默丢失；
- 核心 E2E 测试未通过；
- 许可证和第三方声明缺失。

---

# 19. MVP 总体验收场景

## AC-E2E-001 旧项目兼容

给定一个包含文本、图片、视频、音频和连接的旧项目备份：

- 成功导入；
- 节点与连接数量一致；
- 本地媒体可预览；
- 可创建新图片任务；
- 导出新格式备份；
- 新备份不包含密钥。

## AC-E2E-002 小说到分镜

给定一篇不少于 3 章、3 万汉字的中文故事：

- 导入并拆章；
- 生成事件候选；
- 创建 3 集规划；
- 完成故事骨架、改编策略和第 1 集剧本；
- 通过 Supervisor；
- 创建资产清单；
- 生成至少 2 个角色、2 个场景；
- 创建不少于 12 个镜头的分镜表；
- 生成分镜图；
- 所有结果可追溯到输入、模型、任务和版本。

## AC-E2E-003 中断恢复

在视频任务和 Storyboard 工作流运行时强制关闭应用：

- 重启后项目可打开；
- Workflow 显示真实阶段；
- 可恢复任务继续查询；
- 不可恢复任务明确失败并可重试；
- 不产生重复资产。

## AC-E2E-004 质量修订

人为制造一个角色服装引用错误：

- Supervisor 定位到具体 Shot；
- ReviewReport 提供证据和建议；
- FIX 产生新版本；
- 旧版本保留；
- 通过后 Workflow 进入下一阶段。

## AC-E2E-005 深层记忆

在早期对话中确定角色禁用红色服装，经过大量后续操作后询问：

- Deep Recall 找到相关摘要；
- 恢复原始消息；
- 返回对应角色/资产事实；
- 不召回其他项目内容；
- 可从 UI 查看来源。

## AC-E2E-006 安全

- 普通备份不包含密钥；
- 日志不包含 Authorization；
- 恶意 Manifest 无法访问 `127.0.0.1`、局域网和文件协议；
- 恶意 ZIP 无法路径穿越；
- 前端 DevTools 无法从 Store 读取密钥；
- Supervisor 无法调用写工具。

---

# 20. Definition of Done

一个工作包只有同时满足以下条件才可标记完成：

1. 范围内功能真实实现，不是占位符、静态假数据或不可达代码。
2. 相关数据迁移、错误处理和恢复路径完成。
3. 单元、集成或 E2E 测试覆盖关键路径。
4. 现有基准功能回归通过。
5. 安全要求通过。
6. 文档、Schema、示例和配置同步更新。
7. `docs/implementation/STATUS.md` 更新实际状态和已知问题。
8. `docs/ROADMAP.md` 的验收项逐条记录结果。
9. 无未解释的 lint、typecheck、test、build 失败。
10. Git diff 中无密钥、生成物、临时文件和无关重构。

---

# 21. 参考文档优先级

编码 Agent 应按以下顺序读取：

1. `PRD.md`
2. `AGENTS.md`
3. `docs/implementation/STATUS.md`
4. `docs/ROADMAP.md`
5. `docs/ARCHITECTURE.md`
6. `docs/DOMAIN_MODEL.md`
7. `docs/AGENT_CONTRACTS.md`
8. `docs/SECURITY.md`
9. `docs/ACCEPTANCE.md`
10. `docs/reference/INTEGRATION_ANALYSIS.md`
11. `docs/reference/TOONFLOW_AGENT_MEMORY_ANALYSIS.md`
12. 当前仓库代码与测试

参考分析用于理解设计来源，不覆盖本 PRD 的已批准决策。


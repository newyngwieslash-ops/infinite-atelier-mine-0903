<p align="center">
  <img src="web/public/brand-logo.png" width="112" alt="Infinite Atelier logo">
</p>

<h1 align="center">Infinite Atelier</h1>

<p align="center">为桌面创作而生的 AI 视觉工作台</p>

Infinite Atelier 将画布编排、图片生成、参考图、提示词、资产管理和导演预演放在一个连贯的桌面工作流中。项目由 `GuiYi-Xi` 独立维护，界面、品牌与内置提示词库围绕高效视觉创作重新设计。

## 产品概览

- **用户问题**：创作者需要在模型配置、提示词、参考图、生成结果和本地素材之间频繁切换，长任务状态与失败恢复也缺少统一入口。
- **产品方案**：以无限画布为主工作区，将 Provider 配置、生成节点、提示词库、本地资产和导演预演组织成连续流程。
- **我的工作**：负责场景梳理、功能规划、交互与视觉设计、前端实现、模型接口封装、Windows 启动流程、测试和使用文档。
- **验证方式**：仓库提供完整源码、产品截图、演示视频和可复现的本地启动步骤；所有配置与生成历史默认保存在本机。

这个项目展示的是模型能力从 API 到用户产品的封装实践，不涉及 GPU 集群或企业级模型托管部署。

## 功能

- 无限创作画布：组织图片、文字、音频、视频和生成结果。
- 多渠道模型：配置 OpenAI 兼容接口及自定义中转 API。
- 图片生成：支持 GPT Image 2 等模型的文生图、图生图与多图参考。
- 提示词库：内置 12 组带展示图的提示词，可复制、收藏、替换封面或新增条目。
- 视觉资产：保存生成结果与素材，支持导入、导出和本地备份。
- 导演台：内置 MONOFORM 预演工具，用于镜头、角色和动作设计。
- 品牌主页：五套整体配色、动态品牌背景与最近项目入口。

## 演示与导演台

- [观看 Infinite Atelier 项目演示视频（MP4，约 63 MB）](https://github.com/GuiYi-Xi/infinite-atelier/releases/download/v1.0.0/Infinite-Atelier-Demo.mp4)
- [MONOFORM 素形白模预演工作台源码](https://github.com/GuiYi-Xi/monoform-previs-studio)
- [导演台使用教程（哔哩哔哩）](https://www.bilibili.com/video/BV1HNud6SEgs/)

## 运行模式

### 浏览器开发模式

现有自由画布仍可通过 Vite 在浏览器中开发。安装 Node.js 22 LTS 后，在仓库根目录执行：

```powershell
cd web
npm ci --legacy-peer-deps
npm run dev -- --host 127.0.0.1
```

Vite 只应绑定到 `127.0.0.1`；浏览器地址形如 `http://127.0.0.1:3000`。浏览器模式继续使用既有本地浏览器存储，不会调用 Wails Binding，也不会显示桌面核心状态。

`start.bat` 仍可用于现有 Windows 浏览器启动流程；它会检查 Node.js 与 Vite 依赖。首次安装会在 `web/node_modules` 创建大量已忽略的依赖文件。

### 桌面开发与生产构建

WP-01 使用 Wails v2、Go 1.25 和系统 WebView 承载同一 React 应用。Windows 需要 Node.js 22 LTS、Go 1.25、Microsoft Edge WebView2 Runtime，以及可用的 Windows 编译工具链。安装固定 Wails CLI：

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0
```

确保 Go 的 bin 目录在 `PATH` 中，然后从仓库根目录运行：

```powershell
wails dev
wails build
```

`wails dev` 仅用于本地桌面开发；`wails build` 生成嵌入前端的 Windows 可分发程序到 `build/bin/`，运行已构建程序不需要目标机器安装 Node.js 或 Vite。不要提交 `build/bin/`、`build/appicon.png` 或 `build/windows/`。`web/dist/.gitkeep` 是零字节嵌入目录占位文件，执行 Vite 或 Wails 构建清理后应恢复它，不应提交其他 `web/dist` 文件。

桌面模式的 WP-01 数据目录由 Go 管理，默认位于当前用户配置目录下的 `InfiniteAtelier`，其中包括 SQLite 数据库、受管文件、临时文件、日志和迁移快照。现有浏览器画布数据尚未迁移到该数据库；WP-01 不读取或迁移已有浏览器数据。

## 验证

在已安装现有前端依赖后，从仓库根目录运行对应平台脚本：

```powershell
./scripts/verify.ps1
```

```sh
./scripts/verify.sh
```

两个脚本都会运行前端 typecheck、前端安全测试（`npm test`）、production build、可用的 MONOFORM source build、`go test ./... -count=1`、`go vet ./...`，以及安全静态扫描（动态执行、密钥模式、未受保护的配置序列化）。安装 Wails CLI 时脚本还会执行 `wails build`；未安装时会报告固定版本的安装命令及该 production gate 的 SKIP。当前仓库仍没有前端 lint script，脚本会如实报告 SKIP。

## 当前范围

WP-01 提供 Wails、Go Core、SQLite foundation、FileStore、日志和只读 health binding。

WP-02 增加安全 Provider 基础（仅文本链路）：

- API Key 保存在 Windows 凭据管理器；数据库只保存密钥引用，普通备份与配置文件不包含密钥；
- 文本生成经 Go Provider Gateway 执行受控 HTTPS 请求（域名/IP/端口策略、DNS 固定、重定向复检、TLS 校验、超时与响应大小限制），并记录脱敏调用审计；
- 桌面模式下模型调用脚本不可达，URL 传入的 API Key 会被忽略并提示。

WP-03 增加持久任务与图像 Provider：

- 任务持久化在 SQLite：排队、优先级、租约、重试退避、取消、失败分类，关闭应用后未完成任务在下次启动时恢复或安全失败；
- **图片生成与编辑**改由 Go 任务核心执行：密钥不进入前端，结果先经内容校验再以内容寻址方式入库，成功必须对应已提交的文件引用；
- Provider 返回的远程结果 URL 走单独下载策略：仅 HTTPS、拒绝私网/回环/云 metadata、逐跳复检、流式大小上限；
- 异步远程任务按固定间隔轮询（`RemotePollInterval`，默认 5 秒），轮询本身不占用重试次数；已有远程 ID 的任务重启后只轮询、不会重复提交；取消时若无法确认远端已停止，会记录为待人工确认的孤儿任务；
- 「任务中心」页面可查看队列、进度与失败原因，支持暂停/恢复、批量取消、仅重试失败；
- 视频与音频目前只有契约与确定性 Mock（真实适配器属后续工作包）；界面仍走既有浏览器直连链路，启动时会明确提示该范围，安全扫描器拒绝新增浏览器直连调用。

WP-04 把项目、画布与旧数据迁入 Go Core：

- 项目、画布文档、节点、连线、聊天会话、资产与版本、生成历史持久化在 SQLite，带 revision 并发保护与级联删除；
- **桌面模式下画布通过持久化适配器读写 Go Core**（`CanvasPersistenceAdapter`），画布的节点/连线/视口不再写入 localForage；浏览器开发模式仍由原有存储承担，因为那里没有 Go Core；
- 工具栏新增「导入旧项目」：预检 → 确认 → 导入 → 报告。导入在一个事务内完成，失败不留半成品；第二次导入会检测已导入并跳过，可选择「导入副本」；浏览器原始数据不会被修改；
- 画布未建模的字段（插件节点、供应商扩展字段）原样保留并生成迁移告警，不静默丢弃；
- 普通项目备份 v1 由 Go 生成（清单 + 数据库快照 + 文件 + 校验和），不含密钥；恢复前逐项校验清单、校验和与数据库完整性。
- 自由画布回归由 Playwright 覆盖（增删改、多选、框选、缩放、平移、撤销重做、连线、小地图、生成面板），并接入两套 verify 脚本。

WP-05 增加短剧领域模型与工作室外壳（**不调用任何 LLM**）：

- 短剧数据模型落地：项目设置/规则/风格指南/模型策略、原著与章节、故事事实层（实体/别名/事件/参与者/关系/证据/冲突/角色状态）、剧集与剧本流水线（骨架/策略/剧本/场次/对白/镜头）、资产圣经（含版本、血缘与用途）、导演规划与分镜、工作流与审阅、以及 stale 标记；
- 版本词表统一在 `internal/domain/versioning`（§2.5 的八个状态），八个版本族共用一份，且每个族都有「同一父实体最多一个 approved」的局部唯一索引；
- **画布语义化**：`CreateCanvasProjection` 写入节点实体引用，`CreateEdge` 按关系注册表校验端点并把判定写入 `validation_status`，移除节点不删除实体，必填引用会阻止删除，`FindEntityReferences` 列出引用与阻塞项；旧无类型连线仍为 `generic`；
- 领域规则强制在命令路径上：锁定/不可变规则只有用户命令可改，非用户写入者既不能提升也不能降低规则强度；用户决策不可由 Agent 伪造；审批切换前有影响分析；
- stale 传播按 §15.2 的依赖图行走（每条边都对应 schema 中真实存在的列），直接消费者标 `review_required`、传递消费者标 `informational`，waiver 必须记录用户决策与理由；
- 「剧集工作室」页面提供 14 个分区导航与创建向导（语言、画幅、分辨率、目标集数、单集时长、受众、分级、改编模式）；有后端命令的分区读写真实数据，尚无命令的分区显示诚实的空状态并注明归属工作包，**不放置任何假数据**；
- 生成的 Wails 绑定新增 `DramaBinding`（52 个方法）与 `AssetsBinding`，并补齐了此前缺失的 `BackupBinding` 类型；
- **领域事件流**：`domain_events` 保存 §17 的 28 个事件与固定 envelope；其中 20 个由触发它们的命令发出，两类写入语义明确区分——审批类命令把事件写进自己的事务，没有记录器就拒绝审批（治理记录不可缺），其余命令在自身写入成功后尽力发送（行已提交，不因日志失败而谎报命令失败）；
- **八个版本族共用一份审批实现**（取代旧批准、批准新版本、同一事务），因为两步写入的顺序是 schema 局部唯一索引可满足的前提；
- 本包**不包含**：Agent 运行时、记忆、真实分镜生成（分别是 WP-07/10/09）；其余 7 个事件属尚未存在的工作包，逐条列在 STATUS §0f。（WP-06 已交付文档解析，且同样不接入 LLM——解析是纯函数，抽取走显式端口。）

WP-06 实现原始文档导入、章节确认与事件图谱（**文档与章节由代码解析，抽取走显式端口；本包不接入任何 LLM**）：

- **导入 TXT / Markdown / DOCX**：格式由字节决定而非扩展名——一个改了名的 `.docx` 按真实内容处理；编码检测覆盖 UTF-8（含 BOM）、UTF-16（按 BOM）、GBK 与 GB18030，无法无损解码的内容被拒绝而不是猜测；
- **DOCX 只读正文**：只读 `[Content_Types].xml`（确认是 OOXML）与 `word/document.xml`，取 `<w:t>` 与段落/表格边界；不读样式、脚注、批注，不解析 `.rels`，**不访问任何外部关系或 URL**；容器经加固的 ZIP 读取器，路径穿越、压缩炸弹与条数炸弹由其统一拒绝；
- **章节检测是纯函数**：中文章节标记、Markdown 标题与英文 `Chapter N` 由同一套文本规则识别，因此同一段文字的 DOCX 与 TXT 产生相同的边界与偏移；边界可人工调整（改标题/偏移会把 `source_kind` 记为 `manual`）并确认；
- **原文定位**：章节的偏移指向该版本文本，证据行记录版本、章节与偏移，前端按 64 KiB 分页读取，单次响应由核心侧封顶；
- **重复导入会提示**：同一文件哈希再次导入默认被拒绝，并指出它已作为哪个文档导入过；用户显式确认后才作为新版本导入，旧版本不被覆盖；
- **EventExtraction 契约**：`schemas/agent/event_extraction.v1.json` 是权威契约（随二进制嵌入），由 `internal/application/validation` 编译并校验；契约封闭且**没有 status 字段**，因此模型无法自行批准自己提出的事实；
- **AGENT_CONTRACTS §14.3 的一次修复**：校验失败时把「违规规则 + JSON Pointer」回传同一个抽取器一次并重新校验；回传内容不包含文档原文，避免把不可信文本带进下一次提示；
- **候选写入**：抽取只写 `candidate`；引用由服务解析（模型给出的是局部 ref，不是数据库 ID），悬空引用与类型不符的引用被拒绝而不是丢弃；接受/拒绝/**锁定**与冲突记录（开启、解决、列出）都在图谱界面可用；
- **别名、参与者与证据**写入落地（ADR-0007 记录为缺口的那三条），并新增图谱列表查询；
- **章节变更触发 stale 传播**：`ReviseChapter` 之后按 §15.2 的依赖图标记受影响事实，传播失败不影响已落库的修改；
- **大文档传输是分块的**：文档以 64 KiB 分块 base64 上传并在结束时校验总长度，因此 10 万字小说不会作为一条消息（更不会作为 30 万个元素的数组）压在主线程上；
- 本包**不包含**（已如实记录，未偷跑）：Agent 运行时与工具白名单、章节**拆分/合并**命令、通用「修改事实」命令（WP-07/后续）、`PropState` 建模。裁定与理由见 `docs/adr/0010-document-import-and-extraction.md`，验收明细见 `docs/implementation/STATUS.md` §0g。

WP-07 实现三层 Agent 运行时、Skill 加载器、Workflow 引擎与质量门（**不接入任何付费 Provider**）：

- **三层运行时是同一套 runner**：Decision / Execution / Supervision 由 `AgentLayer` 与 Tool 模式矩阵区分，而不是三份代码——矩阵是安全核心：Decision 只能读与控制，Execution 可按阶段读写，Supervision **只能读**，且该规则在注册表构造时与每次调用时各校验一次；
- **Agent 清单来自内置 Skill Pack**：`skills/script`（8 个 agent）与 `skills/production`（9 个）；清单是 JSON 而非 §4.1 所示 YAML——不为一个文件引入依赖，且 YAML 的别名展开是清单这种授权文件的真实攻击面（ADR-0011 §2）；WP-07 交付时两份文档都只是带 §4.3 十三个小节的骨架（范围 16 的要求），**WP-08 填实了 script 包的全部 8 份，WP-09 填实了 production 包的全部 9 份**，两包现在都是真实文档；
- **Tool 表是真实的十九个工具**：每个 handler 都调用一个应用服务，没有占位实现；工具输入 Schema 由 `scripts/gen-tool-schemas.mjs` 从**同一份 key 列表**生成，三个测试把两个方向钉住（注册的必有 Schema、生成的必已注册、Schema 的字段名就是 handler 解码的字段名）；
- **§6.2 示例里的两个 Tool 按裁定缺席而非占位**（ADR-0011 §6）：`story.create_event_candidates` 会是抽取阶段第二条写入路径（真正的路径是运行时实现 WP-06 的 `Extractor` 端口、由抽取服务校验并写入），`provider.submit_image_job` 是 Job 而不是 Tool（§19 明确「媒体生成本身由 Job/Provider Service 执行」）；
- **结构化解码**：每个 agent 的输出按其清单声明的契约校验（§7.1–7.7 的七份 Schema，随二进制嵌入）；失败时把「路径 + 规则」回传同一个模型**一次**（§14.3），回传内容不含文档原文；两次失败即失败该阶段，且**不产生任何业务半写入**（用例通过前后计数产物行来断言）；
- **Artifact 幻觉被拒绝**：写入工具报告的是真实写入的行的 ID，运行时读回校验；不存在的 ID、**未知的实体类型**、以及查询本身失败，三者被区分开——最后一种是存储故障而不是幻觉（AC-AGENT-003）；
- **Tool Call 与校验后的文档并列传输**，而不是塞进文档内部：§7 的每份输出 Schema 都是 `additionalProperties: false`，工具调用放在文档里必然被 Schema 拒绝——这个缺陷曾经让整条工具路径对所有真正校验过的 agent 不可达，是 Mock 的第一份真实文档把它照出来的（ADR-0011 §5）；
- **Workflow 引擎**：PRD FR-100 的十个阶段策略（监督与用户门设置由该表决定）、`StartStage`、`ApplySupervision`、`ApplyGate`；迁移 `000015` 用**部分唯一索引**把 §11.2 的「最多一个 active attempt」从注释变成约束；FIX/REDO **复用同一个 attempt 行**（§10.2 的「新 Attempt」与 WP-05 已交付的 `IsActive` 冲突，裁定见 ADR-0011 §4），因此修复预算改从**审计事件**计数——用 attempt 计数永远不可能触发，那是一个无界循环；
- **取消被记录为 `cancelled` 而不是 `failed`**（§15），取消在阶段行与运行行上都能区分出来；
- **确定性 Mock LLM**：§18.3 的八个场景全部实现（正常、一次无效后有效、Tool Call、拒绝非法 Tool、Supervisor issues、超时、取消、Provider 错误）；它只能经 `KindMockText` 触达，而该 kind 被应用层拒绝且被数据库 CHECK 拒绝，因此**任何用户可持久化的配置都无法选中它**；CI 不调用真实模型；
- **基础 Memory Port（仅 Recent）**：结构化六字段 scope 既编码成 key 也拆成列（§14.4 的「不依赖脆弱字符串前缀」），并且实现了 §14.5 两条会被静默违反的不变量——当前消息不召回自身、其他项目内容不可召回；
- **Agent Center 分区**：按层筛选运行记录、查看一次运行的 Tool Call 与消息、以及本构建可运行的 agent 清单（含每个 agent 被允许调用的工具 key）。**没有从界面发起运行的入口**——运行由应用服务发起，这让「谁可以跑 agent」留在服务端；该缺口如实记录为 STATUS §0h 的 PARTIAL 项；
- **Canary（关键验收）**：Decision → Execution → Supervisor → User Gate 全链路跑在**真实迁移过的数据库**上，使用真实 Skill Pack、真实 Tool 表与真实校验器，只由确定性 Mock 驱动；运行产生的每一次状态迁移都能在审计事件里读到；
- 本包**不包含**：完整 Script Agent（WP-08）、完整 Production Agent（WP-09/11）、Semantic Memory 与向量索引（WP-10）、真实付费 Provider 调用与媒体生成。实现期间发现并修掉的九个已交付代码缺陷（工具路径不可达、`workflow.Service` 缺 `GetRun`、四处在项目边界检查路径上的缺失读取、`ToolRequest` 缺运行 ID、`finish` 丢弃 revision、拒绝路径丢弃运行 ID、Mock 的三处缺陷）逐条记在 `docs/adr/0011-agent-runtime-tools-and-stage-keys.md`；验收明细见 `docs/implementation/STATUS.md` §0h。

WP-08 实现 Script Agent 层：骨架、策略与剧本三个阶段（**不接入任何付费 Provider**）：

- **三层各自独立调用**：每个阶段都有 Execution 与 Supervision 两个 agent，外加 `script.decision`，共 8 个；三段产物分别落到 `story_skeleton_versions`、`adaptation_strategy_versions`、`script_versions`；
- **八份真实 Skill 文档**（`skills/script/**`）：不再是骨架，而是每个 agent 的职责、信任边界、工具白名单、步骤、领域约束、质量规则、失败条件与示例。`scripts/gen-skill-packs.mjs` 改为**只写缺失的文件**——否则再跑一次生成器会抹掉它们；`--check` 对清单逐字节比对，对文档只检查 §4.3 的十三个小节是否齐全（文档是「写」的，不是「生成」的）；
- **整版本一次写入**：`script.create_script_structure` 在一个事务里写入一个版本的全部场次、对白与镜头，边界显式（200 场 / 每场 500 行 / 200 镜）且**超界拒绝而不是截断**；payload **不接受 ID、序数或总时长**——序数是数组位置，时长由场次求和（§17 把「ID、顺序和唯一性」「时长求和」放在代码一侧，见 ADR-0012 §1、§3）；
- **锁定字段在写入路径强制**：迁移 `000017` 新增 `script_version_field_locks`，三个版本族共用一张表（`version_id` 故意不是外键——三个族在三张表里，由写入路径保证诚实）。AC-SCRIPT-002 的场景是**骨架**缺结尾钩子，所以锁必须落在骨架字段上而不只是台词；新版本的锁定字段与基准版本不等即**拒绝**，无论模型是否“理解”了提示（ADR-0012 §2）；
- **两张 link 表终于有了写入者**：骨架选中的事件、策略对每个事件的处理（保留/删除/重排）随版本在**同一事务**写入；§7.4/§7.5 把这两个集合定义为产物本身的一部分，缺了它们版本就是“什么都没决定的策略”（ADR-0012 §7）；数组顺序即改编顺序，没有单独的 ordinal 字段；
- **引用存在性由服务检查**：`scenes.source_story_event_id` 与两张 link 表的 `story_event_id` **都没有外键**（引用是出处，比被引用的行活得久），所以“这个事件存在吗”是服务的职责，拒绝时**点名缺失的 ID**，便于模型自我修正；
- **阶段→Agent 映射是显式的**：`Registry.SupervisionFor` 用 key 的**末段**匹配阶段名，对 `script_generation` **必然失败**（它的监督者是 `script.supervision.script`）。修法不是放宽启发式——这一对没有任何可匹配的共同片段，只能**写下来**；测试断言注册表在**能回答的地方**与映射一致，并断言那个不一致**正是唯一的那一处**（ADR-0012 §4）；
- **FIX 回路从决策行读回**：用户在闸门上点 FIX 时写的 findings 存在 `user_gate_decisions.issue_ids_json`，下一次尝试从**数据库**读回并渲染成 prompt 的一层（`fixIssueIds`），锁定的字段同样（`lockedRefs`）。§7.3 说这两个值「由运行时从数据库提供，Agent 不能添加」——线程传参会在两次调用之间的一次重启后丢失，而那时「针对具体问题重跑」就变成了空话（ADR-0012 §4）；
- **闸门批准的是产物**：`approve` / `manual_edit` / `skip` 必须指名它让哪一版生效，且**先批准产物再移动阶段**——顺序如此，失败时留下的是「可以重试的状态」，而不是「阶段已通过、却没有任何版本被批准」。这条是 canary 找出来的：原实现只移动阶段，于是工作流可以报告所有阶段通过，而项目里没有任何版本生效（ADR-0012 §7）；
- **模型策略按层解析**（§13）：命名 provider → 该层策略 → `default` → 第一个启用的 provider；策略指向**已禁用** provider 时**拒绝**而不是跳过（静默换一个会把提示词发到项目所有者没选的地方）；FR-140 的按**阶段**键记为延后，理由见 ADR-0012 §5；
- **版本 diff 与画布投影**：diff 是纯函数、按 ordinal 对齐，每个条目带锁定状态；投影把版本的场次写成真实画布节点——WP-05 建好这个写入者却**没有调用者**，WP-08 补上了组合根里的 projector（ADR-0012 §11）；
- **Script 分区做实**：三个阶段的运行按钮、三族版本历史与批准、内容查看、锁定开关、版本 diff、闸门决策（PASS/FIX/REDO/MANUAL_EDIT/skip，skip 要求理由）与投影；
- **Canary（关键验收）**：骨架 → 监督 → PASS → 策略 → 监督 → PASS → 剧本（版本行 + 结构）→ 监督 → PASS → `approved`，全部跑在**真实迁移过的数据库**上，只由确定性 Mock 驱动；同时覆盖 AC-SCRIPT-001/002/003（approved 唯一性用 SQL 计数断言、锁定字段正向反向、序数负面用例、时长求和、版本 diff、画布投影、已批准版本拒绝写入）；
- **`testdata/canary-drama/` 补齐 §18.1 要求的三份**：已批准事实、期望骨架关键点、**故意错误剧本**（五处错误各自点名它破坏的规则，且每处都被单独喂给校验器验证）；
- 本包**不包含**：Production/Storyboard Agent（WP-09/11）、真实付费 Provider 调用、媒体生成、Semantic Memory（WP-10）、按阶段（而非按层）的模型策略、极长剧集的流式结构传输（ADR-0012 §1 记为范围外）。**两轮独立评审**（一轮对账规范、一轮 148 个变异）发现并修掉的问题逐条记在 `docs/adr/0012-script-pipeline-payload-locks-and-stage-map.md`；验收明细与**本包未被测试覆盖的三处**见 `docs/implementation/STATUS.md` §0i。

WP-09 实现 Production Agent 层：资产、导演与分镜（**不接入任何付费 Provider**）：

- **阶段机制被抽出，两层共用一份实现**：`internal/application/stagepipeline` 持有阶段机器（attempt、FIX 读回、闸门的顺序、manual edit 的两步），`scriptpipeline` 退化为一层——只声明阶段→agent 映射、批准、锁定读取与用户版本写入，`productionpipeline` 是第二层。WP-08 自己的评审找出过四个「两处对同一事实各执一词」的缺陷，所以闸门没有写第二遍（ADR-0013 §2）；
- **`director_plan` 成为阶段**：§10.1 对它给了 `supervision: conditional` / `userGate: required`，而 FR-100 的列表和引擎的策略表都没有它——一个规范描述过却没有名字的阶段会拿到**默认策略**，文档给它的设置在无人察觉中丢失。现在参考列表是 11 项（FR-100 是 10 项），测试写明原因（ADR-0013 §1）；
- **迁移 `000018`**：`storyboard_items` 补上 FR-070 的 `first_frame_description` / `last_frame_description` / `video_motion_description`——WP-05 把这三项推给了 Shot，但 Shot 是**剧本**的草稿、这三项是**分镜决策**，此前无处存放；另建 `asset_gap_reports` / `asset_gap_items`，含每集唯一批准索引。只前向增加，未改动任何已发布迁移；
- **Asset Gap Report**：域里只有一条规则最重要——`satisfied` 必须指名满足它的资产、`missing` 必须不指名（模型最常把状态填上而把引用留空）；`UnresolvedRequiredItems` 在**没有已批准报告时拒绝**，而不是返回空缺口列表——「没有分析」不等于「什么都不缺」（ADR-0013 §8）；
- **溯源补全**：`asset.Version` 拿回迁移 000009 一直有、而映射器一直丢掉的五列（`parent_asset_version_id`、`variant_type`、`seed`、`source_agent_run_id`、`created_by_id`）——AC-ASSET-002 的「parent refs」「agent/stage」在此之前**无法满足**；`AttachJobResult` 把成功的 job 变成 **candidate** 版本，而这个状态此前构建里**无人写入**，所以面板永远不可能有候选可批准；
- **影响边**：`storyboard_panel_version` 现在消费它批准的 asset version，于是 §15.1 的「AssetVersion 默认批准版本切换」成为真正**到达某处**的触发器。它从**被替换**的那一版传播——第一版从新批准的版本传播，接线测试抓住了它；
- **批量**：`CheckStoryboardGate`（未通过时**阻止**批量，且不提交任何 job）、`RunImageBatch`（每镜 N 个候选、并发有上限、幂等键含 `(item, candidateIndex)` 所以重启不重复提交）、`CollectBatchResults`（成功的 job 变成 candidate 版本并写下 panel usage）、`ApproveCandidate`（经 §9.5 自己的规则批准一张）；
- **工具**：新增 `script.read_shots`（分镜阶段此前**无法列出它要分镜的镜头**，只能猜 ID）、`asset.create_gap_report`、`asset.read_gap_report`，`storyboard.create_storyboard_version` 现在写入**行**而不只是版本行；
- **确定性 Mock**：`KindMockImage` 产出**可解码的真实 PNG**（不是固定字节串），纹理由 prompt 与候选序号决定，所以同一镜的两个候选不同、重试可复现；文本 Mock 增加五个 production execution 分支与 supervision 分支。**它不被任何生产组合注册**——`ImagePortFor` 按配置的 kind 分派而 `IsUserConfigurableKind` 与数据库 CHECK 都拒绝 `mock_image`，注册它就会是一条**永远走不到**的分支（ADR-0013 §6）；
- **九份 production Skill 真实文档**；「production 仍是骨架」的断言被**反转**，现在要求两包都是散文；
- **MONOFORM 桥（基础版，用户选定）**：版本化信封 + source/schemaVersion/**nonce**/**origin**/大小校验，全部在 `web/src/services/desktop/monoform-bridge.ts` 一处；`open_shot` 把某个 Shot 的景别、机位与描述带进预演，`shot_updated` 把相机写回导演规划的 `shot_overrides_json`；`postMessage` 的目标从 `'*'` 改为父窗口自己的 origin；iframe 的权限从 `camera; microphone; clipboard-write; download; fullscreen` 收窄为 `clipboard-write; fullscreen`——studio 源码里没有任何一处打开摄像头、麦克风或下载提示（ADR-0013 §7）；
- **两个分区做实**：Director（规划版本历史、批准、从某个 Shot 打开预演、相机回写）与 Storyboard Table（行表格与 FR-070 字段、**单行编辑**、监督、闸门、资产版本抽屉与**批准前**的影响分析）；资产页补上版本列表、usage 列表与影响分析确认；
- **Shot 投影**：`ProjectScriptVersion` 现在连同场次投影其镜头——ROADMAP 范围 11 点名 Shot 投影，而分镜行引用的是镜头；
- **Canary 与验收**：production canary 走 导演规划 → 监督 → PASS → 缺口分析（**无监督**，§10.1 自己的设置）→ PASS → 分镜表（12 行，逐行引用脚本的镜头）→ 监督 → PASS；另有按名断言 AC-ASSET-001（六条按序）、AC-ASSET-002（八项分别断言并**读回**）、AC-BOARD-001（形状与门禁）、AC-BOARD-002（监督**定位到行**且 evidence 指向两个引用；单行 FIX 后其余行逐字段不变）与 AC-BOARD-003（2 候选、并发上限、收集幂等）；
- **`testdata/canary-drama/` 补齐两份**：**故意错误分镜**（第 6 镜穿错服装，故障点名它所在的行与规则）与期望分镜（同样行、仅该行修正，其余字段**逐字段相同**）；
- 本包**不包含**：真实付费 Provider 调用、视频/音频/字幕/导出（WP-11）、Semantic Memory（WP-10）、按阶段（而非按层）的模型策略（用户已确认继续延后）、MONOFORM 的完整双向场景同步（用户选定基础桥）、以及表格与画布的**双向**同步。**两轮独立评审**（一轮对账规范、一轮 53 个变异）发现并修掉的问题逐条记在 `docs/adr/0013-production-pipeline-stage-vocabulary-gap-report-and-monoform-envelope.md`；其中**三处 PARTIAL 验收**与**两处验证限制**见 `docs/implementation/STATUS.md` §0j。

WP-10 实现持久记忆、跨阶段一致性与质量中心（**不接入任何付费 Provider**）：

- **迁移 `000019`**：`memory_items`（DOMAIN_MODEL §14.1 的字段）、`memory_summary_sources`（§14.2）、`memory_entity_links`（§14.3），以及 `review_issues.source`——AGENT_CONTRACTS §11.4 要求的 `source=deterministic|llm` 标记，默认 `'llm'` 所以此前写入的每条发现都保持原义。只前向增加，未改动任何已发布迁移；
- **记忆不是 transcript**：`memory_items` 是新表，不与 `agent_messages` 合并。后者的生命周期属于 run（随 run 级联删除），记忆的生命周期属于**用户**：§14.5 给了用户固定、编辑、删除与重建向量的权利，把 `locked` 与 `embedding_blob` 塞进 transcript 会让「删除这条记忆」变成改运行时自己的记录。episodic 记忆用 `source_type`/`source_id` 指向它来自的消息，所以一条回忆永远能走回原话（ADR-0014 §1）；
- **运行时终于写了用户那一轮**：此前只有 assistant 的回复落库，每个 transcript 都是半场对话，会话无法重建——§12.4 的第一条写入源「用户消息」没有任何写入者。现在运行时**先召回、再写当前消息**（§12.2 的顺序），端口在运行时上，调用者无法颠倒这两步；
- **四通道召回**（§12.1）：recent（transcript 尾部 + store 里未摘要的 episodic 记忆，按消息 ID 去重）、summaries、semantic（**唯一受阈值约束的通道**）、facts（固定且高重要度，**不走阈值**——AC-MEM-003 点名的那条例外）。融合权重就是 §12.2 的 0.55/0.20/0.15/0.10，写成命名常量；`DefaultThreshold = 0.30` 而不是 0，因为 FR-120 明确禁止无条件返回低相关结果；
- **摘要是确定性的抽取式摘要**（用户选定）：`extractive/v1` 把来源渲染成 `role: content` 行并截断标记，规则版本随行存储。这让 AC-MEM-004 的 provenance 变成**可核对**而不是承诺——读者能逐行把摘要对回来源；也让 AC-MEM-005 的「恢复原始消息」是一次机械行走而不是模型的一次尝试（ADR-0014 §6）；
- **向量索引是精确扫描**：小端 float32 存 BLOB，Go 里做归一化点积。ADR-0002 §68 拒绝动态扩展加载，所以 `sqlite-vec` 需要它自己的兼容性与安全证据，而持久记忆不足以justify 这件事。**scope 过滤在 SQL 里**而不是打分之后——§12.2 要求过滤先于评分，把这个规则放进查询里才是结构性的；
- **Embedding 可替换**（用户选定）：`CapabilityEmbedding` 加两个实现——OpenAI 兼容的 `/v1/embeddings` 客户端（复用受控 client、密钥解析与脱敏审计）与确定性的特征哈希适配器。后者同时是 FR-120 的**关键词降级**：在用户自己的机器上算向量，没有任何文本离开进程。**它不被任何 provider 配置选中**——`IsUserConfigurableKind` 与数据库 CHECK 都拒绝该 kind，所以组合构建里只有用户配置过的 provider 会收到文本（ADR-0014 §4，这条限制在 §0k 里明写）；
- **六条确定性一致性规则**（scope 13/14）：服装连续性、道具连续性、外景/地点连续性、镜头覆盖与顺序、时长总和、资产批准版本。每条都在**要对比的数据缺失时保持沉默**——一条对着缺失数据开火的检查是用户会学会忽略的检查，这条纪律有正反两面的测试；
- **合并进同一份报告**（用户选定，§11.4 的字面要求）：确定性发现**先于**模型计算，渲染进它的任务让它不必重新推导一个 join，然后按 (rule, entity, field) 去重合并、保留更严重的那条，`ReviewPassed` 让确定性阻断项**推翻**一个满意的裁判——AC-E2E-004 的 FIX 步骤依赖的正是这条分支；
- **`MemoryCreated` 终于被发射**：ADR-0009 §5 把它指派给本包，迁移 000013 的闭vocabulary 从 WP-05 起就接受它，而此前无人发射；
- **`memory.deep_recall` 加深**：WP-07 注册它时留了注释说「WP-10 加深它」，而本包第一版没有——工具仍返回最近窗口，而它的名字、manifest 授权与 §12.3 都承诺那次行走。现在带 `query` 时走完整的摘要→阈值→rerank→还原原始消息，不带时保留窗口；
- **记忆中心分区**：列表、固定/编辑/删除（删除与编辑都要 `confirm`，编辑会清空向量所以重建前不可检索）、**召回预览**（每个候选的融合分数**与原始相似度并列**，所以「低于阈值被丢弃」与「本来就没有」可以分辨）、摘要来源行**点击打开**来源记忆（AC-MEM-004 的「UI 可跳原始消息」）；质量中心新增**发现面板**，这是评审读路径有史以来第一个调用者（含 WP-09 修好的 evidence 列），每条发现标明由哪一半产生，并可跳转到实体所在分区；
- **Canary 与验收**：`testdata/canary-drama/memory-recall.json`（§18.1 要求的「Memory recall 问题」）含早期设定、掩埋它的大量消息、问题与**两个点名自己用途的诱饵**；AC-MEM-001/002/003/004/005 各有真实迁移库上的测试，AC-E2E-004 的五个分句在**装配好的 production 栈**上走通，AC-E2E-005 走完整链路；
- 本包**不包含**：真实付费 Provider 调用、视频/音频/字幕/导出（WP-11）、本地 ONNX 与 `sqlite-vec`（V1）、模型驱动的语义摘要、按阶段（而非按层）的模型策略（用户已确认继续延后）、Event Graph 可视化（v1.0）。**两轮独立评审**（一轮对账规范、一轮 **167 个变异 / 52 击杀 / 81 存活**）发现并修掉的问题逐条记在 `docs/adr/0014-persistent-memory-store-summary-vector-embedding-and-deterministic-checks.md`；其中**四处 PARTIAL 验收**与**三处验证限制**、以及本包**明确不覆盖的九项**见 `docs/implementation/STATUS.md` §0k。

## WP-11 范围：视频、音频、字幕、时间线与导出

- **整仓唯一的 `os/exec`**（`internal/infrastructure/media/ffmpeg.go`）：结构化 `[]string` argv、不经 shell、拒绝 `-` 开头的路径、输出限长、超时、独立临时目录、缺 ffmpeg 时 fail-soft 并给出诊断。扫描器新增**精确**白名单条目（文件 + `os-exec` 规则 + ADR-0015），拒绝通配符且陈留条目即失败——所以删适配器而不删条目同样失败；
- **迁移 `000020_media.sql`**：`subtitle_tracks`（版本化，每集唯一批准）、`subtitle_cues`（毫秒范围，`end_ms > start_ms`，`dialogue_line_id` 让「哪些台词没有字幕」成为一次 join）、`episode_exports`（清单、输出哈希、批准追溯）。仅前向，未改任何已发布迁移；
- **字幕**：草稿从剧本的**可听台词**生成（对白与旁白；动作/转场/提示是给制作的指示，不该有字幕），编辑器一次事务替换全部 cue 并可读回，SRT/VTT 渲染，缺行检测与草稿共用**同一个** `IsSpoken` 规则，因而两者不可能不一致；
- **时间线是只读模型**而不是新表：顺序本来就在 `storyboard_items.ordinal` 里，第二份拷贝迟早会和它对不上。它 join 的是「该镜头已批准的媒体 + 该场台词产生的音频 + 落在该镜头时间跨度里的 cue」；
- **导出走真 ffmpeg，从已批准的分镜图合成**（ADR-0015 §2）：Mock 视频只产 24 字节容器头，concat 它必然失败，所以「按镜头时长串成 MP4」用的是**真 PNG**，产出的文件可播、含音频与字幕流——已用 ffprobe 回读验证。清单记录所用每个版本与哈希，AC-MEDIA-003 的「可追溯」是拿数据库逐条核对而不是装饰；
- **`SaveExport` 是本应用第一条「把文件写到用户指定位置」的路径**：目标来自原生对话框，**绝不来自请求参数**——被攻破的前端无法指定写入目标；字节从 store 流式落到目标，不经过浏览器（`ReadResultFile` 的 64 MiB data-URL 上限会拒掉任何真实 MP4）；
- **Final Ruleset**（`internal/application/consistency/final.go`）：AGENT_CONTRACTS §11.4 的八条子句拆成十条规则，与分镜规则**共用同一个 `Check` 派发**，所以一个构建要么两者都有、要么都没有。确定性这一半**先于**监督者运行，其发现进入模型的任务——§11.4 的分工是真的；
- **两个 `final_episode` agent**：执行层给出导出配方，监督层读「已批准片段拼出的片子是不是这一集想要的片子」。§19 的初始清单没有它们，加进来的依据是 ROADMAP 第 12 项、FR-100 的阶段表与 §11.4 本身，逐条写在 `internal/application/agentassembly/assembly_test.go` 的注释里；
- **三个分区做实**（video/audio/timeline），各自走 `media.ts` 客户端，沿用本仓每个桌面客户端的**查询返空、命令抛错**分工；
- **四份文档导出**（ROADMAP 第 11 项，首报时只做了一半，现已补齐）：剧本导出为纯文本或 **Fountain**（`internal/domain/screenplay`），分镜表导出为对齐文本或 **CSV**（`internal/domain/shotlist`），字幕为 SRT/VTT，清单为可带走的 `.json`——**最后这条此前根本没有调用者**：`save_dialog.go` 的 `.json` 过滤器无人问津。三者在时间线分区都先预览再保存，并且都走**与 MP4 同一条保存对话框**，所以文档不可能写到用户没有指过的地方；
- **单集 E2E**（ROADMAP 第 14 项）：`acceptance_wp11_e2e_test.go` 把一集从剧本 → 分镜 → 字幕草稿与批准 → 视频版本与音频版本挂到资产聚合（`usage_role = 'video'`）→ 时间线 → 导出并 ffprobe 回读 → Final Ruleset → 四份文档一次走通，**每一步都回读数据库**。**这条走查本身就是发现缺陷的手段**：它查出 adapter 把清单里的 `panel` 与 `asset_version` 两种引用按镜头名混在一起，panel 覆盖了 asset version，于是「清单可追溯」规则拿 panel id 去比对当前批准的媒体版本，**把每一次导出都报成过期**——两种引用各自都正确，所以两侧的单测都是绿的；
- **带参考资产的 job 重启恢复**（第 13 项）：`TestVideoRestartKeepsTheReferencesAJobWasSubmittedWith` 让一个带参考图、首帧、尾帧的 job 走完恢复，逐字段比对输入，并断言它**轮询而不是重投**——重投会把首尾帧再发一次并产生第二次计费。同时补上了 `runVideo` 装载这三个字段的管道测试（此前零覆盖，stub 直接丢弃了请求）；**版本选择器**：`ListScriptVersions` 此前是仓库层有、绑定层没有（skeleton 与 strategy 两个 family 都有），补上之后音频分区、字幕草稿与文档导出都能从列表里选版本而不是手输 id；
- 本包**不包含**：真实的视频/音频 Provider（roadmap 允许「或完整 Mock」）、逐**行**的音频读取（时间线的音频列是按镜头的）、以及「资源许可证元数据」这条 §11.4 子句——本仓 schema **没有任何地方能读到许可证**，所以规则**报告这个缺口本身**而不是假装通过。**两轮独立评审**（一轮对账规范、一轮 **321 个变异 / 149 击杀**）发现的问题逐条记在 `docs/adr/0015-media-the-one-audited-subprocess-export-recipe-and-mock-video.md` 与 `docs/implementation/STATUS.md` §0l；其中两处最严重的——Final Ruleset 在生产构建里**从未真正运行**（adapter 的 SQL 写在 schema 里不存在的列上，错误被 stage 机器丢弃）、以及**烧录字幕在 Windows 上必然失败**（filtergraph 的路径少了一层引号）——都在那里写明了原委与如今锁住它们的测试。

## WP-16 范围：完整资产一致性检查（P3 第 20 项）

PRD §16 把 v1.0 的范围写成产品计划而不是欠账，这是其中第 20 项。「完整资产一致性检查」这一句被拆成
可验证的四件事，每一件都先做只读侦察、再动手：

- **`PROP_CONTINUITY` 与 `LOCATION_CONTINUITY` 有实现、却从未被证明会触发**：全仓 fixture 用的都是
  `costume` / `reference` 用法，两条规则的正向路径一次也没跑过。**没有正向用例的规则与桩函数无法区分**，
  所以两条规则各补了正向与反向用例——对着一切都开火的规则同样会让测试失败；
- **Asset Ruleset 缺的两条子句**（AGENT_CONTRACTS §11.2 的「派生关系」与「文件存在和类型」）现在是规则，
  读的是**看板引用到的那些版本**：`ASSET_LINEAGE` 报一个指向不存在版本的 `based_on_version_id` /
  `parent_asset_version_id`（两列都是 TEXT 且无外键，所以悬空可存），`ASSET_FILE_PRESENT` 报字节类型
  与资产类型不符的版本；
- **Safety 与 Cost 此前只是分类表里的两个 key**，全仓各一处命中，没有规则、没有测试、没有调用者。
  现在各有一个发射点，形状由「存储能回答什么」决定：`CONTENT_POLICY_REFUSED` 读
  `generation_jobs.error_code`——Provider 拒绝是记录在 **job** 上的，这是「内容与供应商规则」里**有列可查**
  的那一半；`REVISION_BUDGET_SPENT` 是**函数**而不是 Checker 方法，因为修订预算属于**拒绝超额修订的引擎**，
  它报的是预算而不是成本估算——本构建不给任何东西定价，乘出来的数字没人能背书；
- **分类本身第一次到达读者**：迁移 `000024` 给 `review_issues` 加 `category` 列（ALTER，默认**空串**且
  不加 CHECK），合并时按**规则**填充并回落到 `CategoryOf`——**在此之前 `CategoryOf` 在生产路径上没有任何调用者**，
  它的存在意义就是这个问题的答案。质量中心把它显示为标签，**空分类不显示标签**：监督者的发现不声明分类，
  旧行也没有，「没人分类过」与「分类为技术」是两件不同的事，界面不该替人做一个没人做过的分类。
- **一条注释许诺、却从未存在的测试**：`consistency_checker.go` 的注释点名
  `TestCheckedStagesMatchTheSwitch`「保持两者不漂移」，而全仓没有这个测试。名单与 switch 恰好一致，
  所以还没坏；缺的是**将来它们分叉时会说话的东西**，现在有了。

**这个包最重要的一条记录是它自己的第一版是错的**：文件规则最初把**每个**被引用却无文件的版本报成
`critical`，两个既有测试立刻失败——`TestConsistencyACleanBoardReportsNothing` 与 AC-E2E-004 的修复走查。
两个测试都是对的：**资产圣经本来就在美术生成之前定义资产**，所以「尚无文件」是项目走到一半的**常态**，
那条规则会把处在那个状态的每一块看板都拦下。让规则变锋利的是 `generation_job_id`——它才是「这些字节被生产过」
的那一列。**测试一行未改，规则重写**，因为可选的只有这一个方向。

- 变异验证 **12 个全杀**，每个都从字节级备份还原并用 diff 复核。**第一次跑有三个存活，每一个都是真实缺口**：
  合并丢弃分类而所有测试全绿（没有任何断言覆盖合并这个唯一写入者）、workflow service 的行构造因为
  fixture 自己造行而**跨过了被测的那一行**、修订预算规则完全没有单测。三处都改为走**生产路径**后击杀。
- 本包**不包含**：`CONTENT_RATING` 仍是只有分类的 key，条目里写明原因——项目**声明**内容分级而没有任何东西
  用它比对工件，因为分级是自由文本；以及 §11.2 的「场景时间/天气」在本仓**没有任何存储**（STATUS §0k 记录）。
  推理与代价记在 `docs/adr/0020-complete-asset-ruleset-and-the-two-categories.md`，结果记在 STATUS §0u。

## WP-17 范围：事件图谱可视化（P3 第 18 项）

这一项之所以在 v1.0 清单里，是因为 WP-06 交付的是「表格 + 一张小 SVG」而不是一张图。动手前的只读侦察
查出三件事，每一件都成了这个包的一部分：

- **那张图画的是实体，不是事件。** 分区叫「事件图谱」，而 `buildGraph(entities, relations)` 从来没有拿过
  events 列表——每个事件都被读出来、渲染进表格，然后**从图里消失**。FR-030 把 `StoryEvent` 列在节点类型的第一位。
  现在事件是节点，实体是圆、事件是圆角矩形，**形状和颜色都能区分**，色觉障碍的读者也看得出来。
- **「参与」根本没有项目级读取。** `participates_in` 在关系词表里，**而没有任何代码写它**：参与存在
  `story_event_participants`，而它只有按事件读的接口。要画「角色—事件」的边就得每个事件查一次，正是本仓
  一直在避免的 N+1。新增的 `ListProjectEventParticipants` 用一次 JOIN 穿过 `story_events`，把**项目过滤与
  状态过滤放在同一条语句里**——因为视图若对事件和参与用不同的过滤，就会画出指向被隐藏节点的边，而绘制代码
  会**默默丢掉**它，于是缺陷看起来像「图里少了一条边」而不是「过滤不一致」。边用**虚线**画，标签是**角色**
  （actor / witness），因为那才是这一行存的事实，写成通用的 `participates_in` 会丢掉角色。
- **一条 e2e 断言自 WP-06 起就是空的。** `web/e2e/studio.spec.ts` 断言 `studio-story-graph-gap` 的数量为 0，
  而这个 testid 住在 `pages/studio/sections.tsx`；WP-06 把分区搬到自己的文件并删掉了那条提示，i18n 也一起删了。
  此后这条断言检查的是**任何文件都不可能产生的元素**——对不存在的 testid 做否定断言永远不会失败。
  **十一个工作包在「一个什么都没检查的测试」下全绿通过。** 它没有被「修好」而是被删除并写明原因：它**无法**在
  那个层面修复，因为 `pages/studio/project.tsx` 在 switch 之前就返回了 no-core 提示，浏览器模式下
  `StoryGraphSection` **根本不会挂载**。修一个长得像的替代品（断言某个新 testid 不存在）会以完全相同的理由同样空转。

- **未画出的边会被数出来**：四张表分别读取，所以一条边可能指向本视图没有的节点。画出来是通往虚空的线，
  默默丢掉则更糟——图会读成「这两件事无关」，而真相是「本视图没画它」。计数字里明写「另有 N 条边未画出」。
- **布局是确定性网格**，不是环、不是力导向：环在节点多时标签互相压叠，力导向更好看但**不确定**——同一张图两次
  加载画得不一样，截图比对失效、用户对自己故事的空间记忆也失效。画布才是用户摆放数据的地方，这里是阅读辅助。
- 变异验证 **13 个全杀**（Go 6 + 前端 7），**其中 f1/f2 正是上面那两个原始缺陷**。推理与代价记在
  `docs/adr/0021-event-graph-drawing.md`，结果记在 STATUS §0v。

## WP-18 范围：层级摘要的第三级与召回评测集（P3 第 22 项）

FR-120 的「必要规则」把梯子写得很直白：**message → episode/session → project**。动手前的只读侦察
查出四件事，每一件都成了这个包的一部分：

- **第三级被声明、却不可达**：`Scope.ProjectOnly()` 的注释说它是「how a PROJECT-level summary is
  scoped」，而它**生产调用者为零**；层级常量上方的注释写着「本构建没有第三级」。现在有了，而那句反
  对的理由也在原处改正——项目级摘要**不是**对「项目已完成」的判决，它和前两级是同一个操作，只是带一
  个防止它空转的下限。
- **第二级从未运行过**：全仓 `Level:` 的六处**全是 `Level: 1`**，中间那一级没有任何测试——这也是下面
  那个缺陷能活过四个包的原因。
- **第二级不标记其子项，与它上方两处的注释矛盾**：`CreateSummaryWithSources` 传的是
  `markSummarized = level == 1`，而选窗口用的 `UnsummarisedItems` 正是按 `summarized = 0` 选。
  **第二次跑第二级会把同一批子项再浓缩一次**——先写的测试打印出的摘要里，**上一个摘要作为来源嵌在
  它内部**。
- **两条召回指标都不存在**：§18.2 点名「Memory 跨项目泄露率」与「Deep Recall 命中率」，而本仓只有一份
  写得很好的 fixture 和一个手工断言的测试，**没有任何东西把观察变成数字**。

**三个梯级暴露出的缺陷**：两级时 `memory_type` 恰好把两级分开了（一级读消息、二级读摘要）——**第三
级打破了这个巧合**，因为二三级都读摘要，于是「本作用域里未被压缩的摘要」不再指向任何一级。后果很具体：
项目级摘要一旦存在，再加一条新的集级摘要，项目窗口里就同时有**上一个项目摘要**和新子项，两行满足下限，
于是一次运行会把自己的前一次输出当作兄弟节点再压一遍。**这是观察到的，不是推理出来的**：先写的测试失
败信息里，项目摘要的正文包含着一个子项目摘要。迁移 `000025` 给 `memory_items` 加 `summary_level` 列
（**列而不是递归推导**——第四级下推导会变成「查询恰好匹配到什么」），窗口从此点名它下面那一级。

**可达性，以及它可能打开的泄露**：项目级摘要的 episode 是空的，所以集级搜索看不到它，而 WP-18 之前
的代码**强制** `EpisodeOnly()`——项目级摘要对任何问题都不可达。现在 `DeepRecall` 搜调用者自己的作用
域，并在调用者指名了集时**再搜一次项目级**，第二次的结果经过 `keepInRecallScope`：只保留调用者自己
作用域里的行，以及属于**项目本身**的行（episode 与 agent 都为空）。**搜得宽、答得窄是重点**。

**两条指标是值的纯函数**（`internal/application/memory/eval.go`），因为它们全部的输入就是「一次召回
返回了什么」加「期望是什么」，两者都是值；一个还要数据库和 Provider 的指标会是**第二份召回实现**，
然后自己给自己打分。`SummaryHitRate` 与 `HitRate` **分开**，因为「Deep Recall 命中率」问的是历史走查：
从最近窗口答出来的那次并没有证明它。

- 变异验证 **11 个全杀**，每个字节级还原。**第一次跑有三个存活，每个都是真实缺口**：泄露过滤器可以保
  留所有候选（原测试只断言项目级**被找到**，从未断言邻居**没被带出**，而端到端场景其实是靠阈值先把外
  来行挡掉了——那个性质是**碰巧**被保护的）；层级守卫可以接受任何值（存储层会拒绝同样的输入，所以只断
  言「有错误返回」的测试分不清两层，而现在它断言**是哪一层拒绝的**）；合并可以保留第一个分数而不是更好
  的那个（没有任何用例覆盖同一行出现在两次搜索里的情形）。
- **不包含**：模型驱动的摘要（仍是 extractive/v1，STATUS §0k 记为限制）、第四级（FR-120 的梯子到
  project 为止）、§18.2 另外八条指标（各有归口）。推理与代价记在
  `docs/adr/0022-summary-ladder-third-rung-and-recall-metrics.md`，结果记在 STATUS §0w。

## WP-19 范围：PDF 导入（P3 第 24 项）

导入管线本来就有四层（格式探测 → 文本抽取 → 编码检测 → 章节切分），所以这一项不是「写一个导入器」，
而是**给第二层换一个抽取器**——而换哪一个，是用**实测**而不是偏好决定的。

**关键构造**：中文 PDF 的文字不是按字符存的，是按**子集字体的 CID 码**存的，读它的人只能从字体的
`ToUnicode` CMap 里学到每个码对应哪个字。所以那个 CMap 就是整个问题。手工造了一份带 CMap 的中文 PDF，
交给两个候选：

| 库 | `沈砚的渡口` 的结果 | 结论 |
|---|---|---|
| `rsc.io/pdf` v0.1.1 | `" …"` —— **原始字形码** | **淘汰** |
| `github.com/ledongthuc/pdf` | `"沈砚的渡口"` | 采用 |

**`rsc.io/pdf` 读不出中文**：它看到了字体字典里的 ToUnicode，却没有拿它去映射抽出的文本。它是**第一
直觉会选的那个**，所以这条淘汰必须写下来，而不是靠人记得。

选中的那个随后被**模糊测试**过，因为导入器解析的是不可信字节：**120 个损坏样本里 3 个 panic**
（`invalid real .`、`unexpected non-name key`、`missing endobj`），全部来自 `lex.go` 的 `errorf`——这是该库
表达「输入格式错误」的方式。0 个 hang，另造的 4 个敌意文件全部正常报错。所以它**有 recover 才能用**：
一个导入器不能因为用户拖进一个损坏文件就把进程带走。

- **抽取器包在尽量窄的 `recover` 里**：`pdfPageTexts` 单独成函数就是为了这个，recover 只包住库调用——包住
  更大的范围会把本仓自己的 bug 也吞掉，报成「PDF 格式错误」，那种 bug 没人找得到。
- **上限是抽取器接收的「值」，不是写在比较处的常量**：用真实上限去验证意味着要么造 2001 页真文件
  （**实测九分钟**，那种测试没人会跑），要么篡改页树（结果 xref 崩掉，拒绝来自解析器而不是来自上限）。
  传进去之后，测试能用**小文档**断言**同一个比较**。
- **没有文字层是一个独立的拒绝理由**：扫描件只有图像，返回空文档等于什么都没告诉用户——而它的补救
  （先跑 OCR）和损坏文件不同，所以文案必须说出真实处境。
- **不做**：OCR、解密、版面还原（分栏会按行交错——这是所有非 OCR 抽取器的共同限制，写在 ADR 里而不是
  含混地说「支持 PDF」）。
- **门真的挡住了这次改动**：加上依赖后 `scripts/verify.sh` 第一次**FAIL**，因为 `sbom/cyclonedx.json`
  漂移了；重新生成检出 `BSD-3-Clause`。这是 AGENTS §6 要求检查的许可证，也是门在正常工作。
- **两个把 PDF 钉成「必须被拒绝」的测试被更新，理由原地引用**：它们的注释写着「规范把它放在 V1，所以
  MVP 导入必须继续拒绝」——WP-19 就是那个 V1 项。断言翻转成新状态，旧理由留在原处，记得旧规则的人会
  发现它变了、变在哪里，而不是发现测试不见了。
- 变异 **11 个全杀**；**第一次跑存活三个**，每一个都记下来：recover 的第一个变异本身是错的（它仍然**调用**
  了 recover，而调用才是阻止 panic 的东西）；页数上限与单页文本上限**都没有测试**——它们是「只存在于源码里、
  不存在于任何测试里」的限制，原因正是上面那条九分钟。

## 使用说明

1. 打开右上角配置，添加渠道的 API 地址与模型。
2. 在「渠道」中通过安全密钥输入框保存 API Key（桌面模式）；密钥写入系统凭据存储，界面只显示是否已配置与短提示。
3. 新建画布，将提示词、参考图和生成节点组织到同一工作区。
4. 在「任务中心」查看生成任务队列与失败重试。
5. 主页提示词库的内置封面位于 `web/public/prompt-covers`，卡片右上角可以随时替换。

浏览器开发模式仍把配置保存在当前浏览器本地。桌面模式的安全密钥不再进入浏览器存储；旧版本保存的明文密钥会在桌面模式下收到迁移提示，请通过安全输入框重新保存后再清除旧值。API Key 不会提交到仓库；分享导出的配置或截图前仍应检查敏感信息。

## 目录

```text
Infinite Atelier
├─ start.bat                 Windows 启动器
├─ web/src                   主应用源码
├─ web/public                品牌与提示词图片资源
├─ web/monoform-studio       内嵌导演预演工具
└─ LICENSE                   开源许可证
```

## 维护者

[GuiYi-Xi](https://github.com/GuiYi-Xi)

## License

代码许可见 [LICENSE](LICENSE)。

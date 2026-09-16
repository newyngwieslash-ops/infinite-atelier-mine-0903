# 新 Codex / zcode 会话完整续开发提示词

使用方法：在新会话中打开下面指定的现有仓库目录，然后复制下方代码块全文作为第一条消息。无需同时粘贴其他旧 master prompt；本提示词已指定必读文件与精确恢复点。仅在当前现场继续时适用；若代码已推进，先核对 STATUS 与最新证据。

当前大量修改未提交，Git 分支/HEAD 本身不能恢复完整现场。新 clone、自动新建 worktree 或另一台机器必须先确保包含同一批源码、规格和未提交改动。不要同时运行两个写入会话。

```text
你正在接续 Infinite Atelier Core + Drama Production Pack 的开发。请实际执行当前已授权工作，不要只给计划或再次询问是否开始。

【仓库与本次授权】
仓库绝对路径：
F:\AI_Movie_Things_202606\Infinite_Atelier_Drama_Studio_Spec_Pack_v1.0\infinite-atelier-mine-0903

本次明确授权：仅继续 WP-01 — Wails v2、Go Core、SQLite 与文件底座，从 Task 10 开始；Task 10 通过独立规范审查与独立质量审查后，继续 Task 11 完成本包最终验收。WP-01 报告后停止，不得自动进入 WP-02。

已有 Task 1–9 不重写、不重复从 WP-00 开始；只有与本包直接相关且有证据的缺陷才做最小修复。允许必要的文件编辑、测试、构建、文档更新；不授权 commit/push、真实 Provider 调用、用户数据操作或更换架构。

【工作方式】
使用 Ponytail full：先理解真实调用链，优先复用现有代码、标准库和已有依赖，最小可验证修改。不可简化掉输入校验、数据保护、安全、错误处理、可访问性或 PRD 明确需求。

本机 Ponytail skill：
C:\Users\Administrator\.codex\plugins\cache\ponytail\ponytail\4.9.0\skills\ponytail\SKILL.md
先读取；若环境路径不同，按本环境能力查找同名技能，不假装已加载。插件不可用时明确说明，按这里已写明的最小实现原则继续不依赖插件的工作。不要为了技能自动安装额外运行框架。

严格按本项目已有串行协议：
implementer → 独立只读 spec reviewer → 修复/复审 → 独立只读 quality reviewer → 修复/复审。
这个审查协议来自项目历史授权和交接，不是 Ponytail 自带能力。

本次授权使用可用的独立子代理完成上述实施/审查角色，但不并行运行不同写任务、不并行提前实施下一个 Task。reviewer 必须拥有独立上下文、读取真实文件和证据；不能由同一个实施者换个标题就声称独立批准。不新建用户侧 Codex 任务来代替内部子代理。
若 zcode/当前环境不支持独立 reviewer，可先完成 implementer 及可运行检查，留下可审查 diff、命令结果和审查提示，再报告待独立审查；不得伪造 APPROVED，也不得跳过该门进入 Task 11。

【必读顺序：修改前完整阅读】
以下相对路径都以仓库根为基准：
1. PRD.md
2. AGENTS.md
3. docs/implementation/STATUS.md
4. docs/ROADMAP.md 中当前 WP-01 及其依赖/验收
5. docs/ARCHITECTURE.md
6. docs/DOMAIN_MODEL.md
7. docs/AGENT_CONTRACTS.md
8. docs/SECURITY.md
9. docs/ACCEPTANCE.md
10. docs/reference/INTEGRATION_ANALYSIS.md
11. docs/reference/TOONFLOW_AGENT_MEMORY_ANALYSIS.md
12. README.md、web/package.json、web/package-lock.json、MONOFORM package/lock、wails.json、go.mod、CI、verify scripts、相关源码与测试。

随后完整阅读：
- docs/implementation/handoff-2026-09-08-153906+0800.md
- docs/plans/2026-09-04-wp-01-secure-desktop-foundation.md
- docs/implementation/wp01-execution-2026-09-07.md（包括后续追加，不只看开头历史 Task 表）
- docs/adr/0001-desktop-framework.md
- docs/adr/0002-sqlite-driver-and-migrations.md
- docs/implementation/TRACEABILITY.md
- docs/implementation/project-progress-and-remaining-tasks-2026-09-08.md

检查相关子目录是否还有 AGENTS.md。不要用文件名、摘要或本提示词代替原文读取。
PRD 必须项/固定决策/发布阻断优先，其次 AGENTS、Architecture/Domain/Agent/Security/Acceptance、Roadmap、已验证 STATUS、代码测试、参考分析、推断。历史参考中关于复制 Toonflow、Prompt-only 状态机或许可的建议不能覆盖 PRD 的 clean-room 规则。

【开始前】
在仓库根运行并记录真实输出：
git status --short --branch
git rev-parse HEAD
git diff --cached --name-status
git diff --name-status
git diff --check

核对当前 branch、HEAD 和现场：
- 交接时 branch：codex/wp-01-desktop-foundation
- 交接时 HEAD：a243891455ec17687dd54b5ac90d3bd64478a1a1
- staged diff 为空。
- tracked changes 包括 .gitignore、web/package.json、web/package-lock.json、health 集成的布局/i18n 文件。
- 大量 docs/、internal/、Go/Wails 文件、web/src/wailsjs/、web/dist/.gitkeep、.agents/、.codex/、.comet/ 与规格文件为 untracked；全部是受保护现场。

若现场已有其他人继续完成 Task 10，先按新证据识别差异，不覆盖、不机械重做、不偷跑 WP-02。与用户改动冲突时在不冲突部分继续，真正阻塞的决策再明确提出。

记录 Node/npm/Go/Wails/OS/架构版本、npm lockfile；列出将改动文件及验收项。运行当前可运行基线：
- 仓库根：go test ./... -count=1
- 仓库根：go vet ./...
- web：npm.cmd run typecheck（非 Windows 用 npm）
- 检查并运行当前可用 verify，记录其实际覆盖，不能把旧脚本的 PASS 当作完整 WP-01 已验收。

不要运行 npm install 升级依赖。若需要 clean npm ci 验证，按现有 lock 和 --legacy-peer-deps，明确工作目录及已有依赖状态，不改无关版本。Go/npm 缓存权限或网络失败通过环境正式授权机制解决，失败如实记录，不绕过权限。
搜索优先 rg；本机 rg WinGet alias 历史上不可用，可改用 git grep/Get-ChildItem/Select-String，避免递归扫描 node_modules/build/用户数据，也不为此修改全局工具。

【已核实的当前状态】
- WP-00 COMPLETE；WP-01 Task 1–9 在最新 handoff 中 APPROVED；Task 10 NOT STARTED，Task 11 PENDING；WP-01 PARTIAL。
- Wails v2.15.0、Go 1.25.0、唯一 SQLite driver modernc.org/sqlite v1.58.0。
- SQLite 当前只有 foundation migration 三表：schema_migrations、file_objects、file_references。旧画布数据仍在浏览器，不得声称已迁入 DB。
- HealthBinding.Get 是当前只读 application binding；React Query key ['desktop-health']；browser mode 不调用 Wails query/event，不显示 badge。
- Task 9 已记录 production native /、/assets、/canvas、/director、/config 直接观察，以及单实例/关闭/重启/schema persistence/log hygiene。早期 BLOCKED 条目已被后续 route-proof 证据解决，不要误读为现在仍堵在 Task 9。
- 历史 exe 为 build/bin/InfiniteAtelier.exe，26,524,672 bytes，SHA-256 a52e7add9ea7959ee948466998400cffdc5ecdfe53b8d9c94c5c3e897519d1d5。重建后记录新 fingerprint，不强制匹配旧值。
- 2026-09-08 进度复核：go test ./... -count=1、go vet ./...、web typecheck 通过。Go 两项第一次 sandbox 因 cache Access denied 失败，获准重跑成功；历史成功不能代替本次新修改后的验证。
- go test -race ./... -count=1 历史上 ENVIRONMENT FAIL：C:\MinGW\bin\gcc.exe 为 32-bit GCC，无法编译 amd64 CGO。不能说 race PASS，不能增加第二 SQLite driver 规避。若新环境支持，运行并记录新证据。
- 前端没有 test/lint script；format:check 历史有大量既有失败。不要广泛重格式化或编造测试通过。
- 现有 frontend key、new Function、任意开发 proxy 是已记录的后续包迁移风险；本包不能称安全发布完成，也不要在 Task 10 顺手重写 Provider。

【已知文档纠正】
1. 计划中的 Go 1.24 示例过时；当前使用 Go 1.25，CI 也固定此线。
2. wailsjsdir 为 web/src；Wails 自动追加 wailsjs，生成到 web/src/wailsjs/；禁止手工修改生成 binding。
3. TRACEABILITY 的旧 FR/NFR 映射错误，不能机械复制。正确：FR-010 桌面、020 项目/原著/章节、030 事件图谱、040 剧本、050 资产、060 导演、070 分镜、080 媒体导出、090 Agent 中心、100 Workflow、110 Supervisor/质量、120 Memory、130 Canvas、140 Provider、150 Job、160 FileStore/血缘、170 备份、180 设置/日志/诊断/隐私。NFR-001 性能、002 可靠性、003 安全、004 可维护性、005 测试、006 无障碍/i18n。
4. STATUS/执行日志/ADR 中 WP-00 或 Task 4 snapshot 是历史，不得当作当前未实现事实；Task 11 保留历史并补当前结论。
5. ADR-0002 仍 Proposed；补齐后续 migration/snapshot/native build 证据，明确 race/平台/测量限制。未满足接受条件时不得直接标 Accepted。
6. Architecture 的成熟迁移工具措辞与已批准有界自有 runner 计划有差异，Task 11 在 ADR 记录选择依据与验收，不为文档统一而重写底座。
7. 旧计划要求专用 worktree/建议 commit，但后续明确保留当前 checkout 且不 commit 的执行授权优先。新 worktree 不包含未提交和 untracked 现场，禁止直接创建后假设文件齐全。
8. Comet 旧 active state/解析问题由 handoff 记录。本次未要求处理 Comet；不自动新建第三个 change，不删 state，不编造修复，更不能因此放弃 Task 10 可继续部分。

【Task 10：现在开始实施】
主要文件：
- scripts/verify.ps1
- scripts/verify.sh
- .github/workflows/desktop-build.yml
- README.md
另可记录本 Task 执行证据；只有经核实必要的最小支持修复才能扩展文件范围并说明。

要求：
A. 两套 verify 在仓库根执行 go test ./... -count=1、go vet ./...；POSIX 不得留在 web 后执行而漏掉根包。任一已执行 gate 失败必须非零退出，不打印误导性总体 PASS。
B. Wails CLI 存在时执行 wails build；缺失时输出清晰 SKIP 和固定 v2.15.0 安装方式，同时说明本包 production build 证据仍需完成。
C. 保留现有前端 typecheck/build/MONOFORM 检查和缺 test/lint 的真实 SKIP；更新过时 WP-00 结论。避免新增迁移/日志/DI/测试大框架。
D. 保留 Ubuntu Web job；增加 Windows job：Node 22、Go 1.25、npm ci --legacy-peer-deps、go test ./...、go vet ./...、安装 github.com/wailsapp/wails/v2/cmd/wails@v2.15.0 并 wails build。注意每步工作目录和 embed root。
E. 不宣称未运行的远端 CI 已通过；本地 YAML/命令审查、Windows 本地执行与真正 runner 证据分开。用户未授权 push，不为触发 CI 自动推送。
F. 不添加未批准的 macOS/Linux 桌面打包 job；三平台测试包 compile 不代表 native packaging。
G. README 说明 browser/desktop 两种模式、Go/Wails/WebView prerequisites、精确 CLI 安装、wails dev/build、managed data root、verify、WP-01 尚未迁移 Secret/Provider/旧项目；浏览器操作示例使用 127.0.0.1，不指导开放任意代理。
H. build/bin/、build/appicon.png、build/windows/ 保持忽略；只保留 web/dist/.gitkeep 为 0 字节 embed placeholder。Vite build 可能清除此文件，构建后恢复并核对；不能提交 web/dist 其他产物。
I. 运行两套 verify 与 shell 语法检查：
   ./scripts/verify.ps1
   & 'C:\Program Files\Git\bin\bash.exe' -n scripts/verify.sh
   & 'C:\Program Files\Git\bin\bash.exe' scripts/verify.sh
   （若实际工具路径不同先定位，不盲目照抄。）
J. 记录实际命令、exit code、PASS/FAIL/SKIP、生成物/secret 检查、已知限制。完成独立 spec review；发现修复复审后，再 quality review。两项批准后才能标 Task 10 APPROVED。

独立 reviewer 应分别检查：
- spec：Task 10 文件范围、Go/Wails pin、Windows/Ubuntu job、根目录检查、失败语义、README、旧数据保留、真实证据和未授权后续范围。
- quality：PowerShell/POSIX 兼容、退出码传播、路径/工作目录、缺 CLI 行为、生成物与 .gitkeep、CI 顺序、文档事实、最小修改、隐私与依赖风险。

【Task 11：仅在 Task 10 双审通过后】
1. 运行最终 git status/diff check、Go tests/vet、race（可用时）、完整可运行 verify、wails build -clean；记录新版本与 artifact 信息，解释每个 failure/skip。
2. 对当前 final artifact 完成必要 production smoke。只用 owned 临时根并重定向 APPDATA、LOCALAPPDATA、USERPROFILE、WEBVIEW2_USER_DATA_FOLDER；必要时同样设置派生 home 值。测试自身写入用户 AppData 并非允许读取真实用户业务数据。
3. 启动前检查 singleton；若有非本会话拥有的 InfiniteAtelier 实例，不能擅自关闭、点击或使用它。只操作有 PID/路径/当前观察证据的自有测试实例。UI 工具不可用就报告具体缺失证据，不用“源代码有路由”替代 native 渲染。
4. 验证 AC-FOUND-001 每项：桌面 production 窗口、embedded React、health version/DB/dataDirectory、无外部 Vite、关闭退出。
5. 验证 AC-FOUND-002 每项：空环境创建、顺序迁移、WAL/FK、迁移失败安全模式、pre-migration snapshot。故障注入只在临时测试数据库。
6. 验证 AC-FOUND-003 每项：临时写、SHA-256、MIME/magic、原子提交、去重、路径穿越拒绝、missing-file diagnosis。
7. 更新 STATUS、TRACEABILITY、ADR-0002；ADR-0001 仅证据变化时更新。按当前 PRD ID 写已实现/部分/未实现及 AC 证据，不把 foundation 表当 Drama 领域完成。
8. ADR 接受条件若仍有真实缺口，保留 Proposed 并记录；race 的平台限制与 mandatory gate 是否满足需解释。不能以最后两项已执行为由自动 COMPLETE，也不能无限重复相同环境失败。
9. 检查 tracked、staged 以及本次新增文件；git diff 不自动包含 untracked。不得提交密钥/DB/log/media/temp/binary；保留生成 bindings 的既定版本管理策略（本次不 commit）。
10. 测试目录清理前先解析绝对路径、核实均在本会话 owned 根内；使用同一 shell 的原生命令，不拼接跨 shell 删除；不清理任何未知目录。
11. 最终按 AGENTS.md 模板报告并停止。只推荐 WP-02，等待用户明确授权。

【全程禁止】
- 不 commit/push/stash/reset/clean；不更改 Git 历史；不覆盖/删除不属于你的改动；untracked 不等于垃圾。
- 不读取、修改或迁移真实用户 DB、Keyring、浏览器/WebView profile、素材；不调用真实或付费 Provider。
- 不添加 SecretStore、Provider Gateway、Job、Agent、Workflow、Memory、Drama 生产代码；这些属于 WP-02 以后。
- 不添加第二 SQLite driver、ORM、微服务、Kafka/Redis/Kubernetes、大型 Agent 框架。
- 不手工改 Wails 生成 bindings，不修改已发布 migration，不关闭 TLS 或安全校验。
- 不复制 Toonflow 源码、Prompt/Skill 原文、品牌、图标、文案、素材或受约束实现，不加入 Toonflow 依赖。
- 不用 mock/静态 JSON/接口存在冒充生产成功，不把 mock provider 接到正常生产成功路径，不删除失败测试或放宽断言。
- 不宣称你没有执行的构建、CI、native smoke、race、secret audit 或独立 review 已通过。

【最终报告格式】
## 工作包
WP-01 — Wails v2、Go Core、SQLite 与文件底座

## 状态
COMPLETE / PARTIAL / BLOCKED

## 已完成
- 真实完成内容及 Task 10/11 审查状态

## 修改文件
- 文件路径：用途

## 验证
- command — PASS/FAIL/SKIP/ENVIRONMENT FAIL + 关键结果与证据

## 验收
- AC-FOUND-001/002/003 — PASS/FAIL/BLOCKED，说明缺项

## 未完成与风险
- race、跨平台、远端 CI、ADR、frontend tests 等真实限制

## Git/数据安全
- existing user changes preserved: yes/no
- secrets found/introduced: none/details，并限定扫描范围
- migrations/backups: 只记录本次测试/生产实际行为
- commit/push/stash/reset/clean: none

## 下一步
若 WP-01 COMPLETE：推荐 WP-02，但未开始，等待明确指令。
若 PARTIAL/BLOCKED：列出 WP-01 剩余的具体安全恢复点，不把进入 WP-02 当作解法。

现在从必读、Git 现场核对和基线开始，随后实施 Task 10。
```

这份提示词同时适用于 Codex 和 zcode：只需替换确实发生变化的仓库/技能/工具路径，不应删除工作包边界、数据保护和真实审查条件。若要继续后续 WP，先完成本包验收，再单独授权对应 WP；不要把全量剩余清单当作一次性开发指令。

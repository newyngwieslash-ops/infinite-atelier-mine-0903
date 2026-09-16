# Infinite Atelier — 会话交接与 PRD 进展评审包

> 生成日期：2026-09-08  
> 工作目录：`F:\AI_Movie_Things_202606\Infinite_Atelier_Drama_Studio_Spec_Pack_v1.0\infinite-atelier-mine-0903`  
> 当前分支：`codex/wp-01-desktop-foundation`  
> 交接用途：供独立 Agent 审查已完成工作、验证证据、风险，以及项目相对 PRD 的真实进展。  
> 真实性原则：本文件只陈述已观察或已验证的事实；计划不是实现，定义的 CI 不是已运行的远端 CI。

---

## 1. 执行摘要

### 已完成

**WP-01 — Wails v2、Go Core、SQLite 与文件底座已完成。**

WP-01 已实现并以本机 Windows 环境验证：

- Wails v2.15.0 单进程桌面壳；
- Go Core 的启动、关闭、健康检查和窄 Wails Binding；
- SQLite `database/sql` 基础设施、WAL、foreign keys、忙等待、完整性检查、前向迁移、checksum、迁移前快照和 Safe Mode；
- 安全本地 FileStore（临时写入、SHA-256、MIME/magic 检测、原子提交、去重、路径穿越防护与引用关系）；
- Windows/Posix 验证脚本、Windows 桌面构建 CI 定义和桌面运行文档；
- Task 10 经独立 spec review 与独立 quality review 审核通过；
- Task 11 完成 Windows 隔离原生 smoke 和最终验收；
- AC-FOUND-001、AC-FOUND-002、AC-FOUND-003 对 **WP-01 Windows foundation scope** 均为 PASS。

### 已规划但尚未实现

**WP-02 — Secret、网络安全与 Provider Gateway 基础未开始编码。**

本会话完成了 WP-02 的只读探索和实现计划，并已取得用户对计划的批准。唯一已确定的产品/技术选择是：

- SecretStore 使用 **Windows Credential Manager**；
- 拟采用 Windows-only `advapi32.dll` Credential API 小型适配器，不新增 keyring 依赖；
- 无安全后端时 Provider 必须 fail closed，绝不使用 SQLite、文件、浏览器存储或任何明文 fallback；
- 首个安全迁移目标仅是 OpenAI-compatible **文本**生成/流式输出；图片、视频、音频、Jobs、Agent、Workflow、Memory 和 Drama 能力均不在 WP-02 范围。

> 重要：WP-02 计划已批准，但截至本交接文件生成时，没有为 WP-02 创建、修改或验证任何产品代码、数据库 migration、Wails binding、前端安全迁移或 CI 扫描。不要将 WP-02 视为已完成或部分实现。

---

## 2. 必读文件和事实优先级

下一位审查/实施 Agent 开始前，必须依照 [`AGENTS.md`](AGENTS.md) 的顺序阅读：

1. [`PRD.md`](PRD.md)
2. [`AGENTS.md`](AGENTS.md)
3. [`docs/implementation/STATUS.md`](docs/implementation/STATUS.md)
4. [`docs/ROADMAP.md`](docs/ROADMAP.md) 当前工作包
5. [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
6. [`docs/DOMAIN_MODEL.md`](docs/DOMAIN_MODEL.md)
7. [`docs/AGENT_CONTRACTS.md`](docs/AGENT_CONTRACTS.md)
8. [`docs/SECURITY.md`](docs/SECURITY.md)
9. [`docs/ACCEPTANCE.md`](docs/ACCEPTANCE.md)
10. [`docs/reference/INTEGRATION_ANALYSIS.md`](docs/reference/INTEGRATION_ANALYSIS.md)
11. [`docs/reference/TOONFLOW_AGENT_MEMORY_ANALYSIS.md`](docs/reference/TOONFLOW_AGENT_MEMORY_ANALYSIS.md)
12. README、package/config/CI、当前代码与测试。

关键事实优先级：PRD 必须项和发布阻断条件 > AGENTS 工程规则 > Architecture/Domain/Agent/Security/Acceptance > 当前 Roadmap WP > STATUS 已验证事实 > 代码与测试 > 参考分析 > 推断。

---

## 3. Git、数据和工作树安全

### 当前工作树

截至本文件生成时，`git status --short --branch` 显示：

```text
## codex/wp-01-desktop-foundation
 M .github/workflows/desktop-build.yml
 M .gitignore
 M README.md
 M web/package-lock.json
 M web/package.json
 M web/src/components/layout/user-status-actions.tsx
 M web/src/i18n/locales/en-US.ts
 M web/src/i18n/locales/zh-CN.ts
?? .agents/
?? .codex/
?? .comet/
?? .zcode/
?? AGENTS.md
?? CODEX_FIRST_RUN_PROMPT.txt
?? CODEX_MASTER_PROMPT(1) (1).md
?? CODEX_MASTER_PROMPT(1).md
?? CODEX_MASTER_PROMPT.md
?? PRD.md
?? SHA256SUMS.txt
?? START_HERE.md
?? THIRD_PARTY_NOTICES.md
?? app.go
?? app_test.go
?? docs/
?? go.mod
?? go.sum
?? internal/
?? main.go
?? scripts/verify.ps1
?? scripts/verify.sh
?? wails.json
?? web/dist/
?? web/src/components/layout/desktop-health-status.tsx
?? web/src/hooks/use-desktop-health.ts
?? web/src/services/desktop/
?? web/src/wailsjs/
?? handoff.md
```

### 不可违反的安全约束

- 不执行 `git reset --hard`、`git clean -fd`、强制 checkout、stash、提交或 push，除非用户另行明确授权。
- 现有 untracked 文件并不等于可删除的垃圾；它们包含本项目的规格、Go/Wails 基座和 WP-01 交付。
- 不读取、修改或迁移真实用户 SQLite 数据库、Credential Manager 条目、浏览器/WebView profile、素材或其他本地用户数据。
- 不调用真实或付费 Provider。
- 不向 React、Zustand、localStorage、IndexedDB、SQLite 业务表、普通备份、日志、诊断、Agent context 或 Wails Binding 返回 Secret 值。
- 不把 WP-02 以后的 Job、Workflow、Agent、Memory 或 Drama 工作混入当前包。

### 既有交接材料

历史交接文件位于：

- [`docs/implementation/handoff-2026-09-05-163956+0800.md`](docs/implementation/handoff-2026-09-05-163956+0800.md)
- [`docs/implementation/handoff-2026-09-07-161115+0800.md`](docs/implementation/handoff-2026-09-07-161115+0800.md)
- [`docs/implementation/handoff-2026-09-08-103244+0800.md`](docs/implementation/handoff-2026-09-08-103244+0800.md)
- [`docs/implementation/handoff-2026-09-08-151249+0800.md`](docs/implementation/handoff-2026-09-08-151249+0800.md)
- [`docs/implementation/handoff-2026-09-08-153906+0800.md`](docs/implementation/handoff-2026-09-08-153906+0800.md)

本 `handoff.md` 是截至当前会话的综合性交接，不替代上述原始证据。

---

## 4. 已完成工作：WP-01

### 4.1 WP-01 实现范围

WP-01 的核心交付是：

1. **Wails/Go desktop foundation**
   - Wails v2.15.0；
   - 以 React/Vite 为 presentation layer；
   - Go 为桌面本地权威 core；
   - 窄 `HealthBinding`，不暴露 SQL、文件系统路径、任意网络或 Secret。

2. **应用生命周期与健康状态**
   - 启动时初始化 app directories、日志、SQLite、FileStore；
   - 数据库故障进入安全只读/安全模式；
   - 关闭路径有幂等资源收敛；
   - 前端仅通过 desktop health service/React Query 获取桌面状态。

3. **SQLite 基座**
   - `database/sql` 与唯一驱动 `modernc.org/sqlite v1.58.0`；
   - 连接启用 WAL、foreign keys 和 busy timeout；
   - migration 按数值版本排序；
   - migration 内容计算 SHA-256 并持久记录；
   - 迁移事务化、升级前快照、失败时 Safe Mode；
   - 当前 foundation migration 只含：`schema_migrations`、`file_objects`、`file_references`。

4. **FileStore 基座**
   - 临时写入、流式 hash、MIME/magic 检查、原子提交；
   - 内容去重、metadata 与引用持久化；
   - 路径穿越、无效 hash、缺失文件与外部引用的受控错误；
   - 用户文件名不会作为最终路径；不向前端返回任意绝对路径。

5. **验证、CI 和文档**
   - PowerShell 与 POSIX 验证脚本已接入前端 typecheck/build、Go test/vet 和有条件的 Wails build；
   - 两个脚本会恢复 `web/dist/.gitkeep`，避免 Vite/Wails build 破坏 Go embed 所需 placeholder；
   - GitHub Actions 保留 Ubuntu web job，并增加 Windows Node 22 / Go 1.25 / Wails v2.15.0 build job；
   - README 已描述浏览器 loopback 开发与 Wails 桌面模式、依赖、数据目录与当前延后范围。

### 4.2 WP-01 主要实现文件

| 文件 | 用途 |
|---|---|
| [`app.go`](app.go) | App 生命周期、依赖组合、SQLite/FileStore 初始化与安全错误日志。 |
| [`main.go`](main.go) | Wails App 配置、资源嵌入、HealthBinding 绑定、single instance、日志初始化。 |
| [`go.mod`](go.mod) / [`go.sum`](go.sum) | Go module、Wails v2.15.0、modernc SQLite 依赖图。 |
| [`wails.json`](wails.json) | Wails 桌面工程配置。 |
| [`internal/application/files/`](internal/application/files/) | FileStore application ports/service。 |
| [`internal/application/health/`](internal/application/health/) | Health application service。 |
| [`internal/domain/apperror/`](internal/domain/apperror/) | 稳定安全错误边界与 diagnostic ID。 |
| [`internal/infrastructure/appdirs/`](internal/infrastructure/appdirs/) | 受控应用数据目录布局。 |
| [`internal/infrastructure/database/`](internal/infrastructure/database/) | SQLite handle、driver、migration、FileRepository 与测试。 |
| [`internal/infrastructure/database/migrations/000001_foundation.sql`](internal/infrastructure/database/migrations/000001_foundation.sql) | 已发布的 foundation migration；后续不得修改。 |
| [`internal/infrastructure/filestore/`](internal/infrastructure/filestore/) | 安全 FileStore 实现与测试。 |
| [`internal/infrastructure/logging/`](internal/infrastructure/logging/) | 脱敏 JSON logging。 |
| [`internal/desktop/`](internal/desktop/) | HealthBinding、envelope/event 与 binding 测试。 |
| [`web/src/services/desktop/health.ts`](web/src/services/desktop/health.ts) | 浏览器/桌面安全检测、延迟 binding 加载与 DTO 验证。 |
| [`web/src/hooks/use-desktop-health.ts`](web/src/hooks/use-desktop-health.ts) | React Query desktop-health 数据读取。 |
| [`web/src/components/layout/desktop-health-status.tsx`](web/src/components/layout/desktop-health-status.tsx) | 健康状态 UI。 |
| [`web/src/wailsjs/`](web/src/wailsjs/) | Wails 生成的 TypeScript bindings；禁止手工编辑。 |
| [`scripts/verify.ps1`](scripts/verify.ps1) | Windows 验证入口。 |
| [`scripts/verify.sh`](scripts/verify.sh) | POSIX/Git Bash 验证入口。 |
| [`.github/workflows/desktop-build.yml`](.github/workflows/desktop-build.yml) | Ubuntu web 与 Windows desktop CI 定义。 |
| [`README.md`](README.md) | 桌面/浏览器运行、验证和 WP-01 范围说明。 |
| [`docs/implementation/STATUS.md`](docs/implementation/STATUS.md) | WP-01 实际验证证据。 |
| [`docs/implementation/TRACEABILITY.md`](docs/implementation/TRACEABILITY.md) | PRD/验收映射；需复核其当前 header/历史内容一致性。 |
| [`docs/adr/0002-sqlite-driver-and-migrations.md`](docs/adr/0002-sqlite-driver-and-migrations.md) | SQLite 选择与证据；目前仍是 Proposed。 |

### 4.3 Task 10：完成内容与审查

Task 10 增强了构建/验证交付：

- `scripts/verify.ps1` 和 `scripts/verify.sh`：
  - 在 repository root 执行 `go test ./... -count=1`；
  - 执行 `go vet ./...`；
  - Wails CLI 存在时运行 `wails build`，否则输出带固定 v2.15.0 安装命令的明确 SKIP；
  - 保持任何已执行 gate 的失败 exit code；
  - 成功或失败后均恢复 `web/dist/.gitkeep`。
- POSIX script 纠正了之前在 `web/` 目录执行 Go 命令的风险，明确返回 root。
- Windows CI 增加 desktop build job；远端 GitHub Actions **未运行**，因为未获得 push 授权。
- README 增加 desktop prerequisites、Wails command、managed data directory 说明和延后功能边界。

独立审查结果：

1. 独立 spec reviewer 首次发现 `web/dist/.gitkeep` 在 build 后未被可靠恢复；
2. 根因被确认是 Vite/Wails 清理 embed placeholder，且此前相对路径恢复位置错误；
3. 两个 verify script 均实现无条件 placeholder restoration 后，重新执行并确认文件存在且为零字节；
4. 独立 spec re-review：**APPROVED**；
5. 独立 quality review：**APPROVED**。

### 4.4 Task 11：最终验收证据

已记录的最终验证事实：

- `go test ./... -count=1`：PASS；
- `go vet ./...`：PASS；
- PowerShell verify：PASS；
- POSIX/Git Bash verify：PASS；
- POSIX `bash -n`：PASS；
- Wails v2.15.0 production build：PASS；
- 隔离 Windows native smoke：PASS。

Native smoke 的隔离方式：

- 使用自有进程 PID `5944`；
- 所有下列环境根目录均指向一个新建 system-temp root：
  - `APPDATA`
  - `LOCALAPPDATA`
  - `USERPROFILE`
  - `HOMEDRIVE`
  - `HOMEPATH`
  - `WEBVIEW2_USER_DATA_FOLDER`
- 只创建隔离的 app DB、WebView 和 log 路径；
- 原生窗口打开并正常关闭；
- migration read-back 为 `[(1,)]`；
- 关闭后的只读连接报告 WAL；
- 日志为 130 字节，且未匹配 authorization/bearer/api-key/SQL 指示模式；
- 检查完毕后移除自有 temporary root。

补充说明：一个关闭后的独立 Python SQLite connection 报告 `foreign_keys=0`，是 SQLite 对新、无关连接的默认值；不代表 app 的受管连接配置失败。应用连接的 foreign key 行为由代码和测试覆盖。

### 4.5 WP-01 验收状态

| 验收 | 状态 | 说明 |
|---|---|---|
| AC-FOUND-001 | PASS（Windows foundation scope） | Wails/Go desktop 基础与 Windows 本机构建/启动证据。 |
| AC-FOUND-002 | PASS（foundation scope） | SQLite migration、安全模式、WAL、foreign keys、目录与日志基础。 |
| AC-FOUND-003 | PASS（foundation scope） | FileStore hash、原子写、去重、路径防护与引用关系。 |
| `go test -race ./... -count=1` | ENVIRONMENT FAILURE，非 PASS | 当前 host 的 32-bit MinGW GCC 不支持 amd64 CGO：`cc1.exe: sorry, unimplemented: 64-bit mode not compiled in`。 |

### 4.6 WP-01 未完成事项与非阻断限制

这些不是 WP-01 失败，但不得被误报为已完成：

- 仅证明 Windows native Wails packaging；没有 macOS/Linux native build/runtime 证据；
- GitHub Actions workflow 已定义，但远端 CI 未运行；
- frontend 自动化测试和 lint command 尚未建立；
- `npm` lockfile/reproducibility 风险仍需单独处理；
- `docs/adr/0002-sqlite-driver-and-migrations.md` 仍为 **Proposed**，等待 race toolchain、非 Windows native evidence、性能和完整 backup/restore 证据；
- legacy browser canvas/project data 尚未迁移到 SQLite；
- SecretStore、Provider Gateway、Jobs、Agent、Workflow、Memory 和 Drama 领域能力均尚未实现。

---

## 5. 当前项目相对 PRD 的进展

### 5.1 已实质推进的 PRD 领域

| PRD/需求领域 | 当前真实状态 | 证据/说明 |
|---|---|---|
| FR-010 本地桌面壳 / Go Core | **WP-01 已实现（Windows proof）** | Wails v2、Go app lifecycle、HealthBinding。 |
| FR-020 SQLite metadata source of truth | **基础已实现，产品迁移未完成** | SQLite/migration/repository 基座存在；浏览器项目/canvas 仍是现有事实来源。 |
| FR-030 Managed FileStore | **基础已实现** | 安全 FileStore 和 file metadata/reference foundation；媒体/legacy Blob 迁移后续处理。 |
| AC-FOUND-001/002/003 | **PASS，限定 WP-01 Windows scope** | 见本交接第 4 节。 |
| 错误安全边界和结构化日志 | **基础已实现** | `apperror` 与 redacting logging。 |
| 验证/构建交付 | **基础已实现** | verify scripts、Wails Windows build、CI definition、README。 |

### 5.2 仍属 Gap 或仅 Partial 的 PRD 领域

| PRD/需求领域 | 当前状态 | 规划工作包 |
|---|---|---|
| FR-040 Secure Provider Gateway | **Gap / baseline unsafe** | WP-02/WP-03。 |
| FR-050 Durable multimodal Jobs | **Gap / browser in-memory only** | WP-03。 |
| FR-060 Legacy import/canvas projection | **Gap** | WP-04。 |
| FR-070 Drama Project/Source/Chapter | **Gap** | WP-05/WP-06。 |
| FR-080 Character/Scene/Event/Story Graph | **Gap** | WP-05/WP-06。 |
| FR-090 Three-layer Agent runtime | **Gap** | WP-07。 |
| FR-100 Durable Workflow | **Gap** | WP-07。 |
| FR-110 Script revisions | **Gap** | WP-08。 |
| FR-120 Production assets / shot planning | **Partial legacy browser behavior only** | WP-09。 |
| FR-130 Storyboard/director integration | **Partial legacy iframe behavior only** | WP-09。 |
| FR-140 Persistent scoped Memory | **Gap** | WP-10。 |
| FR-150 Consistency / quality center | **Gap** | WP-10。 |
| FR-160 Video/audio/subtitle/export | **Partial direct-browser generation only** | WP-11。 |
| FR-170 Backup/restore | **Partial and unsafe** | WP-02/WP-12。 |
| FR-180 Packaging/release | **Partial** | Windows local build proof only; release hardening WP-12。 |

### 5.3 当前最大 PRD/安全缺口

以下风险已被识别但尚未修复，不能误判为仅文档债务：

1. raw API key 仍可进入 React、Zustand、localStorage、普通 config export 和 ZIP backup；
2. 浏览器仍直接执行 Provider 网络请求；
3. Vite development proxy 可以充当 arbitrary-target relay；
4. `web/src/services/api/model-plugin.ts` 仍存在 `new Function`，并向用户脚本暴露 raw API key、base URL、HTTP/network helpers；
5. query string 可注入 API key 到持久化配置；
6. legacy backup/import 不具备目标安全的原子性与 archive 防护；
7. 浏览器 canvas/项目数据尚未成为 SQLite source of truth；
8. Provider、Job、Agent、Workflow、Memory 和 Drama 领域均不存在 Go production path。

这些问题与 [`docs/SECURITY.md`](docs/SECURITY.md) 的 S4 Secret、动态执行、Provider/SSRF、backup 和发布阻断要求直接相关，是 WP-02 及后续包的关键验收对象。

---

## 6. WP-02 已批准计划，但尚未执行

### 6.1 工作包目标

WP-02 的目标是：建立 SecretStore、受控 HTTP client、Provider Registry 和第一个 OpenAI-compatible text adapter，消除**新功能**对 frontend-held secret 和 arbitrary proxy 的依赖。

相应路线图和验收入口：

- [`docs/ROADMAP.md`](docs/ROADMAP.md) WP-02；
- [`docs/ACCEPTANCE.md`](docs/ACCEPTANCE.md) AC-FOUND-004、AC-FOUND-005 的 text/security subset、AC-SEC-001；
- [`docs/SECURITY.md`](docs/SECURITY.md) Secret、动态执行、URL/SSRF/TLS/HTTP 限制要求；
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) Go Core/Provider/Secret 边界。

### 6.2 已作出的 SecretStore 决策

用户已选择：**Windows Credential Manager（推荐项）**。

拟定实现：

```text
internal/infrastructure/secretstore/
  wincred_windows.go  # advapi32: CredReadW / CredWriteW / CredDeleteW / CredFree
  wincred_other.go    # 明确 unavailable，外部 Provider disabled
```

决策理由：

- 当前 `go.mod` 没有 Credential Manager/keyring module；
- module cache 未发现可直接复用的 keyring library；
- 现有间接依赖 `golang.org/x/sys/windows` 可支持 lazy DLL/proc 和 UTF-16 handling，但没有 ready-made Credential Manager wrappers；
- Windows-only 小型 adapter 可避免新增供应链面；
- 不得把 DPAPI 偷换为 Credential Manager，也不得明文 fallback。

预计实现安全属性：

- SQLite 只保存 `secret_references` metadata；
- Secret 值只在 infrastructure 内部构建 Authorization，尽快丢弃；
- Application、Wails Binding 和 frontend 只能看到 configured/masked hint/status/updated time；
- 不提供 `Resolve` Wails method；
- native backend 不可用时，Provider capability unavailable，而不是写入 plaintext config/database/file。

### 6.3 已批准的实施顺序

1. 修正 `STATUS.md` 表头与正文对 WP-01 状态的矛盾，记录 WP-02 baseline；
2. 新增 Provider domain types、error taxonomy 与 application ports/services；
3. 新增 forward-only SQLite migrations：`secret_references` 和 Provider/model/capability metadata；不修改 `000001_foundation.sql`；
4. 实现 Windows Credential Manager、non-Windows unavailable backend 和 test fake；
5. 实现 secure Provider HTTP：URL/host/port/DNS/IP/redirect/TLS validation、validated-IP dial、timeouts/body limits；
6. 实现 trusted Provider registry、OpenAI-compatible text generate/SSE/cancel、health check、redacted audit；
7. 在 `app.go` / `main.go` 组合服务，新增窄 Wails bindings 并通过 Wails 生成 TS bindings；
8. 新增 frontend desktop Provider/Secret client；secure mode 下禁用 query-key initialization 与 model script 执行；迁移文本 streaming；
9. 普通 config export/import 与 ZIP backup 移除 raw secret，并显示 legacy migration warning；
10. 接入 static scans、verify scripts、CI 和所有 security tests；
11. 独立 spec review、独立 quality review、文档与最终验证；完成后停止，不进入 WP-03。

### 6.4 WP-02 计划中的建议文件布局

```text
internal/
  domain/provider/
    provider.go
    error.go
  application/secrets/
    ports.go
    service.go
  application/providers/
    ports.go
    types.go
    service.go
  infrastructure/secretstore/
    wincred_windows.go
    wincred_other.go
    fake.go
  infrastructure/database/
    migrations/000002_secret_references.sql
    migrations/000003_provider_configs.sql
    secrets.go
    providers.go
  infrastructure/providerhttp/
    policy.go
    resolver.go
    transport.go
    client.go
  infrastructure/providers/
    registry.go
    openaicompat/
      adapter.go
      stream.go
  desktop/
    secrets_binding.go
    providers_binding.go
```

该布局尚未创建；下一位 Agent 必须基于最终读到的 codebase 和 AGENTS 规则重新确认，不能因为本建议而跳过设计验证。

### 6.5 WP-02 前端迁移热点

以下文件是 raw secret / direct Provider / dynamic code 的已确认路径：

| 文件 | 已识别问题 | WP-02 迁移策略 |
|---|---|---|
| [`web/src/stores/use-config-store.ts`](web/src/stores/use-config-store.ts) | `ModelChannel.apiKey`、`AiConfig.apiKey` 被 Zustand persist；`resolveModelRequestConfig` 返回 raw key。 | 以 secret reference/status/mask 替代新 secure path；保留 legacy data 的受控迁移提示，避免无声数据丢失。 |
| [`web/src/components/layout/channel-editor-drawer.tsx`](web/src/components/layout/channel-editor-drawer.tsx) | raw `Input.Password` 编辑 API key。 | 改为 set/replace/delete secret command 与 masked status UI。 |
| [`web/src/components/layout/app-config-modal.tsx`](web/src/components/layout/app-config-modal.tsx) | readiness 和 channel propagation 依赖 raw key。 | 切换为 backend configuration/secret state。 |
| [`web/src/components/layout/client-root-init.tsx`](web/src/components/layout/client-root-init.tsx) | 从 URL query 接收并持久化 API key。 | secure mode 下禁用，显示迁移/安全提示。 |
| [`web/src/components/layout/model-select-modal.tsx`](web/src/components/layout/model-select-modal.tsx) | browser key/direct upstream model discovery。 | 迁移到 backend provider health/model list 或标注 legacy。 |
| [`web/src/services/api/model-plugin.ts`](web/src/services/api/model-plugin.ts) | `new Function` 执行用户 JS，允许任意 HTTP 和 key exfiltration。 | secure mode 下必须不可达；不将 script 迁移到其他执行机制。 |
| [`web/src/services/api/image.ts`](web/src/services/api/image.ts) | direct image/text request；`requestImageQuestion` 是首个文本迁移候选。 | 仅迁移 text streaming；image/edit 等保留 legacy。 |
| [`web/src/services/api/audio.ts`](web/src/services/api/audio.ts) | direct key/network request。 | WP-02 不迁移。 |
| [`web/src/services/api/video.ts`](web/src/services/api/video.ts) | direct create/poll/download。 | WP-02 不迁移。 |
| [`web/src/lib/api-proxy.ts`](web/src/lib/api-proxy.ts) / [`web/vite.config.ts`](web/vite.config.ts) | arbitrary target proxy、header forwarding、无 SSRF controls。 | 不复用；Go secure client 是新路径。 |
| [`web/src/services/config-file.ts`](web/src/services/config-file.ts) | 普通 config export/import 含完整 `AiConfig`。 | 普通导出移除 secret；legacy import 安全化并提示。 |
| [`web/src/services/backup-restore.ts`](web/src/services/backup-restore.ts) | ZIP backup 序列化完整 `AiConfig`，含 secret/script。 | 普通 backup 移除 secret；完整 restore hardening 延后。 |

### 6.6 WP-02 必须测试的重点

- AC-FOUND-004：native/fake SecretStore，DB 仅存 reference，Binding 无 Resolve，frontend store/log/普通 backup 不存在 test secret；
- AC-FOUND-005 text/security subset：Go 添加 Authorization、timeout、429、5xx、invalid JSON、SSE stream cancellation、URL allowlist、redirect SSRF；
- AC-SEC-001：所有 Security 语料拒绝，且只允许经精确批准的 local Provider host/port；
- DNS rebinding：初始公网、再次解析私网必须拒绝；
- URL credentials、fragment、IPv4-mapped IPv6、metadata endpoint、loopback/private/link-local/ULA/multicast/reserved range 必须拒绝；
- TLS 不可绕过；不采用 `InsecureSkipVerify`；
- 审计/日志/错误/DTO 不能泄漏固定 test secret；
- Windows test 只能使用自有、唯一前缀 credential target，并在 cleanup 删除。

### 6.7 WP-02 明确不做

- 不立即删除所有 legacy browser API calls；
- 不完成 image/video/audio Provider migration；
- 不实现 persistent Job Manager；
- 不实现 Agent runtime、Workflow、Memory、Drama；
- 不调用真实或付费 Provider；
- 不实现完整 Go archive/backup/restore redesign；
- 不引入 plaintext fallback、generic fetch binding、arbitrary proxy、或 Secret resolve UI/API。

---

## 7. 审查者应重点核验的事项

### 7.1 WP-01 实现审查

1. **依赖方向**：确认 Domain 没有 import Wails、SQLite、HTTP、Provider SDK 或 OS API；SQL 保持 Infrastructure；Wails 只绑定 adapter。  
2. **SQLite 安全性**：确认 migration checksum/snapshot/safe mode、WAL/foreign keys、transaction 与 context 使用符合规范。  
3. **FileStore 安全性**：确认路径 containment、atomic commit、hash/dedup、错误处理、引用关系与 no absolute-path frontend API。  
4. **桌面边界**：确认只有 health binding，前端没有可用 SQL/file/network/secret primitive。  
5. **验证脚本**：确认 Wails build cleanup 不会遗失 `web/dist/.gitkeep`，且 Go commands 从 root 执行。  
6. **证据一致性**：区分本地 Windows PASS、未运行 remote CI、未完成 non-Windows proof 和 `-race` environment failure。  
7. **文档一致性**：`STATUS.md` 第 4–5 行仍写 WP-01 IN PROGRESS，但正文第 12、19、142 行记录 WP-01 complete/closed；这是已识别的文档矛盾，应在 WP-02 启动时最小修正，不能据此否定 Task 11 实际记录。

### 7.2 PRD 进展审查

1. 评估 WP-01 foundation 是否足以满足限定的 AC-FOUND-001/002/003；
2. 确认没有把 foundation 误报为“canvas 数据已经数据库化”；
3. 确认 Secret、Provider、SSRF、动态脚本、backup 风险仍被如实列为 Gap；
4. 确认 WP-02 计划严格限定 text adapter，不提前实施 WP-03+ 能力；
5. 检查所有 future work 是否保留在 Roadmap，而非被 STATUS 宣称完成。

### 7.3 安全审查

1. 现有 frontend raw-key / `new Function` / arbitrary proxy 路径是发布阻断级遗留问题；它们尚未被 WP-01 改造。  
2. WP-02 的 Credential Manager adapter 需要审查 Windows `CREDENTIALW` layout、unsafe/syscall error conversion、UTF-16 conversion、owned target cleanup 和 non-Windows behavior。  
3. secure HTTP 设计必须以 tested resolver/dial seam 绑定已验证 IP，而不仅是请求前 `net.LookupIP`。  
4. redirect 必须逐跳验证，Authorization 只能由 adapter 构建，不能转发用户 headers。  
5. 普通 export/backup 禁止 secret；不能把“mask UI”误当成存储层安全。

---

## 8. 推荐的下一步

当前可安全开始的工作包是：

```text
WP-02 — Secret、网络安全与 Provider Gateway 基础
```

开始前应执行：

1. 重新执行 `git status --short --branch` 并标注当前已有用户修改；
2. 依 AGENTS 规定阅读所有必读文件；
3. 运行当前可执行 baseline，记录真实结果；
4. 最小化修正 `STATUS.md` 对当前包的表头状态；
5. 先写 domain/application contracts 与 tests，再实现 Credential Manager、migration、secure HTTP、OpenAI-compatible text adapter、binding 和迁移 UI；
6. 使用 mock/`httptest`，不访问真实 Provider；
7. 完成后独立 spec review、独立 quality review、最终 verify；
8. 更新 STATUS/TRACEABILITY/ADR，报告后停止，不自动进入 WP-03。

---

## 9. 已知限制与未决问题

| 项目 | 状态 | 后续处理 |
|---|---|---|
| `STATUS.md` 顶部状态矛盾 | 已识别 | WP-02 开始时最小修正文档表头；保留 WP-01 历史证据。 |
| Go race test | Host environment failure | 在支持 amd64 race/C compiler 的环境中重新运行；不得标 PASS。 |
| macOS/Linux desktop evidence | 未提供 | 后续 CI/platform matrix 完成。 |
| Remote GitHub Actions | workflow 已定义但未运行 | 有 push/CI 授权后验证。 |
| Frontend test/lint | 缺少 test/lint scripts | 在相应 WP 按 PRD 引入，不能将 typecheck 视为行为测试。 |
| `npm ci` reproducibility | 历史 lockfile 风险 | 独立、窄范围 lockfile 修复与 clean-clone CI evidence。 |
| SQLite ADR-0002 | Proposed | 等待 cross-platform/race/performance/backup-restore evidence。 |
| Raw secret/direct Provider paths | 未修复 | WP-02。 |
| Dynamic model script execution | 未修复 | WP-02 secure mode 下必须不可达。 |
| Legacy data migration | 未开始 | WP-04，先 fixture/backups/read-back。 |

---

## 10. 交接结论

- **WP-01：COMPLETE（仅限已记录的 Windows foundation acceptance scope）。**
- **WP-02：计划已获批准、SecretStore 技术路径已选定，但实现为 0。**
- 当前项目已经从纯浏览器 React/Vite 应用获得了可信的 Go/Wails/SQLite/FileStore 基座；但 PRD 的安全 Provider、持久化工作流/Jobs、领域模型、Agent、Memory、媒体与发布能力仍主要处于 Gap 或 legacy Partial 状态。
- 下一位 Agent 应将本文件视为审查索引和实施边界，不应以此替代必读规范、当前代码、测试和 git 现场的重新核验。

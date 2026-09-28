# PRD Gap Closure 审计基线（RP-00.1，2026-09-28）

> 用途：为 RP 系列工作包提供可复用的执行前基线。新执行者据此区分「用户既有脏改」「审计发现」与「自己的新改动」。
> 本文件只记录已验证事实；推断与未来计划一律不写。密钥只记录「配置/未配置」，不打印值。

## 1. 分支与 Git 状态（2026-09-28 采集）

- 分支：`codex/wp-01-desktop-foundation`，领先 `origin/codex/wp-01-desktop-foundation` **172 commits**。
- HEAD：`4be369d4be8cd2e72ba32fd3199e75c57e608c07`（"handoff: P3-complete state…"）。
- 工作树状态：`git status --porcelain` 共 **124 条**：
  - ` M`（已跟踪修改）82 条；
  - `??`（未跟踪）41 条；
  - ` D`（已删除）1 条：`web/dist/.gitkeep`（verify.sh 会重建该占位文件，属预期行为，非用户数据）。
- 未跟踪项分类（执行时不得覆盖、不得误提交）：
  - 用户文档/分析稿：`Infinite-Atelier-OpenCode集成必要性与完整实施方案.md`、`T32_COMMIT_STEPS.sh`、`.omx/`（工具状态目录）、`scripts/local-toolchain-env.sh`（机器本地工具链路径，已被 .gitignore 声明忽略但目录本身未跟踪）。
  - 上一批次（T-batch）新增代码与测试（**属于待提交的既有工作，不是本计划产物**）：`backup_scheduler.go`、`local_job_handlers.go`、`internal/application/assets/collect.go`、`internal/domain/provider/manifest.go`、`internal/infrastructure/jobs/local_handlers_t06.go`、`internal/infrastructure/media/content_analysis_t16.go`、migrations 000027–000031、各 `*_tXX_test.go` 等。
  - 本计划新增：`docs/plans/2026-09-28-prd-gap-closure-development-plan.md` 与本文件。
- 快照存档：执行时 `git status --porcelain` 全文 124 行已保存于本文件的采集会话；后续各包开始前必须重新执行 `git status --short --branch` 并与上包结束状态对比。

## 2. 工具链环境（2026-09-28 实测）

| 项 | 值 |
|---|---|
| Go（系统 `go`） | go1.25.13 windows/amd64（与 go.mod `toolchain go1.25.13` 一致） |
| go.mod | `go 1.25.0` + `toolchain go1.25.13` |
| GOTOOLCHAIN | `auto` |
| GOPROXY / GOSUMDB | `https://proxy.golang.org,direct` / `sum.golang.org`（本机全局 GOSUMDB=off 的问题在当前 shell 不存在） |
| Node | v22.22.3 |
| npm | 10.9.8 |
| Wails CLI | **不在 PATH**。验证方式：`where.exe wails` 失败；`C:/Users/94278/go/bin` 无 wails.exe。历史 STATUS 记录的构建环境（GOROOT 指向 toolchain 缓存 + `C:/msys64/mingw64/bin/gcc.exe`）**在本机当前盘符上不存在**（`D:\GoWorks1.18`、`C:\msys64` 均缺失）；`C:/Users/94278/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.25.13.windows-amd64` 存在且其 go.exe 可运行 go1.25.13。含义：`wails build` / `wails generate module` 当前**不可直接执行**，涉及 Binding 再生成的包（RP-01 等）需先解决 wails CLI 可用性（安装 v2.15.0）或如实记录 BLOCKED。 |
| gcc / CGO | 未找到可用 gcc（`CGO_ENABLED=0`）。race 与 ONNX(cgo) 测试在当前 shell 无法运行，需按 `scripts/local-toolchain-env.sh` 的历史路径恢复或安装 msys64；找不到时如实标 BLOCKED。 |
| FFmpeg / ffprobe | 8.0.1 essentials（gyan.dev），在 PATH。 |
| makensis | 未找到。NSIS 打包门 BLOCKED。 |
| ONNX Runtime / 模型 | `IA_ONNX_RUNTIME`、`IA_REQUIRE_ONNX` 均未设置；cgo 不可用 ⇒ `internal/infrastructure/onnxemb` 的 cgo 测试文件完全不参与编译，`go test` 显示 `[no test files]` —— 不构成推理证据（对应 RP-10.3）。 |

密钥状态（只记配置与否）：`OPENAI_API_KEY` 在本 shell 环境已配置（值未读取、未打印）。其余 provider 密钥环境变量未见。

## 3. npm 双 lockfile 核对

- `web/package-lock.json` ✓ 存在（主应用）。
- `web/monoform-studio/package-lock.json` ✓ 存在（MONOFORM 独立构建）。
- 仓库根与 `web/` 无 `yarn.lock`、`pnpm-lock.yaml` ✓。
- `web/monoform-studio/node_modules` 存在（MONOFORM build 门可跑）。

## 4. 审计验证结果（2026-09-28 本机实测，作为 RP 系列起点基线）

| 门 | 命令 | 结果 | 备注 |
|---|---|---|---|
| 前端类型检查 | `npm --prefix web run typecheck` | **PASS**（exit 0） | |
| 前端测试 | `npm --prefix web test` | **PASS**（136/136，0 fail 0 skip） | 计划 §1.1 记录为 136/136，一致 |
| 安全扫描 | `node scripts/security-scan.mjs` | **PASS**（732 files；1 dynamic-execution 例外 + 3 legacy direct-call 文件） | |
| SBOM 检查 | `node scripts/gen-sbom.mjs --check` | **FAIL（exit 1）**：`sbom\cyclonedx.json`、`sbom\licences.json` 两份 drift | 与计划 §1.1 一致；RP-10.1 处理，不在本包重生成掩盖 |
| `git diff --check` | `git diff --check` | **PASS**（无 whitespace error；大量 CRLF 提示属 autocrlf 行为，非错误） | |
| Go 全量测试 | `go test ./... -count=1` | 见 §4.1（本包采集时在后台运行，结果回填） | |
| race / ONNX / Wails build / NSIS | — | **BLOCKED**（gcc/wails/makensis 当前不可用，见 §2） | 需环境恢复后重验 |
| EXE/安装包哈希 | `sha256sum build/bin/*` | EXE `a5bc0003…d73616c`（33,962,496 B）；installer `4def0545…abe6cde`（15,899,831 B） | 与 STATUS「T32 执行包」记录的两个哈希一致；均为 **dirty 工作树**产物，不等于干净构建证据 |

### 4.1 Go 全量测试基线（结果回填）

- `go test ./... -count=1`（Go 1.25.13，CGO_ENABLED=0）：**PASS（exit 0，全部包 ok）**。
- `internal/infrastructure/onnxemb` 显示 `[no test files]`：因 cgo 构建标签在 CGO=0 下排除全部 cgo 测试文件——这是环境事实，不是推理通过证据（RP-10.3 的对象）。
- database 包耗时约 218 秒（与计划 §1.1 记录一致）。

## 5. 已知发现登记（重现路径 → 预期 → 关联包）

每项格式：发现 / 重现 / 预期 / 证据 / 归属。

1. **SBOM drift（两份）** / `node scripts/gen-sbom.mjs --check` exit 1 / 可解释的确定性生成，`--check` 通过 / 本文件 §4 / RP-10.1。
2. **wails CLI 不可用** / `where.exe wails` 失败 / `wails generate module`、`wails build` 可执行 / 本文件 §2 / 影响所有涉及 Binding 再生成的包（RP-01.1 等）；执行到相应任务时先安装 wails v2.15.0（`go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0`）再重试，仍失败则该步 BLOCKED。
3. **gcc/makensis 不可用** / `C:\msys64` 不存在 / race、cgo ONNX、NSIS 门可运行 / 本文件 §2 / RP-10.2/10.3；恢复环境前相关门标 BLOCKED，不伪装 PASS。
4. **verify.sh 的 5173 端口清理** / `scripts/verify.sh` `kill_stray_dev_servers` 按 `fuser/lsof -ti tcp:5173` 杀进程 / 只清理自己启动的进程 / RP-10.2 处理 / 已登记。
5. **NSIS 模板未跟踪** / `build/windows/installer/project.nsi` 存在但 `git ls-files` 不含它（STATUS T30 与计划 RP-10.2 均已记录）/ 受版本控制来源 / RP-10.2 处理。

## 6. RP-00.1 验收自检

- [x] 按 AGENTS 顺序读取了必读文档的当前版本（PRD、AGENTS、STATUS、Roadmap、Architecture、Domain、Agent Contracts、Security、Acceptance、两份参考分析在本包采集时按需抽读关键节；完整逐包重读在各包执行时进行）。
- [x] 记录 branch、HEAD、Git 状态清单、环境（§1–2）；密钥只记「配置/未配置」。
- [x] 计划 §1.1 的审计命令结果保存为历史基线（§4）；未无意义重跑全部历史测试——仅重跑 typecheck/npm test/security-scan/SBOM/git diff --check/go test 作为当前工作树的起点事实。
- [x] npm 双 lockfile 核对（§3），无 yarn/pnpm。
- [x] 每项发现记录了重现/预期/证据/归属（§5）。
- [x] 未修改用户数据、Git 历史；未覆盖任何脏文件。

**验收判定：PASS。** 新执行者可据本文件区分用户脏改（§1 分类）、审计发现（§5）与后续新改动（各包开始时对比 §1 快照）。

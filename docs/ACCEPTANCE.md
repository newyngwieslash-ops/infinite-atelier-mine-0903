# Infinite Atelier Drama Studio 验收与测试规范

> 版本：v1.0  
> 状态：已批准  
> 目标：把 PRD 要求转换为可重复的自动化与人工验收

---

# 1. 验收原则

1. 以可执行结果为准，不以代码数量、目录或 Agent 自述为准；
2. 关键路径必须自动化；
3. CI 不依赖真实付费 Provider；
4. 真实 Provider 只做受控手工冒烟；
5. 所有失败必须可重复、可诊断；
6. 安全要求是发布阻断项；
7. 旧自由画布回归与新短剧流程同等重要；
8. 工作包只验收当前范围，不允许用未实现的未来模块代替；
9. Mock 必须明确标注，不能在生产路径返回假成功；
10. 验收证据写入 `docs/implementation/STATUS.md`。

---

# 2. 测试层

## T1 Domain Unit

覆盖：

- 状态机；
- 版本批准；
- locked rule；
- stale；
- relation registry；
- Job retry；
- Memory score；
- URL/IP 分类；
- ID/revision。

要求：快速、确定性、无 I/O。

## T2 Application Use Case

使用内存 Ports 测试：

- 命令校验；
- 事务边界；
- 幂等；
- 权限；
- 领域事件；
- error code。

## T3 Infrastructure Integration

覆盖：

- SQLite migrations；
- Repository；
- FileStore；
- SecretStore fake/native contract；
- Backup/Restore；
- Provider httptest；
- Job recovery；
- VectorIndex。

每个测试使用独立临时目录和数据库。

## T4 Agent Contract

使用 Deterministic Mock LLM：

- Decision Tool selection；
- Execution Tool ACL；
- Supervisor read-only；
- JSON Schema；
- Schema repair；
- max tool calls；
- cancellation；
- Memory context；
- Deep Recall。

## T5 Frontend Unit/Component

覆盖：

- Presenter；
- Store；
- Binding Client；
- Event reducer；
- Quality Gate；
- Job Center；
- Canvas projection；
- error UI；
- i18n key。

## T6 E2E

Playwright 或项目批准的桌面 E2E：

- 旧项目迁移；
- 自由画布；
- 短剧主流程；
- 中断恢复；
- 备份恢复；
- Agent/Quality Gate；
- Secret UI。

## T7 Security

覆盖 `docs/SECURITY.md` 的攻击语料。

## T8 Performance

基准：

- 1,000 node canvas；
- 2,000 edges；
- 10,000 asset metadata；
- 100k Chinese characters；
- 10k memory candidates；
- 500 jobs history；
- large archive limits。

## T9 Packaging

- Windows clean VM；
- install/start/close/restart；
- app data location；
- migration；
- uninstall does not delete user data without confirmation；
- build artifacts and license notices。

---

# 3. Canonical Verification Commands

WP-00 必须根据现有仓库包管理器确认并写入脚本。目标提供：

```text
scripts/verify.sh
scripts/verify.ps1
```

二者执行等价步骤：

```text
Go format check
Go vet/static analysis
Go tests
Frontend install with lockfile
Frontend typecheck
Frontend unit tests
Frontend production build
Schema/Skill validation
Security static scans
License/SBOM checks where available
```

最终推荐命令：

```bash
./scripts/verify.sh
```

Windows：

```powershell
./scripts/verify.ps1
```

开发阶段单项命令至少包括：

```bash
go test ./...
go vet ./...
cd web && npm ci
cd web && npm run typecheck
cd web && npm run test -- --run
cd web && npm run build
```

如现有仓库使用其他 lockfile，WP-00 保留原包管理器，不得同时提交 npm/yarn/pnpm 多套 lockfile。

---

# 4. 测试数据

```text
testdata/
├─ canary-drama/
│  ├─ source.md
│  ├─ expected-chapters.json
│  ├─ approved-facts.json
│  ├─ mock-agent-responses/
│  ├─ expected-script.json
│  ├─ expected-storyboard.json
│  └─ placeholder-media/
│
├─ old-projects/
│  ├─ minimal/
│  ├─ all-node-types/
│  ├─ missing-media/
│  └─ malformed-metadata/
│
├─ provider-fixtures/
│  ├─ openai-compatible/
│  ├─ gemini-compatible/
│  ├─ async-video/
│  └─ errors/
│
└─ malicious-imports/
   ├─ prompt-injection.txt
   ├─ zip-slip.zip
   ├─ zip-bomb-metadata.zip
   ├─ bad-docx.docx
   ├─ svg-script.svg
   ├─ huge-pixel-header.png
   └─ invalid-project-pack.zip
```

测试数据必须原创或可自由使用，并在 `testdata/LICENSES.md` 标明来源。

---

# 5. 基线验收

## AC-BASE-001 仓库可复现

前置：干净 clone。

步骤：

1. 按 README 安装；
2. 使用锁文件安装依赖；
3. 运行当前 typecheck/build；
4. 启动当前应用；
5. 记录失败和环境。

通过：

- `docs/implementation/BASELINE.md` 有真实命令、版本、结果；
- 不因规格工作改动现有业务代码；
- 已知失败不被伪装成通过。

## AC-BASE-002 功能清单

必须记录：

- 路由；
- 节点类型；
- 持久化位置；
- 模型调用路径；
- 备份格式；
- MONOFORM 集成；
- 动态脚本；
- Vite proxy；
- 构建产物。

---

# 6. Foundation 验收

## AC-FOUND-001 Wails/Go Core

- Production build 启动桌面窗口；
- React 由 Wails 承载；
- 一个 Health Binding 返回版本、DB 状态和数据目录；
- 前端不需要外部 Vite Server；
- Windows 关闭后进程退出。

## AC-FOUND-002 SQLite

- 空环境自动创建；
- migrations 顺序执行；
- foreign_keys 开启；
- WAL 生效；
- migration failure 进入安全模式；
- pre-migration snapshot 存在。

## AC-FOUND-003 FileStore

- 导入文件写临时；
- 哈希；
- MIME/Magic；
- 原子提交；
- 重复内容去重；
- traversal 被拒绝；
- missing file 可诊断。

## AC-FOUND-004 Secret

- 写入 native/fake SecretStore；
- DB 只有 ref；
- Wails API 无 Resolve；
- 前端 store 扫描无明文；
- 日志和普通备份扫描无明文。

## AC-FOUND-005 Provider

Mock server 验证：

- Authorization 由 Go 添加；
- timeout；
- 429；
- 5xx；
- invalid JSON；
- stream cancel；
- URL allowlist；
- redirect SSRF。

## AC-FOUND-006 Job

- queued → running → succeeded；
- retry_wait；
- cancel；
- app crash/restart；
- remote ID poll；
- duplicate idempotency；
- temp file commit。

---

# 7. 旧项目兼容验收

## AC-LEGACY-001 导入完整性

输入 `all-node-types`：

- Project 数量一致；
- Node/Edge 数量一致；
- Text 内容一致；
- 图片/视频/音频文件哈希一致；
- Viewport 可恢复；
- unsupported metadata 进入 legacy field + warning；
- 旧数据仍保留。

## AC-LEGACY-002 幂等

同一 legacy 项目导入两次：

- 第二次检测已导入；
- 不重复媒体；
- 用户可选择新副本；
- 默认不覆盖。

## AC-LEGACY-003 回滚

在导入中注入错误：

- 新 DB 无半成品；
- 临时文件清理；
- 旧项目可继续打开；
- 错误报告有具体阶段。

---

# 8. 短剧领域验收

## AC-STORY-001 文档导入

输入 ≥30,000 汉字、≥3 章：

- 编码正确；
- 章节检测；
- 用户调整；
- offsets 有效；
- 原文定位；
- duplicate hash 提示；
- UI 不冻结。

## AC-STORY-002 事件候选

Mock Agent 返回人物、地点、事件、因果：

- 结构校验；
- 来源 chapter/offset；
- candidate 状态；
- 接受/拒绝/锁定；
- 冲突记录；
- 无跨项目污染。

## AC-SCRIPT-001 Story Skeleton

- Workflow 创建；
- Execution 独立 AgentRun；
- 生成 Version；
- Supervisor 读取 DB；
- ReviewReport；
- 用户 PASS；
- approved 唯一。

## AC-SCRIPT-002 FIX

预置缺少结尾 Hook：

- Supervisor 指向 skeleton/version；
- 用户 FIX issue；
- 新 StageRun attempt；
- 新版本；
- 原版本保留；
- 锁定字段不变；
- 二次审核通过。

## AC-SCRIPT-003 Script Structure

- Episode/ScriptVersion/Scene/Dialogue/Shot 正式实体；
- 顺序唯一；
- source event 引用；
- 原创改编标记；
- duration；
- 版本 diff；
- Canvas projection。

---

# 9. 资产与分镜验收

## AC-ASSET-001 版本与批准

- 角色 Asset；
- 两个 candidate versions；
- 批准 v1；
- 批准 v2 时 v1 superseded；
- 使用 v1 的 Shot 触发影响分析；
- v1 不被删除。

## AC-ASSET-002 File Lineage

每个 generated image：

- physical file；
- hash；
- job；
- provider/model；
- prompt；
- parent refs；
- agent/stage；
- asset usage。

## AC-BOARD-001 Storyboard Table

- 从 approved Script/Director/Assets 生成；
- ≥12 Shots；
- 必需字段；
- 资产 refs；
- Supervisor 检查；
- 未通过时阻止批量生成。

## AC-BOARD-002 连续性错误

故意让 Shot 6 使用错误服装：

- deterministic/LLM supervisor 定位 Shot 6；
- evidence 指向两个版本；
- FIX 只更新 Shot 6/关联版本；
- 其他 Shot 不变。

## AC-BOARD-003 Image Jobs

- 每 Shot 2 candidates；
- 并发限制；
- 取消一个；
- 重试失败项；
- 批准一个结果；
- Canvas 显示关系；
- 重启不重复提交。

---

# 10. Agent 验收

## AC-AGENT-001 层隔离

- Decision 无业务写 Tool；
- Execution 只有阶段 Tool；
- Supervisor 只有读 Tool；
- 非法调用返回 `security.tool_not_allowed`；
- Run 记录失败。

## AC-AGENT-002 输出 Schema

- 合法 JSON 成功；
- 第一次 malformed，repair 成功；
- 两次 malformed，Stage 失败；
- 无业务半写入。

## AC-AGENT-003 Artifact 幻觉

模型返回不存在的 ID：

- Runtime 验证失败；
- 不标记 success；
- 记录错误；
- Workflow 不前进。

## AC-AGENT-004 Prompt Injection

源文档包含越权指令：

- 不改变 Skill；
- 不新增 Tool；
- 不读取 Secret；
- Agent 可继续处理故事内容；
- 安全测试通过。

## AC-AGENT-005 最大循环

- Tool Calls 超限停止；
- FIX 超过 2 次转人工；
- 不无限运行。

---

# 11. Memory 验收

## AC-MEM-001 Scope

建立两个项目相似角色设定：

- Project A 查询只返回 A；
- Episode scope 正确；
- Agent scope 正确；
- 跨项目泄露为 0。

## AC-MEM-002 Self-hit

当前消息：`女主不能穿红色`。

- Recall 在写当前消息前或排除其 ID；
- Context 中当前句只出现一次；
- 单元测试验证。

## AC-MEM-003 Threshold

所有候选低于阈值：

- semantic 返回空；
- 不强制 TopK；
- locked high-importance memory 走独立规则。

## AC-MEM-004 Summary provenance

- Summary 关联源消息表；
- role/agent/time 保留；
- 删除/失效行为符合策略；
- UI 可跳原始消息。

## AC-MEM-005 Deep Recall

早期设定经过大量消息：

- Summary 检索；
- rerank；
- 恢复原始消息；
- 返回来源；
- 限制 Token；
- 不每轮自动调用。

---

# 12. Canvas 验收

## AC-CANVAS-001 领域投影

- 创建 Script Scene 后创建 Canvas Node；
- Node 有 entity refs；
- 修改实体后 Node 更新；
- 移除 Node 不删除 Scene；
- 删除 Scene 显示影响。

## AC-CANVAS-002 语义边

- 合法 `references` 成功；
- 非法 source/target 拒绝；
- required ref 删除被阻止；
- stale 显示；
- Edge 不是仅 UI 线。

## AC-CANVAS-003 性能

生成 1,000 nodes/2,000 edges：

- 打开时间达到 PRD 目标；
- pan/zoom 不冻结；
- 选中和移动局部更新；
- 内存无无界增长。

## AC-CANVAS-004 现有功能

- add/move/delete；
- multi-select；
- box select；
- zoom/pan；
- undo/redo；
- image crop/split/mask；
- generation child node；
- minimap；
- connections。

---

# 13. 备份与安全验收

## AC-BACKUP-001 普通备份

- Manifest；
- DB snapshot；
- files；
- checksums；
- restore；
- grep/扫描无 test API Key；
- 无 Authorization/Cookie。

## AC-BACKUP-002 恢复原子性

注入损坏文件：

- restore 失败；
- 当前项目不变；
- 临时目录清理；
- 报告具体哈希。

## AC-SEC-001 SSRF

所有 `docs/SECURITY.md` Provider 案例被阻止，显式批准的精确本地 Provider 可工作。

## AC-SEC-002 Archive

Zip Slip、symlink、duplicate normalized path、oversize、bomb 被阻止。

## AC-SEC-003 Secret

自动扫描：

```text
frontend state
compiled web assets where practical
SQLite
ordinary backup
logs
diagnostics
agent memory
raw prompt fixtures
```

测试 Secret 不能出现。

---

# 14. 媒体与导出验收

## AC-MEDIA-001 Async Video

Mock：

- submit；
- remote ID；
- poll；
- restart；
- fetch；
- validate；
- asset version；
- duplicate response；
- cancel。

## AC-MEDIA-002 TTS/Subtitle

- dialogue line → voice job；
- audio linked to character/line；
- subtitle editable；
- SRT/VTT valid；
- missing line detected。

## AC-MEDIA-003 Timeline/MP4

- ordered Shots；
- audio/subtitle；
- replace clip；
- export；
- Final Supervisor；
- output playable；
- manifest traceability。

---

# 15. 发布矩阵

| 能力 | Foundation | MVP | v1 |
|---|---:|---:|---:|
| Wails/Go Core | 必须 | 必须 | 必须 |
| Legacy migration | 基础 | 完整 | 完整 |
| Free Canvas regression | 必须 | 必须 | 必须 |
| Secure Provider | 必须 | 必须 | 完整路由 |
| Persistent Job | 必须 | 必须 | 完整媒体 |
| Story/Event | 否 | 必须 | 增强 |
| Script Agent | 否 | 必须 | 增强 |
| Production Agent | 否 | 到分镜图 | 视频/音频 |
| Workflow/Quality | 骨架 | 必须 | 增强 |
| Memory | 接口 | 基础完整 | 层级/本地 ONNX |
| MONOFORM | 保留 | 基础桥 | 深度集成 |
| Export | 项目备份 | 基础单集 | 完整 MP4/外部格式 |
| Windows package | 内部 | 必须 | 稳定 |
| macOS/Linux | 否 | 可选验证 | 必须验证 |

---

# 16. 工作包验收记录模板

```markdown
## WP-XX 验收记录

- Commit/branch:
- Date:
- Environment:
- Scope completed:
- Scope not completed:

### Commands

```text
command
result
```

### Acceptance

| ID | Result | Evidence |
|---|---|---|
| AC-... | PASS/FAIL/BLOCKED | test/file/screenshot/log |

### Regression

- Existing canvas:
- Build:
- Security:

### Known issues

- ...

### Next safe work package

- ...
```

禁止只写“测试通过”而不记录命令与结果。


# AGENTS.md

本文件适用于在此仓库工作的 Codex、Cursor、Claude Code、Grok 或其他编码 Agent。

---

# 1. 角色

你是本仓库的资深产品工程师、Go 架构师、React/TypeScript 工程师、桌面应用工程师、AI/Agent 平台工程师、SQLite 工程师、测试工程师和安全工程师。

你的任务不是快速堆出 Demo，而是在保留现有 Infinite Atelier 能力的前提下，按批准工作包把项目演进为：

```text
Infinite Atelier Core
+ Drama Production Pack
+ Go Core
+ Durable Workflow
+ Three-layer Agent Runtime
+ Persistent Memory
+ Secure Provider Gateway
```

---

# 2. 必读顺序

开始任何修改前，按顺序完整阅读：

1. `PRD.md`
2. `AGENTS.md`
3. `docs/implementation/STATUS.md`
4. `docs/ROADMAP.md` 中当前工作包
5. `docs/ARCHITECTURE.md`
6. `docs/DOMAIN_MODEL.md`
7. `docs/AGENT_CONTRACTS.md`
8. `docs/SECURITY.md`
9. `docs/ACCEPTANCE.md`
10. `docs/reference/INTEGRATION_ANALYSIS.md`
11. `docs/reference/TOONFLOW_AGENT_MEMORY_ANALYSIS.md`
12. 仓库 README、package、配置、CI、代码和测试

不要根据文件名或摘要假设内容已经读过。

---

# 3. 事实优先级

1. PRD 的必须项、固定产品决策和发布阻断条件；
2. AGENTS 工程规则；
3. Architecture/Domain/Agent/Security/Acceptance；
4. Roadmap 当前工作包；
5. Status 中已验证事实；
6. 当前代码和测试；
7. 参考分析；
8. 你的推断。

发现冲突：

- 不静默决定；
- 优先保留数据和现有行为；
- 记录到 `STATUS.md`；
- 当前包能安全解决则解决并记录；
- 需要产品决策则停在安全边界，不偷跑。

---

# 4. 工作包协议

## 4.1 一次只做一个包

只执行 `STATUS.md` 指定的当前工作包。

禁止：

- 一次实现整个 PRD；
- 顺手开始下一个 WP；
- 以未来代码弥补当前验收；
- 大范围无关重构；
- 把未完成能力标记完成。

## 4.2 开始前

必须：

1. `git status --short --branch`；
2. 记录现有用户修改；
3. 不覆盖、不 stash、不 reset 不属于你的修改；
4. 识别当前 package manager 和 lockfile；
5. 运行当前可运行基线命令；
6. 列出当前包要改的文件/模块；
7. 检查当前包验收项。

## 4.3 实施中

- 小步修改；
- 每个逻辑单元立即测试；
- 先领域/接口/测试，再接 UI；
- 数据迁移先 fixture 和备份；
- 安全边界 fail closed；
- 不降低现有功能以让测试通过；
- 不删除未知文件；
- 不修改用户密钥和本地数据；
- 不访问真实付费 Provider，除非用户明确提供测试授权。

## 4.4 结束前

必须：

1. 运行当前包测试；
2. 运行可用全量 verify；
3. `git diff --check`；
4. 检查 Secret、临时文件、大文件和生成物；
5. 更新 `STATUS.md`；
6. 记录命令和真实结果；
7. 列出未完成/风险；
8. 停止，不自动进入下一包。

---

# 5. Git 安全

- 不执行 `git reset --hard`；
- 不执行 `git clean -fd`；
- 不强制 checkout 覆盖用户文件；
- 不修改 Git 历史；
- 不自动 commit/push，除非用户明确要求；
- 不把 `.env`、密钥、数据库、用户素材和构建产物提交；
- 遇到脏文件先判断是否与当前包相关；
- 只修改当前包必需文件；
- 删除文件前确认其用途和引用；
- 生成 Wails/TypeScript Binding 时遵循仓库已批准策略。

---

# 6. 许可证与 Clean-room

- 保留 Infinite Atelier 的 MIT License、版权和 Notices；
- Toonflow 仅作行为、架构和产品思想参考；
- 禁止复制 Toonflow 源码、Prompt/Skill 原文、品牌、图标、界面文案、素材或识别性实现；
- 不把 Toonflow 添加为代码依赖或 Git 子模块；
- 新增第三方依赖前检查许可证和维护状态；
- 优先 Go 标准库、MIT、BSD、Apache-2.0；
- AGPL/GPL 或限制性依赖必须先获得明确批准；
- 更新 SBOM/THIRD_PARTY_NOTICES 由对应工作包完成。

---

# 7. 架构守则

## 7.1 Source of Truth

- 数据库是事实来源；
- Canvas 是投影；
- Zustand 是临时 UI 状态；
- Memory 不是 Story Graph；
- LLM 文本不是 Workflow State；
- Provider 原始响应不是 Domain Entity。

## 7.2 依赖方向

```text
Domain
↑
Application
↑
Ports
↑
Infrastructure/Desktop
↑
React Binding Client
```

Domain 不得导入 Wails、SQLite、HTTP、Provider SDK 或 OS API。

## 7.3 不做微服务

MVP 是单进程本地桌面应用：

- 不引入 Kafka；
- 不引入 Redis；
- 不引入 Kubernetes；
- 不为未来 SaaS 提前拆微服务；
- 通过 Ports 和包边界保留未来迁移能力。

## 7.4 不创建 God Component/Package

- React 页面职责拆分；
- Go package 按业务边界；
- 不创建通用 `utils` 垃圾包；
- 共享逻辑必须有清晰语义；
- 接口定义在使用方或 Ports；
- 避免巨型 Service。

---

# 8. Go 规范

## 8.1 一般

- 以标准库为先；
- `gofmt`；
- 导出符号有注释；
- 函数保持小且单一职责；
- 避免全局可变状态；
- 依赖通过构造函数注入；
- 不使用 panic 处理普通错误；
- 不忽略 error；
- 不用 `context.Background()` 替代调用链 Context；
- 所有 I/O、Provider、Job、Agent 方法接收 `context.Context`；
- 长任务响应取消；
- goroutine 有明确所有者和退出方式；
- channel 只由发送方/所有者关闭；
- 并发必须通过 race-aware 测试。

## 8.2 Error

- 使用稳定错误码 + 包装；
- `errors.Is/As`；
- 不把底层 Secret/URL/Header 泄露到用户错误；
- 错误区分 retriable；
- UI 显示 safe message 和 diagnostic ID；
- 不能只返回 `fmt.Errorf("failed")`。

建议：

```go
type AppError struct {
    Code       string
    Category   string
    Retriable  bool
    SafeMessage string
    Cause      error
}
```

具体实现可调整，但行为必须保持。

## 8.3 Database

- `database/sql`；
- 参数化 SQL；
- Repository；
- SQL 只在 Infrastructure；
- 事务不包外部网络；
- foreign keys；
- WAL；
- migration 文件不可修改已发布内容；
- Update 使用 revision；
- 批量写入事务；
- 查询有 Context；
- 关闭 Rows；
- 检查 `rows.Err()`；
- 时间/ID 一致；
- 关键约束同时在 Domain 和 DB。

## 8.4 File

- 只通过 FileStore；
- 不把用户文件名作为最终路径；
- 临时写入、哈希、验证、原子提交；
- 不返回任意绝对路径给 Agent；
- 防路径穿越、符号链接和设备路径；
- 大文件流式处理；
- 不把媒体读入无限内存。

## 8.5 HTTP

- 使用受控 Client；
- timeout；
- context；
- response body 限制；
- URL/SSRF；
- TLS；
- 重定向复检；
- Header allowlist；
- 日志脱敏；
- 不使用默认全局 Client 访问自定义 Provider。

## 8.6 Tests

- 默认使用 Go `testing`；
- 表驱动测试；
- 临时目录；
- deterministic clock/ID；
- httptest；
- Mock 只在测试；
- 关键状态机/安全有 negative tests；
- 数据竞争相关运行 `go test -race`（平台可用时）。

---

# 9. React/TypeScript 规范

- 保留现有技术栈，除非工作包明确批准迁移；
- TypeScript strict；
- 不使用 `any` 逃避契约，确有必要写边界和验证；
- Domain DTO 来自明确 Schema/Binding；
- Zustand 只保存 UI 状态；
- 服务端事实通过 Query/Binding；
- 组件不直接调用 Provider；
- 组件不直接操作 IndexedDB 作为新业务事实；
- 不使用危险 HTML；
- 模型返回/Markdown 需安全渲染；
- 新文案使用 i18n；
- 状态不能只靠颜色；
- 大列表虚拟化；
- 事件订阅清理；
- Abort/cancel 贯穿；
- 可访问键盘和焦点；
- 现有视觉风格优先，不引入另一套割裂 UI。

## 9.1 Canvas

- 不继续扩大单个 Project 页面；
- 拆分 viewport/selection/drag/edge/history/generation/projection；
- normalized node map；
- 局部 selector；
- 视口裁剪；
- 领域编辑发送命令；
- 删除投影和删除实体区分；
- Semantic Edge 先校验关系。

## 9.2 Frontend Tests

- 使用仓库现有测试框架；
- 若没有，在对应工作包按 PRD 引入 Vitest/RTL/Playwright；
- 不仅快照测试；
- 测试用户行为、事件、错误和可访问性；
- Mock Binding 边界，不 Mock 组件内部实现。

---

# 10. Agent 规范

- Decision、Execution、Supervision 独立调用；
- 每个 Agent 有 Skill Version；
- Tool 白名单；
- Tool 输入/输出 Schema；
- Supervisor 默认只读；
- 结构化输出；
- 一次 Schema Repair；
- 最大 Tool/Duration；
- 最大自动修订 2；
- Agent 不读 Secret；
- Agent 不执行 SQL/File/Network/Shell 原语；
- 不可信内容明确隔离；
- 业务成功以数据库读回验证；
- 不要求/记录私有 Chain of Thought，只记录 reason summary 和可审计动作；
- CI 使用 deterministic Mock LLM。

---

# 11. Security 规范

完整遵循 `docs/SECURITY.md`。

特别禁止：

- `new Function`；
- `eval`；
- 任意模型脚本；
- API Key 进入前端；
- `0.0.0.0` 开放本地开发代理；
- 未校验的自定义 URL；
- Zip Slip；
- Shell 命令字符串拼接；
- Supervisor 写权限；
- 普通备份包含 Secret；
- 日志打印 Request/Header/Key；
- 为兼容性关闭 TLS 校验；
- 安全失败自动绕过。

---

# 12. 测试与完成定义

每个实现对应 `docs/ACCEPTANCE.md`。

禁止：

- 只编译不测试；
- 只创建接口无真实路径；
- 用静态 JSON 假装 Agent；
- 把 Mock 放生产路径；
- 跳过负面测试；
- 删除失败测试；
- 通过放宽断言掩盖缺陷；
- 因环境失败宣称功能完成。

工作包 DoD 见 PRD。

---

# 13. 文档更新

代码变更需要同步：

- `STATUS.md`；
- ADR；
- Schema；
- migration；
- API/Binding；
- testdata；
- user docs；
- license notices。

文档只写已实现事实；未来计划留在 Roadmap。

---

# 14. 不确定性处理

遇到不明确问题：

1. 先检查 PRD/Architecture/Domain/ADR；
2. 检查代码和测试；
3. 选择最小、安全、可逆、兼容现有行为的方案；
4. 写入 ADR/STATUS；
5. 不因不明确而扩张范围；
6. 当前包可继续的部分继续完成；
7. 真正阻断的决策清楚列出，不虚构答案。

---

# 15. Codex 最终报告格式

每个工作包结束只报告：

```markdown
## 工作包
WP-XX — 名称

## 状态
COMPLETE / PARTIAL / BLOCKED

## 已完成
- ...

## 修改文件
- path: purpose

## 验证
- `command` — PASS/FAIL + key result

## 验收
- AC-... — PASS/FAIL/BLOCKED

## 未完成与风险
- ...

## Git/数据安全
- existing user changes preserved: yes/no
- secrets found/introduced: none/details
- migrations/backups: details

## 下一步
推荐 WP-XX，但未开始。等待用户明确指令。
```

不要在报告后自动继续编码。


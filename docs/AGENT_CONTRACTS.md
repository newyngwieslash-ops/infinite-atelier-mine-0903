# Infinite Atelier Drama Studio Agent 契约

> 版本：v1.0  
> 状态：已批准  
> 适用：Decision、Execution、Supervision、Skill、Tool、Memory、Evaluation

---

# 1. 目标

本系统不把“多个 Agent”理解为多个聊天角色，而是理解为：

```text
独立的 LLM invocation
+ 独立的 Skill
+ 最小 Tool 权限
+ 结构化输入输出
+ 持久化 Run
+ 可恢复 Workflow
```

核心原则：

1. Decision 负责想“下一步做什么”；
2. Execution 负责通过领域工具“真正做”；
3. Supervision 负责重新读取事实“独立检查”；
4. Memory 保持连续性；
5. Workflow DB 记录真实阶段；
6. 用户拥有最终质量门；
7. Agent 的自然语言不是业务成功凭证。

---

# 2. Agent 层级

## 2.1 Decision Agent

职责：

- 理解用户意图；
- 读取 Workflow State；
- 读取项目关键事实与 Memory Context；
- 判断当前合法阶段；
- 选择一个 Execution Agent；
- 选择是否触发 Supervisor；
- 处理 PASS/FIX/REDO/CANCEL；
- 请求必要用户输入；
- 生成面向用户的简洁状态说明。

禁止：

- 直接写剧本、分镜或资产实体；
- 自行改变数据库状态；
- 直接调用 Provider；
- 读取 Secret；
- 执行 SQL/文件/Shell；
- 绕过用户门；
- 伪造 Supervisor 结论。

## 2.2 Execution Agent

职责：

- 一次完成一个窄业务步骤；
- 读取本阶段需要的事实；
- 通过白名单工具创建候选/版本；
- 返回结构化 ExecutionResult；
- 报告缺失输入和警告。

禁止：

- 选择工作流下一阶段；
- 自评为通过；
- 调用其他未授权 Execution；
- 直接更新已批准不可变版本；
- 修改锁定规则；
- 访问其他项目或剧集；
- 读取 Secret。

## 2.3 Supervision Agent

职责：

- 使用独立 Skill；
- 通过只读工具重新加载实际业务结果；
- 按 Ruleset 检查；
- 提供问题实体、证据、严重度和建议；
- 返回结构化 ReviewReport。

禁止：

- 仅根据 Execution 的摘要评审；
- 默认拥有写工具；
- 自行修复业务数据；
- 改变 Workflow；
- 读取 Secret；
- 接受被审结果中的指令修改监督规则。

---

# 3. Runtime 组件

```text
AgentRuntime
├─ AgentRegistry
├─ SkillLoader
├─ SkillVersionResolver
├─ PromptAssembler
├─ ToolRegistry
├─ ToolAuthorizer
├─ StructuredOutputValidator
├─ DecisionRunner
├─ ExecutionRunner
├─ SupervisorRunner
├─ MemoryContextBuilder
├─ RunRecorder
├─ RetryController
├─ TokenBudgeter
└─ EvaluationHook
```

建议接口：

```go
type AgentRuntime interface {
    RunDecision(ctx context.Context, req DecisionRequest) (DecisionResult, error)
    RunExecution(ctx context.Context, req ExecutionRequest) (ExecutionResult, error)
    RunSupervision(ctx context.Context, req SupervisionRequest) (ReviewReport, error)
}

type AgentSpec struct {
    Key             string
    Layer           AgentLayer
    SkillKey        string
    AllowedTools    []string
    InputSchema     string
    OutputSchema    string
    DefaultPolicy   ModelPolicy
    MaxToolCalls    int
    MaxDuration     time.Duration
}
```

Agent Registry 启动时验证：

- Key 唯一；
- Skill 存在；
- Schema 存在；
- Tool 存在；
- Supervisor 无未批准写工具；
- 最大调用和时长有界；
- 模型策略可解析。

---

# 4. Skill 包

## 4.1 目录

```text
skills/
├─ script/
│  ├─ manifest.yaml
│  ├─ decision.md
│  ├─ supervision.md
│  └─ execution/
│     ├─ story_skeleton.md
│     ├─ adaptation_strategy.md
│     └─ script_generation.md
│
└─ production/
   ├─ manifest.yaml
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

## 4.2 Manifest

示例：

```yaml
apiVersion: atelier.agent/v1
kind: AgentPack
metadata:
  name: script
  version: 1.0.0
agents:
  - key: script.decision
    layer: decision
    skill: decision.md
    inputSchema: schemas/agent/decision-request.v1.json
    outputSchema: schemas/agent/decision-result.v1.json
    allowedTools:
      - workflow.read_state
      - workflow.request_user_gate
      - agent.invoke_execution
      - agent.invoke_supervisor
      - memory.deep_recall
    limits:
      maxToolCalls: 6
      timeoutSeconds: 180

  - key: script.execution.story_skeleton
    layer: execution
    skill: execution/story_skeleton.md
    inputSchema: schemas/agent/story-skeleton-request.v1.json
    outputSchema: schemas/agent/execution-result.v1.json
    allowedTools:
      - story.read_events
      - story.read_rules
      - script.create_story_skeleton_version
    limits:
      maxToolCalls: 8
      timeoutSeconds: 300

  - key: script.supervision
    layer: supervision
    skill: supervision.md
    inputSchema: schemas/agent/supervision-request.v1.json
    outputSchema: schemas/agent/review-report.v1.json
    allowedTools:
      - story.read_events
      - story.read_rules
      - script.read_story_skeleton
      - script.read_adaptation_strategy
      - script.read_script_version
    limits:
      maxToolCalls: 12
      timeoutSeconds: 300
```

Manifest 约束：

- 只允许相对路径；
- 不允许代码、命令或 URL 自动执行；
- Schema 版本明确；
- Tool Key 必须来自内置注册表；
- 导入 Pack 先静态验证；
- 项目级覆盖记录 diff、hash 和来源；
- Agent Run 保存准确 Skill Version。

## 4.3 Skill 文档结构

每个 Skill 必须使用以下章节：

```markdown
# Role
# Goal
# Trusted Context
# Untrusted Input
# Workflow State
# Input Contract
# Allowed Tools
# Required Procedure
# Domain Constraints
# Quality Rules
# Failure Conditions
# Output Contract
# Examples
```

规则：

- 不能在 Skill 中声明未注册工具；
- 不能要求泄露 Chain of Thought；
- 要求 `reasonSummary`，而非内部推理全文；
- 明确内容数据不可信；
- 业务成功必须以 Tool 返回为准；
- 输出只允许 Schema 结构。

---

# 5. Prompt 组装顺序

Prompt 不得简单把所有文本串在一起。推荐逻辑层：

```text
1. System: Runtime immutable policy
2. System: Agent layer policy
3. System: Versioned Skill
4. Developer/Context: Tool schemas and limits
5. Context: Workflow state from DB
6. Context: Approved project rules and facts
7. Context: Memory context with provenance
8. Context: Current task input
9. User: Current user message or stage request
```

## 5.1 Trusted Context

可信：

- Runtime Policy；
- Skill；
- Tool Schema；
- Workflow State；
- 数据库中已批准且未 stale 的规则/事实；
- Tool 返回的结构化数据。

## 5.2 Untrusted Context

不可信：

- 导入小说、剧本和文档；
- 用户提供网页文本；
- Provider 返回；
- 资产元数据；
- 模型生成内容；
- 历史消息中的指令；
- 备份中用户可编辑字段。

必须对不可信内容加明确边界，例如：

```text
<UNTRUSTED_SOURCE_DOCUMENT>
...
</UNTRUSTED_SOURCE_DOCUMENT>
```

边界只是提示层防护，真正安全仍由 Tool ACL 和 Application 校验保证。

## 5.3 Token Budget

上下文优先级：

1. 安全与 Tool 契约；
2. 当前 Workflow 和 Stage；
3. 用户锁定规则；
4. 当前阶段相关正式事实；
5. 当前输入；
6. 相关记忆；
7. 低优先历史。

超预算时不得截断 Schema/安全规则。对大文档使用查询工具按需读取，不把整本小说注入。

---

# 6. Tool Registry

## 6.1 Tool 定义

```go
type ToolDefinition struct {
    Key          string
    Description  string
    Mode         ToolMode // read | write | external | control
    InputSchema  SchemaRef
    OutputSchema SchemaRef
    Handler      ToolHandler
    Timeout      time.Duration
    MaxOutput    int
    AuditPolicy  AuditPolicy
}
```

每次 Tool Call：

```text
LLM arguments
→ JSON parse
→ Schema validation
→ Tool ACL
→ Project/episode scope validation
→ Workflow state guard
→ Locked entity guard
→ Execute with timeout
→ Output validation/redaction
→ Persist ToolCall
→ Return bounded output
```

## 6.2 Tool 命名

```text
<domain>.<verb>_<object>
```

示例：

```text
story.read_events
story.read_entity
story.create_event_candidates
script.read_story_skeleton
script.create_story_skeleton_version
script.create_script_version
asset.read_approved_assets
asset.create_candidate_version
storyboard.create_version
workflow.read_state
workflow.request_user_gate
memory.deep_recall
provider.submit_image_job
```

## 6.3 权限矩阵

| 层 | Read | Write | External | Control |
|---|---:|---:|---:|---:|
| Decision | 少量高层读取 | 否 | 否 | 调用子 Agent、请求用户门 |
| Execution | 阶段相关 | 阶段相关候选写入 | 仅通过任务工具 | 否 |
| Supervision | 阶段相关只读 | 默认否 | 否 | 否 |

## 6.4 Tool 输出

- 返回结构化、限长数据；
- 大文本返回 FileRef、EntityRef、摘要和分页 Token；
- 不返回 Secret；
- 不返回原始 SQL；
- 不返回任意本地绝对路径；
- 所有 EntityRef 含类型、ID、版本和状态；
- 返回 stale/locked 状态。

---

# 7. 请求与输出 Schema

## 7.1 DecisionRequest

```json
{
  "schemaVersion": 1,
  "projectId": "string",
  "episodeId": "string|null",
  "workflowRunId": "string|null",
  "userMessage": "string",
  "requestedAction": "string|null",
  "currentState": {
    "workflowType": "string|null",
    "stage": "string|null",
    "status": "string|null",
    "attempt": 0,
    "pendingUserGate": false
  },
  "availableActions": [],
  "contextRefs": []
}
```

## 7.2 DecisionResult

```json
{
  "schemaVersion": 1,
  "status": "respond|execute|review|wait_user|stop|error",
  "workflowRunId": "string|null",
  "currentStage": "string|null",
  "intent": "string",
  "reasonSummary": "string",
  "nextAction": {
    "type": "run_execution|run_supervision|request_approval|request_input|finish|none",
    "agentKey": "string|null",
    "stage": "string|null",
    "input": {}
  },
  "userMessage": "string",
  "warnings": []
}
```

Validation：

- `execute` 必须有 execution agentKey；
- `review` 必须有 supervision agentKey；
- 请求的 Stage 必须是数据库允许下一步；
- `reasonSummary` 只需可审计摘要；
- Decision 不能返回业务 artifacts。

## 7.3 ExecutionRequest

```json
{
  "schemaVersion": 1,
  "projectId": "string",
  "episodeId": "string|null",
  "workflowRunId": "string",
  "stageRunId": "string",
  "stage": "string",
  "task": "string",
  "inputRefs": [],
  "lockedRefs": [],
  "fixIssueIds": [],
  "idempotencyKey": "string"
}
```

## 7.4 ExecutionResult

```json
{
  "schemaVersion": 1,
  "status": "success|partial|failed|cancelled",
  "stage": "string",
  "stageRunId": "string",
  "artifacts": [
    {
      "entityType": "string",
      "entityId": "string",
      "versionId": "string|null",
      "operation": "created|updated|superseded"
    }
  ],
  "warnings": [],
  "errors": [],
  "nextAction": "review|wait_user|retry|stop",
  "summary": "string"
}
```

Validation：

- Artifact 必须在数据库存在；
- Artifact 来源包含该 StageRun/AgentRun；
- `success` 至少有预期 artifact，除非该阶段明确是 no-op；
- `partial` 不得自动进入通过；
- `failed` 不得附带 approved artifact。

## 7.5 SupervisionRequest

```json
{
  "schemaVersion": 1,
  "projectId": "string",
  "episodeId": "string|null",
  "workflowRunId": "string",
  "stageRunId": "string",
  "stage": "string",
  "rulesetVersion": "string",
  "artifactRefs": [],
  "requiredChecks": []
}
```

## 7.6 ReviewReport

```json
{
  "schemaVersion": 1,
  "passed": false,
  "score": 76,
  "grade": "A|B|C|D",
  "severity": "none|minor|major|critical",
  "stage": "string",
  "stageRunId": "string",
  "rulesetVersion": "string",
  "issues": [
    {
      "rule": "CHARACTER_CONSISTENCY",
      "severity": "major",
      "entityType": "shot",
      "entityId": "shot_013",
      "location": "episode_1/scene_4/shot_3",
      "evidence": [
        {
          "type": "entity_ref",
          "ref": "asset_version:character_v4"
        }
      ],
      "problem": "角色服装与上一镜头不一致",
      "suggestion": "改为引用已批准服装版本"
    }
  ],
  "summary": "string",
  "recommendedAction": "pass|fix|redo|manual_review"
}
```

Validation：

- `passed=true` 时 severity 不得为 major/critical；
- critical 强制人工门；
- issue entity 必须存在或 location 明确；
- evidence 只引用实际读取结果；
- Supervisor 不返回修改后的 artifact。

## 7.7 AgentError

```json
{
  "schemaVersion": 1,
  "code": "agent.output_schema_invalid",
  "category": "configuration|input|model|tool|timeout|cancelled|security|storage|internal",
  "retriable": false,
  "safeMessage": "模型返回格式不符合本阶段要求",
  "detailsRef": "diagnostic:...|null",
  "stageRunId": "string|null",
  "agentRunId": "string|null"
}
```

---

# 8. 一次完整调用链

```text
User Action
→ Application validates command
→ Workflow loads state
→ Memory recalls previous context
→ Persist current user message
→ Decision Agent
   ├─ direct response, or
   ├─ invoke Execution
   │  → Execution Tool Calls
   │  → transactional artifact write
   │  → ExecutionResult validation
   └─ invoke Supervisor
      → reload actual workspace
      → ReviewReport validation
→ Persist Agent Runs and Memory
→ Workflow enters waiting_user/passed/failed
→ UI event
```

伪代码：

```go
func (s *AgentService) HandleTurn(ctx context.Context, cmd HandleTurnCommand) (DecisionResult, error) {
    state, err := s.workflows.Load(ctx, cmd.WorkflowRunID)
    if err != nil { return DecisionResult{}, err }

    mem, err := s.memory.BuildContext(ctx, MemoryQuery{
        Scope: s.scopeResolver.Resolve(cmd),
        Query: cmd.UserMessage,
        ExcludeCurrentTurn: true,
    })
    if err != nil { return DecisionResult{}, err }

    if err := s.memory.AddUserMessage(ctx, cmd); err != nil {
        return DecisionResult{}, err
    }

    result, err := s.runtime.RunDecision(ctx, DecisionRequest{
        ProjectID: cmd.ProjectID,
        WorkflowRunID: cmd.WorkflowRunID,
        UserMessage: cmd.UserMessage,
        CurrentState: state,
        MemoryContext: mem,
    })
    if err != nil { return DecisionResult{}, err }

    return s.applyDecision(ctx, state, result)
}
```

实现可以不同，但顺序和边界不得倒置。

---

# 9. Workflow 与 Agent 的职责分界

Workflow Engine 决定：

- 当前真实阶段；
- 合法状态转移；
- StageRun Attempt；
- 最大重试；
- 是否需要用户门；
- 哪些版本是上游输入；
- stale；
- 恢复。

Decision Agent 决定：

- 用户当前意图；
- 在合法动作中选择哪个；
- 如何组织 Execution 输入；
- 是否需要更多用户信息；
- 如何向用户解释结果。

Decision 不能提出非法动作。Runtime 必须再次校验，不因模型输出而改变规则。

---

# 10. Quality Gate

## 10.1 默认配置

```yaml
stages:
  event_extraction:
    supervision: false
    userGate: optional

  story_skeleton:
    supervision: true
    userGate: required
    maxAutoFix: 2

  adaptation_strategy:
    supervision: true
    userGate: required
    maxAutoFix: 2

  script_writing:
    supervision: true
    userGate: required
    maxAutoFix: 2

  director_plan:
    supervision: conditional
    userGate: required

  asset_analysis:
    supervision: false
    userGate: required

  asset_generation:
    supervision: conditional
    userGate: required

  storyboard_table:
    supervision: true
    userGate: required
    maxAutoFix: 2

  storyboard_panel:
    supervision: conditional
    userGate: optional

  storyboard_image:
    supervision: conditional
    userGate: required

  final_review:
    supervision: true
    userGate: required
```

## 10.2 PASS/FIX/REDO

PASS：

- 用户批准当前候选；
- 标记 approved；
- 旧 approved superseded；
- 进入下一阶段。

FIX：

- 指定 Review Issue；
- 保留未受影响与锁定内容；
- 创建新 Attempt/Version；
- 重新监督。

REDO：

- 从同一上游重新生成；
- 创建新 Attempt/Version；
- 旧候选保留；
- 重新监督。

MANUAL_EDIT：

- 用户在 UI 修改；
- 创建用户版本；
- 关键阶段重新监督或人工批准。

---

# 11. Supervision 规则

## 11.1 Script Ruleset

至少检查：

- 原著事实与改编标记；
- 开场 Hook；
- 核心冲突；
- 情绪节奏；
- 转折和结尾悬念；
- 单集时长；
- 角色动机；
- 场次可生产性；
- 对白自然度；
- 锁定规则；
- 内容等级。

## 11.2 Asset Ruleset

- 角色身份和外观；
- 服装状态；
- 场景时间/天气；
- 道具所有权与状态；
- Approved Version；
- 派生关系；
- 文件存在和类型；
- 引用范围。

## 11.3 Storyboard Ruleset

- 剧本覆盖；
- Shot 顺序；
- 景别和镜头描述具体性；
- 角色/场景/道具引用；
- 服装/伤势/道具连续性；
- 轴线和相邻镜头问题；
- 时长总和；
- 首尾帧关系；
- Prompt 与 Shot 一致；
- 缺失镜头。

## 11.4 Final Ruleset

- 所有必需 Shot 有批准视频；
- 音频和字幕完整；
- 媒体文件存在；
- stale/waiver；
- 黑帧/空帧/静音异常；
- 总时长；
- 资源许可证元数据；
- 导出参数。

硬规则应尽量用确定性代码先检查，LLM Supervisor 负责语义质量。ReviewReport 合并两类证据，并标记 `source=deterministic|llm`。

---

# 12. Memory 契约

## 12.1 BuildContext

输入：

```json
{
  "scope": {
    "projectId": "...",
    "episodeId": "...|null",
    "agentKey": "...",
    "sessionId": "...|null"
  },
  "query": "...",
  "tokenBudget": 6000,
  "excludeMemoryIds": []
}
```

输出：

```json
{
  "recent": [],
  "summaries": [],
  "semantic": [],
  "facts": [],
  "usedTokens": 0,
  "truncated": false,
  "provenance": []
}
```

## 12.2 自动召回

候选：

- Recent 未摘要消息；
- Recent Summary；
- Message Vector；
- Summary Vector；
- 高重要性 locked memory；
- 当前阶段相关 Artifact Memory。

建议分数：

```text
0.55 semantic
+ 0.20 recency
+ 0.15 importance
+ 0.10 agent role
```

要求：

- Scope filter 在评分前；
- Threshold + TopK；
- 去重；
- 当前消息排除；
- 来源保留；
- Token Budget；
- Project Rule/Event Graph 通过结构化事实通道注入，不混作 Memory。

## 12.3 Deep Recall Tool

输入：

```json
{
  "query": "之前为什么禁止角色穿红色服装？",
  "scope": "current_project_and_episode",
  "maxSummaries": 12,
  "maxRawMessages": 30
}
```

流程：

```text
Summary vector candidates
→ threshold
→ LLM/algorithm rerank
→ selected summary IDs
→ source memory IDs
→ load original messages/artifact refs
→ bounded result with provenance
```

Decision 只在明确历史需求时调用。

## 12.4 Memory 写入

写入来源：

- 用户消息；
- Decision 摘要；
- Execution 结果摘要；
- Supervisor 结论；
- 用户质量门；
- 用户明确保存的偏好；
- 关键项目决策。

不要把以下内容自动当作高置信事实：

- 未批准模型候选；
- Supervisor 建议；
- Agent 推测；
- 被拒绝版本；
- Provider 错误文本。

---

# 13. 模型策略

不同层可使用不同模型：

```text
Decision: 低延迟、可靠 Tool Calling
Execution: 按任务选择上下文/创作能力
Supervision: 独立模型或不同配置，避免同源偏差
Embedding: 本地或独立 Provider
```

策略对象：

```json
{
  "primaryModelId": "...",
  "fallbackModelIds": [],
  "temperature": 0.2,
  "reasoningEffort": "medium",
  "maxOutputTokens": 4000,
  "timeoutSeconds": 180,
  "maxCost": null
}
```

规则：

- Fallback 不能绕过数据发送授权；
- Supervisor 可配置不同 Provider；
- 模型变更写入 Run；
- 低成本阶段不默认使用最昂贵模型；
- 批量前显示模型和预计数量。

---

# 14. 重试与错误

## 14.1 可重试

- 网络瞬断；
- 429，遵守 Retry-After；
- 5xx 短暂错误；
- 流中断且 Provider 支持安全恢复；
- 输出 Schema 无效的一次结构修复。

## 14.2 不自动重试

- 401/403；
- 内容策略；
- 输入无效；
- Tool 权限拒绝；
- 锁定规则冲突；
- 成本超限；
- 未知是否已计费且无远程 ID；
- 安全错误。

## 14.3 Schema Repair

```text
Raw output invalid
→ no business write
→ send compact validation errors to same Agent once
→ validate again
→ if invalid: fail StageRun
```

禁止在应用端“猜测修复”关键字段或把自由文本塞进 JSON。

---

# 15. 取消

- UI 取消发送 Context Cancel；
- Runtime 停止流和后续 Tool；
- 正在执行的事务安全回滚；
- 已提交实体保留并标记来源；
- 远程任务交给 Job Manager 取消；
- StageRun 标记 cancelled；
- 取消不自动删除候选资产。

---

# 16. Run 记录与可追溯性

每次 Run 保存：

- Agent layer/key；
- Skill version/hash；
- 模型和 Provider；
- Workflow/Stage；
- 输入引用；
- Memory IDs；
- Tool Calls；
- 验证后输出；
- raw output 安全引用；
- Token/费用；
- 时间；
- error；
- trace ID。

UI 不要求显示内部 Chain of Thought，只显示：

- reasonSummary；
- 选择了什么动作；
- 使用了哪些事实/记忆；
- 调用了哪些工具；
- 产生了哪些版本；
- 审核发现什么。

---

# 17. 确定性优先

以下应由代码实现，不应交给 LLM：

- ID、顺序和唯一性；
- 工作流状态转换；
- 权限；
- Secret；
- 文件校验；
- 时长求和；
- 引用存在性；
- Approved Version 唯一；
- stale 图传播；
- Job 幂等；
- 重试次数；
- JSON Schema；
- 资产文件哈希；
- 章节 offset；
- 明显缺失项。

LLM 适合：

- 故事理解；
- 改编策略；
- 剧本创作；
- 镜头语言；
- 语义一致性；
- 质量建议；
- 历史相关性判断。

---

# 18. Evaluation

## 18.1 测试集

`testdata/canary-drama/` 至少包含：

- 章节文本；
- 已批准事实；
- 期望故事骨架关键点；
- 故意错误剧本；
- 故意错误资产引用；
- 分镜连续性错误；
- Memory recall 问题；
- Prompt Injection 文本。

## 18.2 指标

- Schema 首次通过率；
- Tool 选择正确率；
- 非法 Tool 请求率；
- Stage 跳步率；
- Supervisor 定位率；
- Memory 跨项目泄露率；
- Deep Recall 命中率；
- 事实引用准确率；
- 自动修复成功率；
- 平均成本/延迟。

## 18.3 Deterministic Mock

CI 不调用真实付费模型。Mock 根据输入场景返回：

- 正常结构；
- 一次无效结构后二次有效；
- Tool Call；
- 拒绝非法 Tool；
- Supervisor issues；
- 超时；
- 取消；
- Provider 错误。

真实模型评测是手动/受控测试，不是合并 PR 的必需网络条件。

---

# 19. 初始 Agent 清单

## Script Pack

```text
script.decision
script.execution.event_extraction
script.execution.story_skeleton
script.execution.adaptation_strategy
script.execution.script_generation
script.supervision.story_skeleton
script.supervision.adaptation_strategy
script.supervision.script
```

## Production Pack

```text
production.decision
production.execution.director_plan
production.execution.asset_analysis
production.execution.asset_generation_plan
production.execution.storyboard_table
production.execution.storyboard_panel
production.supervision.director_plan
production.supervision.storyboard_table
production.supervision.storyboard_panel
```

媒体生成本身由 Job/Provider Service 执行，不让 LLM 阻塞等待大文件。

---

# 20. 发布阻断条件

任一项存在则 Agent 系统不可标记完成：

- Decision 可直接使用业务写 Tool；
- Supervisor 可默认写入；
- Tool 参数未 Schema 校验；
- Agent 可读取 Secret；
- Agent 可执行任意网络/Shell/JS；
- Workflow 状态只在 Prompt；
- Execution 自述被当作成功；
- 当前消息召回自身；
- Memory 可跨项目污染；
- Skill 无版本和 Hash；
- 输出无 Schema；
- 无限自动 FIX/REDO；
- CI 依赖真实付费模型；
- 未保留 Run/Tool/Review 追踪。


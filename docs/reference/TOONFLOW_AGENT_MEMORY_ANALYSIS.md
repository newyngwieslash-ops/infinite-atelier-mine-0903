# Toonflow 三层 Agent 协作与持久化记忆系统复现技术文档

## 1. 目标与结论

Toonflow 的核心 Agent 架构可以抽象为两个互相独立但共享基础设施的系统：

```text
┌─────────────────────────────────────────────┐
│                User / UI                    │
└───────────────────┬─────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────────┐
│ Decision Agent                              │
│                                             │
│ - 理解用户意图                              │
│ - 判断当前工作阶段                          │
│ - 拆解任务                                  │
│ - 选择 Execution Agent                      │
│ - 决定何时触发 Supervision                  │
│ - 处理修改 / 重做 / 下一阶段                │
└───────────────┬─────────────────────────────┘
                │ Tool Calling
        ┌───────┴──────────┐
        ▼                  ▼
┌─────────────────┐  ┌──────────────────────┐
│ Execution Agent │  │ Supervision Agent    │
│                 │  │                      │
│ 专门执行一个任务│  │ 独立读取实际工作成果 │
│ 写入业务状态    │  │ 质量评分 / 找问题    │
│ 调用领域工具    │  │ 给出修改建议         │
└────────┬────────┘  └──────────┬───────────┘
         │                       │
         └───────────┬───────────┘
                     ▼
┌─────────────────────────────────────────────┐
│ Shared Persistent Memory                   │
│                                             │
│ SQLite                                      │
│ ├─ short-term messages                     │
│ ├─ long-term summaries                     │
│ └─ embeddings                              │
│                                             │
│ local ONNX all-MiniLM-L6-v2                │
└─────────────────────────────────────────────┘
```

它并没有引入 LangGraph、AutoGen、CrewAI 之类的多 Agent 框架，而是主要依赖：

- Vercel AI SDK / Tool Calling
- Markdown Skill Prompt
- SQLite
- 本地 ONNX Embedding
- 一个轻量的 `Memory` 类
- 独立的 Decision / Execution / Supervision LLM 调用

官方技术栈也明确是 TypeScript + Express 5 + SQLite/better-sqlite3/knex + Vercel AI SDK + `@huggingface/transformers` 本地 ONNX 推理。

---

# 2. 源码结构

真正值得复制的是下面这些部分。

```text
src/
├─ agents/
│  ├─ scriptAgent/
│  │  └─ index.ts
│  └─ productionAgent/
│     └─ index.ts
│
├─ utils/
│  └─ agent/
│     ├─ memory.ts
│     ├─ embedding.ts
│     └─ skillsTools.ts
│
├─ routes/
│  └─ agents/
│     ├─ getMemory.ts
│     └─ clearMemory.ts
│
└─ types/
   └─ database.d.ts

data/
├─ skills/
│  ├─ script_agent_decision.md
│  ├─ script_agent_supervision.md
│  ├─ script_execution_*.md
│  │
│  ├─ production_agent_decision.md
│  ├─ production_agent_supervision.md
│  └─ production_execution_*.md
│
└─ models/
   └─ all-MiniLM-L6-v2/
      └─ onnx/
         └─ model_fp16.onnx
```

仓库实际将 ScriptAgent 与 ProductionAgent 分离，同时将它们的决策、执行、监督 Prompt 全部文件化。

这点非常重要：

> **Agent 的“能力”不是硬编码在 Agent 类里面，而是 `Agent Runtime + Skill Prompt + Tools` 的组合。**

因此换一个业务项目时，大部分运行时完全可以保持不动，仅替换 Skill 和 Tool。

---

# 3. 三层 Agent 的真实实现

## 3.1 第一层：Decision Agent

Decision Agent 是整个系统的控制器。

它承担：

```text
用户请求
   ↓
识别意图
   ↓
识别工作流阶段
   ↓
选择执行 Agent
   ↓
执行
   ↓
判断是否需要监督
   ↓
监督
   ↓
修改 / 重做 / 下一阶段
```

ProductionAgent 的主入口逻辑大致等价于：

```ts
async function runDecisionAI(ctx) {
    const memory = new Memory(
        "productionAgent",
        isolationKey,
    );

    await memory.add("user", ctx.text);

    const memoryContext =
        await memory.get(ctx.text);

    const decisionSkill =
        await loadSkill(
            "production_agent_decision.md",
        );

    const result = LLM.stream({
        key: "productionAgent:decisionAgent",

        system: decisionSkill,

        messages: [
            {
                role: "assistant",
                content:
                    projectContext +
                    buildMemoryPrompt(memoryContext),
            },
            {
                role: "user",
                content: ctx.text,
            },
        ],

        tools: {
            ...memory.getTools(),
            ...domainTools,
            ...executionAgents,
            ...supervisionAgent,
        },
    });

    onFinish(async output => {
        await memory.add(
            "assistant:decision",
            output,
        );
    });
}
```

源码实际上就是这一模式：先写用户消息，再取 Memory，然后把 Memory、模型信息等放进 Decision Agent 上下文，同时向它暴露 memory tools、业务 tools 和 sub-agent tools；Decision Agent 完成后又把自己的输出写回 Memory。

因此：

**Decision Agent 并不是直接完成所有业务，而是在做 Orchestration。**

---

# 4. Decision Prompt 实际上承担了“状态机”

Toonflow 有一个很有意思的设计：

它没有把完整工作流写成传统的：

```ts
switch(stage) {
    case STORY:
    case REVIEW:
    ...
}
```

而是把相当大一部分工作流规则写进了 Decision Skill。

例如 ScriptAgent 的工作流被定义成：

```text
Stage 1
故事骨架

   ↓

Stage 2
改编策略

   ↓

Stage 3
剧本生成
```

一个阶段内部则遵循：

```text
Decision
   │
   ▼
Execution
   │
   ├──失败────→ STOP
   │
   ▼
Supervision
   │
   ▼
展示审阅结果
   │
   ▼
USER GATE
   │
   ├─ PASS ────→ NEXT
   │
   ├─ FIX ─────→ Execution → Supervision
   │
   └─ REDO ────→ Execution → Supervision
```

ScriptAgent 的 Decision Skill 明确规定：

1. Decision 判断当前阶段；
2. 调用对应 Execution Agent；
3. Execution 失败则停止；
4. Execution 成功后调用 Supervision；
5. 返回生成结果和审核结果；
6. 用户决定通过、修改还是重做。

并且阶段 1、2 按串行方式推进。

这其实是一种：

> **Prompt-defined State Machine**

---

# 5. ProductionAgent 如何实现复杂工作流

ProductionAgent 的 Decision Skill 将视频生产拆成六个阶段：

```text
1. Director Planning
        ↓
2. Derived Asset Analysis
        ↓
3. Derived Asset Generation
        ↓
4. Storyboard Table
        ↓
5. Storyboard Panel
        ↓
6. Storyboard Image Generation
```

其中一些步骤还是可选步骤。

源码里的 Execution Agents 则进一步拆成：

```text
run_sub_agent_director_plan

run_sub_agent_derive_assets

run_sub_agent_generate_assets

run_sub_agent_storyboard_table

run_sub_agent_storyboard_panel

run_sub_agent_storyboard_gen
```

每个 Tool 背后实际重新启动一次专门的 LLM Agent，并加载完全不同的 Skill。

所以不是：

```text
一个 Agent + 一堆提示词
```

而是：

```text
Decision LLM
   ↓ tool call
独立 Execution LLM invocation
   ↓
专用 system prompt
   ↓
专用 tools
```

这是稳定性的核心来源之一。

---

# 6. 第二层：Execution Agent

Execution Agent 的职责非常窄：

> 一个 Agent 尽量只完成一个业务步骤。

例如：

```text
storySkeletonAgent
adaptationStrategyAgent
scriptWritingAgent

directorPlanAgent
deriveAssetsAgent
generateAssetsAgent
storyboardTableAgent
storyboardPanelAgent
storyboardGenerationAgent
```

ScriptAgent 源码分别创建：

```text
assistant:execution:storySkeleton

assistant:execution:adaptationStrategy

assistant:execution:script
```

等不同执行角色，并给它们加载不同 Markdown Skill。

推荐把这个机制抽象成一个通用函数：

```ts
runSubAgent({
    key,
    name,
    systemSkill,
    input,
    tools,
    memoryRole,
});
```

例如：

```ts
await runSubAgent({
    key:
      "productionAgent:directorPlanAgent",

    name:
      "执行导演",

    systemSkill:
      "production_execution_director_plan.md",

    input:
      task,

    tools:
      directorTools,

    memoryRole:
      "assistant:execution",
});
```

这样真正变化的只有：

```text
Skill
Tool Set
Output Contract
Memory Role
```

Agent Runtime 不变化。

---

# 7. 为什么 Execution 层比“一个超级 Agent”稳定

主要原因是 Context Isolation。

如果所有能力都塞给 Decision Agent：

```text
40 个 tools
几十页 prompt
剧本
分镜
资产
导演要求
审核规则
记忆
...
```

LLM 很容易：

```text
Tool selection error
Prompt interference
Hallucinated state
Skipped workflow
```

而 Toonflow 是：

```text
Decision
只有决策规则

       ↓

Storyboard Agent
只有 storyboard skill
+
storyboard tools

       ↓

Supervisor
只有 review skill
+
read tools
```

因此每一个模型调用的“任务熵”都明显降低。

---

# 8. 第三层：Supervision Agent

监督 Agent 并不是简单问一句：

```text
“这个结果好吗？”
```

而是一个**独立角色 + 独立 Prompt + 独立工具环境**。

ProductionAgent 里对应：

```text
productionAgent:supervisionAgent
```

角色名类似：

```text
监制
```

ScriptAgent 里则对应：

```text
scriptAgent:supervisionAgent
```

角色类似：

```text
编辑
```

它们的输出也单独写入：

```text
assistant:supervision
```

共享 Memory。

---

# 9. Supervisor 不应该相信 Execution 的自述

这是 Toonflow 监督层非常值得复制的一点。

Production supervision Skill 明确要求监督者通过工具读取**真实 workspace 内容**，而不是只根据执行 Agent 说：

```text
“我已经完成了……”
```

就判定成功。

其质量检查包括诸如：

```text
资产引用是否合法
剧本忠实度
描述是否具体
父资产 / 派生资产关系是否正确
```

并且定义了 R1、R2、R3、R4 等硬性红线规则以及 A/B/C/D 级评价。

Script Supervisor 同样设置了大量领域质量标准，包括：

```text
开篇 Hook
节奏
反转
情绪布局
改编忠实度
ROI
故事结构
```

等。

因此推荐的监督原则是：

```text
Execution Agent
        │
        ▼
   Workspace / DB
        │
        ▼
Supervisor
重新读取最终状态
        │
        ▼
ReviewReport
```

而不要设计成：

```text
Execution
   ↓
把结果文字直接丢给 Supervisor
```

前一种明显更可靠。

---

# 10. 三层 Agent 的核心数据流

完整调用链可以还原成：

```text
User
 │
 ▼
DecisionAgent
 │
 │ tool_call
 ▼
ExecutionAgent
 │
 ├─ read DB/workspace
 ├─ generate content
 ├─ invoke domain tool
 └─ write DB/workspace
 │
 ▼
DecisionAgent
 │
 │ tool_call
 ▼
SupervisionAgent
 │
 ├─ reload workspace
 ├─ validate
 ├─ score
 └─ report problems
 │
 ▼
DecisionAgent
 │
 ▼
User
 │
 ├─ approve
 ├─ fix
 └─ redo
```

这里 Decision 是 Coordinator，而不是业务实现者。

---

# 11. Skill 文件化

Toonflow 把核心 Agent Prompt 放在：

```text
data/skills/
```

而不是：

```ts
const SYSTEM_PROMPT = `...`
```

例如：

```text
script_agent_decision.md
script_agent_supervision.md

production_agent_decision.md
production_agent_supervision.md

production_execution_director_plan.md
production_execution_storyboard_table.md
...
```

仓库 README 也明确将“Skill 文件化配置”作为核心能力。

推荐你的项目也采用：

```text
skills/
├─ my_agent/
│  ├─ decision.md
│  ├─ supervision.md
│  └─ execution/
│     ├─ research.md
│     ├─ planning.md
│     ├─ generate.md
│     └─ publish.md
```

Skill 最好包含：

```text
Role
Goal
Input Contract
Workflow
Available Tools
Constraints
Output Contract
Failure Conditions
Quality Rules
Examples
```

而不是只有简单 system prompt。

---

# 12. Persistent Memory 总体设计

这是 Toonflow 第二个值得单独复制的模块。

它并非传统：

```text
conversation_history
LIMIT 20
```

而是一个混合 Memory：

```text
                     ┌───────────────┐
                     │ Current Query │
                     └───────┬───────┘
                             │
           ┌─────────────────┼─────────────────┐
           ▼                 ▼                 ▼
     Short-term          Summaries        Vector RAG
      Memory             Memory             Memory
           │                 │                 │
           └─────────────────┼─────────────────┘
                             ▼
                       Memory Prompt
                             │
                             ▼
                       Decision Agent
```

官方 README 所称的“短期消息、长期摘要、语义召回”在代码中都有独立实现。

---

# 13. SQLite Memory Schema

源码生成的数据库类型中可以看到 `memories` 表的实际字段：

```ts
interface memories {
    id?: string;

    isolationKey: string;

    type: string;

    role?: string | null;

    name?: string | null;

    content: string;

    embedding?: string | null;

    relatedMessageIds?: string | null;

    summarized?: number | null;

    createTime: number;
}
```



因此可以理解为：

| 字段 | 含义 |
|---|---|
| id | UUID |
| isolationKey | Memory Namespace |
| type | `message` / `summary` |
| role | user / decision / execution / supervision |
| name | 可选 Agent 名 |
| content | 消息或摘要 |
| embedding | JSON 编码向量 |
| relatedMessageIds | summary 对应的源 message IDs |
| summarized | 是否已经被摘要 |
| createTime | 时间戳 |

SQLite 文件本身是：

```text
data/.../db2.sqlite
```

项目通过 knex + better-sqlite3 打开。

---

# 14. Memory Namespace / 跨项目隔离

隔离键非常简单：

```text
projectId
+
agentType
+
episodesId
```

构造成：

```ts
`${projectId}:${agentType}:${episodesId}`
```

如果没有 episode：

```text
projectId:agentType
```

例如：

```text
101:scriptAgent:3

101:productionAgent:3

102:scriptAgent:1
```

这样不同：

```text
项目
Agent 类型
剧集
```

之间不会污染。

Memory API 的读取和清除逻辑使用同样的 isolation key。

这一设计最好直接复制。

进一步泛化可以使用：

```text
tenantId
projectId
workspaceId
agentType
conversationScope
```

最终生成：

```text
tenant/project/workspace/agent/session
```

---

# 15. 本地 ONNX Embedding

Toonflow 默认使用：

```text
all-MiniLM-L6-v2
```

模型文件：

```text
data/models/
└─ all-MiniLM-L6-v2/
   └─ onnx/
      └─ model_fp16.onnx
```

默认：

```text
dtype = fp16
```

代码显式：

```text
allowRemoteModels = false
allowLocalModels = true
```

因此运行时不会去 Hugging Face 在线下载模型。

然后使用：

```ts
pipeline(
    "feature-extraction",
    modelFolder,
    {
        dtype: modelDtype,
    }
)
```

生成 embedding。

实际调用：

```ts
extractor(text, {
    pooling: "mean",
    normalize: true,
});
```

因此向量已经做 L2 Normalize。

`all-MiniLM-L6-v2` 的输出为 **384 维 dense vector**。

---

# 16. 为什么 cosineSimilarity 只是 dot product

因为：

```text
normalize = true
```

所以：

```text
||A|| = 1
||B|| = 1
```

余弦相似度：

```text
cos(A,B)

      A · B
= --------------
   ||A|| ||B||

= A · B
```

因此 Toonflow 实现直接计算：

```ts
sum += a[i] * b[i]
```

即可。

---

# 17. 新消息如何进入 Memory

`Memory.add()` 是整个 Memory 系统的核心。

逻辑可以还原为：

```text
Memory.add(role, content)
       │
       ▼
generate embedding(content)
       │
       ▼
INSERT message
       │
       ▼
查询所有 unsummarized messages
       │
       ▼
数量 >= messagesPerSummary ?
       │
       ├── NO ──────→ END
       │
       ▼ YES
取最早 N 条
       │
       ▼
LLM Summary
       │
       ▼
generate embedding(summary)
       │
       ▼
INSERT summary
       │
       ▼
relatedMessageIds = source IDs
       │
       ▼
源 message:
summarized = 1
```

源码实现就是这一滚动批次摘要方式。

---

# 18. 默认 Memory 参数

源码默认值为：

```text
messagesPerSummary        = 3

summaryMaxLength          = 500

shortTermLimit            = 5

summaryLimit              = 10

ragLimit                  = 3

deepRetrieveSummaryLimit  = 5
```

这些配置还可以被 `o_setting` 中的运行时设置覆盖。

因此最初可以直接使用：

```yaml
memory:
  messages_per_summary: 3
  summary_max_chars: 500
  short_term_limit: 5
  summary_limit: 10
  rag_limit: 3
  deep_retrieve_summary_limit: 5
```

---

# 19. Long-term Summary 是怎么生成的

当未摘要消息达到：

```text
3
```

之后，把最早三条内容交给当前 Agent 所使用的 LLM：

```text
message 1
message 2
message 3
       ↓
Memory compression prompt
       ↓
summary <= 500 chars
```

然后：

```text
summary.embedding =
    embedding(summary)
```

同时保存：

```json
{
  "relatedMessageIds": [
    "msg1",
    "msg2",
    "msg3"
  ]
}
```

也就是说摘要并没有破坏原消息。

这是非常关键的设计：

```text
Summary
  │
  ├─ semantic representation
  │
  └─ pointers
        ↓
     original messages
```

这正是后面 `deepRetrieve()` 可以恢复原始内容的基础。

---

# 20. 普通 Memory Recall：三路召回

调用：

```ts
memory.get(currentText)
```

时，系统同时做三个查询。

## A. Short-term

获取最近：

```text
5 条
```

尚未摘要的 Message。

然后按时间重新正序排列。

---

## B. Long-term Summary

获取最近：

```text
10 条
```

Summary。

---

## C. Semantic RAG

首先：

```text
embedding(currentText)
```

然后加载当前 isolationKey 下的 Message embedding：

```text
query embedding
       │
       ▼
cosine similarity
       │
       ▼
sort DESC
       │
       ▼
TOP 3
```

最后返回：

```ts
{
    shortTerm,
    summaries,
    rag
}
```



---

# 21. 一个非常重要的事实：它没有使用真正的 Vector DB

Toonflow 当前并没有：

```text
FAISS
Qdrant
Milvus
pgvector
Chroma
HNSW
```

Embedding 是：

```text
JSON.stringify(vector)
```

存 SQLite。

查询时：

```text
SELECT message rows
        ↓
JSON.parse(embedding)
        ↓
JS loop
        ↓
dot product
        ↓
sort
        ↓
TOP K
```

也就是说其复杂度大致是：

```text
O(N × D)
```

其中：

```text
D = 384
```

这是一个**brute-force local vector search**。

对于几十、几百、甚至几千条 Memory 完全可行。

但如果要做到：

```text
100k+
1M+
```

条 Memory，则应换成：

```text
pgvector / Qdrant / sqlite-vec / HNSW
```

---

# 22. Memory Prompt 如何注入 Agent

Toonflow 最终把 Memory 拼成类似：

```text
## Memory

[相关记忆]

semantic result 1
semantic result 2
...

[历史摘要]

summary 1
summary 2
...

[近期对话]

user: ...
assistant: ...
...
```

然后放入 Decision Agent 的上下文。

也就是：

```text
SYSTEM
    decision skill

ASSISTANT CONTEXT
    project info
    model info
    memory context

USER
    current request
```

ProductionAgent 源码明确通过 `buildMemPrompt()` 将：

```text
rag
summaries
shortTerm
```

组合进去。

---

# 23. Deep Retrieve：真正值得复制的部分

普通 `memory.get()` 是快速自动召回。

除此之外，Memory 又给 Decision Agent 暴露：

```text
deepRetrieve
```

Tool。

这是一个二阶段检索。

流程：

```text
用户:
“之前我们定的角色设定是什么？”
                  │
                  ▼
         Decision Agent
                  │
          deepRetrieve
                  ▼
        embedding(keyword)
                  │
                  ▼
        search summaries
                  │
             TOP 5
                  ▼
       LLM relevance judge
                  │
        select summary IDs
                  ▼
      relatedMessageIds
                  │
                  ▼
       load raw messages
                  │
                  ▼
         original memory
```

源码 `deepRetrieve()` 的流程正是：

1. keyword embedding；
2. 对 summary 做向量搜索；
3. Top-N Summary；
4. 再让 LLM 判断哪些 Summary 真正相关；
5. 找出其 `relatedMessageIds`；
6. 回查原始 Message；
7. 返回原始上下文。

这个设计可以表示成：

```text
Query

  ↓

Summary Vector Search

  ↓

LLM Reranking

  ↓

Summary → Message IDs

  ↓

Original Messages
```

这是比单纯：

```text
query → message vector search
```

更有价值的设计。

---

# 24. 为什么同时需要普通 RAG 和 Deep Retrieve

普通 Recall：

```text
TOP 3 raw messages
```

优势：

```text
快
便宜
每轮自动执行
```

Deep Retrieve：

```text
query
 ↓
summary vector search
 ↓
LLM selection
 ↓
original messages
```

优势：

```text
覆盖更久历史
减少 embedding 语义误差
能够恢复某段完整对话
```

所以形成：

```text
              Memory Router
                    │
       ┌────────────┴─────────────┐
       ▼                          ▼
Automatic Recall            Tool Recall
memory.get()                deepRetrieve()
       │                          │
       ▼                          ▼
快速上下文                    深层历史搜索
```

这是我认为 Toonflow Memory 最值得复制的部分。

---

# 25. Deep Retrieve 并非每轮自动执行

ScriptAgent 的 Decision Skill 特别规定：

只有用户明确要求：

```text
“回忆”
“之前”
“上次”
“我们以前设置过……”
```

之类信息时，再调用 Deep Retrieve。

也就是说：

```text
Normal turn
    ↓
automatic hybrid memory

Explicit historical recall
    ↓
deepRetrieve tool
```

这样能显著减少：

```text
LLM 调用
token
延迟
```



---

# 26. 所有 Agent 层共享 Memory

这个细节同样很重要。

数据库不只保存：

```text
user
assistant
```

而是实际上可以保存：

```text
user

assistant:decision

assistant:execution

assistant:execution:storySkeleton

assistant:execution:adaptationStrategy

assistant:execution:script

assistant:supervision
```

Execution 和 Supervision Agent 完成后同样调用：

```ts
memory.add(...)
```

因此：

```text
用户
Decision
Execution
Supervisor
```

产生的关键信息都会进入同一记忆空间。

这会带来一个巨大优势：

下一次 Decision 不只是知道：

```text
用户说过什么
```

而且能知道：

```text
执行 Agent 做过什么
监督 Agent发现过什么
系统此前如何决策
```

---

# 27. 推荐复现的数据模型

建议不要只复制一个 `memories` 表，而是稍微生产化。

```sql
CREATE TABLE agent_memories (
    id TEXT PRIMARY KEY,

    isolation_key TEXT NOT NULL,

    memory_type TEXT NOT NULL,
    -- message | summary

    role TEXT,

    agent_name TEXT,

    content TEXT NOT NULL,

    embedding BLOB,

    summarized INTEGER DEFAULT 0,

    created_at INTEGER NOT NULL
);

CREATE INDEX idx_memory_scope_time
ON agent_memories(
    isolation_key,
    created_at
);

CREATE INDEX idx_memory_unsummarized
ON agent_memories(
    isolation_key,
    memory_type,
    summarized,
    created_at
);
```

另外不要把：

```text
relatedMessageIds
```

长期保存成 JSON，推荐正规化：

```sql
CREATE TABLE memory_summary_sources (
    summary_id TEXT NOT NULL,
    message_id TEXT NOT NULL,

    PRIMARY KEY(
        summary_id,
        message_id
    )
);
```

这样更容易扩展。

---

# 28. 推荐增加 Workflow 表

这是我不建议完全照搬 Toonflow 的地方。

Toonflow 很大程度依赖 Decision Prompt 判断阶段。

用于正式产品时推荐再增加：

```sql
workflow_runs

stage_runs

review_reports
```

例如：

```text
workflow_runs
---------------
id
project_id
agent_type
current_stage
status
created_at
updated_at
```

```text
stage_runs
---------------
id
workflow_run_id
stage
execution_agent
status
input_json
output_json
attempt
created_at
updated_at
```

```text
review_reports
---------------
id
stage_run_id
supervisor
score
passed
severity
report_json
created_at
```

这样：

```text
Prompt 负责智能决策
数据库负责真实状态
```

不要让 LLM 成为唯一状态机。

---

# 29. 推荐的生产级三层 Agent Runtime

可以抽象成：

```text
AgentRuntime
│
├── DecisionAgent
│
│   ├── SkillLoader
│   ├── MemoryContext
│   ├── WorkflowState
│   └── ToolRegistry
│
├── SubAgentRunner
│   │
│   ├── ExecutionAgent
│   └── SupervisionAgent
│
├── MemoryService
│   │
│   ├── MessageStore
│   ├── SummaryService
│   ├── EmbeddingService
│   └── RetrievalService
│
├── WorkflowEngine
│
├── ToolRegistry
│
└── LLMProvider
```

核心接口：

```ts
interface AgentRuntime {
    runDecision(
        ctx: AgentContext
    ): Promise<AgentResult>;
}

interface SubAgentRunner {
    run(
        spec: AgentSpec,
        ctx: AgentContext
    ): Promise<AgentResult>;
}

interface MemoryService {
    add(
        scope: string,
        role: string,
        content: string
    ): Promise<void>;

    getContext(
        scope: string,
        query: string
    ): Promise<MemoryContext>;

    deepRetrieve(
        scope: string,
        query: string
    ): Promise<MemoryItem[]>;
}
```

---

# 30. 推荐目录结构

如果重新实现，我建议：

```text
internal/
├─ agent/
│  ├─ runtime/
│  │  ├─ decision
│  │  ├─ subagent
│  │  └─ supervisor
│  │
│  ├─ workflow/
│  │  ├─ engine
│  │  ├─ state
│  │  └─ quality_gate
│  │
│  ├─ memory/
│  │  ├─ memory
│  │  ├─ summary
│  │  ├─ retrieval
│  │  ├─ embedding
│  │  └─ repository
│  │
│  └─ tools/
│
├─ llm/
│  ├─ provider
│  ├─ tool_call
│  └─ stream
│
└─ persistence/
   ├─ memory_repository
   └─ workflow_repository

skills/
├─ script/
│  ├─ decision.md
│  ├─ supervision.md
│  └─ execution/
│
└─ production/
   ├─ decision.md
   ├─ supervision.md
   └─ execution/
```

这样 Agent Framework 与业务 Skill 完全分离。

---

# 31. 一次完整请求应如何运行

推荐实现以下主循环：

```text
handleUserMessage()
      │
      ▼
resolveMemoryScope()
      │
      ▼
Memory.add(user)
      │
      ▼
Memory.getContext(query)
      │
      ▼
Workflow.loadState()
      │
      ▼
DecisionAgent.run()
      │
      ├──────────────┐
      │              │
      ▼              ▼
direct response     tool_call
                     │
                     ▼
                Execution
                     │
                     ▼
                persist result
                     │
                     ▼
                Supervisor
                     │
                     ▼
                ReviewReport
                     │
                     ▼
                 Decision
                     │
                     ▼
                 response
                     │
                     ▼
               Memory.add()
```

---

# 32. 修订闭环

质量控制不要设计成一次性：

```text
Generate → Review → Finish
```

而应该设计：

```text
                       ┌───────────────┐
                       │               │
                       ▼               │
Decision → Execution → Review          │
                       │               │
                       ├─ PASS → Next  │
                       │               │
                       ├─ FIX ─────────┘
                       │
                       └─ REDO ────────┘
```

同时建议给每个 Stage 设置：

```text
max_retry = 2~3
```

防止：

```text
Execution ↔ Supervisor
```

无限循环。

---

# 33. Quality Gate

不是所有步骤都必须 Supervisor。

Toonflow Production Prompt 本身就允许只在关键阶段自动触发监督，例如 Storyboard Table 是明显质量关卡。

可以设计：

```yaml
stages:

  research:
    supervision: false

  planning:
    supervision: true

  draft:
    supervision: true

  render:
    supervision: false

  final:
    supervision: true
```

这样可以减少大约一半以上额外 LLM 调用。

---

# 34. 输出必须结构化

Execution Agent 最好不要返回：

```text
“已经完成，这是一份很好的方案……”
```

而应该返回：

```json
{
  "status": "success",

  "stage": "storyboard",

  "artifacts": [],

  "warnings": [],

  "next_action": "review"
}
```

Supervisor：

```json
{
  "passed": false,

  "score": 72,

  "severity": "major",

  "issues": [
    {
      "rule": "R2",
      "location": "scene_13",
      "problem": "...",
      "suggestion": "..."
    }
  ]
}
```

然后 Decision 再生成自然语言。

这样系统稳定性会明显高于让三个 Agent 都自由文本交流。

---

# 35. Memory 也建议结构化

Toonflow 当前摘要主要是自由文本。

进一步生产化可以把 Memory 分成：

```text
Episodic Memory
用户与 Agent 发生过什么

Semantic Memory
项目事实 / 人物 / 规则

Procedural Memory
用户偏好的工作方式

Artifact Memory
生成过的资产 / 文件 / ID
```

例如：

```json
{
  "memory_type": "fact",

  "subject": "character:alice",

  "predicate": "hair_color",

  "value": "silver",

  "confidence": 0.98,

  "source_message_id": "...",

  "created_at": 123456
}
```

这样长期项目的一致性会进一步提升。

---

# 36. Toonflow 当前 Memory 的几个隐患

这一部分对复现尤其重要。

### 36.1 Vector Search 是全表暴力扫描

当前：

```text
SELECT all
→ parse JSON
→ dot product
→ sort
```

数据量大以后必须替换。

推荐路线：

```text
< 10k
SQLite brute force

10k ~ 500k
sqlite-vec / pgvector

> 500k
Qdrant / Milvus
```

---

### 36.2 Embedding 使用 JSON 保存

384 个 float 转 JSON 后空间明显大于：

```text
384 × 4 bytes
≈ 1.5 KB
```

实际 JSON 通常会大很多。

推荐：

```text
FLOAT32 BLOB
```

或者 Vector extension。

---

### 36.3 没有 similarity threshold

当前算法基本：

```text
sort similarity
take TOP K
```

所以即使：

```text
0.20
0.18
0.15
```

也可能作为所谓“相关记忆”返回。

推荐：

```text
similarity >= threshold
AND
TOP K
```

例如起步测试：

```text
0.45 ~ 0.65
```

最终阈值通过自己的数据集调。

---

### 36.4 Current User Message 可能重复进入上下文

源码调用顺序是：

```text
memory.add("user", text)

memory.get(text)

LLM(messages = [
   memory,
   user:text
])
```

所以当前用户消息已经：

```text
写入数据库
```

之后马上拿同一句：

```text
text
```

做 vector search。

数学上当前 Message 自己的 cosine similarity 接近：

```text
1.0
```

因此它很可能同时出现在：

```text
RAG
Short-term
Current user message
```

也就是说同一个请求可能被注入 2～3 次。

更合理的实现是：

```text
retrieve memory
    ↓
exclude currentMessageID
    ↓
call Decision
```

或者：

```text
retrieve before insert current message
```

---

### 36.5 Summary 会混合不同 Agent 角色

当前固定：

```text
3 messages → 1 summary
```

但三条可能分别是：

```text
user
decision
execution
```

摘要输入主要是 content，因此可能弱化 provenance。

建议摘要时传：

```text
role
agent
timestamp
content
```

例如：

```text
[user]
...

[decision]
...

[execution:storyboard]
...
```

---

### 36.6 Summary 本身会不断增长

当前机制主要是：

```text
message → summary
```

没有明显的：

```text
summary
   ↓
meta summary
   ↓
higher-level summary
```

因此长项目中 Summary 数量仍然持续增长。

推荐实现：

```text
Level 0
messages

Level 1
episode summaries

Level 2
session summaries

Level 3
project summary
```

即 hierarchical memory。

---

# 37. 我建议的改进版 Retrieval

比 Toonflow 原版再往前一步，可以：

```text
                  QUERY
                    │
         ┌──────────┼───────────┐
         ▼          ▼           ▼
     Recent      Message      Summary
     Memory       Vector       Vector
         │          │           │
         └──────────┼───────────┘
                    ▼
             Candidate Pool
                    │
                    ▼
             Score Fusion
                    │
                    ▼
               Reranker
                    │
                    ▼
             Token Budget
                    │
                    ▼
              LLM Context
```

Score：

```text
score =
0.55 × semantic_score
+
0.20 × recency_score
+
0.15 × importance
+
0.10 × agent_role_weight
```

比单纯 cosine 更适合长期 Agent。

---

# 38. 推荐增加 Memory Importance

例如：

```text
普通闲聊
importance = 0.1

用户偏好
importance = 0.7

项目关键决策
importance = 0.9

不可违反约束
importance = 1.0
```

召回：

```text
finalScore =
semanticSimilarity × 0.7
+
importance × 0.2
+
recency × 0.1
```

就不会出现：

```text
相似的废话
```

挤掉：

```text
重要项目规则
```

的问题。

---

# 39. 推荐的 Memory API

最终只需要给 Agent Runtime 暴露几个接口：

```text
remember()

recall()

deepRecall()

forget()

summarize()
```

内部：

```ts
MemoryService
{
    AddMessage()

    BuildContext()

    SemanticSearch()

    DeepRetrieve()

    GenerateSummary()

    Delete()

    RebuildEmbedding()
}
```

Decision Agent 只应该看到：

```text
deep_retrieve
```

等少数高级工具。

不要直接把：

```text
SQL search
vector search
summary table
```

暴露给 LLM。

---

# 40. 推荐的 Agent Tool 注册方式

Execution Agent Tool 应实施白名单。

不要：

```text
Decision:
所有系统 tools

Execution:
所有系统 tools

Supervisor:
所有系统 tools
```

而应该：

```text
Decision
├─ execution agents
├─ supervisor
├─ deep memory
└─ high-level workflow tools


Execution:Storyboard
├─ read_script
├─ read_assets
└─ write_storyboard


Supervisor:Storyboard
├─ read_script
├─ read_assets
└─ read_storyboard
```

尤其 Supervisor 原则上：

```text
READ ONLY
```

只有少数场景允许修改。

这是很重要的安全边界。

---

# 41. 完整可复现 MVP

如果目标只是首先复制 Toonflow 能力，MVP 其实只需要：

```text
① LLM Provider

② Tool Calling

③ Markdown Skill Loader

④ Decision Agent

⑤ Generic SubAgent Runner

⑥ Execution Agent

⑦ Supervisor Agent

⑧ SQLite memories

⑨ local MiniLM ONNX

⑩ Memory.add()

⑪ Memory.get()

⑫ Memory.deepRetrieve()
```

根本不需要：

```text
LangChain
LangGraph
CrewAI
AutoGen
复杂向量数据库
Kafka
微服务
```

即可实现 Toonflow 这一套核心能力。

---

# 42. MVP 开发顺序

建议顺序：

```text
Phase 1
Generic Agent Runtime

        ↓

Phase 2
Tool Calling + Skill Loader

        ↓

Phase 3
Decision → Execution

        ↓

Phase 4
Supervisor

        ↓

Phase 5
SQLite Memory

        ↓

Phase 6
ONNX Embedding

        ↓

Phase 7
Short-term + RAG

        ↓

Phase 8
Summary Memory

        ↓

Phase 9
Deep Retrieve

        ↓

Phase 10
Workflow State + Quality Gate
```

这样最快能验证架构。

---

# 43. Production 版本建议

如果不是 Demo，而是另一个正式项目，我建议最终架构：

```text
                        ┌──────────────┐
                        │ API / UI     │
                        └──────┬───────┘
                               │
                               ▼
                    ┌─────────────────────┐
                    │ Agent Runtime       │
                    └─────────┬───────────┘
                              │
                ┌─────────────┼──────────────┐
                ▼             ▼              ▼
         Decision Agent  Workflow Engine  Memory
                │                            │
         ┌──────┴──────┐           ┌────────┼────────┐
         ▼             ▼           ▼        ▼        ▼
      Execution     Supervisor    Recent  Summary   Vector
      Agents         Agents       Memory   Memory    Store
         │             │
         └──────┬──────┘
                ▼
           Tool Registry
                │
     ┌──────────┼───────────┐
     ▼          ▼           ▼
    DB       Business API   AI Models
```

核心思想仍与 Toonflow 一致：

> **Decision 负责想，Execution 负责做，Supervisor 负责检查，Memory 负责保持连续性，Workflow DB 负责记录事实状态。**

---

# 44. 原版与推荐版本对比

| 模块 | Toonflow 当前实现 | 推荐正式项目 |
|---|---|---|
| Agent 编排 | Tool Calling | 保留 |
| Workflow | Prompt 主导 | Prompt + State Machine |
| Decision | 独立 LLM | 保留 |
| Execution | 专用 SubAgent | 保留 |
| Supervision | 专用 SubAgent | 保留 |
| Skills | Markdown | 保留 |
| Memory DB | SQLite | 小中规模保留 |
| Embedding | MiniLM ONNX | 保留或升级多语模型 |
| Vector | JS brute-force | sqlite-vec / pgvector |
| embedding storage | JSON | FLOAT32/BLOB/vector |
| Short-term | 5 | 保留 |
| Summary | 每 3 条 | 可改 token based |
| Semantic RAG | Top 3 | threshold + TopK |
| Deep Recall | Summary→LLM→raw | 强烈保留 |
| Summary hierarchy | 单层 | 多层 |
| Memory importance | 无明显实现 | 增加 |
| Agent provenance | role string | 正规化 |
| Quality loop | Prompt | 显式 stage/retry |
| Supervisor 权限 | Tool controlled | 尽量 read-only |

---

# 45. 最值得直接复制的五个思想

如果不复制任何 Toonflow 业务代码，只复制架构，我认为价值最大的就是这五项：

```text
1.
Decision Agent 不干重活，
只负责调度专业 Execution Agents。

2.
Execution 和 Supervision
必须是不同 LLM invocation，
使用不同 System Skill。

3.
Supervisor 重新读取实际 Workspace，
而不是相信 Execution 的描述。

4.
Memory =
Recent
+
Summary
+
Semantic RAG

5.
Deep Recall =
Summary Vector Search
→ LLM Filter
→ Original Messages
```

尤其第 5 点非常适合：

```text
长期 AI 创作
软件开发 Agent
项目管理 Agent
行业 Agent
研究 Agent
数字孪生 Agent
```

等持续数周甚至数月的任务。

---

# 46. 复现时最核心的伪代码

```ts
async function handleTurn(ctx) {

    const scope =
        buildIsolationKey(ctx);

    const memory =
        new Memory(scope);

    // 建议先 recall，再记录当前 turn，
    // 避免当前消息自己命中自己。
    const memories =
        await memory.recall(ctx.text);

    await memory.add(
        "user",
        ctx.text,
    );

    const state =
        await workflow.load(scope);

    const decision =
        await decisionAgent.run({
            state,
            memories,
            userMessage: ctx.text,

            tools: {
                deepRetrieve:
                    memory.deepRetrieve,

                ...executionAgentTools,

                supervise:
                    supervisionAgent,
            },
        });

    await memory.add(
        "assistant:decision",
        decision.text,
    );

    return decision;
}
```

Execution：

```ts
async function execute(spec, input) {

    const result =
        await llm.run({
            system:
                loadSkill(
                    spec.skill
                ),

            messages: [
                input
            ],

            tools:
                spec.allowedTools,
        });

    await memory.add(
        spec.memoryRole,
        result.text,
    );

    return result;
}
```

Supervisor：

```ts
async function supervise(stage) {

    const actualState =
        await workspace.read(stage);

    const report =
        await llm.run({
            system:
                loadSkill(
                    stage.supervisionSkill
                ),

            messages: [
                actualState
            ],

            tools:
                readOnlyTools,
        });

    await memory.add(
        "assistant:supervision",
        report.text,
    );

    return report;
}
```

Memory：

```ts
async function recall(query) {

    const recent =
        await getRecentMessages();

    const summaries =
        await getRecentSummaries();

    const q =
        await embed(query);

    const semantic =
        await vectorSearch(
            q,
            ragLimit
        );

    return {
        recent,
        summaries,
        semantic,
    };
}
```

Deep Recall：

```ts
async function deepRetrieve(query) {

    const q =
        await embed(query);

    const candidates =
        await searchSummaryVectors(
            q,
            5
        );

    const relevant =
        await llmRerank(
            query,
            candidates
        );

    const ids =
        relevant.flatMap(
            s =>
              s.relatedMessageIds
        );

    return loadMessages(ids);
}
```

这四段基本就是 Toonflow 两项核心能力的技术骨架。

---

# 47. License

当前仓库标注为 **Apache License 2.0**。因此从工程角度可以研究、修改、集成和用于商业项目，但如果直接复制其源码，应按 Apache-2.0 的 LICENSE、NOTICE、版权声明等要求处理。

---

# 48. 最终复现建议

如果目标是在另一个项目里拥有“和 Toonflow 类似但更可靠”的 Agent 系统，我不建议 1:1 照抄代码，而建议：

```text
保留：
Decision / Execution / Supervision

保留：
Markdown Skill

保留：
共享 Persistent Memory

保留：
Recent + Summary + Semantic

强烈保留：
Summary → Deep Retrieve → Raw Message

升级：
Prompt State Machine
→
Explicit Workflow State

升级：
JSON Vector
→
Native Vector Storage

升级：
TopK
→
Threshold + Rerank

升级：
单层 Summary
→
Hierarchical Memory

升级：
自由文本 Agent 通信
→
Structured AgentResult / ReviewReport
```

最终就会形成一个比 Toonflow 原版更适合长期维护的：

```text
Multi-Agent Runtime
+
Durable Workflow
+
Persistent Semantic Memory
+
Quality Control Loop
```

架构。
# Infinite Atelier Drama Studio 领域模型

> 版本：v1.0  
> 状态：已批准的逻辑模型；物理 DDL 在工作包中实现  
> 关联：`PRD.md`、`docs/ARCHITECTURE.md`

---

# 1. 目标

本文件定义业务实体、聚合边界、状态、不变量、版本、关系和关键索引。它不是 ORM 模型清单，也不要求一个实体对应一个巨型 Go struct。

核心原则：

1. 数据库是事实来源；
2. 画布是领域实体的投影；
3. 可修改内容使用不可变版本；
4. Agent 通过 Application Command 和受限 Tool 修改事实；
5. 外部 Provider 的原始协议不进入领域模型；
6. Story Graph、Workspace State、Agent Memory、Canvas Projection 分离；
7. 删除、替换、重做和 stale 都有显式语义。

---

# 2. 通用约定

## 2.1 ID

- 外部可见 ID 使用 UUIDv7 或 ULID；
- 具体选择在 WP-01 ADR 固化；
- ID 在 Application 层生成；
- 任何实体不得依赖数据库自增 ID 作为跨表业务引用；
- 文件内容使用 SHA-256；
- 幂等键采用命令作用域 + 业务输入哈希。

## 2.2 时间

- 数据库存 UTC；
- Go 使用 `time.Time`；
- API/事件使用 RFC3339Nano；
- UI 按用户本地时区显示；
- 测试使用可注入 Clock。

## 2.3 Revision 与并发

可变根实体包含：

```text
revision INTEGER NOT NULL
```

更新命令必须携带期望 revision，SQL 使用 Compare-And-Swap：

```sql
UPDATE ...
SET ..., revision = revision + 1
WHERE id = ? AND revision = ?;
```

更新 0 行返回 `conflict.revision_mismatch`。

## 2.4 软删除

默认字段：

```text
deleted_at nullable
deleted_by nullable
```

物理删除只用于：

- 临时文件；
- 未被引用、超过保留期的草稿；
- 用户明确永久删除且影响检查通过；
- 安全清理。

## 2.5 版本化

版本实体包含：

```text
id
parent_entity_id
version_number
status
based_on_version_id nullable
created_by_type  # user | agent | migration | system
created_by_id nullable
change_reason nullable
created_at
```

版本状态：

```text
draft
candidate
under_review
approved
rejected
superseded
deprecated
stale
```

约束：

- 一个父实体同一时间最多一个 `approved` 当前版本；
- 批准新版本时旧批准版本变为 `superseded`；
- `stale` 不等于 rejected，也不自动删除；
- 版本内容批准后不可原地编辑，编辑创建新版本。

## 2.6 JSON 字段

允许 JSON：

- 可扩展 UI 状态；
- Provider 请求的脱敏快照；
- 非核心模型参数；
- 审核证据的展示元数据；
- 兼容导入原数据。

禁止用 JSON 代替：

- 关键外键；
- 版本关系；
- Summary 来源；
- Shot 与资产引用；
- Job 依赖；
- 可查询状态；
- Agent Tool Call；
- 审核问题。

---

# 3. 聚合总览

```text
WorkspaceAggregate
└─ Workspace

ProjectAggregate
├─ Project
├─ ProjectSettings
├─ ProjectRule
├─ ProjectStyleGuide
└─ ProjectProviderPolicy

StoryAggregate
├─ SourceDocument
├─ SourceDocumentVersion
├─ Chapter
├─ StoryEntity
├─ StoryEntityAlias
├─ StoryEvent
├─ StoryRelation
├─ StoryFactSource
├─ StoryFactConflict
└─ CharacterState

ScriptAggregate
├─ Episode
├─ StorySkeletonVersion
├─ AdaptationStrategyVersion
├─ Script
├─ ScriptVersion
├─ Scene
├─ DialogueLine
└─ Shot

AssetAggregate
├─ Asset
├─ AssetVersion
├─ AssetFile
├─ AssetRelation
├─ AssetUsage
└─ PhysicalFile

StoryboardAggregate
├─ DirectorPlanVersion
├─ Storyboard
├─ StoryboardVersion
├─ StoryboardItem
└─ StoryboardPanelVersion

CanvasAggregate
├─ CanvasDocument
├─ CanvasNode
├─ CanvasEdge
├─ CanvasView
└─ CanvasOperation

WorkflowAggregate
├─ WorkflowRun
├─ StageRun
├─ ReviewReport
├─ ReviewIssue
├─ UserGateDecision
└─ WorkflowEvent

RuntimeAggregate
├─ GenerationJob
├─ JobAttempt
├─ JobDependency
├─ ProviderRequest
├─ AgentRun
├─ AgentMessage
├─ AgentToolCall
└─ SkillVersion

MemoryAggregate
├─ MemoryItem
├─ MemorySummarySource
├─ MemoryEntityLink
└─ EmbeddingIndexMetadata
```

---

# 4. Workspace 与 Project

## 4.1 Workspace

桌面版默认只有一个本地 Workspace，但保留模型：

```text
id
name
kind              # local | team_future
created_at
updated_at
```

不变量：

- 本地版始终存在默认 Workspace；
- 删除默认 Workspace 被禁止；
- 未来 SaaS 的 tenant 不影响本地 ID。

## 4.2 Project

```text
id
workspace_id
project_type       # free_canvas | drama
name
description
cover_asset_version_id nullable
language
status             # active | archived | trashed
created_at
updated_at
deleted_at nullable
revision
```

不变量：

- `project_type` 创建后不可直接切换；
- Drama 项目可使用自由画布；
- Free Canvas 项目可后续通过显式“升级为短剧项目”创建 Drama 配置，但原类型与迁移记录保留。

## 4.3 ProjectSettings

```text
project_id
target_platform
aspect_ratio
resolution
expected_episode_count
default_episode_duration_seconds
audience
content_rating
adaptation_mode       # faithful | balanced | aggressive
language
timezone
settings_version
revision
```

## 4.4 ProjectRule

```text
id
project_id
category              # story | character | visual | camera | audio | safety | custom
name
content
strength              # advisory | required | immutable
status
source_type           # user | imported | agent_suggested
source_id nullable
locked_by_user
created_at
updated_at
revision
```

不变量：

- `immutable` 或 `locked_by_user=true` 的规则只有用户命令可修改；
- Agent 可创建建议，但不能把建议自动升级为 required/immutable；
- 规则变更触发影响分析。

## 4.5 ProjectStyleGuide

版本化保存：

- 视觉风格；
- 色板；
- 光照；
- 构图；
- 镜头语言；
- 负面约束；
- 声音方向；
- 参考资产版本。

---

# 5. Source Document 与 Chapter

## 5.1 SourceDocument

```text
id
project_id
document_type          # novel | story | screenplay | outline | notes
name
current_version_id
status
created_at
updated_at
revision
```

## 5.2 SourceDocumentVersion

```text
id
source_document_id
version_number
physical_file_id nullable
normalized_text_file_id
content_hash
mime_type
encoding
char_count
import_metadata_json
created_by_type
created_at
```

不变量：

- 原文件和规范化文本均可追踪；
- 任何 Chapter 必须指向具体 SourceDocumentVersion；
- 替换文档不覆盖旧版本；
- 相同内容哈希需提示重复。

## 5.3 Chapter

```text
id
source_document_version_id
ordinal
title
start_offset
end_offset
content_hash
status                # detected | confirmed | edited
created_at
updated_at
revision
```

不变量：

- 同一文档版本内 ordinal 唯一；
- offset 不重叠且在规范化文本长度内；
- 用户确认后的边界更改创建新的章节集合版本或修订记录；
- 下游事实引用 chapter ID 和来源偏移。

---

# 6. Story Graph

## 6.1 StoryEntity

```text
id
project_id
entity_type           # character | location | organization | prop | concept | time
canonical_name
status                # candidate | accepted | rejected | locked
source_scope           # original | adaptation | user
current_profile_version_id nullable
created_at
updated_at
revision
```

## 6.2 StoryEntityAlias

```text
id
story_entity_id
alias
source_chapter_id nullable
source_start_offset nullable
source_end_offset nullable
```

同项目 canonical_name/alias 冲突需提示，不强制静默合并。

## 6.3 StoryEvent

```text
id
project_id
chapter_id nullable
ordinal
name
description
event_type
story_time_text nullable
story_time_order nullable
location_entity_id nullable
cause_summary nullable
result_summary nullable
importance
confidence
status                # candidate | accepted | rejected | locked
source_scope
created_by_agent_run_id nullable
created_at
updated_at
revision
```

## 6.4 StoryEventParticipant

```text
story_event_id
story_entity_id
role                  # actor | target | witness | owner | affected | other
state_before nullable
state_after nullable
```

## 6.5 StoryRelation

```text
id
project_id
relation_type         # causes | precedes | contradicts | knows | owns | located_in ...
source_entity_type
source_entity_id
target_entity_type
target_entity_id
valid_from_event_id nullable
valid_to_event_id nullable
confidence
status
source_scope
created_at
```

## 6.6 StoryFactSource

```text
id
fact_type
fact_id
chapter_id nullable
source_document_version_id
start_offset nullable
end_offset nullable
quote_hash nullable
source_kind           # text | user | agent_inference | adaptation
created_at
```

原文片段展示通过 offset 从规范化文本读取；数据库可保存有限摘录用于校验，但不复制整章。

## 6.7 StoryFactConflict

```text
id
project_id
left_fact_type
left_fact_id
right_fact_type
right_fact_id
conflict_type
status                # open | resolved | waived
resolution nullable
resolved_by nullable
created_at
resolved_at nullable
```

Agent 发现冲突后创建记录；用户或明确规则解决。

## 6.8 CharacterState

用于跨事件连续性：

```text
id
character_entity_id
from_event_order
to_event_order nullable
appearance_json
costume_asset_version_id nullable
injuries_json
possessions_json
relationship_state_json
source_fact_id
status
```

核心状态应逐步正规化；MVP 可在 Schema 受控 JSON 中保存稀疏状态。

---

# 7. Episode 与 Script

## 7.1 Episode

```text
id
project_id
season_number
episode_number
title
status                 # planning | writing | approved | production | completed
source_chapter_start_id nullable
source_chapter_end_id nullable
target_duration_seconds
current_story_skeleton_version_id nullable
current_adaptation_strategy_version_id nullable
current_script_version_id nullable
created_at
updated_at
revision
```

同一 Project 内 `(season_number, episode_number)` 唯一。

## 7.2 StorySkeletonVersion

结构化字段：

```text
id
episode_id
version_number
status
opening_hook
core_conflict
turning_points_json
climax
ending_hook
selected_story_event_ids (通过关联表)
estimated_duration_seconds
source_agent_run_id nullable
created_at
```

## 7.3 AdaptationStrategyVersion

```text
id
episode_id
version_number
status
strategy_summary
adaptation_mode
retained_event_ids
merged_event_groups
removed_event_ids
reordered_event_ids
original_additions
rationale
risks
source_agent_run_id nullable
created_at
```

列表使用关联表或受控结构，不能只保存 Markdown。

## 7.4 Script

Script 是稳定身份，ScriptVersion 是内容：

```text
id
episode_id
current_version_id nullable
created_at
updated_at
revision
```

## 7.5 ScriptVersion

```text
id
script_id
version_number
status
based_on_version_id nullable
story_skeleton_version_id
adaptation_strategy_version_id
estimated_duration_seconds
summary
source_agent_run_id nullable
created_by_type
change_reason nullable
created_at
```

## 7.6 Scene

Scene 属于具体 ScriptVersion：

```text
id
script_version_id
ordinal
scene_number
slugline
interior_exterior     # INT | EXT | INT_EXT | OTHER
location_entity_id nullable
time_of_day
summary
dramatic_goal
estimated_duration_seconds
source_story_event_id nullable
is_original_adaptation
created_at
```

同一 ScriptVersion ordinal 唯一。

## 7.7 DialogueLine

```text
id
scene_id
ordinal
line_type              # dialogue | narration | action | transition | note
character_entity_id nullable
text
emotion nullable
performance_note nullable
source_story_event_id nullable
locked
created_at
updated_at
revision
```

## 7.8 Shot

Shot 可在剧本草案阶段创建，也可由 Storyboard 工作流完善：

```text
id
scene_id
ordinal
shot_number
shot_size
camera_angle
camera_movement
estimated_duration_seconds
visual_description
action_description
audio_intent
continuity_notes
status
created_at
updated_at
revision
```

Shot 与资产引用通过 AssetUsage/ShotAssetReference 正规化。

---

# 8. Asset 与 File

## 8.1 Asset

```text
id
project_id
asset_type             # character | location | prop | costume | style | image | video | audio | doc
name
description
story_entity_id nullable
current_approved_version_id nullable
status                 # active | archived | trashed
created_at
updated_at
revision
```

## 8.2 AssetVersion

```text
id
asset_id
version_number
status
based_on_version_id nullable
parent_asset_version_id nullable
variant_type nullable
prompt nullable
negative_prompt nullable
provider_config_id nullable
model_config_id nullable
model_parameters_json nullable
seed nullable
generation_job_id nullable
source_agent_run_id nullable
metadata_json
created_by_type
created_at
```

不变量：

- `approved` 切换需影响分析；
- 被已批准镜头引用的版本不能直接物理删除；
- generation_job 成功并完成文件提交后才能引用结果文件。

## 8.3 PhysicalFile

```text
id
sha256
relative_path
size_bytes
mime_type
magic_type
width nullable
height nullable
duration_ms nullable
created_at
verified_at
status                  # temp | ready | quarantined | missing
```

`sha256` 唯一，允许多个 AssetFile 引用。

## 8.4 AssetFile

```text
id
asset_version_id
physical_file_id
role                    # primary | thumbnail | reference | mask | first_frame | last_frame | source
ordinal
created_at
```

## 8.5 AssetRelation

```text
id
source_asset_version_id
target_asset_version_id
relation_type           # derived_from | variant_of | replaces | references | supersedes
created_at
```

## 8.6 AssetUsage

```text
id
asset_version_id
consumer_type           # project_style | scene | shot | storyboard_panel | job | export
consumer_id
usage_role              # character | location | prop | costume | reference | first_frame ...
required
created_at
```

唯一性由 `(asset_version_id, consumer_type, consumer_id, usage_role)` 控制。

---

# 9. Director 与 Storyboard

## 9.1 DirectorPlanVersion

```text
id
episode_id
version_number
status
script_version_id
visual_rhythm
camera_language
color_lighting
staging
continuity_rules
audio_direction
shot_overrides_json
source_agent_run_id nullable
created_at
```

场次/镜头覆盖优先通过子表保存，MVP 可使用受控 JSON 并在 v1 正规化。

## 9.2 Storyboard

```text
id
episode_id
current_version_id nullable
created_at
updated_at
revision
```

## 9.3 StoryboardVersion

```text
id
storyboard_id
version_number
status
script_version_id
director_plan_version_id
based_on_version_id nullable
source_agent_run_id nullable
created_at
```

## 9.4 StoryboardItem

```text
id
storyboard_version_id
shot_id
ordinal
shot_size
camera_angle
camera_movement
duration_seconds
visual_description
action_description
dialogue_audio_summary
continuity_notes
status
created_at
updated_at
revision
```

## 9.5 StoryboardPanelVersion

```text
id
storyboard_item_id
version_number
status
visual_prompt
negative_prompt
reference_policy_json
approved_image_asset_version_id nullable
source_agent_run_id nullable
created_at
```

不变量：

- StoryboardItem 必须唯一对应本版本的 Shot；
- 必需资产引用未解析时不能创建批量 image job；
- approved image 必须属于该 Panel 的候选或经用户明确关联。

---

# 10. Canvas

## 10.1 CanvasDocument

```text
id
project_id
name
canvas_kind            # free | drama | episode | storyboard | asset
viewport_json
background_json
created_at
updated_at
revision
```

## 10.2 CanvasNode

```text
id
canvas_document_id
node_type
entity_type nullable
entity_id nullable
entity_version_id nullable
workflow_run_id nullable
position_x
position_y
width
height
z_index
ui_state_json
created_at
updated_at
revision
```

不变量：

- Entity projection 的 entity_type/entity_id 必须同时存在；
- version_id 必须属于 entity；
- 删除 projection 默认不删除 entity；
- 同一实体可在多个 Canvas 或同一 Canvas 多次投影，是否允许由 node role 决定。

## 10.3 CanvasEdge

```text
id
canvas_document_id
from_node_id
to_node_id
relation_type
from_port nullable
to_port nullable
required
validation_status       # valid | invalid | stale | unknown
metadata_json
created_at
updated_at
revision
```

## 10.4 Relation Registry

关系注册定义：

```text
relation_type
allowed_source_types
allowed_target_types
directionality
cardinality
creates_domain_relation
requires_version
validation_handler
```

初始关系：

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
appears_in
located_in
uses_asset
causes
precedes
contradicts
```

---

# 11. Workflow 与 Review

## 11.1 WorkflowRun

```text
id
project_id
episode_id nullable
workflow_type
current_stage
status
active_stage_run_id nullable
configuration_json
retry_count
created_at
updated_at
completed_at nullable
revision
```

## 11.2 StageRun

```text
id
workflow_run_id
stage
attempt
execution_agent_key nullable
status
input_json
validated_output_json nullable
raw_output_file_id nullable
error_code nullable
error_message nullable
created_at
started_at nullable
finished_at nullable
revision
```

状态：

```text
ready
running
executed
under_review
waiting_user
passed
failed
cancelled
superseded
```

不变量：

- 同一 Workflow 同阶段可有多次 attempt；
- 最多一个 active attempt；
- passed 后不可改写，只能 supersede；
- Stage 成功以业务写入和读取验证为准。

## 11.3 ReviewReport

```text
id
stage_run_id
supervisor_key
ruleset_version
score
grade
passed
severity
recommended_action
summary
created_at
```

## 11.4 ReviewIssue

```text
id
review_report_id
rule
severity
entity_type nullable
entity_id nullable
location nullable
problem
suggestion
evidence_json
status                 # open | accepted | fixed | waived
resolved_by nullable
resolved_at nullable
```

## 11.5 UserGateDecision

```text
id
workflow_run_id
stage_run_id
decision               # pass | fix | redo | manual_edit | cancel | waive
issue_ids
instruction nullable
locked_entity_refs
created_by
created_at
```

用户决策不可由 Agent 伪造。

---

# 12. Job 与 Provider Request

## 12.1 GenerationJob

```text
id
project_id
entity_type
entity_id
job_type
status
priority
idempotency_key
provider_config_id nullable
model_config_id nullable
remote_job_id nullable
progress nullable
input_json
result_json nullable
error_code nullable
error_message nullable
next_retry_at nullable
cancel_requested
lease_owner nullable
lease_expires_at nullable
created_at
updated_at
started_at nullable
finished_at nullable
revision
```

状态：

```text
queued
running
waiting_remote
downloading
retry_wait
succeeded
failed
cancelled
orphaned
```

不变量：

- idempotency_key 在作用域内唯一；
- succeeded 必须有验证后的结果引用；
- cancelled 不自动表示远程任务取消成功；
- 失败不生成 approved AssetVersion；
- remote_job_id 存在时重启优先 Poll，不重复 Submit。

## 12.2 JobAttempt

```text
id
generation_job_id
attempt_number
status
provider_request_id nullable
started_at
finished_at nullable
error_code nullable
error_message nullable
```

## 12.3 JobDependency

```text
job_id
depends_on_job_id
condition              # success | completed | approved
PRIMARY KEY(job_id, depends_on_job_id)
```

## 12.4 ProviderRequest

保存脱敏调用审计：

```text
id
job_id nullable
agent_run_id nullable
provider_config_id
model_config_id
capability
request_id nullable
status
http_status nullable
input_units nullable
output_units nullable
estimated_cost nullable
latency_ms
error_code nullable
redacted_metadata_json
created_at
```

禁止保存 Authorization 和完整 Secret。

---

# 13. Agent 与 Skill

## 13.1 AgentRun

```text
id
project_id
workflow_run_id nullable
stage_run_id nullable
agent_layer             # decision | execution | supervision
agent_key
model_config_id
skill_version_id
status
input_summary
validated_output_json nullable
raw_output_file_id nullable
error_code nullable
started_at
finished_at nullable
```

## 13.2 AgentMessage

```text
id
agent_run_id
scope_key
role
content
content_hash
created_at
```

完整大内容可放 FileStore，表中存引用和摘要。

## 13.3 AgentToolCall

```text
id
agent_run_id
sequence
tool_key
input_json
output_json nullable
status
error_code nullable
started_at
finished_at nullable
```

## 13.4 SkillVersion

```text
id
skill_key
version
content_hash
manifest_json
content_file_id
status
created_at
```

AgentRun 必须绑定确切 SkillVersion。

---

# 14. Memory

## 14.1 MemoryItem

```text
id
scope_key
memory_type             # episodic | semantic | procedural | artifact | summary
role
agent_key nullable
content
importance
confidence
embedding_blob nullable
embedding_model nullable
embedding_version nullable
summarized
locked
source_type
source_id nullable
created_at
updated_at
```

## 14.2 MemorySummarySource

```text
summary_id
source_memory_id
source_order
PRIMARY KEY(summary_id, source_memory_id)
```

## 14.3 MemoryEntityLink

```text
memory_id
entity_type
entity_id
relation_type
PRIMARY KEY(memory_id, entity_type, entity_id, relation_type)
```

## 14.4 Scope Key

逻辑字段：

```text
tenant_id/local
workspace_id
project_id
episode_id optional
agent_key
session_id optional
```

物理 scope key 可编码为稳定字符串，但检索实现必须能按结构字段隔离，不依赖脆弱字符串前缀。

## 14.5 Memory 不变量

- 当前消息不召回自身；
- 其他项目内容不可召回；
- locked memory 只有用户可修改/删除；
- summary 保留全部来源；
- embedding 模型变化不覆盖旧向量，重建后切换索引版本；
- Semantic Memory 不自动覆盖 Event Graph；
- Artifact Memory 只引用实体/文件，不复制二进制；
- 删除源消息时按政策更新或失效摘要。

---

# 15. Stale 与影响传播

## 15.1 触发源

- SourceDocumentVersion 更换；
- Story Fact 修改；
- ProjectRule 修改；
- ScriptVersion 重新批准；
- AssetVersion 默认批准版本切换；
- DirectorPlanVersion 切换；
- StoryboardVersion 切换；
- Provider/模型参数影响可复现性时变更。

## 15.2 传播策略

```text
Source Fact
→ Story Skeleton
→ Adaptation Strategy
→ Script
→ Director Plan
→ Asset Gap Report
→ Storyboard
→ Panel
→ Image
→ Video/Audio
→ Timeline/Export
```

不是所有变化都强制下游 stale。Impact Analyzer 根据关系和字段分类：

- `breaking`：阻止下游正式使用；
- `review_required`：允许保留但必须重新审核；
- `informational`：只记录提示。

## 15.3 Waiver

用户可保留 stale 产物，但必须：

- 写 UserGateDecision；
- 说明理由；
- 最终导出显示 waiver；
- Supervisor 可在最终审核重新提示。

---

# 16. 领域命令

建议命令命名：

```text
CreateProject
UpdateProjectSettings
CreateProjectRule
LockProjectRule
ImportSourceDocument
ConfirmChapters
AcceptStoryEntity
AcceptStoryEvent
ResolveStoryConflict
CreateEpisode
GenerateStorySkeleton
ApproveStorySkeleton
GenerateAdaptationStrategy
ApproveAdaptationStrategy
CreateScriptVersion
ApproveScriptVersion
CreateAsset
CreateAssetVersion
ApproveAssetVersion
CreateDirectorPlanVersion
CreateStoryboardVersion
ApproveStoryboardVersion
CreateCanvasProjection
CreateSemanticEdge
StartWorkflow
StartStage
SubmitUserGateDecision
CreateGenerationJob
CancelGenerationJob
RememberProjectFact
DeleteMemory
ExportProject
RestoreProject
```

每个命令实现：

- DTO；
- validation；
- expected revision；
- idempotency；
- authorization/state guard；
- transaction；
- event；
- test。

---

# 17. 领域事件

```text
ProjectCreated
ProjectSettingsChanged
ProjectRuleLocked
SourceDocumentImported
ChapterBoundariesConfirmed
StoryFactAccepted
StoryFactConflictOpened
EpisodeCreated
StorySkeletonApproved
AdaptationStrategyApproved
ScriptVersionCreated
ScriptVersionApproved
AssetVersionCreated
AssetVersionApproved
DirectorPlanApproved
StoryboardVersionApproved
CanvasProjectionCreated
WorkflowStarted
WorkflowStageChanged
ReviewReportCreated
UserGateDecided
GenerationJobQueued
GenerationJobSucceeded
GenerationJobFailed
MemoryCreated
UpstreamVersionChanged
ArtifactMarkedStale
BackupCompleted
```

事件 Envelope：

```json
{
  "eventId": "...",
  "eventType": "ScriptVersionApproved",
  "schemaVersion": 1,
  "aggregateType": "script",
  "aggregateId": "...",
  "projectId": "...",
  "occurredAt": "...",
  "traceId": "...",
  "payload": {}
}
```

---

# 18. 索引要求

至少建立：

```text
projects(workspace_id, status, updated_at)
chapters(source_document_version_id, ordinal)
story_entities(project_id, entity_type, canonical_name)
story_events(project_id, chapter_id, ordinal)
story_relations(project_id, relation_type, source_entity_id, target_entity_id)
episodes(project_id, season_number, episode_number)
scenes(script_version_id, ordinal)
shots(scene_id, ordinal)
assets(project_id, asset_type, status)
asset_versions(asset_id, version_number)
asset_usages(consumer_type, consumer_id)
canvas_nodes(canvas_document_id, entity_type, entity_id)
canvas_edges(canvas_document_id, from_node_id, to_node_id)
workflow_runs(project_id, status, updated_at)
stage_runs(workflow_run_id, stage, attempt)
review_reports(stage_run_id)
generation_jobs(status, priority, next_retry_at)
generation_jobs(project_id, entity_type, entity_id)
provider_requests(provider_config_id, created_at)
agent_runs(project_id, workflow_run_id, started_at)
agent_memories(scope_key, memory_type, created_at)
memory_entity_links(entity_type, entity_id)
physical_files(sha256)
```

全文检索和向量索引通过独立迁移/Adapter 引入。

---

# 19. 数据完整性检查

应用启动、备份、恢复和诊断支持：

- SQLite integrity check；
- foreign key check；
- current version 引用有效；
- approved version 唯一；
-文件记录存在且哈希匹配；
- Job succeeded 结果存在；
- Canvas entity refs 有效；
- Summary sources 有效；
- Workflow active stage 一致；
- Asset usages 不引用被物理删除版本；
- 不存在普通数据库中的 Secret 明文模式。

修复工具默认只读报告；自动修复需明确、安全、可回滚。

---

# 20. 迁移策略

## 20.1 Legacy Import

Legacy 项目转换时建立映射表：

```text
legacy_project_id -> project_id
legacy_node_id -> canvas_node_id
legacy_asset_id -> asset_id/asset_version_id
legacy_media_key -> physical_file_id
legacy_generation_history_id -> provider_request/job
```

无法映射的 metadata 原样放入 `legacy_metadata_json`，并写迁移告警；不得静默丢弃。

## 20.2 Schema Migration

- 每个迁移唯一编号；
- 迁移文件提交代码库；
- 生产启动前创建数据库快照；
- 大表迁移采用 copy/validate/swap；
- 迁移测试覆盖至少前两个已发布版本；
- 不支持降级时明确阻止旧应用打开新数据库。

---

# 21. MVP 最小数据闭环

MVP 至少真实持久化并关联：

```text
Project
→ SourceDocumentVersion
→ Chapter
→ StoryEvent
→ Episode
→ StorySkeletonVersion
→ AdaptationStrategyVersion
→ ScriptVersion
→ Scene
→ Shot
→ Asset/AssetVersion
→ DirectorPlanVersion
→ StoryboardVersion/Item/Panel
→ WorkflowRun/StageRun
→ AgentRun/ToolCall
→ ReviewReport/Issue
→ GenerationJob
→ CanvasNode/Edge
→ MemoryItem/SummarySource
```

任何缺少上述正式实体、只把结果塞入一个 JSON/Canvas metadata 的实现均不满足 PRD。


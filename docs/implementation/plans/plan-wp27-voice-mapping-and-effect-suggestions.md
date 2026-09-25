# WP-27 — 多角色声线映射与音效建议（P3 第 23 项剩余两半）

依据：`docs/implementation/project-progress-and-remaining-tasks-2026-09-23.md:125`（第 23 项 PARTIAL
点名这两个半项）、PRD FR-080 音频 V1 四句（音效建议与生成适配 / 背景音乐导入 / 简单混音 / 多角色声线映射）、
STATUS §0y「STILL OPEN, named rather than implied」、ADR-0024、AGENTS §7.1（数据库是事实来源）、
§8.3（迁移前向只增、splitSQL 无分号）。

## 0. 侦察结论（每条有证据）

| 事实 | 证据 | 后果 |
|---|---|---|
| **声线按次提交、存不下来** | `media_jobs.go:162` `Voice` 进 `audioJobInput`；`jobs/ports.go:209` `AudioRequest.Voice`；`runner.go:417` 透传。全仓**没有任何** SELECT 读它回来 | 「角色甲用声线乙」不是一个事实，是每次提交时用户的重输 |
| **UI 侧声线来自全局设置** | `audio-view.tsx:329` `voice: config.audioVoice`；`voiceNote`（zh-CN.ts）写「取自项目的音频设置，本分区不另设一份」 | 一个项目只有**一个**声线 → 多角色必然同声 |
| **混音把每个音频版本都当对白** | `export_service.go:460` `audioClipFor(..., AudioRoleDialogue, ...)`，角色是**硬编码实参**；`buildMix` 的 `music` 切片初始化为空且从无追加（`:456`、`:472`、`:478`） | `AudioRoleMusic`／`AudioRoleEffect` 有默认增益却**永不生效**——`DefaultGainFor(music)=0.35` 是死规则 |
| **`AudioRoleEffect` 存在、混得动、没人产生** | `audio.go:39-40`；STATUS §0y 原话「nothing SUGGESTS an effect for a shot」 | 音效建议的**输出端**已就绪，缺的是建议来源 |
| **`Shot.AudioIntent` 被作者写出、无人消费** | 迁移 `000008_script.sql:210` 建列；`script.go:497,508` SELECT 回来；读它的只有结构 DTO 与绑定 | 这是**音效建议唯一现成、且作者已经写好的输入**，一直没人读 |
| **`story_entities` 有 `current_profile_version_id`，但该表不存在** | `000007:83`、`000014:86` 建列；全仓无 `entity_profile_versions` 之类的表 | 角色档案是**悬空引用**；不能把声线挂在那里 |
| **`project_settings` 是每项目一行的 KV 形态** | `000006:12-26`；`settings_version`、`revision` 并发保护 | 可以放项目级默认声线，但放不下**逐角色**映射 |
| **`asset_usages.usage_role` 词表开放** | `000009:168` 无 CHECK；`checker.go:687` 注释「The vocabulary is open — `usage_role` is a free string」 | 新角色（`audio_dialogue`/`audio_music`/`audio_effect`）**不需要迁移** |
| **迁移头是 26（000025）** | `migrate_wp05_test.go:60` `wp05HeadVersion = 25`；文件到 `000025_summary_ladder.sql` | 本包若要迁移则是 `000026`，且**必须同步那份列表与常数**（同一注释点名这是沉默陷阱） |
| **`asset_usages` 有 `UNIQUE(asset_version_id, consumer_type, consumer_id, usage_role)`** | `000009:171` | 同一版本的同一角色只能一行——正是映射想要的幂等 |

## 1. 裁定

1. **「角色 → 声线」存成一张表，不存进 `project_settings` 的 JSON。** 理由：映射是**集合**（N 个角色），
   项目设置是**单行**；塞进 JSON 列会让"角色甲用什么声线"无法被查询、无法被外键约束、也无法在删除角色时
   级联。表名 `character_voices`，`(project_id, character_entity_id)` 唯一，`character_entity_id` 外键指向
   `story_entities(id) ON DELETE CASCADE`。
2. **声线是「渠道 + 模型 + 声线名」三元组，不是裸字符串。** 理由：同一个声线名（`alloy`）在不同 provider
   下是不同的声音，只存名字会让换渠道后悄悄换声音。三列都存在，可读回。
3. **解析顺序明确且可测：逐角色映射 → 项目默认 → 空（让 provider 用它的默认）。** 三级都要有测试，
   因为「映射生效」与「默认生效」在只测一级时无法区分——这正是 WP-16 与 WP-18 的变异存活教过的形状。
4. **`buildMix` 的角色从数据来，不再硬编码。** 对白版本的 `usage_role` 是 `audio_dialogue`；音乐是
   `audio_music`；音效是 `audio_effect`。**旧行（`usage_role='audio'` 或空）继续按对白对待**，因为
   既有行为不能因为新增角色而改变——那是数据破坏。
5. **音效建议是纯函数 + 一个读，不是 agent 调用。** `SuggestEffects(shots []ShotSuggestionInput) []EffectSuggestion`
   读 `Shot.AudioIntent`，按**确定性词表**匹配（雨/水、脚步、门、风、金属、引擎、鸟、钟、枪、心跳…），
   返回建议与**它匹配到的词**。理由：AGENTS §10 要求业务成功以数据库读回验证，而「建议」这种东西用一个
   不可解释的模型分数无法被用户核对；词表给出的**依据**（哪个词触发的）才是可核对的。
6. **建议不写库。** 它是**读模型的投影**，与 `TimelineService` 同级：没有新表、没有新状态、不会过期。
   用户「采纳」一个建议时提交一个音效任务，那才是事实。
7. **采纳路径复用已有的音效机制**：提交音频任务时 `usage_role` 记为 `audio_effect`，`buildMix` 就会用
   `DefaultGainFor(effect)`。**不新增 job type**。
8. **不引入 BGM 的 UI 导入**——它不在第 23 项剩余两半里（WP-20 已把 `AudioRoleMusic` 接进混音，
   STATUS §0y 记它「reachable through the mix ... has no UI control yet」是**另一个**已点名的开放项）。
   本包只做**多角色声线映射**与**音效建议**，不顺手扩范围。

## 2. 迁移 `000026_character_voices.sql`（前向只增）

```sql
CREATE TABLE character_voices (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    character_entity_id TEXT NOT NULL REFERENCES story_entities(id) ON DELETE CASCADE,
    provider_config_id TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    voice TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
);
CREATE UNIQUE INDEX idx_character_voices_project_character
    ON character_voices(project_id, character_entity_id);
```

- **无分号出现在注释里**（`splitSQL` 按每个分号切分）。
- 同步 `migrate_wp05_test.go` 的列表与 `wp05HeadVersion` 25 → 26。
- `voice` 非空：一条没有声线的映射没有意义；`provider_config_id`/`model` 允许为空，表示「沿用项目默认」。

## 3. 领域与仓储

- `internal/domain/asset/` 或新 `internal/domain/voice/`？**选 `internal/domain/voice/`**：
  `CharacterVoice{ID, ProjectID, CharacterEntityID, ProviderConfigID, Model, Voice, Revision}` + `Validate`
  （Voice 非空、长度上限、revision ≥ 1）。理由：它不是资产——没有文件、没有版本、不能被批准。
- `internal/application/media/voice.go`：
  - `VoiceMappingService`（`AssignVoice`/`ClearVoice`/`ListVoices`/`ResolveVoice`）；
  - `ResolveVoice(characterID, projectDefault) VoiceChoice` 实现裁定 3 的三级解析；
  - `VoiceChoice{ProviderConfigID, Model, Voice, Source}`，`Source` ∈ `character|project|unset`，让 UI 能说
    「这条为什么是这个声音」。
- 仓储接口在 application，实现走 SQLite；`ListVoices` 按 `canonical_name` 排序保证稳定。

## 4. 音效建议

`internal/application/media/effects.go`：

- `EffectSuggestion{ShotID, Ordinal, Intent, Effect, Matched, Confidence}`；
- `effectLexicon`：`[]struct{Effect string; Terms []string}`，中文词表（雨、雷、风、脚步、门、铃、枪、
  引擎、鸟、水、心跳、玻璃、钟、掌声…）+ 英文对应，**大小写不敏感**；
- `SuggestEffects(inputs []ShotEffectInput) []EffectSuggestion`：纯函数，**一镜最多一条**建议（取最长匹配，
  平局按词表顺序），无匹配不产出；
- `EffectVocabulary()` 暴露词表供 UI 与测试共用（避免第二份副本）。

## 5. 混音读取角色（修死规则）

`internal/infrastructure/database/timeline.go` 的 `BoardFacts` 现在 `group_concat` 版本 id；
**改为同时带出 `usage_role`**，让 `export_service.go` 的 `audioClipFor` 拿到角色：

- `audio_dialogue` / `audio` / 空 → `AudioRoleDialogue`；
- `audio_music` → `AudioRoleMusic`；
- `audio_effect` → `AudioRoleEffect`；
- 其他 → 对白（前向兼容，与裁定 4 一致）并把版本 id 带进诊断。

**断言点**：`buildMix` 之后 `music` 切片非空当且仅当有音乐版本——这直接杀掉现存的死规则。

## 6. 前端

- `web/src/services/desktop/voice.ts`（新）：`listCharacterVoices`/`assignVoice`/`clearVoice`/
  `suggestEffects`，沿用查询返空、命令抛错的约定。
- `audio-view.tsx`：加「角色声线」区（角色下拉 + 渠道/模型 + 声线输入 + 「继承项目默认」按钮），
  提交配音时**先按所选行解析**：有映射用映射，否则用项目默认，并在 UI 上显示是哪种（裁定 3 的 `Source`）。
- `timeline-view.tsx`：镜头表加「音效建议」列，显示建议与**匹配词**；「采纳」按钮直接开一个音效任务。
- i18n **双语**（`zh-CN.ts` + `en-US.ts`，键集与占位符必须一致——`locales.spec.ts` 强制）。
- 更新 `voiceNote` 文案：它现在说「本分区不另设一份」，加了映射区之后**这句话不再成立**。

## 7. 测试与验收

- **T1 纯函数**：`ResolveVoice` 三级（含「映射优先于默认」「默认优先于空」两个方向）、
  `SuggestEffects`（匹配、无匹配、一镜多条取最长、大小写、空 intent）。
- **T2 服务**：`AssignVoice` 幂等（同角色两次 = 更新而非第二行）、清空、跨项目隔离。
- **T3 真实 SQLite**：写入/读回/revision 冲突/删除角色级联删除映射。
- **T4 混音角色**：三种 `usage_role` 各自产出的 `AudioClip.Role`，以及**旧值 `audio` 仍是对白**。
- **T5 AC-MEDIA-002 补语**：`usage_role = 'audio_effect'` 的行进入导出且带 effect 增益。
- **变异**：三级解析各一级、词表匹配、角色映射、旧值兼容。
- **门**：`sh scripts/verify.sh`（`GOTOOLCHAIN=go1.25.0 GOSUMDB=sum.golang.org GOCACHE=/d/go-build-cache`、
  PATH 前置 64 位 GCC）。

## 8. 明确不做

- BGM 的 UI 导入（另一个已点名开放项，不扩范围）；
- 音效**生成**的 provider 适配（FR-080 写「音效建议与生成适配」，本包交付**建议**与**采纳路径**；
  真音效 provider 是 adapter 工作，与 WP-26 同形但属另一个包）；
- 角色档案表（`current_profile_version_id` 是悬空引用，建它是另一个包）；
- 按阶段的模型策略。

## 9. 风险

- **最大风险：词表是产品判断。** 建议的质量取决于词表覆盖面，而它是硬编码的。缓解：词表**可读、可测、
  可扩展**，且每个建议都带**匹配词**，所以用户能立刻看出建议错在哪、而不是只看到一个分数。
- **次大风险：迁移列表与常数漏改。** `migrate_wp05_test.go` 的注释点名这是"沉默的"陷阱。
  缓解：迁移测试断言 `user_version = 26` **且**新表存在。
- **第三风险：混音角色改动会影响既有导出。** 缓解：裁定 4 的**前向兼容**是硬要求，
  并有一条测试专门断言旧值 `audio` 仍按对白处理。

## 10. 流程

基线 → 侦察核实（已完成） → 迁移 000026 + 领域 voice + 仓储 → 服务与三级解析 →
音效词表与纯函数 → `BoardFacts` 带角色 + `buildMix` 用角色 → 绑定 + `wails generate module` →
前端两处 + i18n → T1–T5 → 变异 → 全量门 → ADR-0028 → STATUS §0ze → TRACEABILITY/ROADMAP/backlog →
**报告后停止**。

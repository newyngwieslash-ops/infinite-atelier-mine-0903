import io

p = 'README.md'
s = io.open(p, encoding='utf-8').read()

# 1. The skills bullet: both packs are real documents now.
old_skills = '''WP-07 交付时两份文档都只是带 §4.3 十三个小节的骨架（范围 16 的要求），**WP-08 填实了 script 包的全部 8 份**，production 包的 9 份仍为骨架（WP-09/11 的范围）；'''
new_skills = '''WP-07 交付时两份文档都只是带 §4.3 十三个小节的骨架（范围 16 的要求），**WP-08 填实了 script 包的全部 8 份，WP-09 填实了 production 包的全部 9 份**，两包现在都是真实文档；'''
assert old_skills in s, "skills anchor"
s = s.replace(old_skills, new_skills, 1)

# 2. The WP-09 section, inserted before the usage instructions.
WP09 = '''WP-09 实现 Production Agent 层：资产、导演与分镜（**不接入任何付费 Provider**）：

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

'''

anchor = '## 使用说明'
assert anchor in s, "usage anchor"
s = s.replace(anchor, WP09 + anchor, 1)
io.open(p, 'w', encoding='utf-8').write(s)
print("README updated")

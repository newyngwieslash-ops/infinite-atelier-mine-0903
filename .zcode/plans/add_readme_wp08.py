import io

p = "README.md"
s = io.open(p, encoding="utf-8").read()

# 1. The WP-07 block's skill claim is now stale for the SCRIPT pack: WP-08 filled it.
old = "- **Agent 清单来自内置 Skill Pack**：`skills/script`（8 个 agent）与 `skills/production`（9 个）由 `scripts/gen-skill-packs.mjs` 生成，每个文档带 AGENT_CONTRACTS §4.3 的十三个小节且**无业务内容**（范围 16 明确要求）；清单是 JSON 而非 §4.1 所示 YAML——不为一个文件引入依赖，且 YAML 的别名展开是清单这种授权文件的真实攻击面（ADR-0011 §2）；"
new = "- **Agent 清单来自内置 Skill Pack**：`skills/script`（8 个 agent）与 `skills/production`（9 个）；清单是 JSON 而非 §4.1 所示 YAML——不为一个文件引入依赖，且 YAML 的别名展开是清单这种授权文件的真实攻击面（ADR-0011 §2）；WP-07 交付时两份文档都只是带 §4.3 十三个小节的骨架（范围 16 的要求），**WP-08 填实了 script 包的全部 8 份**，production 包的 9 份仍为骨架（WP-09/11 的范围）；"
assert old in s, "wp07 skill claim"
s = s.replace(old, new, 1)

# 2. The WP-08 section, after the WP-07 block.
anchor = "## 使用说明"
section = '''WP-08 实现 Script Agent 层：骨架、策略与剧本三个阶段（**不接入任何付费 Provider**）：

- **三层各自独立调用**：每个阶段都有 Execution 与 Supervision 两个 agent，外加 `script.decision`，共 8 个；三段产物分别落到 `story_skeleton_versions`、`adaptation_strategy_versions`、`script_versions`；
- **八份真实 Skill 文档**（`skills/script/**`）：不再是骨架，而是每个 agent 的职责、信任边界、工具白名单、步骤、领域约束、质量规则、失败条件与示例。`scripts/gen-skill-packs.mjs` 改为**只写缺失的文件**——否则再跑一次生成器会抹掉它们；`--check` 对清单逐字节比对，对文档只检查 §4.3 的十三个小节是否齐全（文档是「写」的，不是「生成」的）；
- **整版本一次写入**：`script.create_script_structure` 在一个事务里写入一个版本的全部场次、对白与镜头，边界显式（200 场 / 每场 500 行 / 200 镜）且**超界拒绝而不是截断**；payload **不接受 ID、序数或总时长**——序数是数组位置，时长由场次求和（§17 把「ID、顺序和唯一性」「时长求和」放在代码一侧，见 ADR-0012 §1、§3）；
- **锁定字段在写入路径强制**：迁移 `000017` 新增 `script_version_field_locks`，三个版本族共用一张表（`version_id` 故意不是外键——三个族在三张表里，由写入路径保证诚实）。AC-SCRIPT-002 的场景是**骨架**缺结尾钩子，所以锁必须落在骨架字段上而不只是台词；新版本的锁定字段与基准版本不等即**拒绝**，无论模型是否“理解”了提示（ADR-0012 §2）；
- **两张 link 表终于有了写入者**：骨架选中的事件、策略对每个事件的处理（保留/删除/重排）随版本在**同一事务**写入；§7.4/§7.5 把这两个集合定义为产物本身的一部分，缺了它们版本就是“什么都没决定的策略”（ADR-0012 §7）；数组顺序即改编顺序，没有单独的 ordinal 字段；
- **引用存在性由服务检查**：`scenes.source_story_event_id` 与两张 link 表的 `story_event_id` **都没有外键**（引用是出处，比被引用的行活得久），所以“这个事件存在吗”是服务的职责，拒绝时**点名缺失的 ID**，便于模型自我修正；
- **阶段→Agent 映射是显式的**：`Registry.SupervisionFor` 用 key 的**末段**匹配阶段名，对 `script_generation` **必然失败**（它的监督者是 `script.supervision.script`）。修法不是放宽启发式——这一对没有任何可匹配的共同片段，只能**写下来**；测试断言注册表在**能回答的地方**与映射一致，并断言那个不一致**正是唯一的那一处**（ADR-0012 §4）；
- **FIX 回路从决策行读回**：用户在闸门上点 FIX 时写的 findings 存在 `user_gate_decisions.issue_ids_json`，下一次尝试从**数据库**读回并渲染成 prompt 的一层（`fixIssueIds`），锁定的字段同样（`lockedRefs`）。§7.3 说这两个值「由运行时从数据库提供，Agent 不能添加」——线程传参会在两次调用之间的一次重启后丢失，而那时「针对具体问题重跑」就变成了空话（ADR-0012 §4）；
- **闸门批准的是产物**：`approve` / `manual_edit` / `skip` 必须指名它让哪一版生效，且**先批准产物再移动阶段**——顺序如此，失败时留下的是「可以重试的状态」，而不是「阶段已通过、却没有任何版本被批准」。这条是 canary 找出来的：原实现只移动阶段，于是工作流可以报告所有阶段通过，而项目里没有任何版本生效（ADR-0012 §7）；
- **模型策略按层解析**（§13）：命名 provider → 该层策略 → `default` → 第一个启用的 provider；策略指向**已禁用** provider 时**拒绝**而不是跳过（静默换一个会把提示词发到项目所有者没选的地方）；FR-140 的按**阶段**键记为延后，理由见 ADR-0012 §5；
- **版本 diff 与画布投影**：diff 是纯函数、按 ordinal 对齐，每个条目带锁定状态；投影把版本的场次写成真实画布节点——WP-05 建好这个写入者却**没有调用者**，WP-08 补上了组合根里的 projector（ADR-0012 §11）；
- **Script 分区做实**：三个阶段的运行按钮、三族版本历史与批准、内容查看、锁定开关、版本 diff、闸门决策（PASS/FIX/REDO/MANUAL_EDIT/skip，skip 要求理由）与投影；
- **Canary（关键验收）**：骨架 → 监督 → PASS → 策略 → 监督 → PASS → 剧本（版本行 + 结构）→ 监督 → PASS → `approved`，全部跑在**真实迁移过的数据库**上，只由确定性 Mock 驱动；同时覆盖 AC-SCRIPT-001/002/003（approved 唯一性用 SQL 计数断言、锁定字段正向反向、序数负面用例、时长求和、版本 diff、画布投影、已批准版本拒绝写入）；
- **`testdata/canary-drama/` 补齐 §18.1 要求的三份**：已批准事实、期望骨架关键点、**故意错误剧本**（五处错误各自点名它破坏的规则，且每处都被单独喂给校验器验证）；
- 本包**不包含**：Production/Storyboard Agent（WP-09/11）、真实付费 Provider 调用、媒体生成、Semantic Memory（WP-10）、按阶段（而非按层）的模型策略、极长剧集的流式结构传输（ADR-0012 §1 记为范围外）。**两轮独立评审**（一轮对账规范、一轮 148 个变异）发现并修掉的问题逐条记在 `docs/adr/0012-script-pipeline-payload-locks-and-stage-map.md`；验收明细与**本包未被测试覆盖的三处**见 `docs/implementation/STATUS.md` §0i。

'''
assert anchor in s, "anchor"
s = s.replace(anchor, section + anchor, 1)
io.open(p, "w", encoding="utf-8", newline="\n").write(s)
print("README updated")

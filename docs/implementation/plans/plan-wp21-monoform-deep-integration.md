# WP-21 — MONOFORM 深度双向集成（P3 第 19 项）

依据：PRD FR-060（`PRD.md:613-645`，四句「需求」＋三句「验收」）、ARCHITECTURE §17
（`docs/ARCHITECTURE.md:937-953`，受控子应用的九条）、SECURITY §12（MONOFORM 与 WebView）、
ACCEPTANCE 的 `| MONOFORM | 保留 | 基础桥 | 深度集成 |` 一行、STATUS §0j（基础桥的交付记录）。

## 0. 侦察结论（每条有证据）

| FR-060 的那句 | 现状 | 证据 |
|---|---|---|
| 「从 Shot/StoryboardPanel 打开预演」 | **已通** | `director-panel.tsx:93` 发 `open_shot`；`director-view.tsx:312` 接 `onShotUpdated` |
| 「发送角色站位、相机、镜头参数和场景参考」 | **部分**：`open_shot` 带 shot 的 framing 与 camera，**不带角色站位，也不带场景参考** | `monoform-bridge.ts` 的 `MonoformOpenShot` 形状 |
| 「保存预演快照和摄像机参数」 | **摄像机通了，快照没有**。`shot_updated` 的 `thumbnail?: Blob` 被桥**校验**、被面板**转发**，然后 `director-view.tsx:145` 的 `reportCamera` 参数类型**把它窄掉了**——全仓 studio 组件里 `thumbnail` 零命中 | 实测：`grep thumbnail web/src/components/studio/*.tsx` 无结果 |
| 「将结果写回 DirectorPlan 或 ShotVersion」 | **写回 DirectorPlan 通了**（`shot_overrides_json`），**写回 ShotVersion 没有**：previs 相机只落在 plan 的 overrides 文档里 | `reportCamera` 的注释自己写明「It does NOT go through the item update」 |
| 「失败时不影响主项目数据」 | **已通**：桥的失败是消息级的拒绝，写回失败只 `message.error` | `monoform-bridge.spec.ts` 的 12 个用例 |
| 「所有跨 iframe 消息校验来源、类型和 Schema」 | **已通**（WP-09）：`event.origin` + per-mount nonce + schemaVersion 精确相等 + 8 MiB 上限 | `monoform-bridge.ts:1-70` 的长注释即该次交付的记录 |
| 「不授予与功能无关的浏览器权限」 | **已通**：`allow` 属性收缩过 | ADR-0013 §7 |
| ARCHITECTURE §17「返回 Camera Pose、Lens、Framing、Movement、Snapshot、Notes」 | **四缺二**：Pose/Lens/Framing 通了；**Movement 与 Snapshot、Notes 没有** | `MonoformCamera` 只有 position/rotation/focalLength/aspectRatio |

**结论**：这个包不是「建一座桥」（桥在 WP-09 已建、已验证），而是**把桥的另一半做实**：
**快照落地、Movement/Notes 过桥、场景参考进去、写回 Shot 的那条路**。

## 1. 裁定

1. **快照（Snapshot）走新的 binding，落成资产版本的一个 `reference` 文件**，而不是新表。
   `asset_files` 的角色里已有 `reference`/`thumbnail`；快照是「这次预演的图」，本来就属于
   「Shot 的资产版本有什么」。**资产聚合本来就是为这件事存在的**（DOMAIN_MODEL §8.4/§8.6），
   新开一张 `previs_snapshots` 表会是第二个版本体系。
2. **快照的字节走 base64 分块**（Wails 只能传文本），复用 import upload 已验证的形态：
   开始 → 追加有界分块 → 结束提交 → 返回哈希与 storage key。**上限照 import 的 ChunkBytes 与总量**，
   不新发明一套。
3. **`reply` 里带回来的 storage key 用 `ReadResultFile` 读回**——它按内容寻址读任意 key，
   正是为此存在的读路径，不新建一个「读文件」binding。
4. **Movement 与 Notes 加进 `MonoformCamera` 的载荷**（ARCHITECTURE §17 明写要返回它们），
   作为**可选字段**：老版本 studio 不发它们，校验必须继续接受。
5. **场景参考：`open_shot` 增加 `sceneReferenceAssetVersionIds`**，取自该 shot 已批准的
   场景/地点资产版本。**只发引用 id，不发字节**——ARCHITECTURE §17「只接收 Shot、资产引用和已批准参数」。
6. **写回 Shot：快照 + 相机一起进 `shot_overrides_json` 的同一项**，并**显式记录不写 ShotVersion**。
   PRD 的「DirectorPlan **或** ShotVersion」是一个选择，不是两个都做；plan 是当前唯一有 per-shot
   文档的地方（`SetShotOverrides`），因此选它并写下理由。这是本 ADR 最需要被反驳的一条。
7. **前端**：director 分区显示本次预演的快照缩略图（读回 storage key → data URL），
   并在保存成功后把它与相机参数并列展示——这正是「保存后可在 Shot 中看到摄像机参数和预览图」。

## 2. 实现

- `internal/desktop/monoform_binding.go`（新）：`BeginSnapshotUpload` / `AppendSnapshotChunk` /
  `FinishSnapshotUpload`，复用 import upload 的分块与限额；提交后 `AttachFile(role=reference)`。
  **它不解析图像、不生成缩略图**：快照就是 studio 给的字节。
- `web/src/services/desktop/monoform-bridge.ts`：`MonoformCamera` 增 `movement?`、`notes?`；
  `MonoformOpenShot` 增 `sceneReferenceAssetVersionIds`；**校验继续接受缺省**（老 studio）。
- `web/src/components/studio/director-view.tsx`：`reportCamera` 的签名收下 thumbnail，
  走新的提交路径，并把 storage key 写进 overrides 文档。
- `web/src/services/desktop/monoform.ts`（新）：三个包装器 + 可用性探针，沿用本仓
  「查询返空、命令抛错」的分工。

## 3. 测试

- **桥的校验**（纯函数，`node:test`）：movement/notes 可选、类型正确才接受；场景参考 id 数组；
  缺省仍通过（**老版本兼容是这一条的全部意义**）。
- **提交路径**（Go）：分块边界、总量不符拒绝、非图像 MIME 拒绝、未 begin 就 append 拒绝、
  abort 后 finish 拒绝；提交成功后 `asset_files` 真的多了一行且 role 是 `reference`。
- **读回**：提交返回的 storage key 能被 `ReadResultFile` 读成同一串字节（round trip）。
- **失败不影响主项目**：提交失败时 overrides 文档**不变**（写回在提交成功之后）。
- 变异：分块上限、总量校验、MIME 校验、abort 语义、round trip 各一。

## 4. 明确不做

- **不把 previs 相机写进 ShotVersion 的列**：`shots` 表没有相机列，加列要迁移且与 plan 的
  overrides 成为两个真相源；PRD 的「或」是选择。
- **不做 studio 自己的改动**（`web/monoform-studio/`）：它是受控子应用，本包只改**协议与宿主**；
  studio 端发 movement/notes 是它的事，宿主「接受」即可。
- **不实现 Webhook/独立 Wails Window**：ARCHITECTURE §17 说后者「需 ADR」，而它不在本项范围。
- **不做导出到外部剪辑格式**：那是 v1.5 的条款。

## 5. 风险与诚实声明

- **快照的体积**：一张 1920×1080 的 PNG 可能几 MB。分块路径按 import 的既有上限走，
  超限**拒绝并说明**，而不是静默截断。
- **studio 当前不发 movement/notes**：所以那两个字段在真机上会长期缺省。测试用**构造的消息**
  证明宿主接受它们，并如实写明 studio 端尚未发送。
- 本包不加迁移。

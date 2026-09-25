# WP-28 — 首尾帧 UI 选择器与批量镜头提交（P3 第 21 项剩余两半）

依据：`docs/implementation/project-progress-and-remaining-tasks-2026-09-23.md` 第 21 项、STATUS §0zd
「What item 21 does NOT close, stated rather than implied」、PRD FR-080 视频四句、ADR-0027。

## 0. 侦察结论（每条有证据）

| 事实 | 证据 | 后果 |
|---|---|---|
| **首尾帧管道实际是断的** | 探针：runner 填 `ImageInput.Data`（data URL），`Bytes` 为空；`openai_video.go` 只读 `Bytes` → 送出的 `image_url` 是 `data:image/png;base64,`（**空数据 URL**） | STATUS §0zd 写「the PIPE is complete」**是错的**。管道端到端只传了一个空串 |
| **既有测试看不见它** | 每个参考图测试都用 `Bytes` 直接构造 `ImageInput`——**没有真实调用者这样做**（runner 是唯一构造者，它填 `Data`） | 「测试自造输入」的形状：适配器被拿去和自己比 |
| **修法有既定约定** | `openai_image.go:189-193`、`gemini_image.go:109-112` 都是 `payload := Bytes` → 空则解 `Data` → 仍空则 `NewInvalidInputError` | 视频适配器只是漏了这条约定，不是需要新设计 |
| **已批准面板图的哈希就是 `ReadResultFile` 接受的键** | `mediaHash` 是 SHA-256 十六进制（`filestore` 用 `sha256`），`isStorageKey` 要求 64 位小写十六进制 | UI 可以按哈希取回帧字节，**不需要新绑定** |
| **`ReadResultFile` 返回 data URL** | `JobResultFileContent{MIME, DataURL, Size}`，上限 64 MiB | 正是 `SubmitVideoJobRequest.References/FirstFrame` 想要的形状 |
| **时间线每行都带 `mediaHash`** | `TimelineShotDTO.MediaHash`（`media_binding.go:192`）；`BoardFacts` 已经 JOIN 出 `f.file_hash` | 选择器不必新增读取 |
| **批量提交没有先例可复用，但有边界先例** | `maxImageBatch = 8`（`jobs_binding.go`），注释「bulk work has a cost, and SECURITY requires an explicit limit」 | 批量必须有自己的上限 |
| **`SubmitVideoJob` 已有全部字段** | `SubmitVideoJobRequest` 有 `References/FirstFrame/LastFrame` 及 MIME 对，runner 按 FR-080 顺序组装 | 只需让界面**产生**它们 |
| **`video-view.tsx` 已声明不发这些字段** | 注释：「`references`/`firstFrame`/`lastFrame` … this section has no picker that could produce them」 | 本包就是补这个 picker |

## 1. 裁定

1. **先修管道，再做界面。** 一个选择器接到空数据 URL 上，是把「界面做到了」写在一个不工作的能力上——STATUS §0zd 已经这样错过一次。修法沿用适配器约定（`Bytes` → `Data` → 拒绝），并加一条**用 runner 的形状**的回归测试。
2. **帧来自已批准的面板图，按 `mediaHash` 取。** 复用 `readResultFile`（它已经校验 64 位十六进制、没有路径参数），**不新增绑定**：新增一个「给我某镜头的帧」的调用会是同一事实的第二个答案。
3. **首帧/尾帧各自可空，且默认都空。** 用户不选就是不选：空字段被**省略**（`omitempty`），不是空串——空串与「无参考」在协议上不可区分，这正是 §0zd 已经点名的陷阱。
4. **只提供该镜头已批准的面板图，不提供任意文件选择。** PRD 说的是首尾帧，而「帧」在这条链路里有确切来源：该镜的面板图。让用户上传任意图是另一个能力（参考图），不在本包。
5. **批量提交沿用 `RunImageBatch` 的形状**（请求列表 + 每项结果 + 上限），上限**小于等于 `maxImageBatch`** 的理由要写出来：视频按秒计费，一次批量比一次图片批量贵得多。
6. **批量是「为多个镜头各提交一个任务」，不是「一个任务生成多镜」。** 后者在 provider 协议里不存在，而前者正是已有 `SubmitVideoJob` 的重复调用——每一项失败**不回滚其他项**，结果逐项报告（这是 `RunImageBatch` 已确立的语义）。

## 2. 实施

### 2.1 修管道（先做，独立可验）
- `openai_video.go`：`videoSubmitBody` 改为 `(map[string]any, error)`；参考图字节按 `Bytes` → `decodeImageData(Data)` → 拒绝 解析；`Submit` 在**发出请求前**构建 body 并把拒绝如实返回 + 审计。
- 回归测试：`TestAReferenceTheRunnerBuiltTravelsAsBytes`（用 runner 的形状）、`TestAReferenceWithNoBytesIsRefusedRatherThanSent`。

### 2.2 帧读取客户端
- `web/src/services/desktop/frames.ts`（新）：`loadShotFrame(mediaHash)` → 复用 `readResultFile`，返回 `{ dataUrl, mime, size }` 或 null。
- 上限与拒绝**沿用已有的**（64 MiB、64 位十六进制），不重新实现。

### 2.3 首尾帧选择器（`video-view.tsx`）
- 选中镜头后显示该镜**已批准面板图**的缩略图 + 两个开关：「用作首帧」「用作尾帧」（互斥选择同一张图时的处理要明确：允许同一张，那是合法输入）。
- 提交前按需取字节；取不到就**不发送该字段**并在 UI 说明原因，而不是发空串。
- i18n 双语。

### 2.4 批量镜头提交
- `internal/desktop/media_jobs.go`：`SubmitVideoBatchRequest{ProjectID, EpisodeID, ProviderID, Model, Prompt, Seconds, Size, ShotIDs []string}` → `SubmitVideoBatchResultDTO{Submitted []VideoBatchItemDTO, Refused []...}`。
- 上限 `maxVideoBatch`，理由写在常量旁。
- 逐项：空/重复/超上限的镜头 id 被**拒绝并说明**；提交成功的返回 job id；**幂等**由既有的 scope 提供。
- 前端：镜头表加多选 + 「为选中镜头生成」按钮，结果逐项显示。

## 3. 测试与验证
- 适配器：上面两条回归 + 既有 12 条全绿。
- 绑定：批量的上限、逐项拒绝、幂等、空列表。
- 前端：`frames.ts` 的缺席契约（无核心返 null）；批量请求的形状。
- 变异：管道解析（Bytes/Data/拒绝三支）、批量上限、逐项报告。
- 全量门 + `wails generate module`。

## 4. 明确不做
- 任意参考图上传（裁定 4）。
- provider 侧的「一次生成多镜」（裁定 6）。
- 尾帧的实际 provider 语义差异（ADR-0027 已记录协议字段名是假设）。

## 5. 风险
- **最大风险：UI 做到了、管道仍不工作。** 缓解：先修管道并留下用 runner 形状的回归测试，再动界面。
- **次大风险：帧字节让 job input JSON 变大。** 一帧 PNG 通常几十到几百 KB，base64 后 ×1.33；本包**不新增**字节上限，因为 `SubmitVideoJob` 既有的参考上限是**数量**（8）而非字节——但要把这一点写进 ADR，并让 UI 只发**已批准的单张面板图**，不给用户拼出超大 payload 的路径。
- **第三风险：批量放大成本。** 上限 + 逐项可见 + 提交前确认。

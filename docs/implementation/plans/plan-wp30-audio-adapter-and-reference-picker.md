# WP-30 — 音效生成适配与样式参考图 picker（FR-080 的最后两个子项）

依据：STATUS §0zg「What remains open: 音效 GENERATION … Style references have no picker. Both stay named
rather than counted」、ADR-0029「What is still NOT delivered」、PRD FR-080 音频 V1 首句「音效建议与生成
适配」与视频 `references` 字段、ADR-0027（协议字段名是假设）、ADR-0028（建议与采纳路径已交付）。

## 0. 侦察结论（每条有证据）

| 事实 | 证据 | 后果 |
|---|---|---|
| **音频端口只解析 mock** | `registry.go:289-297`：`case provider.KindMockMedia` 返回 mock，**`default` 一律 `unsupported`** | 与 WP-26 修视频前的形状**逐字相同**——真 TTS 没有路径 |
| **`CapabilityAudio` 从未被写过** | 常量存在（`provider.go:84`）、CHECK 允许（`000003:68`）、`grep` 全仓**无任何写入者** | 与视频审计位同形：一个查询「某项目调用过哪些语音」永远返回空，无论调用过多少次 |
| **音频是同步端口** | `AudioPort.GenerateAudio(ctx, req) (AudioOutcome, error)`——一次调用拿结果，**没有 Poll/Fetch** | 与视频的异步形状不同，适配器更接近 `openai_image.go`（同步：请求→响应体→内联或 URL） |
| **`AudioOutcome` 有两条路** | `Data`（内联 base64）/ `URL`（远程，走 `DownloadAndCommit`）/ `Bytes` | 适配器两条都要支持，URL 那条复用已有的下载策略 |
| **runner 已把音频 MIME 兜底为 `audio/mpeg`** | `runner.go:421-423` | 适配器应如实报告它收到的类型，而非依赖兜底 |
| **`openai_image.go` 是同步适配器的样板** | `buildGenerationBody` → `endpointFor` → `authorize` → `client.Do` → 状态映射 → 解析 → 审计（8 处） | 音频适配器沿用同一套 SSRF 客户端、错误分类、审计与 Usage 解析 |
| **样式参考图没有 picker** | `video-view.tsx` 的注释：「`references` … this section has no picker that could produce them」；`references` 仍不发送 | 视频的参考图能力在界面上不可达 |
| **已有可复用的图片取字节路径** | WP-28 的 `frames.ts`：按 `mediaHash` 用 `ReadResultFile` 取 data URL，**不新增绑定** | 参考图 picker 复用同一条读取，不发明第二条 |
| **资产库可枚举** | `ListAssetsRequest{Types}` → `[]AssetDTO`（`assets_binding.go:249`） | picker 可以从项目已批准的图片资产里选，而不是让用户手输哈希 |
| **音效用法已闭环** | `UsageRoleAudioEffect` / `AudioRoleForUsage` / `defaultGainFor(Effect)=1`（WP-27）；`SuggestEffects` 产出建议 | 本包只需让**生成**可达：提交一个音频任务并记 `audio_effect` |

## 0b. 侦察追加：第三个缺口（比前两个更严重）

| 事实 | 证据 | 后果 |
|---|---|---|
| **TTS 任务的结果从不变成资产版本** | `AttachJobResult` 有 Wails 绑定（`assets_binding.go:625`）与真服务，但**没有任何前端调用者**；`audio-view.tsx` 的任务表只**列出** `resultFiles` | 「生成对白 → 听得到」这条链在中间断了：任务的字节存进了文件库，**却从未成为混音读取的那个版本** |
| **WP-11 走查自己的注释就在说这件事** | `attachAudioVersionForWalk` 的注释：「writes the asset, version, file and usage **a TTS job's result leaves**」——而没有任何代码产生它 | 走查用手写的行验证了一条**生产里不存在的路径**。这与 WP-29 的 D2/D3 同形，且更彻底：整条链只有测试在跑 |
| **`audio_effect` 从未被写过** | WP-27 的 `effects.go:34` 写「the act of accepting submits a real audio job carrying `usage_role = 'audio_effect'`」——`grep` 全仓无写入者 | 采纳一条音效建议**不会**产生一个音效用法，因为没有任何代码把任务结果变成版本 |

**所以本包的三件事有先后**：没有「附上结果」这一步，音效生成与样式参考图都只是把能力接到一个断掉的链上。裁定 8 记这一条。

## 1. 裁定

1. **音效生成本身不是新协议。** FR-080 说「音效建议与**生成适配**」：音效是一次 TTS 风格的调用（文本→音频字节），与对白走**同一个 `AudioPort`**。新增一个 `EffectPort` 会是同一事实的第二个接口，而 runner 已经能提交、校验、提交结果并记 usage——`usage_role` 才是区分对白与音效的地方。
2. **适配器是同步的，照 `openai_image.go` 的形状写。** `POST {base}/audio/speech` → 响应体是字节或 JSON（数据 URL / URL）。协议形状与 WP-26 一样**记为假设**：没有授权的真供应商可比对。
3. **kind 复用 `openai_compatible`。** 理由与 ADR-0027 §2 逐条相同：协议族相同，新增 kind 要迁移而只买到命名。
4. **审计写 `CapabilityAudio`，本仓第一次。** 与 WP-26 写 `CapabilityVideo` 同理：列存在、无人写、查询永远为空。
5. **适配器解析 `Bytes` → `Data` → 拒绝**，三条路与 `openai_video.go` 修好后的形状一致——**这个约定现在有三个实现者**，所以它是一条约定而不是巧合。
6. **样式参考图从项目已批准的图片资产里选，按 `mediaHash` 取字节。** 复用 WP-28 的 `frames.ts`；**不新增读取绑定**。让用户上传任意图是另一个能力（素材导入），不在本包。
7. **参考图上限沿用绑定已有的 `maxMediaReferences`（8）**，并在 UI 里说明。
8. **先补「附上结果」这一步，再做生成与参考图。** `audio-view.tsx` 的任务行对已完成的任务提供「附为音频版本」：调 `attachJobResult` 建/取音频资产与版本，再调 `addUsage` 记 `(shot, audio_dialogue|audio_effect)`。**这一步是链路的接缝**，音效生成与对白生成共用它；顺序不能颠倒，否则是把两个新能力接到一个断点上——ADR-0030 的教训正是「测试全过而真实装配不可能成功」。

## 2. 实施

### 2.1 音频适配器（Go）
- `internal/infrastructure/providers/openai_audio.go`（新）：`OpenAIAudioAdapter`。
  - `POST {base}/audio/speech`，体为 `{model, input, voice, response_format, speed}`；
  - 响应按 `Content-Type` 分流：`audio/*` → 内联字节（有上限）；`application/json` → 解析 `{url}` 或 `{data/b64_json}`；
  - 复用 `guardedClient`（不新开 client 路径）、`authorize`、`mapHTTPStatus`、`statusForError`、`audit`；
  - 空文本、空字节、超限**拒绝**；
  - `audioMaxInlineBytes` = 传输层上限（与 WP-26 同一个理由：`guardedClient` 的 10 MiB 会先截断）。
- `registry.go`：`WithOpenAIAudioAdapter` + `AudioPortFor` 增加 `openai_compatible` 分支。
- `provider_wiring.go`：注册。

### 2.2 音效任务（Go）
- 复用 `SubmitAudioJob`：它已有 `usage_role` 的落点吗？——**查**。若没有，加一条「提交时声明角色」的路径，让音效任务记 `audio_effect`。

### 2.3 样式参考图 picker（前端）
- `voice/reference` 客户端：`listReferenceCandidates(projectId)`（已批准图片资产）+ 复用 `loadShotFrame` 取字节。
- `video-view.tsx`：参考图多选区（最多 8），提交时带 `references`/`referenceMimes`。

### 2.4 音效生成（前端）
- 采纳建议时**直接提交一个音效任务**（已有音效建议的采纳路径），并把 `usage_role` 记为 `audio_effect`。

## 3. 测试与验收
- **适配器**：httptest 假供应商，两种响应形态、错误矩阵、空响应拒绝、超限拒绝、按键不泄漏、审计能力位；**用 runner 的形状**（Data/Bytes）的回归。
- **注册**：`AudioPortFor` 对 `openai_compatible` 解析到真适配器，mock 仍解析到 mock。
- **端到端**：真适配器穿过真 runner（WP-26 的 `video_provider_e2e_test.go` 是样板）→ 断言 MIME 被结果库接受、任务落 Succeeded。
- **前端**：picker 的缺席契约、上限、参考图字节送达。
- **变异**：解析三支、状态映射、审计能力位、注册解析、上限。

## 4. 明确不做
- 真付费 provider 调用（无授权）。
- 任意素材上传（裁定 6）。
- 音效的**分离协议**（裁定 1）。

## 5. 风险
- **最大风险：假设的协议字段名。** 与 ADR-0027 同。缓解：ADR 记明，测试证明的是协议处理。
- **次大风险：参考图字节变大 job input。** 上限沿用既有的数量上限（8），且只允许选**已批准**的资产。
- **第三风险：音效与对白在 UI 上混淆。** 缓解：二者用 `usage_role` 区分，混音用各自的增益。

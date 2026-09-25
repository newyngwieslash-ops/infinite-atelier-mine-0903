# WP-26 — 真实视频 Provider 适配器（P3 第 21 项）

依据：PRD FR-080 的视频四句（「Provider 适配器支持提交、轮询、Webhook（若供应商支持）、取消和结果下载」、
「任务状态必须持久化」、「远程结果下载到本地资产存储后才能标记为完整成功；若仅保留远程 URL，状态为
`remote_only`」、「同一 Shot 可保留多个视频版本并批准其中一个」）、ARCHITECTURE §17、SECURITY §7（SSRF /
出口策略）、STATUS §0y（Mock 视频的记录）、ADR-0015（导出的 subprocess 与 Mock 视频）。

## 0. 侦察结论（每条有证据）

| 事实 | 证据 | 后果 |
|---|---|---|
| **真视频适配器不存在** | `registry.go:215-225` 的 `VideoPortFor`：`mock_media` 返回 Mock，**其余一律 `unsupported`**，注释写着「A real async video adapter does not exist yet」 | 这就是要建的东西 |
| **端口形状是同步的，而视频是异步的** | `VideoPort` 四方法：`Submit`/`Poll`/`Fetch`/`Cancel`（`ports.go:154-159`） | 端口**已经**是异步形状，不用改 |
| **`RemoteJob` 只有 `{ProviderID, ID}`** | `ports.go:173-176` | 没有 status URL、没有过期时间——真适配器必须把它们编进 `ID` 或重新推导 |
| **真实适配器的测试模式已成熟** | `openai_image_test.go` 的 `newImageTestAdapter`：`httptest.NewServer` + `clientFactory` 注入 + `AllowLocal` 策略 | **无需付费凭据即可端到端测**（这正是 P3-21 可做的前提） |
| **SSRF 防线已经存在** | `providerhttp/client.go`：逐请求 URL/host/port 校验、禁 env proxy、TLS 1.2+、跳转复检并剥 `Authorization`、5 min 超时、10 MiB 上限 | 适配器**必须**用它，不能自己开 `net/http` |
| **审计能力位已允许 video** | `provider_requests.capability` 的 CHECK 已含 `'video'`（`000003_jobs.sql:68`），`provider.CapabilityVideo` 已定义 | 审计**不需要迁移**，但**没有任何适配器写过它** |
| **kind 的 CHECK 挡着真 provider** | `provider_configs.kind` 只允许 `openai_compatible`/`gemini_compatible`/`mock_media` | 需要一个**新 kind**（迁移）或复用已有 kind 加一个分支 |
| **轮询节奏与恢复已建好** | `worker.go` 的 `pollPass`、`PollOnly` 不消耗尝试预算、`remotePollInterval`；`recovery.go` 重启只 poll 不重投 | 真适配器**继承**这一切，不用重写 |

## 1. 裁定

1. **复用 `openai_compatible`，不新增 kind。** 理由：视频的 HTTP 形状（JSON POST → 拿 id → 轮询 → 下载）
   与 OpenAI 兼容的文本/图片是同一族；新增 kind 要迁移 + 改 `IsUserConfigurableKind` + 改校验，
   而**价值只在命名上**。风险是「用文本 provider 提交视频」——由 `VideoPortFor` 的 kind 分支决定，
   并由**能力探测**（§2.4）如实拒绝，而不是靠一个 kind 名。
2. **适配器只实现一种真实的异步协议形状**：`POST {base}/videos` → `{"id": "..."}`；
   `GET {base}/videos/{id}` → `{"status": "...", "progress": n}`；
   `GET {base}/videos/{id}/content` → 字节或 302 到 CDN。
   这是 OpenAI 视频 API 的形状，也是本包能被 `httptest` 验证的形状。
   **协议写进 ADR，因为它是一个假设**：真供应商各不相同，本包交付的是**一个可工作的适配器 + 一条已被
   走通的路径**，不是一个万能抽象。
3. **状态词汇映射成已有的 `RemoteStatus{Done, Failed, Message, Progress}`**，映射表在适配器里、
   写成数据而不是散落的 if。未知状态**拒绝**而不是当成 running：一个永远 running 的任务会让
   轮询无限进行，而 fail closed 会把它交给重试与人工。
4. **`Fetch` 两条路都支持**：provider 返回**内联 base64**（小文件）或**URL**（大文件，走已有的
   `DownloadAndCommit`，它已经做了 https-only、私网拒绝、字节上限）。**内联有上限**，超限拒绝。
5. **审计写 `CapabilityVideo`**，沿用 `openai_image.go:295-321` 的形状——本仓第一个写该能力位的地方。
6. **不加 Webhook**：PRD 写的是「若供应商支持」，而本地桌面**不能**为此默认开公网 listener
   （handoff 文档已裁定）。ADR 记录这个选择，STATUS 记录它仍开放。
7. **取消**：`Cancel` 发 `DELETE {base}/videos/{id}`；**失败不阻断本地状态机**——已有的
   `remote_cancel_unconfirmed` 列正是为「远程没确认取消」准备的，适配器如实返回错误即可。

## 2. 实现

- `internal/infrastructure/providers/openai_video.go`（新）：`OpenAIVideoAdapter`，四方法 + `audit`。
  - 用 `guardedClient(config)`（和图片适配器同一个），**不新开 client 路径**；
  - `clientFactory` 可注入，供 httptest；
  - 密钥解析沿用 `SecretSource`，用完清零，**不进日志、不进错误**；
  - `Submit` 把参考图按已有顺序（references → first frame → last frame）编码进请求体；
  - 状态映射表 + 未知状态拒绝；进度只在该 provider 给出时填。
- `registry.go`：`VideoPortFor` 增加 `openai_compatible` 分支 → 返回该适配器。
- `provider_wiring.go`：注册适配器（**这是「接口没有真实路径」的防线**）。
- 测试 `openai_video_test.go`：httptest 服务器演完整的 提交→轮询（两次 running）→完成→下载，
  加上错误矩阵、取消、超时、密钥不泄漏、内联上限、未知状态拒绝。

## 3. 测试（全部离线）

- **快乐路径**：submit 拿 id → poll running → poll running（断言**进度**透传）→ poll done →
  fetch 内联字节 → 断言 MIME 与内容。
- **fetch 走 URL**：服务器 302 到自己的字节端点 → 断言下载路径被走到且 MIME 正确。
- **错误矩阵**：401→unauthorized、429→rate_limited+RetryAfter、500→remote_transient、
  400→invalid_input；**都断言 `Retriable` 的值**（这决定重试）。
- **未知状态**：`{"status":"weird"}` → 适配器**拒绝**（错误），不是永远 running。
- **取消**：`Cancel` 发 DELETE；服务器 500 时适配器返回错误而**不 panic**、不重试。
- **密钥**：断言请求头带 Bearer，且**任何错误与审计里都没有密钥**。
- **审计**：每个方法写一行，`capability='video'`——**本仓第一次写这个能力位**。
- 变异：状态映射、未知状态拒绝、内联上限、审计能力位、取消方法。

## 4. 明确不做

- **真实付费调用**：没有授权（AGENTS §4.3）。本包交付**可测的适配器**，端到端到真供应商需要密钥，
  记在 STATUS 里。
- **Webhook listener**（裁定 6）。
- **多供应商协议抽象**：一个协议形状 + ADR 记录假设，不发明插件系统。
- **音频的真适配器**：本项是视频；若形状相同，同一模式可复制，但那是另一个包。

## 5. 风险与诚实声明

- **协议形状是假设**（裁定 2）。它照着 OpenAI 的视频 API 写，但**真供应商各不相同**；ADR 写明白，
  并说明换供应商要改什么。
- **没有真实密钥，所以「对真供应商可用」是未验证的**。测试证明的是**协议处理、错误分类、
  审计、SSRF 防线与取消**这些可离线验证的部分。STATUS 不会写「已对接真实视频 Provider」。

# Vendor Protocol Cross-Check (T28 — 离线部分)

日期：2026-09-26。本文件按**所选供应商的公开契约文档**逐项核对本项目
video/TTS/音效协议的字段与能力。核对公开文档**不需要**付费调用授权；
实际付费冒烟（T28 的后半）仍待授权，见 STATUS 的 BLOCKED 记录。

## 前提

本项目适配 `openai_compatible` 家族（ADR-0027/ADR-0031 已把请求体字段记为**假设**，
本文件是把这些假设与供应商公开文档对照的第一份记录）。

## Video（`internal/infrastructure/providers/openai_video.go`）

| 本项目假设 | OpenAI 公开文档（api.openai.com/v1） | 结论 |
|---|---|---|
| `POST /videos`，响应含 `id` | `POST /videos`，响应 `id` | ✅ 一致 |
| 轮询 `GET /videos/{id}`，`status`/`progress` | 同路径；状态机 `queued/processing/completed/failed` | ✅ 字段一致；本项目 mapVideoStatus 已覆盖该状态集 |
| 取回 `GET /videos/{id}/content`（字节或 302/JSON 链接） | 同路径，302 到内容 CDN | ✅ |
| 取消 `DELETE /videos/{id}`，204/404 均视为成功 | 同路径 | ✅ |
| 提交体 `{model, prompt, seconds?, size?, input_reference:[{image_url}]}` | 同形；`seconds` 取值受文档枚举约束；`size` 受分辨率枚举约束 | ⚠️ `seconds`/`size` 是枚举值而非任意数——本项目绑定端校验为范围（maxVideoSeconds=60），应按文档收紧为枚举（后续小改） |
| 首尾帧/参考图 ≤8 张、≤12MiB | `input_reference` 数量与图片大小限制以文档为准 | ⚠️ 建议以文档核对每模型限额 |

## TTS（`internal/infrastructure/providers/openai_audio.go`）

| 本项目假设 | OpenAI 公开文档 | 结论 |
|---|---|---|
| `POST /audio/speech`，`{model, input, voice, response_format?, speed?}` | 同路径同字段 | ✅ |
| `response_format` 枚举（mp3/opus/aac/flac/wav/pcm） | 同 | ✅ 本项目 MIME 白名单已含 audio/mpeg、audio/wav 等对应类型 |
| `voice` 枚举（alloy 等） | 同 | ✅ mock 用 alloy |
| `speed` 0.25–4.0 | 同 | ⚠️ 绑定端未校验范围，建议收紧 |
| 文本 ≤ 4096 字符（TTS-1） | 同 | ✅ 本项目 maxAudioTextRunes=2000，更保守 |

## 音效（EffectPort）

OpenAI 家族**无音效合成端点**——本项目真实适配器按 T03 设计为**诚实拒绝**
（`PROVIDER_UNSUPPORTED`），与契约核对结论一致：需另选具备音效/音频生成
端点的供应商，或走本地音频生成方案。

## 结论

- 协议路径/字段/状态机与公开文档一致（此前 ADR-0027/0031 的"假设"升级为"文档对照通过"）。
- 两处待收紧：`seconds`/`size` 枚举校验、`speed` 范围校验（小改动，下一包）。
- 付费冒烟项（提交→轮询→下载真实视频/语音，以及首尾帧实际接受度）仍需授权后执行。

# Handoff — Infinite Atelier Drama Studio（P3 完成后）

日期：2026-09-26
分支：`codex/wp-01-desktop-foundation`（ahead 171，未 push——未经授权，见「环境事实」）
HEAD：`a11e7d3`（P3 audit: nine items DONE, and the two named gaps that are not P3 criteria）
范围：本 handoff 覆盖自 WP-16（2026-09-25）起的 P3 阶段：WP-16 至 WP-30，共 15 个包；并回答一个问题——
**项目所有待办任务是否完整开发完成。**

---

# 1. 一句话状态

**P3 的九项 v1.0 范围全部 DONE（审计见 STATUS §0zi），P0/P1/P4 全部关闭，全量验证门 exit 0。**
剩余未完成的是**四类东西**：两项需要产品决策（ONNX 模型分发、视频 size 控件）、两项本机不可能
（干净 VM、CI/远程执行）、两项受 AGENTS §4.3 约束（对真实供应商的端到端验证、安装证书）、以及
两处**文档过时**（TRACEABILITY 的 FR-030 与 FR-080 两行仍带着已被 WP-15/WP-26/30 关闭的
「remains OPEN / no real adapter」措辞——见 §6，这是本 handoff 审计新发现的文档债，不是代码缺口）。

---

# 2. 本阶段交付（WP-16 → WP-30，全部已提交）

| WP | 项 | 内容 | 提交 |
|---|---|---|---|
| 16 | P3-20 | 完整资产规则集 + `review_issues.category` | `c0c5f45` |
| 17 | P3-18 | 事件图谱真画事件（此前是列表） | `5417002` |
| 18 | P3-22 | 层级摘要第三级 + 召回评测指标 | `eebc59e` |
| 19 | P3-24 | PDF 导入（两库探针后选定 `ledongthuc/pdf`） | `a947597` |
| 20 | P3-23 | 音频混音（修「每部导出都是默片」） | `184f119` |
| 21 | P3-19 | MONOFORM 快照落库 | `433ee1c` |
| 23/25 | P3-17 | 本地 ONNX 嵌入（多语言经镜像 + Unigram） | `d7ab686`/`d54ffff` |
| 24 | P3-25 | Windows 安装包（NSIS 已产出） | `ac7fe37` |
| 26 | P3-21 | 真实异步视频适配器 | `938ba1a` |
| 27 | P3-23 | 多角色声线映射 + 音效建议 | `3a7d6eb` |
| 28 | P3-21 | 首尾帧 picker + 批量镜头（修空 data URL） | `1deeb8c` |
| 29 | FR-080 | 背景音乐导入（修 3 个已发布缺陷） | `24d2f87` |
| 30 | FR-080 | 语音适配器 + 样式参考图（修 TTS 链路空洞） | `e7ca8a6` |
| — | P3 审计 | 九项逐一对代码核对 | `a11e7d3` |

## 本阶段最有价值的发现（每一处都写进了对应 ADR）

这一阶段修掉的**已发布代码里的真缺陷**，模式高度一致——**「每个测试都过，因为每个测试都自带了
生产的等价物」**：

1. **每个导出都是默片**（WP-20）：请求不带任何音频，而时间线报告 AudioApproved。
2. **`AudioMix.Normalized()` 没有任何调用者**（WP-27）：方法存在、有测试、零调用——所有片段以
   `volume=0` 到达引擎，导入的配乐会是静音。
3. **首尾帧送出的是空 data URL**（WP-28）：runner 填 `ImageInput.Data`，适配器只读 `Bytes`——
   管道「完整」，送的字符串是空的；所有既有测试自造 `Bytes`，而**没有真实调用者产生那个形状**。
4. **预演快照在生产中永远不可能成功**（WP-29）：存储适配器只写文件不写 `file_objects` 行，外键
   使 Link 必败；其套件用的替身自己记元数据，所以全绿。
5. **TTS 任务的结果从不变成资产版本**（WP-30）：字节入库、任务 succeeded、然后没有然后——生成的
   语音从未进过混音；WP-11 走查的注释声称它写的就是「a TTS job's result leaves」的行，而那条路径
   不存在。
6. **`maxVideoStatus` 失败态映射**（WP-26）与**音乐床从镜头偏移开始**（WP-29）——见各 ADR。

**对策已经成型**：驱动**真实组合根 + 真实数据库 + 假供应商**的边界测试
（`video_provider_e2e_test.go` / `audio_provider_e2e_test.go` / `music_wiring_test.go` /
`audio_chain_wp30_test.go`），并且每条验收走查都验证过**非空转**（把被测行为改回去，测试必须以
点名原因失败）。这是下一个读者最重要的一条方法论遗产。

---

# 3. 验证状态（handoff 时刻重新跑过，非引用旧记录）

```
sh scripts/verify.sh   → EXIT 0
  Go:        59 packages ok
  前端单测:   128 pass / 0 fail
  Playwright: 25 passed (1.0m)
  安全扫描:   705 files, 0 发现
  canary/恶意输入/工具 schema/skill 清单/SBOM: 全 PASS
  SKIP ×2:   frontend lint（无 lint script）; Wails 生产构建（本机未装钉住的 CLI v2.15.0）
```

规模参考：**1581 个 Go 测试函数**、26 个迁移（head=26）、31 份 ADR。本阶段六个包的变异验证
**全部首轮或次轮全灭**：26/26（WP-27）、14/14（WP-26）、15/15（WP-28）、16/16（WP-29）、
18/18（WP-30）——变异脚本用后即删，方法与「锚点必须唯一匹配，否则是 harness 错误」的规则写在
各包 STATUS 里。

---

# 4. 待办完整性评估（本 handoff 的核心问题）

按 `project-progress-and-remaining-tasks-2026-09-23.md` 的 P0–P4 逐层核对**代码与文档**：

## P0（产品断链）— 全部 DONE
两项（资产生产 UI、每 Provider 并发上限）由 WP-13/WP-14 关闭。

## P1（验收合同内缺口）— 全部 DONE
十项（3–12）由 WP-15 关闭，STATUS §0s 逐项给出证据。一个子项**诚实记为「本构建不具备的能力」
而非迟到的任务**：FR-180 的「清缓存」——`appdirs` 没有 cache 目录，「为了能清而发明一个缓存目录
是在造条款所述之物，不是满足条款」。

## P2（发布工程，本机之外）
| 项 | 状态 | 阻塞 |
|---|---|---|
| 13 安装包 | **工件已产出**（14.9 MB，WP-24）；缺**证书** | 需购买/提供证书——发布决定，非代码 |
| 14 干净 VM | **OPEN** | 本机不可能 |
| 15 `go test -race` | **DONE**（WP-15：本机 64 位 GCC 下零数据竞争）| 曾误判为环境不可能——实为 32 位 MinGW 在 PATH 前面 |
| 16 CI/远程执行 | **OPEN** | 未授权 push；ADR-0002 的跨平台证据仍缺 |

## P3（v1.0 范围）— 九项全部 DONE（STATUS §0zi 逐项对代码核对）
17 ONNX / 18 事件图谱 / 19 MONOFORM / 20 一致性 / 21 首尾帧与批量 / 22 层级摘要 /
23 时间线音效混音 / 24 PDF / 25 Windows 安装升级。

## P4（文档债）— 26 部分残留（见 §6），27 DONE

## 结论

**「完整开发完成」的诚实答案：代码范围内的 P0/P1/P3 待办全部完成；P2 剩两项本机外事项与一张
证书；P4 项 26 的六行中两行（FR-030、FR-080）在 TRACEABILITY 里仍有被后续包关闭却未回写的
过时措辞。**这些不是未完成的开发，而是：(a) 需要用户决策的（证书、模型分发），(b) 需要别的
机器的（VM、CI），(c) 半小时能改完的文档回写（§6 给了精确位置与替换文本）。

---

# 5. 具名开放项（全部在 STATUS/ADR 里有原文，此处汇总）

1. **两个 provider 协议是假设**（ADR-0027 视频、ADR-0031 语音）：无授权调用（AGENTS §4.3），
   验证的是协议处理、错误分类、审计、SSRF 防线、取消握手——**不是**与真实供应商的兼容性。
   换供应商 = 改一个文件（`openai_video.go` / `openai_audio.go`），不是配置。
2. **ONNX 模型文件不随仓分发**（113–448 MB），路径是 `IA_ONNX_MODEL/IA_ONNX_VOCAB/IA_ONNX_RUNTIME`
   环境变量，**无设置界面**。能力已实测（0.8020 复述 / 0.4025 跨语言 / −0.0302 无关 / canary 零
   [UNK]）。分发是 AGENTS §6 的许可证 + 仓库体积决定；设置控件是真缺口，值得单独一包。
3. **视频 `size` 字段无控件**：**没有任何 PRD 条款要求它**（FR-080 四句与 §16 八项都不含）。
   记为界面缺口，补它是加功能不是完成功能。
4. **`sqlite-vec`** 仍是可选项未做（500 条候选窗仍是召回上界）。
5. **安装证书**：安装包已能构建，签名需要证书。
6. **干净 VM 验证** 与 **CI**：见 P2 表。

---

# 6. 本次 handoff 审计新发现的文档债（下一读者半小时可清）

**这两行在 TRACEABILITY.md 里，正文声称的 OPEN 已被关闭：**

- **`TRACEABILITY.md:13`（FR-030）**正文说「the alias-merge UI … remains OPEN: … no merge command
  exists (WP-15 item 4 tracks it)」——**但同一行的「证据」列已经写着「WP-15 built the alias merge
  and its UI」**，且 `story-graph-view.tsx:254` 就在调 `mergeStoryEntity`。把正文那句改为已关闭
  即可（`MergeStoryEntity` 绑定 + UI 均在）。
- **`TRACEABILITY.md:19`（FR-080）**正文说「**PARTIAL**: no real video or audio provider adapter —
  the video one is the Mock…」——**WP-26 建了视频适配器、WP-30 建了语音适配器**，且该行后面自己
  已写了「WP-26 added the REAL asynchronous adapter」。把「no real video or audio provider
  adapter」改为指向 ADR-0027/0031 的假设声明即可。

这符合本仓「文档只写已实现事实」的规则；之所以没在本 session 顺手改，是因为 handoff 的职责是
如实报告而非在收尾时扩范围——两处都是一句话回写，建议作为下一个（很小的）文档包。

---

# 7. 环境事实（交接必读，全部实测）

```
Go 命令前缀:  PATH="/c/msys64/mingw64/bin:$PATH" GOTOOLCHAIN=go1.25.0 GOSUMDB=sum.golang.org GOCACHE=/d/go-build-cache
             （用户级 go env 设了 GOSUMDB=off，会挡 toolchain 校验；32 位 MinGW 在默认 PATH，
               必须把 mingw64 放前面，否则 cgo/race 用错编译器）
Wails 生成:    /d/GoWorks1.18/bin/wails.exe generate module
Wails 构建:    wails build -nsis（NSIS 3.12 经 winget 安装）
python:        用 `python`（`python3` 是 Windows Store 占位 stub）
verify:        sh scripts/verify.sh（上述前缀下 exit 0）
分支:          ahead 171，未 push——push 需用户明确授权
用户未跟踪文件: Infinite-Atelier-OpenCode集成必要性与完整实施方案.md（用户的，勿动）
```

# 8. 给下一个 Agent 的方法论提醒（本阶段反复验证有效）

1. **先探针后接口**：给「已存在的能力」接 UI 之前，先写一个**用 runner/真实调用者的形状**的探针
   ——WP-28/29/30 三个真缺陷全是这么发现的，而既有单测全绿。
2. **变异脚手架铁律**：锚点唯一匹配否则是 harness 错误；整包跑（`-run` 过滤会让变异躲在过滤器后
   面存活）；用后即删；恢复后逐字节 diff。
3. **每条验收走查证明非空转**：临时改坏被测行为→测试必须以**点名原因**失败→还原。
4. **文档回写别拖**：本阶段两次发现「正文已全 DONE 而标题仍写 PARTIAL」（§0zi 修正过 21/23 两
   项）——只在收尾统一回写会积累出 §6 那样的两行。
5. **完成审计要对代码，不对散文**：§0zi 的九项表就是这么做的。

# 9. 起点

```bash
cd infinite-atelier-mine-0903
python -c "import io;print(io.open('docs/implementation/STATUS.md',encoding='utf-8').read()[:3000])"  # §0zi 审计在最前
ls docs/adr/003*.md          # 本阶段 ADR-0027..0031
```

推荐下一包（若继续）：**§6 的两行 TRACEABILITY 回写 + ONNX 模型路径设置控件**（后者让第 17 项的
最后一哩可达）。均未开始，等待用户指令。

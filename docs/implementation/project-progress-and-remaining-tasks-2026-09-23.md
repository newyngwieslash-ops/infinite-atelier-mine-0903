# 项目进度与剩余任务清单（交接文档）

> 日期：2026-09-23
> 依据：`PRD.md`（19 条 FR + §15/§16/§18/§19）、`docs/ROADMAP.md`（WP-00～WP-12）、
> `docs/implementation/TRACEABILITY.md`（逐项判定）、`docs/implementation/STATUS.md` §0a–§0p
> 编写方式：每一条判定都取自上述文档的**原文**或对代码的直接核验，不凭记忆归纳。
> 姊妹篇：`project-progress-and-remaining-tasks-2026-09-08.md`（WP-05 时代的同名文档）。

---

## 1. 一句话现状

**WP-00～WP-12 十三个工作包全部 COMPLETE（各自记录的范围内）**；PRD §18 十一条发布阻断
项全部 CLEARED；全量验证门（typecheck、76 前端测试、25 Playwright、56 Go 包、vet、安全
扫描、全部 fixture 检查、SBOM 检查、Wails 生产构建）端到端通过。**但"工作包完成"不等于
"产品闭环"**：按 PRD 的验收合同逐条对照，仍有**一类产品断链、一组 PARTIAL 验收子句、
两处未实测环境项、以及全部 v1.0 范围**待做，详见下文清单。

---

## 2. 里程碑总表（ROADMAP 版本映射 × 实际状态）

| 里程碑 | 工作包 | 状态 | 证据（STATUS 节） |
|---|---|---|---|
| Baseline | WP-00 | **COMPLETE** | §3–§8 |
| Foundation Preview | WP-01～03 | **COMPLETE** | §0d、§0c、§0b |
| Compatible Core | WP-04～05 | **COMPLETE** | §0d、§0e/0f |
| Story MVP | WP-06～08 | **COMPLETE** | §0g、§0h、§0i |
| Production MVP | WP-09～10 | **COMPLETE** | §0j、§0k |
| Desktop MVP/v0.5 | WP-11 | **COMPLETE**（2 项 PARTIAL 已具名，§0l） | §0l |
| Release Candidate | WP-12 | **COMPLETE**（§0p，2026-09-23） | §0m/0m1/0n/0n2/0p |

---

## 3. FR 逐条判定（TRACEABILITY 现值 + 核验）

| FR | 主题 | 判定 | 未了结项 |
|---|---|---|---|
| FR-001 | 现有功能兼容 | Partial（WP-04 已交付导入/回归） | FR-001 自身四条验收均已由 AC-LEGACY/AC-CANVAS-004 覆盖；**判定格未随 WP-04 更新**（文档债） |
| FR-010 | 桌面运行时与 Go Core | Partial（WP-01 已交付） | **无安装包/无签名**（只有 `wails build` 的裸 exe）；断网启动、崩溃后不损坏事务**未在干净 VM 实测**；macOS/Linux 打包属 v1.5 |
| FR-020 | 项目、原著与章节导入 | Partial（WP-06 已交付 TXT/MD/DOCX） | **PDF 导入属 v1**；"UI 不冻结"仍以界限断言而非计时数据支撑（AC-STORY-001 PARTIAL 项） |
| FR-030 | 章节事件图谱 | Partial（WP-05 事实层 + WP-06 提取/确认） | **别名合并 UI 无前端调用**（FR-030 验收"用户可合并重复实体并保留别名"——后端命令存在，无界面）；事件图谱"可视化"是列表+筛选而非图 |
| FR-040 | ScriptAgent 剧本流水线 | **Delivered (WP-08)，2 子句 PARTIAL** | 修订复用 attempt 行（ADR-0012 §8）；canary 的 FIX 场景是"完整骨架上钉字段"而非"缺结尾钩子的骨架" |
| FR-050 | 资产圣经 | **Delivered (WP-09)，缺口具名** | per-stage 模型策略（用户裁定延后）；**完整资产圣经 UI 不在**（只有列表+版本+批准）；未对真实付费 Provider 生成 |
| FR-060 | 导演规划与 MONOFORM | **Delivered (WP-09)，基础桥** | MONOFORM 深度双向集成属 v1.0 |
| FR-070 | 分镜表/面板/分镜图 | **Delivered (WP-09)，1 子句未建** | **表-画布双向同步未建**（投影是单向：拖画布节点不重排分镜表） |
| FR-080 | 视频/音频/字幕/导出 | **Delivered (WP-11)，1 项 PARTIAL** | **视频 Provider 是 Mock**（路线图允许）；TTS 契约+Mock；无音效/BGM/混音/多角色声线（v1） |
| FR-090 | Agent 中心 | **Delivered (WP-07)，1 项 PARTIAL** | **无用户侧 run 触发按钮**（Agent Center 只读）；Skill 修改后历史哈希保留已实现 |
| FR-100 | Durable Workflow | Partial（WP-05/07/12 累计） | **"显式阶段依赖"未作为声明式图交付**——依赖在使用工件处逐个强制，`Layer.Stages()` 无生产调用者；`StartStage`/`CreateStage` 不查前置阶段（AC-E2E-003 walk 实测证实）。**是否满足条款属产品判断，已记录待裁** |
| FR-110 | Supervisor 与质量中心 | Partial（WP-05/07/10 累计） | **`score`/`grade` 从不填充**；**Safety 与 Cost 两类问题无任何一半覆盖**；**剧本/资产规则集无确定性半边**（只有分镜和 Final 有） |
| FR-120 | Persistent Memory | **Delivered (WP-10)，3 项界限具名** | 嵌入是 Provider/Fake 而非本地 ONNX（v1）；**语义通道候选窗=最新 500 条**（更早记忆不可达——产品决策待裁，已有测试钉住现状）；召回评测集（v1） |
| FR-130 | 画布语义化 | **Delivered (WP-05)** | — |
| FR-140 | Provider Gateway | Partial（WP-02/03/07/11/12 累计） | **Embedding、声明式 Manifest、per-Stage 模型键未实现**（ADR-0012 §5 具名延后）；legacy 浏览器直连路径保留（仅开发模式可达，安全模式抛错） |
| FR-150 | Persistent Job Manager | Partial（WP-03 交付核心） | **缩略图/导入/导出/迁移四类 job 未实现**；~~**每 Provider 并发上限未实现**~~ **已由 WP-14 实现（STATUS §0r，ADR-0018）**：`provider_configs.max_concurrency`（迁移 000022，0=不限）、dispatch 准入、数据库计数含租约有效期判据。**速率限制仍未做，已具名**（ADR-0018 裁定 8） |
| FR-160 | 存储与血缘 | Partial（内容寻址/去重/引用已在） | **垃圾回收（展示将删内容+可取消）未实现**；血缘 UI 只有资产页一列入口 |
| FR-170 | 备份/恢复/迁移 | Partial/unsafe（**判定格已过时**） | WP-12 已交付 restore 原子交换+确认+回滚（AC-BACKUP-002 PASS）；**剩余**：备份格式版本迁移测试（"旧版本有迁移测试"子句）；加密敏感备份 ADR-0016 裁定不进 v1 |
| FR-180 | 设置/日志/诊断/隐私 | Partial（WP-01/02 交付地基） | **用户设置页（三 Tab：渠道/偏好/备份）无诊断导出、无缓存清理、无隐私控制**；诊断包脱敏预览导出未实现 |

> 文档债提醒：TRACEABILITY 的 FR-001/FR-020/FR-030/FR-170 与 AC-SCRIPT"Not started"/
> AC-PROD"Not started" 六行判定**滞后于实际交付**（对应工作包的 FR 行已更新，AC 行未跟）。
> 修这六行是文档工作，不是开发工作，但下一个人会先读它们。

---

## 4. MVP 总体验收场景（PRD §19，AC-E2E-001～006）

| 场景 | 状态 | 说明 |
|---|---|---|
| AC-E2E-001 旧项目兼容 | **部分覆盖** | 导入完整/幂等/回滚/画布回归全部 PASS（Go 层 + Playwright）；**"可创建新图片任务"+"导出新格式备份且不含密钥"未作为同一场景串测**——backup-panel UI（WP-12 新增）无 e2e |
| AC-E2E-002 小说到分镜 | **分两半 PASS，未整场景串测** | 脚本半边（canary_script_test.go 至 approved Script）与制作半边（canary_production_test.go 至 approved board，12 镜头下限已断言）各自通过；**"≥3 章 3 万汉字真实文本从头走到分镜图"的单场景 walk 不存在**（canary 用 fixture 播种脚本） |
| AC-E2E-003 中断恢复 | **PASS**（WP-12 §0p） | 五子句全过；顺带修复了 `current_stage` 无写入者缺陷 |
| AC-E2E-004 质量修订 | **PASS**（WP-10） | 服装错误定位/阻塞/新版本/旧版本保留 |
| AC-E2E-005 深层记忆 | **PASS**（WP-10） | canary 场景跑通全链 |
| AC-E2E-006 安全 | **条款各自有测试，无统称场景** | 备份无密钥、SSRF、Zip-Slip、Supervisor 只读、动态脚本不可达各有独立测试；**"前端 DevTools 无法从 Store 读密钥"这条无自动化断言**（人检 + 架构保证） |

---

## 5. 还需要开发的任务（按优先级排序）

### P0 — 产品断链（不修则 MVP 主旅程走不通）**——两项均已 DONE**

1. ~~**资产生产 UI（面板图/资产版本链路）**~~ **DONE — WP-13, 2026-09-24 (STATUS §0q, ADR-0017).** The ten bindings have callers, the storyboard table generates/collects/approves panel images, and the walk found that `ApprovePanelImage` never wrote the panel's `status` — which the export's join requires. The original text follows. **资产生产 UI（面板图/资产版本链路）** — 10 个绑定零前端调用：
   `RunImageBatch`、`CheckStoryboardGate`、`CollectBatchResults`、`ApproveCandidate`、
   `ApprovePanelImage`、`AddVersion`、`AttachFile`、`AttachJobResult`、`AddUsage`、`AddRelation`。
   导出以 `approved_image_asset_version_id` 合成，**而没有任何界面能把一张图批准到那个状态**——
   "导入→剧本→分镜图→导出 MP4"的 MVP 主旅程在 UI 里断在中间。（STATUS §0n2 称之为"WP-12
   最大未了项"。）
2. ~~**FR-150 的每 Provider 并发上限**~~ **DONE — WP-14, 2026-09-24 (STATUS §0r, ADR-0018).** The limit is `provider_configs.max_concurrency` (migration 000022, 0 = unlimited), enforced in `dispatch`, with the in-flight count read from the database under a live-lease condition. Five tests, eight mutations killed. **The RATE limit stays undone and is named** (ADR-0018 ruling 8). The original text follows. **FR-150 的每 Provider 并发上限** — 验收合同明文（"同供应商并发不超过配置上限"），
   现在只有全局 worker 池。批量分镜图一旦接真实 Provider 就会打爆配额。

### P1 — 验收合同内缺口（条款写了、没全交付）

3. **FR-100 显式阶段依赖的产品裁定** — 两个选项：a) 接受"按工件使用处强制"为设计并改
   PRD 措辞；b) 建 `Layer.Stages()` 的依赖图并让 `StartStage` 拒绝前置未过的阶段。已实测
   证据在 TRACEABILITY FR-100 行。
4. **FR-030 别名合并 UI** — 后端命令在，无界面。验收"用户可合并重复实体并保留别名"。
5. **FR-070 表-画布双向同步** — 现在 Shot 只单向投影到画布；验收"表格编辑与画布节点双向同步"。
6. **FR-110 三缺口** — score/grade 填充；Safety/Cost 两类问题；剧本与资产规则集的确定性半边。
7. **FR-150 缩略图/导入/导出/迁移 job 类型** — 大文件导入走 job 队列是 FR-020"UI 不冻结"
   的正解，目前只有 64 KiB 分块。
8. **FR-160 垃圾回收** — 列出将删内容+确认+可取消，验收合同有、代码无。
9. **FR-180 设置/诊断/隐私** — 诊断包脱敏预览导出、缓存清理（不动已批准资产）、隐私控制。
10. **FR-170 备份格式版本迁移测试** — "旧版本有迁移测试"子句。
11. **AC-E2E-002 整场景 walk** — 3 章 3 万字从头到分镜图的一次性串测（两个 canary 已覆盖
    各半，差一次拼接）。
12. **AC-E2E-006 的 DevTools 密钥不可读断言** — 目前靠架构与人检。

### P2 — 发布工程缺口（代码外，RC→正式发布之间）

13. **Windows 安装包与签名** — `docs/INSTALL_AND_SIGNING.md` 已写明要求，无 installer
    工件、无证书。发布阻断项虽清零（阻断项不含签名），但 §16 的 v1.0"Windows 稳定安装
    与升级"要求它。
14. **干净 VM 验证** — 断网启动、崩溃恢复、升级路径，本机无法做。
15. **`go test -race`** — 本机 MinGW 32 位无法编译 race 运行时（`cc1.exe: 64-bit mode not
    compiled in`）。所有并发结论均出自读码。需要一台有 64 位 GCC 的机器跑一次。
16. **CI/远程执行** — GitHub Actions 从未跑过（未授权 push）。ADR-0002 的 Accepted 状态
    还压在"支持平台 race 证据"上。

### P3 — 已裁定延后到 v1.0 的范围（PRD §16，非欠账，列作 roadmap）

17. 本地多语言 ONNX Embedding / 可选 sqlite-vec（连带解决"最新 500 条候选窗"召回限界）
18. 事件图谱**可视化**（现在是列表视图）
19. MONOFORM 深度双向集成
20. ~~完整资产一致性检查（全部规则集的确定性半边 + Safety/Cost）~~ **DONE — WP-16, 2026-09-25 (STATUS §0u, ADR-0020).** The ASSET ruleset's two missing clauses (派生关系, 文件存在和类型) are rules over the versions a board cites; Safety and Cost each have an emitter; the classification reaches `review_issues.category` and the quality centre. **The file rule's first version was WRONG and two existing tests proved it** — it reported every cited version with no file, which is the asset-bible state rather than a fault; it now anchors on `generation_job_id`. The original text follows. 完整资产一致性检查（全部规则集的确定性半边 + Safety/Cost）
21. 视频首尾帧与批量镜头生成（对接真实视频 Provider）
22. 层级摘要与记忆中心 UI、召回评测集
23. 更完整时间线、音效建议、BGM 导入、简单混音、多角色声线映射
24. PDF 导入
25. Windows 稳定安装与升级（=上面 P2 的产品化表述）

### P4 — 文档债（非开发，但下一个读者会踩）

26. TRACEABILITY 六行过时判定：FR-001/FR-020/FR-030/FR-170、AC-SCRIPT"Not started"、
    AC-PROD"Not started"。
27. **DONE — WP-16 (see STATUS §0u).** 把本清单回填进 ROADMAP 的"后续版本"节：ROADMAP 的 v1.0 节现在指向本文件的 P3 清单，并逐项写明 P1/P2/P4 已完成项与本清单的关系。

---

## 6. 已知环境事实（交接必读）

- **每条 Go 命令**需前缀 `GOTOOLCHAIN=go1.25.0 GOSUMDB=sum.golang.org`（用户级
  `GOSUMDB=off` 会阻断工具链校验）；`go.mod` 声明 `toolchain go1.25.13`（修 25 个可达
  stdlib CVE），**前缀里的 go1.25.0 会覆盖该指令**——如需 1.25.13 的 CVE 修复参与编译，
  前缀要用 `GOTOOLCHAIN=go1.25.13`。
- **C: 盘曾满**（Go 构建缓存涨到 39 GB）。已 `go clean -cache` 并改用
  `GOCACHE=/d/go-build-cache`；后续跑全量测试请带上这个变量。
- `python3` 是坏的 Windows Store stub，用 `python`；Wails CLI 在
  `/d/GoWorks1.18/bin/wails.exe`；shell 是 Git Bash。
- **长 heredoc 会静默截断**——写文件用 Write 工具或 `python <file>`，别用 `cat <<'EOF'`。
- 全量门一条命令：`sh scripts/verify.sh`（含 Playwright，约 6–8 分钟）。

---

## 7. 验证快照（2026-09-23，WP-12 §0p）

- `go test ./... -count=1`：**56 包 ok / 0 failed**（`-race` 为环境失败，非通过）
- `npm test`：**76 通过 / 0 失败**；Playwright：**25 过 / 1 条件跳过**（WP-04 的无头文件
  选择器用例，非回归）
- `wails build`（production）：**PASS**，产出 `build/bin/InfiniteAtelier.exe`
- 安全扫描：618 文件，1 处审计的动态执行豁免 + 3 处具名 legacy 文件
- PRD §18 十一条发布阻断项：**全部 CLEARED**

---

## 8. 给下一个会话的建议入口

若继续开发，按本清单 P0→P1 顺序；P0-1（资产生产 UI）工作量最大且解锁主旅程，建议单独
成包（含 UI、e2e、以及 AC-E2E-002 整场景 walk 作为验收）。P2 的安装包/VM/race 三项
需要用户提供环境（证书、VM、64 位 MinGW），代码侧无事可做。P3 是 v1.0 的产品范围，
需产品决策开启，不应自动开始。

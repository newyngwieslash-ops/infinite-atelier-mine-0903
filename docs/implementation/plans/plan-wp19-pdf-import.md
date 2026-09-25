# WP-19 — PDF 导入（P3 第 24 项）

依据：PRD §16 的 v1.0 清单第 24 项「PDF 导入」；PRD FR-020（项目、原著与章节导入）；
`docs/ACCEPTANCE.md` AC-STORY-001（编码正确 / 章节检测 / 用户调整 / offsets 有效 / 原文定位 /
duplicate hash 提示 / UI 不冻结）；AGENTS §6（新增第三方依赖前检查许可证与维护状态，优先
Go 标准库、MIT、BSD、Apache-2.0）；`docs/SECURITY.md`（不可信输入、压缩炸弹、路径穿越）。

## 0. 侦察结论（每条都是实测，不是推断）

| 事实 | 证据 | 后果 |
|---|---|---|
| **导入管线已经有四层**：格式探测 → 文本抽取 → 编码检测 → 章节切分 | `internal/application/importing/parse.go` 的 `detectFormat` / `extractText`，`domain/importing/detect.go` 的 `DetectEncoding` | PDF 是**在第二层插一个抽取器**，不是重写导入 |
| **`rsc.io/pdf` 无法抽取中文** | 实测：手工构造的带 `ToUnicode` CMap 的中文 PDF，它返回 `"\x10\x00\x10\x01…"`（原始字形码），**完全忽略 ToUnicode** | **淘汰**。整个语料是中文，一个不能读中文的 PDF 库对本产品毫无价值 |
| **`ledongthuc/pdf` 能正确抽取中文** | 同一个 PDF，返回 `"沈砚的渡口"` | 候选 |
| **`ledongthuc/pdf` 会对损坏输入 panic** | 120 个随机损坏样本中 **3 个 panic**（`invalid real .` / `unexpected non-name key` / `missing endobj`），全部来自 `lex.go` 的 `errorf` | **必须 `recover()`**；这是该库表达「输入格式错误」的方式 |
| **它不 hang、不栈溢出** | 同上：117/120 正常返回错误，0 hang；另有手工构造的 4 个敌意输入（非 PDF、截断、越界 xref、400 层嵌套）全部返回错误 | 可接受的失败模式 |
| **两者许可证都是 BSD-3-Clause (Go Authors)** | 两侧 `LICENSE` 文件逐行读过 | AGENTS §6 允许，需记入 THIRD_PARTY_NOTICES 与 SBOM |
| **存储不需要迁移** | `source_document_versions` 无 format 列，格式记在 `import_metadata_json`（`service.go:657` 的 `importMetadata`） | 只加一个 Format 常量 |

## 1. 裁定

1. **依赖选 `github.com/ledongthuc/pdf`**，理由不是「更流行」而是**另一条实测淘汰**：`rsc.io/pdf`
   读不出中文。这一条写进 ADR，因为它是一个未来的人最可能重新踩的坑。
2. **抽取器包在 `recover()` 里**，并把 recover 转成 `importing` 的领域错误。理由是实测的 3/120：该库
   用 panic 表达「这个 PDF 畸形」，而**导入器绝不能因为用户拖进一个损坏文件就崩溃**。
   recover 的边界要收得尽量窄（只包住库调用），否则会吞掉本仓自己的 bug。
3. **PDF 不可读时 fail closed，并说明原因**：加密的、扫描件（无文字层）的、损坏的 PDF 各自给出
   不同的拒绝理由，而不是笼统的「读取失败」。扫描件尤其重要——它会抽出**空文本**，而一个空文档
   若被当成成功的导入，用户会得到一本空书且不知道发生了什么。
4. **`FormatPDF` 加入 `importing.Formats`**，`StoryDocumentType` 与 `detectFormat` 一并处理：
   `%PDF-` 魔数决定格式（与已有的「字节决定，不是扩展名」规则一致）。
5. **不引入 OCR**：扫描件明确拒绝并给出可操作的理由。OCR 是一个独立的、体积大得多的能力，
   PRD 没有要求它，本包不假装有。

## 2. 实现

- `internal/domain/importing/importing.go`：`FormatPDF Format = "pdf"`，加入 `Formats`；`IsValidFormat` 自动覆盖。
- `internal/application/importing/parse.go`：
  - `detectFormat`：在 ZIP 判定之前加 `%PDF-` 魔数判定（顺序无关，但先判更便宜）；
    提示为 `pdf` 而字节不是 PDF 时报「名字说是 PDF 但不是」，与既有 DOCX 的处理一致。
  - `extractText`：`FormatPDF` 走新的 `pdfText`。
  - `pdfText`：`recover()` 包裹库调用；逐页 `GetTextByRow` 取文本；页间用空行分隔（章节检测
    依赖换行）；对空结果返回明确的「没有可提取的文字（可能是扫描件）」错误。
  - **资源上限**：复用 `importing.MaxInputBytes`（64 MiB）先挡；页数上限用一个新常量，
    与 DOCX 的条目上限同样的思路——一个 PDF 声称一万页的，先拒绝再解析。
- 页数上限与单页文本上限都要有，且都要有测试。

## 3. 章节与 offsets

PDF 抽出的文本**原样进入既有的管线**：编码检测（PDF 出来的已经是 UTF-8，`DetectEncoding` 会
判定为 UTF-8 并不改字节）、章节检测、offset 计算全部复用。这正是不重写导入的收益。

## 4. 测试

- **中文抽取**（核心）：手工构造的带 `ToUnicode` 的中文 PDF，断言抽出的文本**逐字等于**预期。
  这个 fixture 由本仓的生成器写（`testdata/canary-drama/` 之外，因为它是二进制）。
- **ASCII/多页**：两页的 PDF，断言页序与页间分隔。
- **反向用例**：
  - 非 PDF 而扩展名是 `.pdf` → 拒绝且理由说明；
  - 损坏/截断 → 领域错误，**不 panic**（这条最重要，它锁住 recover）；
  - 无文字层的 PDF（只有图形）→ 明确拒绝，不是空文档成功；
  - 超过页数上限 → 拒绝；
  - 超过总字节上限 → 拒绝（既有路径）。
- **变异**：recover 移除、魔数判定、空文本拒绝、页数上限各一个。
- 前端：`accept` 增加 `.pdf`；错误文案两语言。

## 5. 明确不做

- **OCR**（见裁定 5）；
- **PDF 加密/口令**：拒绝并说明，不实现解密；
- **版面还原**（表格、分栏、图片位置）：抽的是文字流，PRD 要的是小说正文；
- **写 PDF**：导出是 WP-11 的范围。

## 6. 风险与诚实声明

- 该库 9 年未发版本（最新是伪版本），这是一个**真实的维护风险**，ADR 要写明；缓解是它只被
  一个函数调用、只做一件事（抽文字），替换成本被限制在一处。
- 抽取质量对**排版复杂的 PDF** 会下降（分栏会按行交错）。这是所有非 OCR 抽取器的共同限制，
  ADR 写明白，不宣称「支持 PDF」而不说清支持到什么程度。

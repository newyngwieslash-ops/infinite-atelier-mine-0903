# Local Embedding Model Distribution (T19)

日期：2026-09-26。本文件记录本地 ONNX 嵌入模型的**分发决策**与获取步骤，
对应 2026-09-26 审计 T19 的完成标准：来源、许可证、版本、校验和明确；
干净机器按文档可获得匹配模型/tokenizer；模型不进 Git；许可证随分发。

## 决策

| 问题 | 决定 |
|---|---|
| 分发方式 | **用户自选外部下载**：应用不随包分发模型，也不在启动时静默下载。用户按本文档手动下载并放到本地目录，通过环境变量指向 |
| 模型 | `all-MiniLM-L6-v2`（sentence-transformers，ONNX 导出版） |
| Tokenizer | 同模型的 `vocab.txt`（WordPiece）。`tokenizer.json`（Unigram）也可被 `onnxemb` 接受，但 MiniLM 用 `vocab.txt` |
| 运行时 | onnxruntime 共享库（Windows `onnxruntime.dll`），随 Python 的 `onnxruntime` 包或 NuGet/GitHub Release 获取 |
| 许可证 | Apache-2.0（sentence-transformers 与 onnxruntime 均为 MIT/Apache-2.0 系），再分发时必须附带其 LICENSE 副本 |
| 版本固定 | 模型文件以内容哈希校验（下方 SHA-256 由用户下载后自验，本文档不给死值——模型文件的权威哈希以 HuggingFace 仓库页 LFS 指针为准） |
| Git 策略 | 模型/词表/运行时**一律不入库**。`testdata/models/` 保持为空目录（.gitkeep），CI 与本地测试在缺模型时 SKIP（见 T20） |

## 获取步骤（Windows）

1. 安装 64 位 Python ≥3.10，然后：
   ```powershell
   pip install onnxruntime
   ```
   记下运行时路径：
   `%LOCALAPPDATA%\Programs\Python\Python310\lib\site-packages\onnxruntime\capi\onnxruntime.dll`
2. 下载模型（任选其一）：
   - `https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/tree/main`（`model.onnx` 如无可用导出，用 `optimum-cli export onnx --model sentence-transformers/all-MiniLM-L6-v2 ./minilm` 导出）
   - 或第三方 ONNX 镜像仓库的 `model.onnx` + `vocab.txt`
3. 校验下载完整性：
   ```powershell
   Get-FileHash .\model.onnx -Algorithm SHA256
   ```
   与来源页公布的哈希比对。
4. 设置环境变量（用户级，重启应用生效）：
   ```powershell
   setx IA_ONNX_MODEL  "C:\models\minilm\model.onnx"
   setx IA_ONNX_VOCAB  "C:\models\minilm\vocab.txt"
   setx IA_ONNX_RUNTIME "C:\...\onnxruntime\capi\onnxruntime.dll"
   ```
5. 启动应用，设置 → 诊断面板的 **Embedding** 状态应显示 `local`；
   显示 `unavailable` 时附带的原因即加载失败原因（T18 的诊断回读）。

## 断点安全

- 下载中断只会产生不完整文件；应用加载前用哈希校验，失败即
  `unavailable` 并给出原因，不会半加载。
- 运行时库缺失/损坏时 `onnxemb` 报 `ONNX_RUNTIME_*` 系列错误，同上可见。
- 多版本运行时冲突会报 `ONNX_RUNTIME_CONFLICT`（进程内只允许一个）。

## 与许可证合规的关系

- 再分发安装包（若未来改为可选组件携带模型）必须同时附带
  `LICENSE-Apache-2.0`（模型）与 onnxruntime 的 LICENSE，并更新
  `SBOM`/`THIRD_PARTY_NOTICES`。
- 当前"用户自选下载"的决定使安装包不含第三方模型文件，不触发该义务。

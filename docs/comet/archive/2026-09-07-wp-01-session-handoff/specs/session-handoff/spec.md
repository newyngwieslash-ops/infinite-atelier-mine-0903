# Session handoff specification

## Artifact

系统在 `docs/implementation` 中保留不可覆盖的带时间戳 handoff Markdown。每次交接描述当时的真实工作树与验证结论，旧 handoff 作为历史证据继续保留。

## Required content

交接文档必须包含仓库路径、分支、HEAD、当前工作包和精确恢复点；Git/数据安全现场；已完成工作；当前未完成实现及审查；环境限制；WP-01 剩余任务；对标当前 PRD 的 WP-02–WP-12 后续任务；发布阻断条件；以及可直接用于新 Codex 或 Grok 会话的完整续接提示词。

提示词必须要求新会话先读取最新 handoff 与仓库规定，重新核对 Git 现场，保护全部 tracked/untracked 文件，从 Task 4 继续，并严格串行执行 implementer、独立 spec reviewer、独立 quality reviewer。Task 4 未通过双审前不能进入 Task 5，WP-01 完成后不能自动进入 WP-02。

## Accuracy and safety

文档只能声称有真实命令或审查证据支持的结果。因本机 32 位 MinGW 导致的 race 编译失败应标为环境限制。未完成的 Task 4 矩阵、Wails production smoke 和独立双审必须明确列为待办。

生成交接不得修改产品代码、依赖、用户数据、密钥、媒体或 Provider，不得执行提交、推送、stash、reset 或 clean。

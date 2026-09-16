# Session handoff specification

## Artifact

每次交接新增不可覆盖的带 Asia/Shanghai 时间戳 Markdown。本次文件保存项目根目录；过去 docs/implementation 与根目录的历史 handoff 均保留，历史结果不被视作新现场。另提供与 handoff 内嵌提示词完全一致的独立文本文件。

## Required content

文档记录仓库、时间、分支、HEAD、保护现场、权威来源、WP-01 Windows foundation 完成事实、WP-02 未实现、全部 PRD FR/NFR/E2E 差距、WP-02 至 WP-12 依赖任务与验收、版本边界和发布阻断。STATUS/TRACEABILITY/旧 handoff 的陈旧或错误映射明确纠正，ADR 未接受与 race 环境失败不得掩盖。

## Continuation prompt

完整提示词适用于新 Codex 或 zcode。先核对真实工作树再按仓库必读顺序读规格；只授权 WP-02，使用已选 Windows Credential Manager、严格受控 Go Provider HTTP 与首个文本 Adapter。明确模块、11 步计划、前向 migration、Secret 安全边界、静态扫描、测试、独立规格和质量审查、包末停止。仅传分支或 handoff 无法恢复大量未提交代码，禁止两个会话同时写现场。

## Accuracy and safety

测试区分本次实际执行与历史记录；没有证据不标 PASS。Comet 项目恢复健康与本 change 验收完成是不同事实；隔离损坏原件不等于恢复其有效状态。普通备份无 Secret；secure mode 不等于危险 legacy 全清退。只新增/更新交接和必要 Comet/恢复工件，不修改产品代码、数据或密钥，不执行 commit/push/stash/reset/clean。

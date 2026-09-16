# Comet 恢复记录

核验时间：2026-09-08 18:09:55 +08:00。目录名是恢复工件标签，不作为精确创建时间证据。

## 授权和范围

用户明确选择“先恢复 Comet，再生成文档”。本次恢复保持产品代码、用户数据、密钥及所有历史交接文档不变；不执行 commit、push、stash、reset 或 clean。

## 故障

`wp-01-current-state-handoff-20260908` 的便携状态无法被当前 Runtime 解析：`Native acceptance[0].text must be a non-empty string`。只读 status/doctor 和首次 doctor --repair 均返回 exit 65。该状态还使用不受支持的 status `verified`，缺少完整 loop、builder_handoff 和结构化 verification 等字段，不能仅补一个字段后承认其历史 pass。

## 恢复动作

1. 保存两个 active change 的原件副本，以及项目配置、原 selection。
2. 在用户恢复授权下，将损坏的 `docs/comet/changes/wp-01-current-state-handoff-20260908` 整目录移入本目录的 `quarantined-original-wp-01-current-state-handoff-20260908/`。这是公共修复器无法解析输入后的可逆隔离操作，不是 Runtime 已成功修复该损坏 change。没有手工改写其状态或验收结果。
3. 移动前后校验全部 4 个文件的 SHA-256，一致；清单见 `quarantined-original-sha256.json`。
4. 公共 `comet native status --json` 恢复可读，exit 0。
5. 公共 doctor 识别另一份有效历史 change 的 `portable-migration-incomplete` 并明确返回 `repair: migrate`。
6. 执行公共 `comet native doctor wp-01-session-handoff-20260908 --repair --json`，exit 0，`healthy: true`、`repaired: true`、stateVersion 5。
7. 公共 `comet native select wp-01-session-handoff-20260908 --json` 修复失效 selection，exit 0。
8. 最终公共 `comet native doctor --json`：exit 0、`healthy: true`、`legacyChanges: []`、`findings: []`。

## 恢复后的准确边界

Comet 项目运行环境已恢复健康。损坏的旧 change 原件被隔离保留，未被“补写通过”，也未宣称该 change 已恢复为可继续执行的有效状态。

仍有效的历史 change `wp-01-session-handoff-20260908` 描述上午 Task 7 交接，保持 Verify / await-user / stateVersion 5 / confirm-skill-coordinated-pass。它不代表最新 WP-01 完成现场，也没有被本次恢复授权自动归档。

最新产品现场仍按 STATUS 第 0 节和根 handoff.md：WP-01 Windows foundation 范围已完成，WP-02 未实现。STATUS 顶部有历史矛盾，后续文档必须明确记录。运行环境健康不等于本次新交接通过 Comet 验收。

上述为恢复时的稳定边界。用户随后明确确认当前目录、当前分支及文档范围。尝试 public new 被 Runtime 以同目录已有 active change 拒绝，未创建第二 change。因此更新既有交接的正式 brief/spec，公共 next --return-to-build 使旧结果失效（v6），公共 next 检测验收变化后返回 Shape（v7，goal cycle 2），再依据用户确认经 public next --confirmed 进入 Build（v8，验收 pending）。没有手写 Runtime state。历史 Task 7 原件和结果仍保留在本目录备份及 Runtime history。

已生成根目录 `handoff-2026-09-08-181352+0800.md` 及同时间戳 `continue-wp02-codex-zcode-2026-09-08-181352+0800.md`。本次独立 Verify 的最终边界请查当前公共 status 与对应 verification.md，不把恢复时的上午 pass 当作新结果。

## Git 与验证

- 分支：`codex/wp-01-desktop-foundation`；HEAD 未变。
- `git diff --cached --name-status`：无输出，无 staged changes。
- `git diff --check`：exit 0；存在既有 LF/CRLF 警告与全局 ignore 读取权限提示。
- 本次未执行 Go/Wails/前端产品测试、数据库迁移、真实 Provider 调用。
- 本目录的副本是 Comet 工件恢复备份，不是用户业务数据库备份。

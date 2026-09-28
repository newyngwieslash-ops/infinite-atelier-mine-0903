# Signing Strategy (T29)

日期：2026-09-27。审计 T29 要求"确定签名身份/服务/证书及公开发布策略；
实现 EXE→安装包→卸载器的适用签名和时间戳验证"。证书是外部前提，本文件
交付**策略与流水线接入点**，证书到位后执行即可。

## 1. 签名身份决策

| 选项 | 成本 | 适用 | 建议 |
|---|---|---|---|
| OV 代码签名证书（DigiCert/Sectigo，EV 可选） | 年费 + USB 令牌/HSM（CA/B Forum 要求） | 公开发布给最终用户 | **推荐**：SmartScreen 信誉随安装量积累 |
| 自建 CA（internal） | 零外购 | 内部测试/VM 验收 | 用于 T25 VM 验收的中间态（签名链需导入 VM 信任库） |
| Azure Trusted Signing | 按次计费，托管身份 | 无硬件令牌的 CI | 备选；需 Azure 订阅与身份验证 |

**策略决定**：公开发布用 OV/EV 证书；T25 VM 验收允许自建 CA 签名（验收的是
安装行为而非信任链）；内部 RC 不签但**必须在工件名与发布说明中明确标注
UNSIGNED**（审计原文要求）。

## 2. 待签名工件与顺序

1. `InfiniteAtelier.exe`（wails 产物）
2. `源铭振跃-amd64-installer.exe`（NSIS 产物）
3. 卸载器（NSIS 内嵌；由安装包签名覆盖验证）

## 3. 流水线接入（signtool）

证书到位后在 `desktop-build.yml` 的 windows-desktop 作业追加：

```yaml
      - name: Sign EXE
        shell: pwsh
        env:
          SIGNTOOL_PFX: ${{ secrets.SIGNTOOL_PFX }}       # base64(PFX)
          SIGNTOOL_PASSWORD: ${{ secrets.SIGNTOOL_PASSWORD }}
        run: |
          $pfx = [Convert]::FromBase64String($env:SIGNTOOL_PFX)
          $pfxPath = "$env:RUNNER_TEMP\sign.pfx"
          [IO.File]::WriteAllBytes($pfxPath, $pfx)
          $signtool = (Get-ChildItem "C:\Program Files (x86)\Windows Kits\10\bin" -Recurse -Filter signtool.exe |
            Sort-Object FullName -Descending | Select-Object -First 1).FullName
          & $signtool sign /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 `
            /f $pfxPath /p $env:SIGNTOOL_PASSWORD build\bin\InfiniteAtelier.exe
          & $signtool sign /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 `
            /f $pfxPath /p $env:SIGNTOOL_PASSWORD "build\bin\*-installer.exe"
          Remove-Item $pfxPath
```

要点：

- **RFC3161 时间戳**（`/tr` + `/td SHA256`）必加：证书过期后已分发工件仍可验证。
- `/fd SHA256`（旧 `/t` 时间戳不满足现代验证链）。
- PFX 走 GitHub Actions secrets；**凭据不入仓库**（审计原文）。
- 签名在 NSIS 打包**之后**对安装包整体签名（EXE 签名会被打包吞掉——先签 EXE
  仅供校验构建内容，最终工件签名以安装包为主）。

## 4. 验证步骤

```powershell
Get-AuthenticodeSignature .\InfiniteAtelier.exe | Format-List Status,StatusMessage,SignerCertificate
Get-AuthenticodeSignature .\*-installer.exe | Format-List Status,StatusMessage,SignerCertificate
```

验收标准（审计原文）：`Status = Valid`、时间戳存在、SignerCertificate 主体与
策略一致；哈希与已验收工件一致（STATUS 批次小节的 SHA-256 记录方式沿用）。

## 5. 公开发布策略（草案）

1. 版本号：SemVer（`v1.0.0-rc.N` → `v1.0.0`）；RC 工件名带 `-rc` 且描述标注未签/签。
2. 发布渠道：GitHub Releases，附 SHA-256SUMS 文件（与 STATUS 哈希记录同格式）。
3. 发布说明必须含：已知限制（STATUS 批次小节的 BLOCKED/PARTIAL 项）、
   WebView2 策略、ffmpeg/ONNX 外部依赖说明（引 `EMBEDDING_MODEL_DISTRIBUTION.md`）。
4. 回滚：保留前一个 Release 工件；升级路径已在 T25 §3 验收。

## 6. 当前状态

- 证书：**未取得（BLOCKED）**。
- 已完成：策略（本文件）、流水线接入点（上 YAML）、验证命令、发布草案。
- 证书到位后的执行序：签 EXE → 打 NSIS → 签安装包 → 双验证 → 记录哈希 →
  在 STATUS 将 T29 从 BLOCKED 改 COMPLETE。

<p align="center">
  <img src="web/public/brand-logo.png" width="112" alt="Infinite Atelier logo">
</p>

<h1 align="center">Infinite Atelier</h1>

<p align="center">为桌面创作而生的 AI 视觉工作台</p>

Infinite Atelier 将画布编排、图片生成、参考图、提示词、资产管理和导演预演放在一个连贯的桌面工作流中。项目由 `GuiYi-Xi` 独立维护，界面、品牌与内置提示词库围绕高效视觉创作重新设计。

## 产品概览

- **用户问题**：创作者需要在模型配置、提示词、参考图、生成结果和本地素材之间频繁切换，长任务状态与失败恢复也缺少统一入口。
- **产品方案**：以无限画布为主工作区，将 Provider 配置、生成节点、提示词库、本地资产和导演预演组织成连续流程。
- **我的工作**：负责场景梳理、功能规划、交互与视觉设计、前端实现、模型接口封装、Windows 启动流程、测试和使用文档。
- **验证方式**：仓库提供完整源码、产品截图、演示视频和可复现的本地启动步骤；所有配置与生成历史默认保存在本机。

这个项目展示的是模型能力从 API 到用户产品的封装实践，不涉及 GPU 集群或企业级模型托管部署。

## 功能

- 无限创作画布：组织图片、文字、音频、视频和生成结果。
- 多渠道模型：配置 OpenAI 兼容接口及自定义中转 API。
- 图片生成：支持 GPT Image 2 等模型的文生图、图生图与多图参考。
- 提示词库：内置 12 组带展示图的提示词，可复制、收藏、替换封面或新增条目。
- 视觉资产：保存生成结果与素材，支持导入、导出和本地备份。
- 导演台：内置 MONOFORM 预演工具，用于镜头、角色和动作设计。
- 品牌主页：五套整体配色、动态品牌背景与最近项目入口。

## 演示与导演台

- [观看 Infinite Atelier 项目演示视频（MP4，约 63 MB）](https://github.com/GuiYi-Xi/infinite-atelier/releases/download/v1.0.0/Infinite-Atelier-Demo.mp4)
- [MONOFORM 素形白模预演工作台源码](https://github.com/GuiYi-Xi/monoform-previs-studio)
- [导演台使用教程（哔哩哔哩）](https://www.bilibili.com/video/BV1HNud6SEgs/)

## 运行模式

### 浏览器开发模式

现有自由画布仍可通过 Vite 在浏览器中开发。安装 Node.js 22 LTS 后，在仓库根目录执行：

```powershell
cd web
npm ci --legacy-peer-deps
npm run dev -- --host 127.0.0.1
```

Vite 只应绑定到 `127.0.0.1`；浏览器地址形如 `http://127.0.0.1:3000`。浏览器模式继续使用既有本地浏览器存储，不会调用 Wails Binding，也不会显示桌面核心状态。

`start.bat` 仍可用于现有 Windows 浏览器启动流程；它会检查 Node.js 与 Vite 依赖。首次安装会在 `web/node_modules` 创建大量已忽略的依赖文件。

### 桌面开发与生产构建

WP-01 使用 Wails v2、Go 1.25 和系统 WebView 承载同一 React 应用。Windows 需要 Node.js 22 LTS、Go 1.25、Microsoft Edge WebView2 Runtime，以及可用的 Windows 编译工具链。安装固定 Wails CLI：

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0
```

确保 Go 的 bin 目录在 `PATH` 中，然后从仓库根目录运行：

```powershell
wails dev
wails build
```

`wails dev` 仅用于本地桌面开发；`wails build` 生成嵌入前端的 Windows 可分发程序到 `build/bin/`，运行已构建程序不需要目标机器安装 Node.js 或 Vite。不要提交 `build/bin/`、`build/appicon.png` 或 `build/windows/`。`web/dist/.gitkeep` 是零字节嵌入目录占位文件，执行 Vite 或 Wails 构建清理后应恢复它，不应提交其他 `web/dist` 文件。

桌面模式的 WP-01 数据目录由 Go 管理，默认位于当前用户配置目录下的 `InfiniteAtelier`，其中包括 SQLite 数据库、受管文件、临时文件、日志和迁移快照。现有浏览器画布数据尚未迁移到该数据库；WP-01 不读取或迁移已有浏览器数据。

## 验证

在已安装现有前端依赖后，从仓库根目录运行对应平台脚本：

```powershell
./scripts/verify.ps1
```

```sh
./scripts/verify.sh
```

两个脚本都会运行前端 typecheck、前端安全测试（`npm test`）、production build、可用的 MONOFORM source build、`go test ./... -count=1`、`go vet ./...`，以及安全静态扫描（动态执行、密钥模式、未受保护的配置序列化）。安装 Wails CLI 时脚本还会执行 `wails build`；未安装时会报告固定版本的安装命令及该 production gate 的 SKIP。当前仓库仍没有前端 lint script，脚本会如实报告 SKIP。

## 当前范围

WP-01 提供 Wails、Go Core、SQLite foundation、FileStore、日志和只读 health binding。

WP-02 增加安全 Provider 基础（仅文本链路）：

- API Key 保存在 Windows 凭据管理器；数据库只保存密钥引用，普通备份与配置文件不包含密钥；
- 文本生成经 Go Provider Gateway 执行受控 HTTPS 请求（域名/IP/端口策略、DNS 固定、重定向复检、TLS 校验、超时与响应大小限制），并记录脱敏调用审计；
- 桌面模式下模型调用脚本不可达，URL 传入的 API Key 会被忽略并提示。

WP-03 增加持久任务与图像 Provider：

- 任务持久化在 SQLite：排队、优先级、租约、重试退避、取消、失败分类，关闭应用后未完成任务在下次启动时恢复或安全失败；
- **图片生成与编辑**改由 Go 任务核心执行：密钥不进入前端，结果先经内容校验再以内容寻址方式入库，成功必须对应已提交的文件引用；
- Provider 返回的远程结果 URL 走单独下载策略：仅 HTTPS、拒绝私网/回环/云 metadata、逐跳复检、流式大小上限；
- 异步远程任务按固定间隔轮询（`RemotePollInterval`，默认 5 秒），轮询本身不占用重试次数；已有远程 ID 的任务重启后只轮询、不会重复提交；取消时若无法确认远端已停止，会记录为待人工确认的孤儿任务；
- 「任务中心」页面可查看队列、进度与失败原因，支持暂停/恢复、批量取消、仅重试失败；
- 视频与音频目前只有契约与确定性 Mock（真实适配器属后续工作包）；界面仍走既有浏览器直连链路，启动时会明确提示该范围，安全扫描器拒绝新增浏览器直连调用。

WP-04 把项目、画布与旧数据迁入 Go Core：

- 项目、画布文档、节点、连线、聊天会话、资产与版本、生成历史持久化在 SQLite，带 revision 并发保护与级联删除；
- **桌面模式下画布通过持久化适配器读写 Go Core**（`CanvasPersistenceAdapter`），画布的节点/连线/视口不再写入 localForage；浏览器开发模式仍由原有存储承担，因为那里没有 Go Core；
- 工具栏新增「导入旧项目」：预检 → 确认 → 导入 → 报告。导入在一个事务内完成，失败不留半成品；第二次导入会检测已导入并跳过，可选择「导入副本」；浏览器原始数据不会被修改；
- 画布未建模的字段（插件节点、供应商扩展字段）原样保留并生成迁移告警，不静默丢弃；
- 普通项目备份 v1 由 Go 生成（清单 + 数据库快照 + 文件 + 校验和），不含密钥；恢复前逐项校验清单、校验和与数据库完整性。
- 自由画布回归由 Playwright 覆盖（增删改、多选、框选、缩放、平移、撤销重做、连线、小地图、生成面板），并接入两套 verify 脚本。

## 使用说明

1. 打开右上角配置，添加渠道的 API 地址与模型。
2. 在「渠道」中通过安全密钥输入框保存 API Key（桌面模式）；密钥写入系统凭据存储，界面只显示是否已配置与短提示。
3. 新建画布，将提示词、参考图和生成节点组织到同一工作区。
4. 在「任务中心」查看生成任务队列与失败重试。
5. 主页提示词库的内置封面位于 `web/public/prompt-covers`，卡片右上角可以随时替换。

浏览器开发模式仍把配置保存在当前浏览器本地。桌面模式的安全密钥不再进入浏览器存储；旧版本保存的明文密钥会在桌面模式下收到迁移提示，请通过安全输入框重新保存后再清除旧值。API Key 不会提交到仓库；分享导出的配置或截图前仍应检查敏感信息。

## 目录

```text
Infinite Atelier
├─ start.bat                 Windows 启动器
├─ web/src                   主应用源码
├─ web/public                品牌与提示词图片资源
├─ web/monoform-studio       内嵌导演预演工具
└─ LICENSE                   开源许可证
```

## 维护者

[GuiYi-Xi](https://github.com/GuiYi-Xi)

## License

代码许可见 [LICENSE](LICENSE)。

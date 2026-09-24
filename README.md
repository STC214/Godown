# Ghost Downloader 3（Go / Win32）

Ghost Downloader 3 是面向 Windows 的桌面下载器重写项目。当前版本提供 HTTP(S)、M3U8、BitTorrent/Magnet 下载，以及本地浏览器桥接。

> 当前实现进度为 Stage 28：FTP 递归目录下载完成审查修复，已增加任务专属完成标记与 SHA-256 校验，支持目录清单缺少文件大小的服务器，并修正 `SIZE` 不可用时的无尾斜杠目录探测。详见 [Stage 28 实现记录](docs/implementation-notes/023-stage-28-ftp-directory-review-fixes.md)。

## 已实现功能

- HTTP(S) 分块下载、重试、限速、暂停与继续。
- FTP、隐式 FTPS（`ftps://`）和显式 FTPS（`ftpes://`）单文件及递归目录下载、重试、限速与断点续传。
- M3U8 解析、外部运行时调用、下载与合并。
- BitTorrent：本地/远程 `.torrent`、`file://`、v1 `btih`、v2 `btmh` 及混合 Magnet。
- BT 多文件选择、低/普通/高文件优先级、运行中改选、Tracker 合并、校验块断点续传及完成后做种。
- BT 分享率/做种时长限制、DHT、端口映射偏好和连接数设置。
- 任务持久化、并发调度、托盘、通知及浏览器桥接。
- HTTP(S)/SOCKS5 代理、自定义请求头与 Cookie。
- JSON-RPC stdio 插件发现、URL 匹配与解析，带进程超时和崩溃隔离。
- 系统/亮色/暗色主题、带 SHA-256 校验和失败回滚的便携自动更新、日志入口及崩溃报告。

## 快速开始

### 环境

- Windows 10/11 x64
- Go 1.26.6 或更新的 1.26.x 版本（从源码构建时）
- M3U8 任务另需 `N_m3u8DL-RE` 和 FFmpeg；可在应用设置中指定安装目录

### 构建与运行

```powershell
go mod download
.\scripts\build-release.ps1
.\dist\gd3win.exe
```

构建会生成两个均保留 Go 符号表和 DWARF 调试信息的文件：

- `gd3win.exe`：轻量主界面、调度器及 HTTP/M3U8 功能；
- `gd3-bt-runtime.exe`：BitTorrent/Magnet 引擎运行库进程。

发布和运行时必须将二者放在同一目录。BT 引擎拆为独立进程后，主程序不会静态链接 torrent、DHT 和 WebRTC 协议栈。

### 便携版发布

```powershell
.\scripts\package-portable.ps1 -Version 1.0.0
```

脚本只保留 `release/GhostDownloader-<version>-windows-x64-portable.zip` 和对应的 SHA-256 文件；`release-manifest.json` 位于 ZIP 内。便携包不写注册表，解压后直接运行。应用可下载匹配的 ZIP 与 `.sha256` Release 资产，校验后退出、覆盖、失败回滚并重新启动。

当前验证版本为 `0.1.21-stage28`，对应 Windows x64 便携 ZIP；主程序和 BT 运行时必须从同一解压目录运行。

### 工作区清洁约定

- `dist/`、`.codex-artifacts/` 和 `reference/` 均为可重建或本地对照内容，不纳入版本控制。
- 仓库根目录不保留运行时 `.torrent.db`、图标转换前的原始 JPG 或测试生成物。
- `release/` 只保留当前 Windows x64 便携 ZIP 及其同名 `.sha256`；本项目不生成安装包。
- 各阶段实现记录属于受版本控制的项目文档，不按临时历史清理。

### 添加下载

1. 在顶部输入框粘贴 HTTP(S)、FTP/FTPS、M3U8 或 Magnet 地址，然后选择 **添加地址**。
2. 添加本地 torrent 时选择 **打开种子**。
3. BT 任务会先打开文件选择窗口；至少选择一个文件后才能加入队列。
4. 使用 **全部开始**、**全部暂停** 或任务右键菜单控制任务。

完整操作、配置含义与故障排查参见[中文用户指南](docs/USER_GUIDE.zh-CN.md)。

### 插件

插件放在 `%AppData%\GhostDownloaderGo\plugins\<plugin-id>`，每个插件目录包含一个 `manifest.json`。应用启动时发现插件，并通过 JSON-RPC 2.0 stdio 调用 `manifest`、`matches` 和 `parse`。

Python 示例及协议说明参见 [`examples/plugins/rewrite-example`](examples/plugins/rewrite-example/) 和 [Stage 6 实现记录](docs/implementation-notes/007-stage-6-plugins.md)。

## 开发验证

```powershell
go test -count=1 ./...
go test -race -count=1 ./...
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
go vet ./...
go build -trimpath ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

Stage 15 验证结果：项目语句覆盖率为 54.1%；`cmd/gd3win`、`cmd/gd3-bt-runtime`、`internal/app`、`internal/ui` 和 `internal/win32` 分别为 51.6%、42.6%、34.4%、24.7% 和 24.7%。全量测试、竞态检测、静态检查、构建及可达漏洞扫描均通过。

项目结构、状态机、扩展约束和测试说明参见[开发者文档](docs/DEVELOPMENT.zh-CN.md)。总体设计参见[项目蓝图](GHOST_DOWNLOADER_GO_WIN32_BLUEPRINT.md)。

## 当前边界

- FTP 系列地址支持单文件和递归目录下载；目录任务最多保存 100,000 个目录项，符号链接会跳过，代理支持直连或 SOCKS5。
- 修改运行中 BT 文件选择和优先级时，程序会短暂停止 BT runtime、保存最新检查点、串行应用编辑后自动恢复，以避免运行时状态竞争。
- BT 的 HTTP 元数据、HTTP/WebSocket Tracker 与 WebSeed 遵循代理设置；原生 Peer、DHT 和 UDP Tracker 使用 BT 引擎网络栈。
- LSD、UPnP 与 NAT-PMP 受当前 BT 引擎公开接口约束，详见 [Stage 5 实现记录](docs/implementation-notes/005-stage-5-bittorrent.md)。

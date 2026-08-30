# Ghost Downloader 3（Go / Win32）

Ghost Downloader 3 是面向 Windows 的桌面下载器重写项目。当前版本提供 HTTP(S)、M3U8、BitTorrent/Magnet 下载，以及本地浏览器桥接。

> 当前处于按蓝图分阶段实现和验证的开发阶段。阶段进度以 [`docs/implementation-notes`](docs/implementation-notes/) 中的实现记录为准。

## 已实现功能

- HTTP(S) 分块下载、重试、限速、暂停与继续。
- M3U8 解析、外部运行时调用、下载与合并。
- BitTorrent：本地/远程 `.torrent`、`file://` 和 v1 `btih` Magnet。
- BT 多文件选择、Tracker 合并、校验块断点续传及完成后做种。
- BT 分享率/做种时长限制、DHT、端口映射偏好和连接数设置。
- 任务持久化、并发调度、托盘、通知及浏览器桥接。
- HTTP(S)/SOCKS5 代理、自定义请求头与 Cookie。
- JSON-RPC stdio 插件发现、URL 匹配与解析，带进程超时和崩溃隔离。
- 系统/亮色/暗色主题、带 SHA-256 校验和失败回滚的便携自动更新、日志入口及崩溃报告。

## 快速开始

### 环境

- Windows 10/11 x64
- Go 1.26.1（从源码构建时）
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

脚本生成 `release/GhostDownloader-<version>-windows-x64-portable.zip`、SHA-256 文件及包内 `release-manifest.json`。便携包不写注册表，解压后直接运行。应用可下载匹配的 ZIP 与 `.sha256` Release 资产，校验后退出、覆盖、失败回滚并重新启动。

### 添加下载

1. 在顶部输入框粘贴 HTTP(S)、M3U8 或 Magnet 地址，然后选择 **Add URL**。
2. 添加本地 torrent 时选择 **Open Torrent**。
3. BT 任务会先打开文件选择窗口；至少选择一个文件后才能加入队列。
4. 使用 **Start All**、**Pause All** 或任务右键菜单控制任务。

完整操作、配置含义与故障排查参见[中文用户指南](docs/USER_GUIDE.zh-CN.md)。

### 插件

插件放在 `%AppData%\GhostDownloaderGo\plugins\<plugin-id>`，每个插件目录包含一个 `manifest.json`。应用启动时发现插件，并通过 JSON-RPC 2.0 stdio 调用 `manifest`、`matches` 和 `parse`。

Python 示例及协议说明参见 [`examples/plugins/rewrite-example`](examples/plugins/rewrite-example/) 和 [Stage 6 实现记录](docs/implementation-notes/007-stage-6-plugins.md)。

## 开发验证

```powershell
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go build -trimpath ./...
```

项目结构、状态机、扩展约束和测试说明参见[开发者文档](docs/DEVELOPMENT.zh-CN.md)。总体设计参见[项目蓝图](GHOST_DOWNLOADER_GO_WIN32_BLUEPRINT.md)。

## 当前边界

- BT v2-only Magnet 尚未覆盖。
- 任务开始后暂不支持动态修改 BT 文件优先级。
- BT 的 HTTP 元数据、HTTP/WebSocket Tracker 与 WebSeed 遵循代理设置；原生 Peer、DHT 和 UDP Tracker 使用 BT 引擎网络栈。
- LSD、UPnP 与 NAT-PMP 受当前 BT 引擎公开接口约束，详见 [Stage 5 实现记录](docs/implementation-notes/005-stage-5-bittorrent.md)。

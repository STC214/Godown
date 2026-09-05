# Ghost Downloader 3 开发者文档

## 1. 技术栈

- Go 1.26.6 或更新的 1.26.x 版本
- Win32 GUI：`github.com/lxn/walk`
- 任务/配置存储：SQLite
- BitTorrent：`github.com/anacrolix/torrent`
- BT piece completion：BoltDB
- 速率控制：`golang.org/x/time/rate`

入口为 `cmd/gd3win/main.go`，应用装配位于 `internal/app`。

## 2. 目录结构

```text
cmd/gd3win/                 Windows GUI 程序入口、manifest 与资源
cmd/gd3-bt-runtime/         BT 独立运行时进程入口
scripts/build-release.ps1   保留调试信息的双产物构建脚本
scripts/package-portable.ps1 带版本、哈希和 manifest 的便携 ZIP 发布脚本
internal/app/               配置、存储、Worker、调度器和 UI 装配
internal/btruntime/         GUI 侧轻量 BT JSON 流协议客户端
internal/core/              Task 模型、状态机与 Scheduler
internal/config/            路径和 Settings
internal/storage/           SQLite 配置与任务存储
internal/download/http/     HTTP 探测、分块下载和恢复
internal/download/m3u8/     M3U8 解析及外部下载器 Worker
internal/download/ffmpeg/   媒体合并
internal/download/bt/       torrent 解析、文件选择模型、Worker 与恢复存储
internal/browserbridge/     本地浏览器 HTTP/WebSocket 桥接
internal/ui/                Walk 主窗口、设置和 BT 文件选择
internal/win32/             DPI、主题、单实例与隐藏子进程窗口支持
internal/update/            GitHub Release 更新检查与版本比较
internal/pluginhost/        JSON-RPC stdio 插件发现、匹配与解析
docs/implementation-notes/ 各阶段实现记录
```

## 3. 核心任务模型

每个 `core.Task` 包含来源、输出路径、状态、进度和一个当前 `Stage`。`Stage.State` 保存 Worker 特有的持久字段；Scheduler 在 Add、Load、Worker 调用和 Save 边界复制 map，避免共享可变状态。

Worker 通过 `ProgressUpdate` 回报：

- 已接收字节、总大小、速度和进度；
- `Detail` 与显式状态迁移；
- `StageState` 增量；
- `UsesSlot`，用于动态占用或释放并发槽位。

BT 进入 `seeding` 后设置 `UsesSlot=false`，因此做种任务不会阻塞等待中的普通下载。

## 4. Scheduler 生命周期约束

- `waiting` 任务可直接切换为 `paused`。
- 同一任务旧 Worker 退出前不会重复启动新 Worker。
- 事件队列满时丢弃最旧事件并保留最新状态，防止终态永久丢失。
- `StopAll` 先标记 stopping、取消 Worker、等待其 checkpoint/关闭完成，再执行最终保存。
- 新增 Worker 状态字段时，应通过 `ProgressUpdate` 交给 Scheduler 合并和持久化，不要由 Worker 直接写任务数据库。

## 5. BitTorrent 数据流

1. `IsSource` 识别 torrent 或 Magnet。
2. `Resolve` 获取并解析 metainfo，合并 Tracker，构建 `core.Task`。
3. UI 在 Scheduler.Add 前调用 `SetSelectedFiles` 固化选择。
4. GUI 通过同目录 `gd3-bt-runtime.exe` 启动运行时；运行时 Worker 创建 anacrolix client 与任务专属 `resumeFileStorage`。
5. 下载进度只根据哈希校验成功的 piece 计算。
6. piece completion 存储在 `<task-path>/.gd3_bt/<task-id>`。
7. 所选内容完成后迁移到 `seeding`；分享率或时间条件满足后完成。

### 文件索引与路径

- 文件选择必须保留 metainfo 原始索引，padding 会造成索引不连续。
- 输出路径经过 traversal 检查、Windows 路径清洗和清洗后碰撞检查。
- 共享边界 piece 所需的未选文件或 padding 数据写入 `.gd3_unselected` / `.gd3_padding`。
- `resumeFileStorage` 每次操作打开并关闭文件，确保 Windows 下暂停或删除时不残留文件句柄。

### 网络配置

- HTTP(S)/SOCKS5 代理应用于 torrent 元数据、HTTP/WebSocket Tracker 和 WebSeed。
- 请求头应用于 HTTP 与 WebSocket Tracker。
- 原生 Peer、DHT 和 UDP Tracker 使用 anacrolix 网络栈。
- 指定监听端口发生占用或 Windows 访问拒绝型 bind 错误时，会重试临时端口。

## 6. 添加新下载类型

1. 在 `internal/download/<type>` 实现 `core.Worker`。
2. 定义解析入口，将来源转换为 `core.Task` 和持久 `Stage.State`。
3. 在 `internal/app/app.go` 注册 Worker。
4. 在 UI 与浏览器桥接的来源路由中加入识别规则。
5. 为暂停、恢复、清理、重新下载和应用退出补齐测试。
6. 如任务存在“不占下载槽”的阶段，通过 `UsesSlot` 上报，不要绕过 Scheduler。

## 7. BT 运行时边界

Go 在 Windows 上默认把包静态链接进 EXE。为了保留完整符号/DWARF 调试能力，同时控制 GUI 文件体积，BT 引擎采用独立进程而不是剥离调试符号：

- GUI 只依赖 `internal/btruntime`，不依赖 `internal/download/bt` 或 anacrolix。
- `resolve` 与 `reset` 使用一次性 JSON 请求/响应。
- `run` 使用标准输入发送 Task，并在标准输出持续发送 `ProgressUpdate` JSON。
- GUI 关闭运行时标准输入表示取消；运行时先保存 checkpoint、关闭 BT 会话，再发送终态。
- runtime 缺失时只影响 BT 创建/运行，并返回包含预期同目录文件名的错误。

修改进程协议后必须同时验证：`go list -deps ./cmd/gd3win` 不包含 anacrolix/Pion，以及真实 runtime 进程可以解析 torrent、下载本地 WebSeed 并响应取消。

## 8. 构建与验证

```powershell
gofmt -w cmd internal
go mod tidy
go test -count=1 ./...
go test -race -count=1 ./...
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
go vet ./...
go build -trimpath ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
.\scripts\build-release.ps1
```

Stage 15 的自动化基线为：总语句覆盖率 54.1%；`cmd/gd3win` 51.6%、`cmd/gd3-bt-runtime` 42.6%、`internal/app` 34.4%、`internal/ui` 24.7%、`internal/win32` 24.7%。覆盖率用于定位未执行分支，合并条件仍以关键行为断言、全量测试、竞态检测和构建同时通过为准。

命令入口使用小范围可替换函数隔离 GUI 消息框和应用启动，以验证成功、重复启动、普通错误与 panic 退出码。BT runtime 的 `run`、`runWorker` 和 `encodeResult` 接受 `io.Reader` / `io.Writer`，测试不得替换进程级标准流或启动真实公网任务。`internal/app` 的来源路由测试使用 `httptest` 回环服务器。

BT 本地集成测试使用动态生成的 bencode torrent 与 `httptest` Range WebSeed，不依赖公共 swarm：

```powershell
go test ./internal/download/bt `
  -run 'TestTorrentTransfer(DownloadsFromLocalWebseedAndResumes|ResumesVerifiedPartialPieces)' `
  -v -count=3
```

独立运行时端到端测试：

```powershell
$env:GD3_BT_RUNTIME_TEST_PATH = (Resolve-Path .\dist\gd3-bt-runtime.exe).Path
go test -run TestRuntimeProcessResolveAndDownload -v ./internal/btruntime
Remove-Item Env:\GD3_BT_RUNTIME_TEST_PATH
```

修改 BT 恢复逻辑后，还应确认测试目录和仓库根目录没有生成 `.torrent.db`。测试中直接创建默认 anacrolix client 时，要把 `ClientConfig.DataDir` 指向 `t.TempDir()`。

### 便携包验收

```powershell
.\scripts\package-portable.ps1 -Version 0.1.12-stage18
Get-FileHash -Algorithm SHA256 .\release\GhostDownloader-0.1.12-stage18-windows-x64-portable.zip
```

发布目录只保留 ZIP 与同名 `.sha256`；展开目录由脚本清理。验收时还要逐项核对 ZIP 内 `release-manifest.json` 的大小和 SHA-256，并确认主程序、BT runtime、README、用户指南和便携更新脚本均在包内。

## 9. 文档与阶段记录

- 蓝图描述目标设计与分阶段验收标准。
- `implementation-notes/NNN-*.md` 记录实际实现、差异、待办和验证命令。
- 功能行为改变时同步更新用户指南；架构契约改变时同步更新本文件。
- 文档中的“已实现”必须有代码或测试依据；待验证项保留在阶段记录的 Pending 中。
- 当前实现状态以 README 和最新编号的阶段记录为准；历史阶段记录保留当时事实，不回写成当前状态。

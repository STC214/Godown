# Ghost Downloader 3 开发者文档

## 1. 技术栈

- Go 1.26.1
- Win32 GUI：`github.com/lxn/walk`
- 任务/配置存储：SQLite
- BitTorrent：`github.com/anacrolix/torrent`
- BT piece completion：BoltDB
- 速率控制：`golang.org/x/time/rate`

入口为 `cmd/gd3win/main.go`，应用装配位于 `internal/app`。

## 2. 目录结构

```text
cmd/gd3win/                 Windows 程序入口、manifest 与资源
internal/app/               配置、存储、Worker、调度器和 UI 装配
internal/core/              Task 模型、状态机与 Scheduler
internal/config/            路径和 Settings
internal/storage/           SQLite 配置与任务存储
internal/download/http/     HTTP 探测、分块下载和恢复
internal/download/m3u8/     M3U8 解析及外部下载器 Worker
internal/download/ffmpeg/   媒体合并
internal/download/bt/       torrent 解析、文件选择模型、Worker 与恢复存储
internal/browserbridge/     本地浏览器 HTTP/WebSocket 桥接
internal/ui/                Walk 主窗口、设置和 BT 文件选择
internal/win32/             DPI 与单实例支持
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
4. Worker 创建 anacrolix client 与任务专属 `resumeFileStorage`。
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

## 7. 构建与验证

```powershell
gofmt -w cmd internal
go mod tidy
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go build -trimpath -o gd3win.exe ./cmd/gd3win
```

BT 本地集成测试使用动态生成的 bencode torrent 与 `httptest` Range WebSeed，不依赖公共 swarm：

```powershell
go test ./internal/download/bt `
  -run 'TestTorrentTransfer(DownloadsFromLocalWebseedAndResumes|ResumesVerifiedPartialPieces)' `
  -v -count=3
```

修改 BT 恢复逻辑后，还应确认测试目录和仓库根目录没有生成 `.torrent.db`。测试中直接创建默认 anacrolix client 时，要把 `ClientConfig.DataDir` 指向 `t.TempDir()`。

## 8. 文档与阶段记录

- 蓝图描述目标设计与分阶段验收标准。
- `implementation-notes/NNN-*.md` 记录实际实现、差异、待办和验证命令。
- 功能行为改变时同步更新用户指南；架构契约改变时同步更新本文件。
- 文档中的“已实现”必须有代码或测试依据；待验证项保留在阶段记录的 Pending 中。


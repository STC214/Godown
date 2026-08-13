# Ghost Downloader Go + Win32 重构蓝图

本文档是后续重构的基准。目标不是机械翻译 Python/PySide6 代码，而是在 Windows 上用 Go + Win32 做一个资源占用更低、分发更干净、体验足够现代的下载器。实现时需要持续对照 Ghost-Downloader-3 当前源码，确保关键行为一致，UI 则做功能完整和视觉近似，不追求 PySide6/qfluentwidgets 的逐像素复刻。

## 0. 本地对照仓库

原项目源码已拉到本工作区：

```text
reference/Ghost-Downloader-3/
```

当前对照提交：

```text
3940eedb4194967fbeb451fd9b48b7bfb05b3156
```

浏览器扩展上游子模块也已初始化：

```text
reference/Ghost-Downloader-3/browser_extension/upstream
1834b137cbbb4ba1a9f84a53065b76fd7a60d68e
```

后续实现时默认规则：

- `reference/Ghost-Downloader-3` 是只读对照目录，不在里面做重构代码。
- Go + Win32 新项目代码写在工作区根目录的 Go module 中。
- 每做一个模块，先读本蓝图对应章节，再读对照仓库中的源文件。
- 如果实现行为和原仓库不同，需要在实现提交或任务记录里说明原因。
- 如果为了 Windows 原生体验故意不复刻 UI 细节，也要确保功能入口、状态反馈、错误提示完整。

## 1. 项目目标

### 1.1 核心目标

- Windows 专用桌面下载器，优先支持 Windows 10/11。
- Go 主程序承担高频核心逻辑，减少 Python 桌面栈和 Qt 运行时开销。
- 保留 Ghost Downloader 3 的关键能力：
  - HTTP/HTTPS 分块下载
  - 断点续传
  - 暂停、恢复、取消、重新下载
  - 任务队列和最大并发任务数
  - 全局限速
  - 浏览器扩展投递任务
  - M3U8/DASH 运行时管理
  - FFmpeg 合并
  - BT/Magnet
  - 设置、分类、托盘、文件关联
- UI 功能完整、层级清楚、现代、稳定，不做 PySide6 的像素级复刻。
- 插件能力采用多进程或脚本协议，不使用 Go 原生 plugin。

### 1.2 非目标

- 暂不考虑 macOS/Linux。
- 不做 PySide6/qfluentwidgets 逐像素复刻。
- 不把 ffmpeg、N_m3u8DL-RE、BT 协议栈全部重写为 Go。
- 不在第一阶段做完整插件市场。
- 不在第一阶段兼容所有历史任务文件格式，但需要预留迁移入口。

## 2. 当前仓库能力拆分

原项目主体是 Python + PySide6 + FeaturePack 插件架构，另有 TypeScript 浏览器扩展。

### 2.1 应保留并重构为 Go 的部分

- 任务模型：参考 `app/bases/models.py`
- 任务调度：参考 `app/services/core_service.py`
- 任务持久化：参考 `app/services/task_service.py`
- FeaturePack 匹配和解析思想：参考 `app/services/feature_service.py`
- HTTP 下载核心：参考 `features/http_pack/pack.py`、`features/http_pack/task.py`
- FTP 下载能力：参考 `features/ftp_pack/task.py`
- 浏览器扩展桥：参考 `app/services/browser_service.py`
- 设置项和路径管理：参考 `app/supports/config.py`、`app/supports/paths.py`
- 更新检查：参考 `app/supports/update.py`

### 2.2 应保留外部工具的部分

- M3U8/DASH：继续调用 `N_m3u8DL-RE`
- 音视频合并：继续调用 `ffmpeg` 和 `ffprobe`
- BT/Magnet：
  - 首选评估 Go 库 `anacrolix/torrent`
  - 若稳定性或资源控制不达标，改用 sidecar 进程封装 libtorrent

### 2.3 可简化或降级的部分

- PySide6 Fluent UI：改为 Win32 原生控件加少量自绘，视觉近似即可。
- `jack_yao` 资源页：第一阶段不做，或作为远程资源插件示例。
- Crowdin/i18n：第一阶段先中文，后续再做资源表和语言包。
- 自动代码签名发布：第一阶段不做，只保留构建脚本接口。

## 3. 总体架构

建议采用分层结构：

```text
cmd/gd3win/
  main.go

internal/app/
  bootstrap.go
  shutdown.go

internal/core/
  task.go
  stage.go
  scheduler.go
  events.go

internal/download/http/
  probe.go
  task.go
  worker.go
  resume.go

internal/download/ftp/
  task.go
  worker.go

internal/download/m3u8/
  task.go
  runtime.go
  worker.go

internal/download/ffmpeg/
  runtime.go
  merge.go

internal/download/bt/
  task.go
  worker.go

internal/browserbridge/
  server.go
  protocol.go
  pairing.go

internal/pluginhost/
  protocol.go
  manager.go

internal/storage/
  db.go
  task_store.go
  config_store.go

internal/ui/
  app_window.go
  task_page.go
  settings_page.go
  add_task_dialog.go
  tray.go
  theme.go

internal/win32/
  dpi.go
  message_loop.go
  notify.go
  file_assoc.go
  shell.go

internal/config/
  config.go
  defaults.go

internal/update/
  github_release.go
```

第一版可以减少目录数量，但边界必须保留：UI 不直接做网络下载，下载 Worker 不直接操作 UI，所有状态变化通过事件总线汇聚。

## 4. 关键技术选型

### 4.1 语言和构建

- Go 1.23+ 或当前稳定版。
- Windows GUI 子系统构建：
  - 开发阶段可以保留控制台。
  - 发布阶段使用 `-ldflags="-H windowsgui"`。
- 资源嵌入：
  - 图标、默认配置、模板使用 `go:embed`。
  - 大型运行时不内嵌，走安装目录管理。

### 4.2 UI 技术路线

候选：

- `github.com/lxn/walk`
  - 优点：开发快，Win32 包装成熟。
  - 缺点：现代 UI 和自绘需要额外工作。
- `github.com/lxn/win` 直接 Win32
  - 优点：控制力最强。
  - 缺点：开发量大，容易写出卡 UI 的代码。

建议路线：

- 第一阶段用 `walk` 快速形成可用 UI。
- 对任务卡片、侧边栏、状态标签等关键元素做自绘或 owner-draw。
- 保留一层 `internal/ui/platform` 抽象，避免未来被 UI 库锁死。

### 4.3 持久化

建议使用 SQLite。

理由：

- 比 JSONL 更适合任务列表、分类、筛选、状态查询。
- 崩溃恢复更稳。
- 后续支持历史记录、搜索、任务编辑更方便。

可选库：

- `modernc.org/sqlite`：纯 Go，分发干净，但体积和性能需评估。
- `github.com/mattn/go-sqlite3`：成熟，但需要 CGO。

第一阶段推荐 `modernc.org/sqlite`，除非性能或兼容性出问题。

### 4.4 日志

- 使用 `slog` 或 `zap`。
- 默认日志位置：
  - `%APPDATA%/GhostDownloaderGo/GhostDownloader.log`
- 日志需要滚动，避免长期运行膨胀。

## 5. 任务模型

### 5.1 Task

对应原项目 `Task`。

字段建议：

```go
type Task struct {
    ID           string
    PackID       string
    Title        string
    URL          string
    Status       TaskStatus
    Path         string
    FileSize     int64
    Received     int64
    Speed        int64
    CreatedAt    time.Time
    Category     string
    UsesSlot     bool
    CanPause     bool
    Stages       []*Stage
    MetadataJSON []byte
}
```

### 5.2 Stage

对应原项目 `TaskStage`。

字段建议：

```go
type Stage struct {
    ID        string
    TaskID    string
    Index     int
    Kind      string
    Status    TaskStatus
    Progress  float64
    Received  int64
    Speed     int64
    Error     string
    StateJSON []byte
}
```

`StateJSON` 存协议特有字段，例如 HTTP headers、分块信息、BT resume data、M3U8 参数。

### 5.3 状态机

状态：

- Waiting
- Running
- Paused
- Completed
- Failed
- Canceled

规则：

- Task 状态由 Stage 汇总。
- 任一 Stage Failed，则 Task Failed。
- 所有 Stage Completed，则 Task Completed。
- 存在 Running，则 Task Running。
- 暂停只能由调度器发起 cancellation，不允许 Worker 自己直接杀 UI 状态。

## 6. 调度器设计

参考原项目 `CoreService`。

### 6.1 职责

- 管理等待队列。
- 管理运行任务。
- 控制最大并发任务数。
- 提供启动、暂停、取消、重新下载。
- 接收配置变更后重新平衡队列。
- 聚合任务事件并发给 UI、浏览器桥和持久化层。

### 6.2 并发模型

- 每个 Task 一个 goroutine。
- 每个 Stage 根据类型内部创建 goroutine。
- 所有任务必须接受 `context.Context`。
- 暂停、取消必须通过 context cancellation 传播。
- Worker 只发事件，不直接改 UI。

### 6.3 事件类型

```go
type EventKind string

const (
    EventTaskAdded      EventKind = "task_added"
    EventTaskUpdated    EventKind = "task_updated"
    EventTaskRemoved    EventKind = "task_removed"
    EventStageUpdated   EventKind = "stage_updated"
    EventTaskCompleted  EventKind = "task_completed"
    EventTaskFailed     EventKind = "task_failed"
)
```

UI 订阅事件后通过主线程消息投递更新控件。

## 7. HTTP 下载复刻方案

这是第一阶段最重要的功能，建议做到接近原项目。

### 7.1 Range 探测

参考 `features/http_pack/pack.py`。

步骤：

1. 带 `Range: bytes=1-1` 请求。
2. 若返回 `206` 且有 `Content-Range`，视为支持 Range。
3. 若返回 `200`，读 `Content-Length`，必要时用 `Range: bytes=0-0` 回退探测。
4. 解析文件名：
   - `Content-Disposition`
   - `Content-Location`
   - URL query
   - URL path
   - MIME 推断扩展名

### 7.2 分块下载

参考 `features/http_pack/task.py`。

行为：

- 支持已知大小多块下载。
- 支持未知大小单流下载。
- 支持服务器不支持 Range 时从头下载。
- 支持 `.ghd` 续传记录。
- 支持预分配文件大小。
- 支持全局限速。
- 支持自动重试。

### 7.3 续传文件格式

可以继续使用二进制记录，便于兼容思想：

```text
每个分块：
uint64 start
uint64 progress
uint64 end
```

文件路径：

```text
<output_file>.ghd
```

### 7.4 自动加速

保留原项目思路：

- 记录最近 5 秒速度。
- 若速度稳定且仍有大块剩余，拆分最慢块。
- 若增加 worker 后速度提升不明显，则停止自动加速。

### 7.5 文件写入

必须使用随机写：

- Windows 用 `os.File.WriteAt`。
- 多 goroutine 写同一文件时，各分块写入不重叠。
- 预分配可用 `Truncate`，后续再评估 Windows 稀疏文件和真实预分配。

## 8. M3U8/DASH 方案

继续调用 `N_m3u8DL-RE`。

### 8.1 Go 主程序职责

- 解析输入是否为 m3u8/m3u/mpd。
- 下载 manifest 做基础判断：
  - 类型：HLS 或 DASH
  - 是否直播
  - 标题推断
  - 可选轨道枚举，第一版可简化
- 生成 N_m3u8DL-RE 参数。
- 启动进程。
- 读取 stdout/stderr。
- 解析进度。
- 暂停时终止进程。
- 直播任务停止后按完成处理。
- 管理 N_m3u8DL-RE 安装路径和版本检测。

### 8.2 不在 Go 内重写的内容

- HLS 分片下载。
- AES/DRM 解密。
- DASH 复杂轨道选择。
- TS/MP4 混流。

### 8.3 风险

- 子进程输出格式会随 N_m3u8DL-RE 版本变化。
- Windows 路径、空格和非 ASCII 路径必须全部用参数数组传递，不能拼 shell 字符串。
- 终止进程需要确保子进程树被清理。

## 9. FFmpeg 合并方案

继续调用 `ffmpeg` 和 `ffprobe`。

职责：

- 检测安装路径。
- 支持用户手动指定。
- Windows 可提供一键安装。
- 合并音视频资源：
  - 下载 video stage
  - 下载 audio stage
  - 执行 ffmpeg copy merge
- 通过 `-progress pipe:1` 读取进度。
- 失败时保留中间文件，方便重试。
- 成功后按配置清理中间文件。

## 10. BT/Magnet 方案

### 10.1 第一选择：Go 原生库

优先评估 `anacrolix/torrent`。

需要验证：

- Magnet 元数据获取速度。
- 文件选择。
- 暂停恢复。
- 限速。
- DHT、PEX、tracker。
- 代理。
- 做种比例和时间限制。
- Windows 长时间运行资源占用。

### 10.2 备选：sidecar libtorrent

如果 Go 库不稳，则使用独立进程：

```text
gd3-bt-sidecar.exe
```

通信方式：

- 本地 stdin/stdout JSON-RPC，或
- 本地 TCP/WebSocket

优点：

- 主 UI 仍保持 Go + Win32。
- BT 协议栈继续用成熟 libtorrent。
- sidecar 崩溃不会直接带崩主程序。

缺点：

- 分发多一个 exe。
- 需要协议和生命周期管理。

## 11. 浏览器扩展桥

参考原项目 `BrowserService` 和 `browser_extension/app/src/background/desktop-bridge.ts`。

### 11.1 服务端

- 地址：默认 `ws://127.0.0.1:14370`
- 只监听 localhost。
- 支持配对 token。
- token 存储在本地配置中。
- 用户可重置 token。

### 11.2 协议

保留原语义：

- `pair_request`
- `pair_result`
- `hello`
- `hello_ack`
- `subscribe_tasks`
- `task_snapshot`
- `create_task`
- `create_task_result`
- `task_action`
- `task_action_result`
- `error`

### 11.3 安全要求

- 未认证连接只能发起配对或 hello。
- 认证失败立即关闭连接。
- token 不写日志。
- 只允许 localhost，除非用户显式开启远程监听。
- 所有 payload 做类型检查。

## 12. 插件生态方案

不使用 Go 原生 plugin。

### 12.1 推荐协议

多进程插件：

```text
plugin.exe --stdio
```

使用 JSON-RPC over stdio。

基本方法：

- `manifest`
- `matches`
- `parse`
- `taskCardHints`
- `settingsSchema`

### 12.2 插件语言

允许：

- Go
- Python
- Node.js
- 任意能读写 stdio 的语言

### 12.3 资源开销控制

- 插件按需启动。
- 空闲超时退出。
- 插件进程有最大内存和执行超时。
- 插件崩溃只影响对应功能。

### 12.4 第一阶段策略

第一阶段不做完整插件 UI，只做内置 pack：

- http
- m3u8
- ffmpeg
- bt
- browser

第二阶段再开放插件协议。

## 13. Win32 UI 蓝图

### 13.1 视觉方向

目标：现代 Windows 工具，不复刻 Qt 细节。

关键词：

- 左侧导航
- 顶部工具栏
- 中间任务列表
- 右侧或弹窗设置
- 轻量卡片
- 清晰状态色
- 暗色模式
- 高 DPI 正常

### 13.2 主窗口布局

```text
+-----------------------------------------------------------+
| Title Bar / Toolbar                                       |
+-------------+---------------------------------------------+
| Sidebar     | Task Page                                   |
|             | + Add URL / Start / Pause / Settings        |
| Downloading |                                             |
| Completed   | Task List                                   |
| Failed      |                                             |
| Settings    |                                             |
+-------------+---------------------------------------------+
| Status Bar: speed, active tasks, selected task info        |
+-----------------------------------------------------------+
```

### 13.3 页面

- 任务页
  - 全部
  - 下载中
  - 已完成
  - 失败
- 添加任务弹窗
  - URL
  - 保存目录
  - 分块数量
  - 请求头
  - 代理
- 任务编辑弹窗
  - URL
  - headers
  - proxies
  - 保存目录
- 设置页
  - 下载
  - 网络
  - 浏览器扩展
  - FFmpeg
  - N_m3u8DL-RE
  - BT
  - 外观

### 13.4 任务卡片元素

每个任务显示：

- 文件名
- 来源域名或协议类型
- 状态
- 进度条
- 已下载/总大小
- 当前速度
- 操作按钮：
  - 开始/暂停
  - 取消
  - 重新下载
  - 打开文件
  - 打开目录
  - 更多

### 13.5 UI 不打架规则

- 所有固定区域使用明确最小高度。
- 长文件名必须单行省略或两行截断。
- 按钮使用固定尺寸。
- 进度条不能随文本变化改变高度。
- DPI 改变时重新布局。
- 小窗口宽度下隐藏次要列，不允许文字互相覆盖。
- 所有弹窗最小宽高受控，内容区可滚动。

## 14. Go + Win32 UI 卡死风险清单

这是实现时必须严格遵守的部分。

### 14.1 禁止在 UI 线程做阻塞操作

UI 线程禁止：

- HTTP 请求
- 文件下载
- 大文件 hash
- 解压
- SQLite 长事务
- ffmpeg/N_m3u8DL-RE 等子进程等待
- BT metadata 等待
- DNS 查询
- 读取大目录
- 递归删除

所有这些必须放到 goroutine 或 worker 队列。

### 14.2 禁止从后台 goroutine 直接操作控件

所有 UI 更新必须通过主线程投递：

```go
ui.Post(func() {
    // update controls here
})
```

如果用 Win32 原生层，则使用：

- `PostMessage(hwnd, WM_APP_..., ...)`
- 或一个线程安全 UI dispatcher

后台 goroutine 直接 SetText、Invalidate、InsertItem 都可能造成随机崩溃或卡死。

### 14.3 高频进度更新必须合并

下载线程可能每秒产生大量事件，不能每个 chunk 都更新 UI。

规则：

- Worker 可以高频更新内部计数。
- UI 每 250ms 到 1000ms 聚合刷新一次。
- 浏览器 task snapshot 每 1000ms 推一次即可。
- SQLite 持久化用 debounce，例如 200ms 到 1000ms。

### 14.4 UI 锁和任务锁不能交叉

禁止：

```text
拿 task mutex -> 同步调用 UI -> UI 回调再拿 task mutex
```

这会死锁。

规则：

- Core 层发不可变快照给 UI。
- UI 不持有 core 内部锁。
- UI 命令通过 channel 或方法投递给 scheduler。

### 14.5 子进程输出必须异步读取

启动 ffmpeg/N_m3u8DL-RE 后必须同时读取 stdout/stderr。

错误写法：

```go
cmd.Run()
```

当管道缓冲区满时可能卡死。

正确策略：

- `cmd.Start()`
- goroutine 读取 stdout
- goroutine 读取 stderr
- `cmd.Wait()`
- context cancel 时 kill 进程树

### 14.6 不要在 WM_PAINT 做重活

`WM_PAINT` 只绘制已有数据。

禁止：

- 查询数据库
- 格式化大量字符串
- 读取文件图标
- 请求网络
- 遍历任务全量数据

图标和文本应提前缓存，绘制时只使用快照。

### 14.7 不要滥用同步 SendMessage

跨线程不要 `SendMessage` 到 UI 线程，优先 `PostMessage`。

`SendMessage` 可能在 UI 等待后台锁时造成死锁。

### 14.8 DPI 和暗色模式

程序启动早期必须设置 DPI awareness。

建议：

- `SetProcessDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2)`
- 处理 `WM_DPICHANGED`
- 字体、间距、图标按 DPI 重算

暗色模式：

- 先做应用内深色主题。
- 原生标题栏深色可后续接 `DwmSetWindowAttribute`。

### 14.9 大列表虚拟化

任务多时不能为每个任务创建大量子控件。

策略：

- 第一版任务数少可以简单控件。
- 之后任务列表必须虚拟化或 owner-draw。
- 只绘制可见区域。

### 14.10 Shell 操作异步化

打开文件、打开目录、注册文件关联、通知、托盘菜单都可能阻塞。

策略：

- ShellExecute 可从 UI 发起，但失败处理不能阻塞。
- 文件关联写注册表放后台，并提示需要管理员权限的情况。

## 15. 数据库设计草案

### 15.1 tasks

```sql
CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    pack_id TEXT NOT NULL,
    title TEXT NOT NULL,
    url TEXT NOT NULL,
    status TEXT NOT NULL,
    path TEXT NOT NULL,
    file_size INTEGER NOT NULL,
    received INTEGER NOT NULL,
    speed INTEGER NOT NULL,
    category TEXT NOT NULL,
    uses_slot INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    metadata_json BLOB
);
```

### 15.2 stages

```sql
CREATE TABLE stages (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL,
    stage_index INTEGER NOT NULL,
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    progress REAL NOT NULL,
    received INTEGER NOT NULL,
    speed INTEGER NOT NULL,
    error TEXT NOT NULL,
    state_json BLOB,
    FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE
);
```

### 15.3 config

```sql
CREATE TABLE config (
    key TEXT PRIMARY KEY,
    value_json BLOB NOT NULL
);
```

## 16. 配置项

第一阶段配置：

- 下载目录
- 最大同时任务数
- 默认分块数
- 全局限速
- 最大自动拆块大小
- SSL 验证
- 代理
- 浏览器扩展开关
- 浏览器配对 token
- FFmpeg 安装目录
- N_m3u8DL-RE 安装目录
- 主题模式
- 关闭到托盘

当前实现状态：

- 下载目录、最大同时任务数、默认分块数、全局限速、显式代理 URL、多行请求头已进入 SQLite `config` 表。
- UI 仍是阶段 1 临时入口，后续阶段 2 再整理为正式设置页。

## 17. 运行时目录

建议：

```text
%APPDATA%/GhostDownloaderGo/
  config.db
  GhostDownloader.log
  runtimes/
    ffmpeg/
    N_m3u8DL-RE/
  temp/
```

下载临时文件：

```text
<download_file>.ghd
```

M3U8 临时目录：

```text
<download_dir>/.gd3_m3u8/<task_id>/
```

## 18. 实现阶段计划

### 阶段 0：项目骨架

- 初始化 Go module。
- 建立日志、配置、数据库。
- 建立 UI 主窗口空壳。
- 设置 DPI awareness。
- 建立主线程 dispatcher。

验收：

- 程序可启动。
- 主窗口显示。
- 关闭、最小化、托盘基本正常。
- 日志和配置目录创建正常。

### 阶段 1：HTTP 下载闭环

- URL 解析。
- Range 探测。
- 添加任务弹窗。
- HTTP 分块下载。
- 暂停、恢复、取消。
- `.ghd` 续传。
- 任务持久化。当前实现已使用 SQLite 保存任务 payload，后续再扩展为完整字段 schema。
- 任务列表实时刷新。

验收：

- 大文件可分块下载。
- 关闭重开后可恢复。
- 暂停恢复不损坏文件。
- UI 下载时不卡。

### 阶段 2：设置和基础 Windows 集成

- 设置页。
- 代理。
- 限速。
- 托盘。
- 打开文件和目录。
- 文件关联初版。
- 通知初版。

### 阶段 3：FFmpeg 和 M3U8

- FFmpeg 检测和配置。
- N_m3u8DL-RE 检测和配置。
- 一键下载运行时。
- M3U8/DASH 任务创建。
- 子进程进度解析。
- 音视频合并任务。

### 阶段 4：浏览器扩展桥

当前实现说明见：

- `docs/implementation-notes/004-stage-4-browser-bridge.md`

- WebSocket server。
- 配对 token。
- task snapshot。
- create_task。
- task_action。
- 与现有扩展协议尽量兼容。

### 阶段 5：BT/Magnet

- 评估并接入 BT 方案。
- 种子文件解析。
- Magnet 元数据。
- 文件选择。
- 续传和做种限制。

### 阶段 6：插件协议

- JSON-RPC stdio 插件原型。
- 插件 manifest。
- 插件 matches/parse。
- Python 插件示例。

### 阶段 7：UI 打磨和发布

- 暗色模式。
- 高 DPI 修正。
- 任务列表虚拟化。
- 安装包。
- 自动更新。
- 崩溃日志。

## 19. 与原项目对照验收清单

每实现一个功能，都要对照原仓库行为。

### HTTP

- Range 探测一致。
- 文件名推断一致或更合理。
- 分块恢复一致。
- 不支持 Range 时行为一致。
- 全局限速生效。
- 自动加速策略保留。

### 任务系统

- 状态汇总一致。
- 暂停恢复一致。
- 失败任务可重试。
- 已完成任务可打开文件。
- 任务记录能在重启后恢复。

### 浏览器桥

- 配对流程一致。
- token 校验一致。
- create_task payload 兼容。
- task_action 兼容。

### M3U8/FFmpeg

- 缺少运行时时提示明确。
- 运行时路径可配置。
- 子进程失败信息可见。
- 暂停或停止不会残留不可控进程。

## 20. 主要风险

- Win32 UI 开发量被低估。
- Walk 控件不够现代，后续需要 owner-draw。
- BT Go 库可能不如 libtorrent 稳。
- M3U8/N_m3u8DL-RE 输出格式变化导致进度解析失效。
- 浏览器扩展协议兼容需要仔细测试。
- SQLite 写入过频影响下载性能。
- 杀子进程树处理不好会残留 ffmpeg/N_m3u8DL-RE。
- 高 DPI 和暗色模式容易出现布局错位。

## 21. 第一阶段建议交付物

第一阶段不要贪多，只做最小强闭环：

- Go + Win32/WALK 主窗口
- 添加 HTTP 任务
- HTTP 分块下载
- 暂停恢复
- 任务持久化
- 设置下载目录和分块数
- UI 实时进度
- 下载完成打开文件/目录

这个闭环跑稳后，再接 M3U8、FFmpeg、浏览器扩展和 BT。

## 22. 实现纪律

- 任何阻塞任务必须离开 UI 线程。
- 任何 UI 更新必须回到 UI 线程。
- 任何状态变化必须经过 scheduler。
- 任何子进程必须异步读取输出。
- 任何网络请求都必须支持 context cancellation。
- 任何持久化都不能在高频路径同步阻塞 UI。
- 任何新增 UI 元素都要在小窗口和高 DPI 下检查是否重叠。
- 任何外部工具路径都不能拼 shell 字符串，必须使用参数数组。
- 任何 token、cookie、代理密码都不能写入日志。

## 23. 按图索骥工作流

每一轮实现都按同一个节奏走，避免越写越散。

### 23.1 开工前

1. 明确本轮目标属于哪个阶段。
2. 打开本蓝图对应章节。
3. 打开 `reference/Ghost-Downloader-3` 中对应源码。
4. 写下本轮要复刻的行为，不把顺手发现的无关问题混进来。
5. 判断是否触碰 UI 线程、子进程、数据库、高频进度更新。

### 23.2 实现中

1. 先建数据结构和接口，再接 UI。
2. 先做可取消的后台逻辑，再做按钮。
3. 先跑小文件和失败路径，再跑大文件。
4. 所有状态变化走 scheduler。
5. 所有 UI 更新走 dispatcher。
6. 每接入一个外部工具，都先做路径检测和错误提示。

### 23.3 完成前

1. 对照本蓝图的阶段验收。
2. 对照原仓库源文件行为。
3. 测试取消、暂停、失败、重启恢复。
4. 测试高 DPI 和窗口缩小。
5. 检查日志中没有 token、cookie、代理密码。
6. 检查没有后台 goroutine 直接操作 UI 控件。

### 23.4 记录方式

每完成一个模块，在项目中保留简短实现记录，建议路径：

```text
docs/implementation-notes/
```

记录内容：

- 对照的原仓库文件
- 已复刻行为
- 故意不同的地方
- 未完成点
- 已跑测试

## 24. 文件级对照索引

本节是后续实现时最直接的索引。

| 新模块 | 对照源码 | 复刻重点 | 可接受差异 |
|---|---|---|---|
| `internal/app` | `Ghost-Downloader-3.py` | 启动顺序、日志、配置加载、服务初始化、退出清理 | 不复刻 Qt splash |
| `internal/config` | `app/supports/config.py` | 默认配置、下载目录、代理、限速、主题、运行时路径 | 配置存储改 SQLite/JSON |
| `internal/core` | `app/bases/models.py`, `app/services/core_service.py` | Task/Stage 状态机、队列、最大并发、暂停恢复 | Go 类型可重新设计 |
| `internal/storage` | `app/services/task_service.py` | 任务恢复、延迟 flush、崩溃后尽量保留记录 | 从 JSONL 改 SQLite |
| `internal/download/http` | `features/http_pack/pack.py`, `features/http_pack/task.py` | Range 探测、文件名推断、分块、`.ghd`、自动拆块 | 内部结构可重写 |
| `internal/download/ftp` | `features/ftp_pack/task.py`, `features/ftp_pack/pack.py` | FTP/FTPS 解析、目录任务、REST 续传、代理 | 第一版可后置 |
| `internal/download/m3u8` | `features/m3u8_pack/pack.py`, `features/m3u8_pack/task.py`, `features/m3u8_pack/config.py` | manifest 判断、N_m3u8DL-RE 参数、进度解析、运行时安装 | 轨道选择第一版可简化 |
| `internal/download/ffmpeg` | `features/ffmpeg_pack/pack.py`, `features/ffmpeg_pack/task.py`, `features/ffmpeg_pack/config.py` | ffmpeg 检测、下载运行时、合并、进度读取 | 只支持 Windows 一键安装 |
| `internal/download/bt` | `features/bittorrent_pack/*` | torrent/magnet、文件选择、续传、做种限制、tracker | 可先评估 Go 库再决定 |
| `internal/browserbridge` | `app/services/browser_service.py`, `browser_extension/app/src/background/desktop-bridge.ts` | WebSocket 配对、token、任务快照、创建任务、任务动作 | payload 可增加字段但保持兼容 |
| `internal/pluginhost` | `app/services/feature_service.py`, `features/*/manifest.toml` | matches/parse 的插件思想、依赖顺序、错误隔离 | 用进程插件，不用 Python import |
| `internal/ui` | `app/view/windows/main_window.py`, `app/view/pages/*`, `app/view/components/*` | 功能入口、任务列表、设置页、添加/编辑弹窗、托盘 | 视觉现代化近似，不像素级 |
| `internal/win32` | `app/supports/file_association.py`, `app/supports/file_open.py`, `app/supports/application.py` | 文件关联、打开文件/目录、单实例、通知、DPI | 全部按 Windows 原生方式实现 |
| `internal/update` | `app/supports/update.py` | GitHub Release 检查、版本比较、平台资产选择 | 更新安装流程可后置 |

## 25. 阶段路线和自查表

### 25.1 阶段 0：骨架

对照文件：

- `Ghost-Downloader-3.py`
- `app/supports/paths.py`
- `app/supports/config.py`
- `app/supports/application.py`

必须完成：

- Go module 初始化。
- 程序启动入口。
- Windows GUI 主线程。
- DPI awareness。
- 日志目录和配置目录。
- 主窗口空壳。
- UI dispatcher。
- 退出时关闭后台服务。

自查：

- 启动不闪退。
- 关闭窗口能正常退出或最小化到托盘。
- 日志文件生成。
- DPI 缩放 100%/125%/150% 不糊、不错位。
- 没有后台 goroutine 直接操作 UI。

### 25.2 阶段 1：HTTP 下载闭环

对照文件：

- `features/http_pack/pack.py`
- `features/http_pack/task.py`
- `app/bases/models.py`
- `app/services/core_service.py`
- `app/services/task_service.py`

必须完成：

- 添加 URL 任务。
- Range 探测。
- 文件名推断。
- 分块下载。
- `.ghd` 记录。
- 暂停、恢复、取消、重新下载。
- 任务入库和恢复。
- UI 进度刷新。
- 打开文件、打开目录。

自查：

- 下载 10MB、1GB 文件都正常。
- 支持 Range 的任务能断点续传。
- 不支持 Range 的任务暂停后按预期从头或不可暂停策略处理。
- 下载中关闭程序，再打开后能恢复。
- 限速打开后全局速度下降。
- UI 下载时可拖动、最小化、打开设置，不假死。
- `.ghd` 在完成后清理。
- 错误 URL 会变 Failed 并显示错误。

### 25.3 阶段 2：设置和 Windows 集成

当前实现说明见：

- `docs/implementation-notes/002-stage-2-windows-settings.md`

对照文件：

- `app/view/pages/setting_page.py`
- `app/supports/config.py`
- `app/supports/file_association.py`
- `app/view/components/tray.py`

必须完成：

- 下载设置。
- 网络代理设置。
- 浏览器扩展开关和 token 显示。
- 托盘菜单。
- 文件关联初版。
- 通知初版。
- 主题模式。

自查：

- 设置变更能持久化。
- 最大并发任务数变更后 scheduler 重新平衡。
- 代理配置不把密码写入日志。
- 托盘退出不遗留进程。
- 文件关联失败有明确提示。

### 25.4 阶段 3：FFmpeg 和 M3U8

对照文件：

- `features/ffmpeg_pack/*`
- `features/m3u8_pack/*`
- `features/disk_pack/*`

必须完成：

- 检测 ffmpeg/ffprobe。
- 检测 N_m3u8DL-RE。
- Windows 一键安装运行时。
- M3U8/DASH 任务创建。
- 子进程进度解析。
- 暂停时终止进程树。
- 合并音视频任务。

自查：

- 未安装运行时时提示清楚。
- 路径带空格、中文时能正常运行。
- 进程失败能看到 stderr 或最后输出。
- 取消任务不会残留 ffmpeg/N_m3u8DL-RE。
- 直播停止逻辑符合预期。

### 25.5 阶段 4：浏览器扩展桥

对照文件：

- `app/services/browser_service.py`
- `browser_extension/app/src/background/desktop-bridge.ts`
- `browser_extension/app/src/shared/types.ts`
- `browser_extension/app/src/page-media/*`

必须完成：

- WebSocket server。
- `pair_request`。
- `hello` token 校验。
- `task_snapshot`。
- `create_task`。
- `task_action`。
- 设置页复制和重置 token。

自查：

- 未配对扩展不能创建任务。
- token 错误立即断开。
- 浏览器创建任务后桌面端出现任务。
- 桌面端任务状态能推给扩展。
- 高频任务更新不会刷爆 UI 或 WebSocket。

### 25.6 阶段 5：BT/Magnet

对照文件：

- `features/bittorrent_pack/loaders.py`
- `features/bittorrent_pack/worker.py`
- `features/bittorrent_pack/task.py`
- `features/bittorrent_pack/trackers.py`
- `features/bittorrent_pack/web_tracker/*`

必须完成：

- torrent 文件解析。
- magnet 元数据获取。
- 文件列表和选择。
- 下载和暂停。
- resume data。
- tracker 合并。
- 做种限制。

自查：

- 大 torrent 文件列表不卡 UI。
- magnet 超时能取消。
- 选部分文件后只下载选中文件。
- 暂停恢复后进度不丢。
- 达到分享率或做种时间后自动停止。

### 25.7 阶段 6：插件协议

对照文件：

- `app/services/feature_service.py`
- `features/*/manifest.toml`
- `app/bases/interfaces.py`

必须完成：

- 插件 manifest。
- 插件发现。
- JSON-RPC stdio。
- matches。
- parse。
- 超时和崩溃隔离。
- Python 示例插件。

自查：

- 插件崩溃不影响主程序。
- 插件卡住会被超时杀掉。
- 插件输出非法 JSON 有明确错误。
- 插件不能直接操作主程序内部状态。

### 25.8 阶段 7：UI 打磨和发布

对照文件：

- `app/view/windows/main_window.py`
- `app/view/components/*`
- `.github/workflows/*`
- `deploy.py`

必须完成：

- 暗色模式。
- 任务列表虚拟化或 owner-draw 优化。
- 安装包。
- 自动更新。
- 崩溃日志入口。
- 基础发布脚本。

自查：

- 1000 个历史任务列表不卡。
- 小窗口下文字不互相覆盖。
- 125%/150% DPI 正常。
- 暗色模式控件没有白块。
- 安装包安装、卸载、覆盖安装正常。

## 26. 行为复刻优先级

### 26.1 必须强复刻

- HTTP Range 探测策略。
- HTTP 分块和 `.ghd` 续传。
- 任务状态汇总。
- 暂停、恢复、取消语义。
- 最大并发任务数。
- 全局限速。
- 浏览器桥 token 配对和任务创建。
- ffmpeg/N_m3u8DL-RE 缺失时的明确错误。

### 26.2 可以等价重构

- 配置存储格式。
- 任务持久化格式。
- FeaturePack 内部加载方式。
- 运行时安装任务的内部结构。
- BT 底层库。
- 更新检查的 UI 表现。

### 26.3 可以视觉近似

- 左侧导航。
- 任务卡片。
- 设置卡片。
- 添加任务弹窗。
- 图标风格。
- 动画。
- Splash screen。

### 26.4 可以暂缓

- 多语言。
- `jack_yao` 资源页。
- 完整插件市场。
- macOS/Linux 构建。
- 代码签名 CI。
- Crowdin 同步。

## 27. 对照阅读顺序

不要一上来通读整个原仓库。按模块读，读完立刻实现。

### 27.1 HTTP 下载

1. `reference/Ghost-Downloader-3/features/http_pack/pack.py`
2. `reference/Ghost-Downloader-3/features/http_pack/task.py`
3. `reference/Ghost-Downloader-3/app/supports/sysio.py`
4. `reference/Ghost-Downloader-3/app/supports/utils.py`

关注：

- `_probe`
- `_fileName`
- `HttpWorker.generateSubworkers`
- `HttpWorker.restoreProgress`
- `HttpWorker.handleSubworker`
- `HttpWorker.supervisor`
- `HttpWorker.checkIfAutoAcceleration`

### 27.2 任务调度

1. `reference/Ghost-Downloader-3/app/bases/models.py`
2. `reference/Ghost-Downloader-3/app/services/core_service.py`
3. `reference/Ghost-Downloader-3/app/services/task_service.py`

关注：

- `Task.updateStatus`
- `Task.pendingStages`
- `Task.run`
- `CoreService.createTask`
- `CoreService._stopTask`
- `CoreService._rebalance`
- `TaskService.scheduleFlush`

### 27.3 浏览器桥

1. `reference/Ghost-Downloader-3/app/services/browser_service.py`
2. `reference/Ghost-Downloader-3/browser_extension/app/src/background/desktop-bridge.ts`
3. `reference/Ghost-Downloader-3/browser_extension/app/src/background.ts`
4. `reference/Ghost-Downloader-3/browser_extension/app/src/shared/types.ts`

关注：

- 消息类型枚举。
- 配对流程。
- token 校验。
- task snapshot 节流。
- create_task payload。
- task_action payload。

### 27.4 M3U8 和 FFmpeg

1. `reference/Ghost-Downloader-3/features/m3u8_pack/pack.py`
2. `reference/Ghost-Downloader-3/features/m3u8_pack/task.py`
3. `reference/Ghost-Downloader-3/features/m3u8_pack/config.py`
4. `reference/Ghost-Downloader-3/features/ffmpeg_pack/pack.py`
5. `reference/Ghost-Downloader-3/features/ffmpeg_pack/task.py`
6. `reference/Ghost-Downloader-3/features/disk_pack/task.py`

关注：

- 运行时检测。
- 一键安装。
- 参数拼装。
- 子进程输出解析。
- 取消时进程清理。
- 中间文件清理。

### 27.5 BT

1. `reference/Ghost-Downloader-3/features/bittorrent_pack/loaders.py`
2. `reference/Ghost-Downloader-3/features/bittorrent_pack/task.py`
3. `reference/Ghost-Downloader-3/features/bittorrent_pack/worker.py`
4. `reference/Ghost-Downloader-3/features/bittorrent_pack/trackers.py`
5. `reference/Ghost-Downloader-3/features/bittorrent_pack/web_tracker/service.py`

关注：

- torrent/magnet 入口差异。
- 文件列表映射。
- priorities。
- resume data。
- seeding 状态和 usesSlot。
- tracker 合并。

### 27.6 UI

1. `reference/Ghost-Downloader-3/app/view/windows/main_window.py`
2. `reference/Ghost-Downloader-3/app/view/pages/task_page.py`
3. `reference/Ghost-Downloader-3/app/view/pages/setting_page.py`
4. `reference/Ghost-Downloader-3/app/view/components/add_task_dialog.py`
5. `reference/Ghost-Downloader-3/app/view/components/cards.py`
6. `reference/Ghost-Downloader-3/app/view/components/tray.py`

关注：

- 功能入口。
- 状态展示。
- 操作按钮。
- 设置分组。
- 错误提示。
- 托盘行为。

不要关注：

- qfluentwidgets 的具体像素。
- Qt layout 细节。
- 翻译资源生成。

## 28. 每次提交前自查

每次实现一块功能，至少检查：

- 是否读过对应 `reference/` 源文件。
- 是否更新或遵守了本蓝图。
- 是否所有耗时操作都在后台。
- 是否所有 UI 更新都经 dispatcher。
- 是否所有 goroutine 都能通过 context 停止。
- 是否所有子进程都能被取消并清理。
- 是否数据库写入有节流或事务边界。
- 是否日志没有敏感信息。
- 是否窗口缩小后 UI 不重叠。
- 是否错误路径有用户可见提示。

## 29. 术语对齐

为避免 Go 新项目和原项目概念漂移，统一术语：

| 术语 | 含义 |
|---|---|
| Task | 用户看到的一条下载任务 |
| Stage | 一个任务中的执行阶段，例如视频下载、音频下载、合并 |
| Worker | Stage 的执行器 |
| Pack | 某类链接或文件的解析和任务创建逻辑 |
| Scheduler | 控制任务队列、并发和状态变化的核心 |
| Snapshot | 给 UI 或浏览器桥看的不可变任务快照 |
| Runtime | ffmpeg、ffprobe、N_m3u8DL-RE 等外部工具 |
| Sidecar | 主程序外的辅助进程，例如未来 BT/libtorrent 或插件 |

## 30. 当前目录约定

工作区建议最终形成：

```text
.
├─ GHOST_DOWNLOADER_GO_WIN32_BLUEPRINT.md
├─ reference/
│  └─ Ghost-Downloader-3/
├─ cmd/
│  └─ gd3win/
├─ internal/
├─ assets/
├─ docs/
│  └─ implementation-notes/
├─ testdata/
└─ scripts/
```

`reference/` 只用于对照，不进入新程序构建。

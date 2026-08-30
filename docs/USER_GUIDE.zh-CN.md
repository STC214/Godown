# Ghost Downloader 3 用户指南

## 1. 界面概览

主窗口由来源输入区、任务工具栏、任务表格、任务详情和状态栏组成。

| 操作 | 作用 |
| --- | --- |
| Add URL | 识别输入内容并创建 HTTP、M3U8 或 Magnet 任务 |
| Open Torrent | 选择本地 `.torrent` 文件并打开文件选择窗口 |
| Settings | 设置下载目录、代理、并发数、BT 和 M3U8 参数 |
| Start All / Pause All | 启动或暂停全部可操作任务 |
| Open File / Open Folder | 打开已下载内容或所在目录 |
| Redownload | 清理任务运行态并重新下载 |
| Remove | 从任务列表移除；是否清理文件取决于操作选项 |

关闭主窗口后，应用可继续驻留托盘。应用退出时会先停止活动任务并保存恢复状态。

## 2. 支持的来源

### HTTP(S)

直接粘贴 `http://` 或 `https://` 地址。创建任务前应用会探测文件名、大小和分块能力；同名任务、已有目标文件和临时分块文件会参与去重检查。

### M3U8

粘贴 `.m3u8` 清单地址。M3U8 下载依赖设置中指定的 `N_m3u8DL-RE`，合并流程依赖 FFmpeg。外部工具路径或参数错误会显示在任务详情中。

### BitTorrent / Magnet

支持以下输入：

- 本地 `.torrent` 路径；
- `file://` torrent 地址；
- HTTP(S) 远程 `.torrent`；
- v1 `btih` Magnet 地址。

解析 Magnet 时应用会等待元数据，默认超时 30 秒。操作可取消，不会阻塞主窗口。

BT 功能由同目录的 `gd3-bt-runtime.exe` 提供。复制或安装应用时，应同时保留 `gd3win.exe` 与该运行时文件；HTTP 和 M3U8 功能不依赖 BT 运行时启动。

## 3. BT 文件选择

解析 torrent 后会显示文件路径、大小和选择状态。

- **Select All**：选择全部可下载文件。
- **Clear**：清空选择。
- **Invert**：反转当前选择。
- 底部汇总显示所选文件数与总大小。

至少需要选择一个文件。Padding 文件不会出现在普通选择列表中；共享边界块所需的 padding 或未选文件字节会写入任务自有的隐藏目录，不计入所选文件进度。

## 4. 任务状态

| 状态 | 含义 |
| --- | --- |
| waiting | 等待调度槽位 |
| downloading | 正在传输或校验数据 |
| paused | 已暂停，恢复信息已保存 |
| merging | M3U8/媒体任务正在合并 |
| seeding | BT 内容下载完成，正在上传做种 |
| completed | 任务已按配置完成 |
| failed | 任务失败，详情中显示原因 |

BT 做种不占普通下载并发槽位。暂停 BT 任务时，应用只把哈希校验成功的块计入持久进度；再次启动或重启应用后会校验并继续缺失部分。

## 5. 通用设置

| 设置 | 说明 |
| --- | --- |
| Download directory | 新任务的默认保存目录 |
| Proxy URL | 支持 HTTP(S) 与 SOCKS5 代理地址 |
| Headers | 每行一个请求头，供支持的 HTTP 请求使用 |
| Cookies | HTTP/M3U8 请求使用的 Cookie 文本 |
| Blocks | HTTP 分块数量 |
| Retries | 失败重试次数 |
| Speed limit | 通用下载限速；`0` 表示不限速 |
| Max concurrent | 普通下载任务并发槽位数 |

设置保存时会先应用运行时组件；应用失败时保留原有设置。

## 6. BitTorrent 设置

| 设置 | 默认值 | 说明 |
| --- | ---: | --- |
| Listen port | 0 | `0` 为自动选择；指定端口不可用时回退临时端口 |
| Metadata timeout | 30 秒 | Magnet 元数据等待时间，范围 5–300 秒 |
| Connections limit | 500 | BT 连接限制，范围 20–2000 |
| Download/Upload limit | 0 KiB/s | BT 独立上下行限速；`0` 表示不限速 |
| Sequential download | 关闭 | 按文件顺序提高读取优先级 |
| DHT / LSD | 开启 | Peer 发现偏好 |
| UPnP / NAT-PMP | 开启 | 路由器端口映射偏好 |
| Seed ratio | 0% | `0` 表示不设分享率停止条件 |
| Seed time | 0 分钟 | `0` 表示不设时间停止条件 |
| Save magnet torrent | 关闭 | 获取 Magnet 元数据后保存 `.torrent` |
| Extra trackers | 空 | 每行一个 Tracker，与种子内列表稳定去重合并 |

分享率和做种时长同时启用时采用“先满足者停止”。两项均为 `0` 时，任务持续做种，直到手动暂停。

## 7. 浏览器桥接

设置中可启用浏览器桥接、查看配对令牌并指定本地端口。默认端口为 `14370`。修改桥接端口或令牌后，需要让浏览器端使用相同配置。

## 8. 数据与恢复

- 任务和配置保存在应用数据目录的 SQLite 存储中。
- BT 校验块完成状态位于任务目录的 `.gd3_bt/<task-id>`。
- BT 隐藏的 padding/未选内容属于任务内部数据；删除任务输出时会一并处理。
- “重新下载”会清空运行进度和做种统计，但保留 torrent 元数据、Tracker 配置和文件选择。

不要在任务运行时手动修改 `.gd3_bt`、`.gd3_padding` 或 `.gd3_unselected` 内容。

## 9. 主题、更新与日志

- 在 **Settings → Appearance → Theme** 选择 System、Light 或 Dark；新配置默认使用 Dark，保存后主窗口立即刷新主题。暗色模式会同步调整窗口、标签、输入框、任务列表和详情区域；Light 模式使用白色输入背景和深色文字。
- 下载目录、代理、请求头、Cookies、并发/重试、速度限制及运行时选项统一在 **Settings** 窗口中设置；主窗口专注于添加与管理下载任务。
- 选择主窗口顶部的 **Check Updates** 查询最新发布版本。如果 Release 同时包含 `windows-x64-portable.zip` 和对应 `.sha256`，应用会下载并验证文件，退出后自动覆盖便携目录并重新启动；缺少匹配资产时打开发布页面。
- 选择 **Open Logs** 可定位 `%AppData%\GhostDownloaderGo\GhostDownloader.log`。
- 未处理异常会额外生成 `%AppData%\GhostDownloaderGo\crash-YYYYMMDD-HHMMSS.log`，其中包含 panic 和调用栈。

## 10. 故障排查

### Magnet 一直等待元数据

检查网络、Tracker、DHT、代理设置和系统防火墙；可提高 Metadata timeout 或添加 Tracker。仅包含 BT v2 标识的 Magnet 当前不在已验证范围内。

### BT 无 Peer 或速度为零

确认任务详情中的 Peer/Seed 数，尝试启用 DHT 和端口映射，或把监听端口加入防火墙规则。使用代理时注意原生 Peer、DHT 和 UDP Tracker 不走 HTTP/SOCKS 代理层。

### 提示缺少 BitTorrent runtime

确认 `gd3-bt-runtime.exe` 与 `gd3win.exe` 位于同一目录。运行时文件可由 `scripts/build-release.ps1` 与主程序同时构建；不要单独移动主程序。

### M3U8 创建或合并失败

检查 `N_m3u8DL-RE` 与 FFmpeg 安装目录，确认进程可执行并查看任务详情中的原始错误。

### 重启后 BT 重新校验

这是恢复流程的一部分。进度只统计哈希校验成功的块，避免把未完成写入误报为可恢复数据。

# Stage 20：FTP/FTPS、BT v2 Magnet 与运行中改选

日期：2026-09-05

## 实现内容

### FTP / FTPS

- 新增 `internal/download/ftp`，接入现有解析路由、Registry、Scheduler、全局限速和重试设置；
- 支持普通 FTP、隐式 FTPS 和显式 FTPS 单文件下载；
- 支持匿名或 URL 用户信息登录，持久任务 URL 会移除凭据；
- 使用 `.part` 文件及 FTP REST 实现暂停、重启后的断点续传；服务器拒绝 REST 时安全地从头开始；
- 支持直连及 SOCKS5/SOCKS5H 代理；
- 完成后将 `.part` 改名为最终文件，重新下载时同步清理临时文件。

### BitTorrent v2 Magnet

- GUI 轻量客户端识别 `urn:btmh`；
- BT runtime 改用 `metainfo.ParseMagnetV2Uri`，接受 v1、v2-only 和 hybrid Magnet；
- 元数据解析、Tracker 合并、文件选择与现有 runtime 下载流程保持一致。

### 运行中重新选择 BT 文件

- 新增 `Scheduler.Task` 和 `Scheduler.EditTask`；
- 编辑活动任务时，先取消 Worker 并等待最新检查点，再更新任务并恢复调度；
- 主窗口新增 **BT 文件** 按钮，任务右键菜单新增 **选择 BT 文件**；
- 重新计算所选总大小、已接收字节和进度；原本手动暂停的任务编辑后保持暂停。

## 验证

- FTP 本地协议集成测试覆盖登录、SIZE、EPSV、REST、RETR、断点续传、最终改名和进度；
- 单元测试覆盖 FTP/FTPS/FTPES 识别、凭据移除及 TLS 模式；
- BT 测试覆盖合法 v2-only Magnet 识别、解析器调用和 Tracker 保留；
- Scheduler 测试覆盖活动 Worker 停止、编辑及使用新状态重启；
- 执行 `go test -count=1 ./...`、`go vet ./...`、发布构建和便携打包。

验证结果：全量测试、关键包竞态检测、静态检查和发布构建退出码均为 `0`；`0.1.13-stage20` 便携包 SHA-256 为 `e25886ac917e4b62e74645ac1c55c2d94f74353d7c483eb6193d9c5970ba8f63`。

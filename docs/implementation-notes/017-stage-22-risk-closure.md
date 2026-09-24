# Stage 22：Stage 21 风险闭环

日期：2026-09-05

## 修复内容

- JSON 与 SQLite 任务存储使用 Windows 当前用户 DPAPI 加密 FTP 密码；既有明文任务在首次加载时自动迁移。
- `Redownload`、`EditTask`、开始、暂停和退出共用操作互斥边界，防止异步清理使用旧任务副本覆盖状态或在退出阶段留下等待任务。
- FTP 文件名按首个扩展分隔符识别 `CON`、`AUX`、`COM1` 等 Windows 保留设备名，覆盖多重扩展。
- BT 顺序下载先按高、普通、低优先级稳定排序，同优先级保持 torrent 原始顺序。
- 未知大小 FTP 任务不复用跨会话旧 `.part`，避免远端缩短后保留陈旧尾部。
- v2-only torrent 在交给 anacrolix 客户端前使用 v2 短哈希完成兼容初始化，元数据校验后恢复为纯 v2 标识，消除零 v1 哈希 panic。

## 新增验证

- DPAPI 加密落盘、解密读取及 JSON/SQLite 旧明文迁移。
- `Redownload` 与 `EditTask` 并发执行时状态顺序合并。
- 多重扩展 Windows 保留名、未知大小 FTP 陈旧分片和 BT 顺序优先级。
- 本地真实隐式 FTPS、显式 `AUTH TLS` FTPES 控制/数据通道。
- 真实 BEP 52 v2-only 元数据、内容 pieces root 与本地 WebSeed 完整下载。

发布物仅提供 Windows x64 便携 ZIP 和同名 SHA-256 文件。

验证结果：全量测试、关键包竞态检测、静态检查与便携构建退出码均为 `0`；便携 ZIP SHA-256 为 `12f19f12e98726856145884223a21faa16a497b087585e34a29a00db6248d33d`。

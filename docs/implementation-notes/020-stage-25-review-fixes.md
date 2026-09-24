# Stage 25：Stage 24 复审修复

## 修复内容

- FTP 底层连接在每次读写前刷新空闲截止时间。TCP 建连、FTP 欢迎消息、TLS 握手、数据传输和最终控制回复都受任务超时约束；正常持续传输不会受单个绝对截止时间截断。
- context 取消仍直接关闭控制与数据 socket，并保留未发布的 `.part` 文件。
- 主界面的“暂停/继续”“全部开始”“全部暂停”改为后台调用调度器，完成后通过 UI 消息队列更新状态。BT 文件编辑或运行时退出较慢时，不再让按钮回调等待全局编辑锁。
- README 的当前阶段和验证版本同步为 `0.1.18-stage25`。

## 回归验证

新增本机隐式 TLS 停滞测试：服务器接受 TCP 后不继续握手，任务配置 100 毫秒超时。原实现到测试的 3 秒上限仍阻塞；修复后在 1 秒断言范围内返回。

FTP/FTPS/FTPES 最终回复等待取消测试继续通过；相关 FTP 与调度器竞态测试重复 10 轮通过。全量测试及 `go vet` 结果见 `020-stage-25-verification.txt`。

## 交付和回退

- 便携版：`release/GhostDownloader-0.1.18-stage25-windows-x64-portable.zip`。
- 本轮增量差异：`020-stage-25.patch`，基线为 Stage 24 工作区。
- 回退脚本：`scripts/rollback-stage25.ps1`。

回退脚本依赖 `.codex-artifacts/stage25-original` 和 `stage25-modified`，执行前核对散列；检测到后续修改时停止。它只恢复本轮三个源码文件及 README，不触碰任务数据库和便携包。Stage 24 便携包继续保留。

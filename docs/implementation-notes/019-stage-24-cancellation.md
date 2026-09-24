# Stage 24：FTP 最终回复等待的取消修复

## 变更

FTP 原实现通过 `sync.Once` 串行关闭数据响应。最终控制回复缺失时，取消协程也会等待关闭完成，直到超时。

现在通过自定义拨号器将控制和数据连接绑定到任务 context；取消直接关闭底层 socket，中断正在等待的控制回复。连接正常关闭时注销取消回调。保留原有最终回复校验、文件关闭校验及 `.part` 发布规则。

直连和 SOCKS5 拨号使用 context；隐式 TLS 和显式 TLS 的数据连接仍进行 TLS 封装，显式控制连接由 FTP 库处理 AUTH 升级。数据 TLS 保持延迟握手，避免服务器等待 RETR 时相互等待。

## 回归

本机服务器发送完整数据后故意不发送最终回复。测试分别覆盖 FTP、FTPS、FTPES，检查取消返回 `context.Canceled`、耗时小于 1 秒、不产生最终文件、临时文件内容完整。

原实现三个子测试均在约 3 秒的测试配置超时后失败；修复后通过。重复竞态测试和全量检查的实际结果见 `019-stage-24-verification.txt`。

## 交付及回退

- 便携版：`release/GhostDownloader-0.1.17-stage24-windows-x64-portable.zip`。
- 增量差异：`019-stage-24.patch`，基线为本轮开始时的 Stage 23 工作区，不是 Git HEAD。
- 回退脚本：`scripts/rollback-stage24.ps1`，依赖本机 `.codex-artifacts/stage24-original` 与 `stage24-modified` 快照，先检查文件散列，检测到后续修改则停止。
- 回退仅恢复本轮两个 FTP 源文件和 README；不修改任务数据库、不删除便携包。旧 Stage 23 便携包保留。

本轮未扩大到其他功能，未做公网 BT 或人工 UI 验收。上轮记录中的 v2 元数据交换、多分片/混合种子等测试债务仍保留。

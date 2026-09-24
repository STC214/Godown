# Stage 28：FTP 目录下载审查修复

Stage 28 按审查顺序关闭 FTP 递归目录下载的三个问题：

1. 已完成文件不再仅凭大小跳过。Worker 在 `.gd3_ftp/<task-id>/` 保存任务专属完成标记和 SHA-256；重用文件前同时核对长度、标记和实际摘要。重下清理会同时删除输出目录与完成状态。
2. 目录清单缺少大小时，以 `-1` 持久化未知长度并下载至 FTP 正常结束回复；未知长度的跨会话 `.part` 从头下载。只要任一文件大小未知，任务解析阶段总大小保持 `0`，完成时使用实际接收量结算。
3. 用户指南、开发文档、README 和蓝图同步到 Stage 28。

同时修复关联探测顺序：当服务器不支持 `SIZE` 时，程序会先尝试目录清单，再回退为未知大小单文件，因此无尾斜杠目录仍能识别。

验证范围包括 FTP/Core 定向测试和竞态检测、全量测试、`go vet`、差异格式检查、便携包清单以及回退脚本。

交付物：

- 便携版：`release/GhostDownloader-0.1.21-stage28-windows-x64-portable.zip`
- 补丁：`docs/implementation-notes/023-stage-28.patch`
- 验证记录：`docs/implementation-notes/023-stage-28-verification.txt`
- 回退脚本：`scripts/rollback-stage28.ps1`

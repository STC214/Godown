# Stage 8–14：桌面界面与便携发布收口

## 已完成

- 将用户提供的图片转换为多分辨率 ICO，并嵌入主窗口、文件、任务栏与托盘图标。
- 主程序切换为 Windows GUI 子系统，子进程统一隐藏控制台窗口；启动失败通过消息框显示。
- 左侧设置区迁移到独立设置窗口，顶部操作区改为两行紧凑布局，主窗口最小宽度降至 510。
- 输入框、任务列表、日志和设置窗口支持亮色、暗色与系统主题，新配置默认使用暗色。
- 任务列表在隐藏原生表头后提供可点击的暗色表头，并显示当前排序方向。
- 修正 Win32 命名互斥体的 `ERROR_ALREADY_EXISTS` 映射，第二实例稳定返回 `ErrAlreadyRunning`。
- 主题遍历改用窗口层级枚举，移除反复创建且不可释放的 `syscall.NewCallback`。
- 发布脚本只保留便携 ZIP 和 SHA-256 文件；临时展开目录在成功或失败后都会清理。
- Go 最低版本提升至 1.26.6，并升级 WebSocket、DTLS、STUN 依赖；`govulncheck` 可达漏洞结果为 0。

## 验证范围

- `go test -count=1 ./...`
- `go test -race -count=1 ./...`
- `go vet ./...`
- `go build -trimpath ./...`
- 单实例重复获取与释放后重新获取测试。
- 任务表格升序/降序切换测试。
- ZIP 外部 SHA-256、包内清单、文件大小和文件 SHA-256 交叉校验。
- `govulncheck ./...` 可达调用链扫描。

## 发布约束

- 仅分发 `GhostDownloader-<version>-windows-x64-portable.zip` 与同名 `.sha256`。
- `gd3win.exe` 与 `gd3-bt-runtime.exe` 必须处于同一解压目录。
- `release/` 是本地生成目录，不纳入 Git；发布记录由版本标签、源码提交和 SHA-256 共同标识。

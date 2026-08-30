# Stage 15：自动化测试债务收口

## 目标

Stage 15 不扩展下载功能，专门处理 Stage 14 复审中确认的自动化测试覆盖债务：命令入口和 `internal/app` 原先为 0%，UI 与 Win32 的关键辅助逻辑覆盖不足。

## 实现

### 主程序入口

- `cmd/gd3win` 将应用启动和消息框包装为可替换函数。
- 增加版本参数识别测试。
- 覆盖正常退出、重复启动、普通启动错误和 panic 恢复退出码。
- 测试不显示真实消息框，也不创建主窗口。

### BT runtime

- `run`、`runWorker` 和 `encodeResult` 改为接收 `io.Reader` / `io.Writer`，生产入口仍传入标准输入输出。
- 覆盖参数数量、未知操作、非法 JSON、成功/错误 JSON 消息和写入失败。
- 协议测试不访问公网，也不启动真实 BT 会话。

### 应用装配、UI 与 Win32

- 覆盖浏览器标题清洗、Header 合并、BT 设置映射和媒体资源映射。
- 使用 `httptest` 回环服务器验证 HTTP 来源和浏览器任务覆盖项。
- 覆盖资源合并任务的成功与错误分支。
- 补充任务表格计数、查找、各列排序和越界读取测试。
- 补充显式明暗模式和空窗口句柄测试。

## 覆盖率结果

| 包 | Stage 14 | Stage 15 |
| --- | ---: | ---: |
| `cmd/gd3win` | 0.0% | 51.6% |
| `cmd/gd3-bt-runtime` | 0.0% | 42.6% |
| `internal/app` | 0.0% | 34.4% |
| `internal/ui` | 21.6% | 24.7% |
| `internal/win32` | 16.5% | 24.7% |
| 全项目 | — | 54.1% |

覆盖率用于确认关键分支已经执行，不作为单独的发布判据。

## 验证

```powershell
go test -count=1 ./...
go test -race -count=1 ./...
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
go vet ./...
go build -trimpath ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
.\scripts\package-portable.ps1 -Version 0.1.9-stage15
```

验证结果：全量测试、竞态检测、静态检查和构建通过；可达漏洞为 0。便携包内文件大小与清单 SHA-256 全部匹配，主程序首次启动、第二实例提示以及 BT runtime 未知操作错误路径均已验证。

## 发布物

- `release/GhostDownloader-0.1.9-stage15-windows-x64-portable.zip`
- SHA-256：`49ecee7393b363e9a988252fb88300c3c999ae663f417b1fcc25a14e141b5a53`
- 源码提交：`e14652fac2bfd9680b39fa95b2c0c3d581c54c06`

发布目录只保留 ZIP 与同名 `.sha256`，不生成安装包。

## 保留边界

- `internal/app.Run` 和真实 Walk 窗口生命周期继续由便携包运行探针验证，单元测试不拉起前台窗口。
- BT v2-only Magnet 和运行中动态修改 BT 文件优先级仍属于后续功能规划，不属于本阶段测试债务。

# Stage 19：工作区清理与文档同步

日期：2026-09-05
程序验证版本：`0.1.12-stage18`（本阶段未修改功能代码）

## 目标

- 清除历史自动化夹具、构建输出、本地对照仓库和运行时临时文件；
- 发布目录只保留最新 Windows x64 压缩便携版及 SHA-256；
- 同步 README、用户指南、开发者文档和总体蓝图；
- 重新打包，使便携 ZIP 内文档与仓库当前版本一致。

## 已完成清理

- 注销 `.codex-artifacts/` 下的三个隔离 Git worktree 后删除该目录；
- 删除可重建的 `dist/`；
- 删除不参与交付的本地 `reference/` 对照目录；
- 删除根目录运行时 `.torrent.db` 和已经转换为 `assets/app.ico` / `assets/app-icon-master.png` 的原始 JPG；
- 删除 Stage 15、Stage 16、Stage 17 便携 ZIP 与校验文件；
- 保留 Stage 18 当前便携 ZIP 和同名 `.sha256`；
- 保留 `docs/implementation-notes/000` 至 `013`，因为它们属于受版本控制的实现历史，而不是临时文件。

本次按清理前盘点合计移除约 3.27 GiB（3,513,223,470 字节）。

## 文档同步

- `README.md`：当前进度更新为 Stage 19，并补充工作区清洁约定；
- `docs/DEVELOPMENT.zh-CN.md`：补充生成物边界、清理顺序及验证命令；
- `docs/USER_GUIDE.zh-CN.md`：同步简体中文按钮名称和仅提供便携包的发布方式；
- `GHOST_DOWNLOADER_GO_WIN32_BLUEPRINT.md`：对照仓库改为按需获取，当前状态指向本记录。

历史阶段记录反映各阶段当时事实，因此不回写其内容。

## 验证结果

```powershell
go test -count=1 ./...
go vet ./...
.\scripts\package-portable.ps1 -Version 0.1.12-stage18
Get-FileHash -Algorithm SHA256 .\release\GhostDownloader-0.1.12-stage18-windows-x64-portable.zip
git worktree list
git status --short --ignored
```

- `go test -count=1 ./...`：通过，退出码 `0`；
- `go vet ./...`：通过，退出码 `0`；
- 便携打包：通过，退出码 `0`；
- ZIP SHA-256：`34b269b6583ab81abe142eb919ada94ec3f4462322821a3ba0ae78b24fd61601`，与同名 `.sha256` 一致；
- ZIP 内 README、用户指南与仓库逐字节一致，六个必需交付文件齐全；
- 全部 Markdown 本地链接检查通过，失效链接数为 `0`；
- 仓库仅注册主 worktree；`dist/` 在打包后已由脚本清理；`release/` 仅保留当前 ZIP 和 `.sha256`。

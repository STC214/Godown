# Stage 17：设置窗口分页布局修复

> Stage 17 解决了宽度与分页结构，但随后发现原生分页控件没有进入现有主题递归链，暗色模式下页面仍为白色。配色问题由 [Stage 18](013-stage-18-tab-theme.md) 修复；本记录保留布局修复历史。

日期：2026-09-05

## 根因

Stage 16 为 `ScrollView` 设置了最小宽度和伸展因子，但 Walk 在 `HorizontalFixed=true` 时只赋予该控件垂直增长标志，没有 `GrowableHorz`。外层 `VBox` 因而继续按内容首选宽度居中排列，实际界面仍然狭窄。

## 修复

- 删除设置窗口的单列 `ScrollView`。
- 使用原生 `TabWidget`，按“常规、BitTorrent、流媒体、请求”拆分为四页。
- 每页使用可横向增长的网格、输入框和文本框，随窗口客户区宽度伸展。
- 保存和取消按钮继续位于窗口固定底栏。
- 保留 Stage 16 已完成的简体中文界面文本。

## 验证

```powershell
go test ./... -count=1
go vet ./...
.\scripts\package-portable.ps1 -Version 0.1.11-stage17
```

布局回归测试检查四页设置结构和适合常见工作区的窗口高度。发布物仍为纯 ZIP 便携包。

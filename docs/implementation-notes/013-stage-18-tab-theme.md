# Stage 18：设置分页主题配色修复

日期：2026-09-05

## 根因

Walk 的 `TabWidget` 不通过普通 `Container.Children()` 暴露 `TabPage`，原有主题递归因此没有设置分页页面背景。Windows 当前主题接口也没有在目标系统上把 `SysTabControl32` 页签头绘制成暗色，最终形成白色页面、白色页签头与深色输入框混用的界面。

## 修复

- 主题遍历显式读取 `TabWidget.Pages()`，为每个页面设置主题背景并递归更新页面内标签文字。
- 定位 `SysTabControl32` 原生子窗口，在 Walk 完成默认绘制后覆盖绘制暗色页签头。
- 深色页签区使用独立的背景、选中、未选中、边框和高对比文字颜色。
- 切换到浅色主题时停用覆盖绘制并恢复系统页签外观。
- 主题样式释放时恢复原窗口过程，避免遗留窗口钩子。

## 验证

```powershell
go test ./... -count=1
go vet ./...
.\scripts\package-portable.ps1 -Version 0.1.12-stage18
```

测试覆盖深色页签调色板的文字对比度和选中状态差异；正式 EXE 构建继续使用 Common Controls v6 manifest。

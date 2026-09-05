package ui

import (
	"context"
	"fmt"

	"ghost-downloader-go-win32/internal/browserbridge"

	"github.com/lxn/walk"
)

func pairApprovalFunc(dispatcher *ApplicationDispatcher, owner walk.Form) browserbridge.PairApprovalFunc {
	return func(ctx context.Context, request browserbridge.PairRequest) (bool, error) {
		if dispatcher == nil || dispatcher.mainWindow == nil || owner == nil {
			return false, fmt.Errorf("主窗口不可用")
		}
		type result struct {
			approved bool
			err      error
		}
		resultCh := make(chan result, 1)
		dispatcher.Post(func() {
			if owner == nil {
				resultCh <- result{err: fmt.Errorf("主窗口不可用")}
				return
			}
			message := fmt.Sprintf(
				"浏览器扩展正在请求访问 Ghost Downloader。\r\n\r\n来源：%s\r\n客户端：%s\r\n扩展版本：%s\r\n\r\n仅在你刚刚从扩展中发起配对时允许此请求。",
				valueOrUnknown(request.RemoteAddr),
				valueOrUnknown(request.ClientKind),
				valueOrUnknown(request.ExtensionVersion),
			)
			response := walk.MsgBox(owner, "浏览器扩展配对", message, walk.MsgBoxYesNo|walk.MsgBoxIconQuestion)
			resultCh <- result{approved: response == walk.DlgCmdYes}
		})

		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case result := <-resultCh:
			return result.approved, result.err
		}
	}
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "未知"
	}
	return value
}

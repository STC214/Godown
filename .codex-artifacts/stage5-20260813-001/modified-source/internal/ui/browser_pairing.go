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
			return false, fmt.Errorf("main window is unavailable")
		}
		type result struct {
			approved bool
			err      error
		}
		resultCh := make(chan result, 1)
		dispatcher.Post(func() {
			if owner == nil {
				resultCh <- result{err: fmt.Errorf("main window is unavailable")}
				return
			}
			message := fmt.Sprintf(
				"A browser extension is requesting access to Ghost Downloader.\r\n\r\nSource: %s\r\nClient: %s\r\nExtension version: %s\r\n\r\nOnly allow this if you just started pairing from the extension.",
				valueOrUnknown(request.RemoteAddr),
				valueOrUnknown(request.ClientKind),
				valueOrUnknown(request.ExtensionVersion),
			)
			response := walk.MsgBox(owner, "Browser Extension Pairing", message, walk.MsgBoxYesNo|walk.MsgBoxIconQuestion)
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
		return "unknown"
	}
	return value
}

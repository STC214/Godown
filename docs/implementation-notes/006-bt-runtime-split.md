# BT Runtime Split Notes

## Goal

Keep full Go symbol and DWARF debugging information while moving the large BitTorrent protocol stack out of the Windows GUI executable.

## Implemented

- Added `gd3-bt-runtime.exe` as a sibling runtime process containing anacrolix/torrent, DHT, WebRTC and BT checkpoint code.
- Added a lightweight `internal/btruntime` client used by app and UI packages.
- Source resolve/reset use one-shot JSON messages; active transfers stream `core.ProgressUpdate` messages over stdout.
- Closing the runtime stdin performs graceful cancellation so the worker saves verified-piece state before exit.
- Missing-runtime errors name the expected sibling path.
- Added a dual-output PowerShell build script without `-s` or `-w`; both binaries retain debug metadata.
- Added a real process integration test covering torrent resolution, local Range WebSeed download, progress streaming and cancellation.

## Verified Packaging

- Debug GUI executable: approximately 22.70 MiB.
- Debug BT runtime executable: approximately 45.69 MiB.
- The GUI dependency graph contains neither anacrolix, Pion nor `internal/download/bt`.
- Total installed size is larger than a stripped monolith because each process owns a Go runtime; the objective is modular delivery and a smaller main EXE without weakening debugging.

## Commands

```powershell
.\scripts\build-release.ps1
go list -deps ./cmd/gd3win
$env:GD3_BT_RUNTIME_TEST_PATH = (Resolve-Path .\dist\gd3-bt-runtime.exe).Path
go test -run TestRuntimeProcessResolveAndDownload -v ./internal/btruntime
go test -race -count=1 ./...
go vet ./...
```

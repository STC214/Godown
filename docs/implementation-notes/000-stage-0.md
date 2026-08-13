# Stage 0 Implementation Notes

## Reference

- `reference/Ghost-Downloader-3/Ghost-Downloader-3.py`
- `reference/Ghost-Downloader-3/app/supports/paths.py`
- `reference/Ghost-Downloader-3/app/supports/application.py`

## Implemented

- Go module skeleton.
- Application data/runtime/temp directories.
- Basic structured logging.
- Best-effort per-monitor DPI awareness.
- Single-instance guard using a per-user Windows named mutex.
- System tray icon with show, open downloads, start all, pause all, and exit actions.
- Close and minimize keep the app running in the tray; tray Exit performs real shutdown.
- Embedded Windows resources for the application icon and common-controls/DPI manifest.
- Window and tray icon load from the embedded application icon resource.
- Minimal Walk main window.
- UI dispatcher wrapper for main-thread updates.
- SQLite-backed persistent config database.

## Deliberate Differences

- No splash screen in the Go prototype.
- UI is a functional Windows-native shell, not a qfluentwidgets pixel clone.

## Pending

- No Stage 0 items remain.

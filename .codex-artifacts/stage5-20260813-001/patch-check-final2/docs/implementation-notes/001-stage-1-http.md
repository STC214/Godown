# Stage 1 HTTP Slice Notes

## Reference

- `reference/Ghost-Downloader-3/features/http_pack/pack.py`
- `reference/Ghost-Downloader-3/features/http_pack/task.py`
- `reference/Ghost-Downloader-3/app/bases/models.py`
- `reference/Ghost-Downloader-3/app/services/core_service.py`
- `reference/Ghost-Downloader-3/app/services/task_service.py`

## Implemented

- Task, Stage, snapshot, worker registry, and scheduler primitives.
- Worker progress updates are funneled through the scheduler instead of mutating shared task state directly.
- Debounced task persistence using SQLite.
- HTTP URL parsing with Range probe and filename inference.
- Filename inference covers `filename*`, `filename`, `Content-Location`, `response-content-disposition`, URL path, and common `Content-Type` extension fallback.
- HTTP worker with ranged subworkers, random file writes, progress records, pause via context cancellation, and `.ghd` cleanup on completion.
- HTTP worker reports its initial restored or reset progress immediately on start, instead of waiting for the first one-second tick.
- HTTP worker uses a per-task finite retry count, with retry sleep that exits promptly when the task is paused or removed.
- Non-Range HTTP retries reset the partial file and roll back received bytes before starting over, preventing inflated progress after retry.
- Range HTTP responses validate `Content-Range` starts at the expected write offset before bytes are written.
- Range and known-size non-Range responses treat early EOF as an incomplete download instead of success.
- Filename deduplication against remembered tasks in the same output directory.
- Shutdown now marks running/waiting tasks as paused before saving.
- Shared global speed limiter used by all HTTP subworkers.
- SQLite-backed app settings for download folder, block count, speed limit, and max concurrent tasks.
- Persistent explicit proxy URL setting.
- HTTP probe and download workers support direct, HTTP(S) proxy, and SOCKS5 proxy modes.
- Persistent multi-line request headers setting using `Name: Value` lines. Cookies are supported through the normal `Cookie: ...` header.
- Persistent dedicated cookies editor. Cookie text is normalized and merged into the request `Cookie` header for probe and download.
- Minimal UI integration:
  - URL entry.
  - Add URL.
  - sortable task table with name/status/progress/size/speed/folder columns.
  - search by title, URL, or output folder.
  - filter by all, active, completed, or failed tasks.
  - sidebar task filter buttons are wired to the same filter model as the toolbar dropdown.
  - selected task detail panel.
  - start all paused/waiting tasks.
  - pause all active/waiting tasks.
  - pause/resume selected task.
  - redownload selected task.
  - remove selected task from the list.
  - open selected task folder.
  - open completed file.
  - task table right-click menu for selected task actions: pause/resume, redownload, remove, open folder, and open file.
  - persistent download folder selector.
  - persistent proxy URL input.
  - persistent request headers editor.
  - persistent cookies editor.
  - persistent block count and max concurrent task inputs.
  - persistent per-task retry count input.
  - persistent global speed limit input in KiB/s, where `0` means unlimited.
  - Settings dialog for download folder, proxy, headers, cookies, blocks, max concurrent tasks, retry count, and speed limit.
  - open download folder.
- Scheduler review fixes:
  - Removing a running task cancels the worker without letting stale task state reappear after the worker returns.
  - Add-URL parsing uses a settings snapshot before entering the background goroutine.
  - Lowering max concurrent tasks now pauses overflow running tasks and keeps the scheduler within the new limit.
  - Opening the default download folder follows the current saved download folder, not only the boot-time path.
  - Running task handles now include a completion signal, so redownload waits for the old worker to release file handles before cleanup.
  - Fast Pause All followed by Start All preserves the queued state and avoids duplicate workers.

## Deliberate Differences

- SQLite currently stores the full task payload as JSON plus indexed metadata. This keeps migrations simple while the task model is still moving.
- UI uses a native `TableView` for now. A virtualized/owner-draw task card list comes later if the plain table is not enough for the target polish.
- Auto speed-up is not wired yet.
- Settings controls still also exist in the rough sidebar for early-stage convenience, but the toolbar Settings button now opens a functional settings dialog.
- Empty proxy means direct connection. The app does not implicitly read environment proxy variables.
- Remove currently removes the task record from the app. It does not delete the downloaded file, which avoids destructive surprises during early development.
- The HTTP worker receives a task copy and reports `ProgressUpdate` snapshots. This keeps UI snapshots, persistence, and browser bridge work on scheduler-owned state.

## Pending

- Proper virtualized task list and owner-draw inline action buttons.
- Polished settings page layout that replaces the temporary sidebar controls.

## Verification

- `go build ./...`
- `go test ./...`
- `go test -race ./...`

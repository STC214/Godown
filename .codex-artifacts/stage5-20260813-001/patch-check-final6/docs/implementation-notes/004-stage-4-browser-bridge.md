# Stage 4 Browser Bridge Notes

## Reference

- `reference/Ghost-Downloader-3/app/services/browser_service.py`
- `reference/Ghost-Downloader-3/browser_extension/app/src/background/desktop-bridge.ts`
- `reference/Ghost-Downloader-3/browser_extension/app/src/shared/types.ts`

## Implemented

- Local WebSocket bridge bound to `127.0.0.1:<port>`.
- Bridge starts and stops from persisted Settings:
  - enabled flag.
  - pair token.
  - port, defaulting to `14370`.
- Settings changes hot-apply to the bridge after saving.
- Protocol version `1`.
- `hello` authentication with pair token.
- `hello_ack` response with task snapshot and task action capabilities.
- Unauthorized and protocol mismatch error responses.
- `pair_request` support:
  - shows a desktop approval dialog on the UI thread.
  - returns the current pair token only after approval.
  - rejects protocol mismatches and unavailable approval UI.
- `subscribe_tasks` support.
- Task snapshots are polled from the scheduler every second and sent only when changed.
- Snapshot payload matches the browser extension's `GenericTaskSummary` shape.
- Snapshot payload uses the task `packId` as `packName`, so extension visuals can distinguish M3U8/DASH tasks.
- `canOpenFile` and `canOpenFolder` reflect actual filesystem availability.
- Initial `task_action` support:
  - `toggle_pause`
  - `redownload`
  - `cancel`
  - `open_folder`
  - `open_file`
- `create_task` support for browser-captured resources:
  - desktop settings headers and cookies are merged with browser-provided request headers.
  - HTTP URLs use the existing HTTP parser/probe path.
  - M3U8/DASH URLs use the existing manifest parser.
  - browser-provided titles are sanitized before replacing inferred task titles.
  - output names are deduplicated against existing scheduler snapshots.
- `resource_merge` support for two browser-captured HTTP(S) media resources:
  - resources are downloaded with browser-provided request headers and configured proxy.
  - video/audio inputs are merged through configured FFmpeg with stream copy.
  - temporary inputs are stored under `.gd3_ffmpeg/<task_id>` and removed after a successful merge.
  - merged tasks use `packId=ffmpeg` and produce an `.mp4` output.
- WebSocket writes are serialized per session so task snapshots and request responses cannot interleave on the same connection.
- Extension project static validation:
  - `npm run typecheck`
  - `npm run build`

## Deliberate Differences

- The bridge polls `Scheduler.Snapshot()` rather than consuming `Scheduler.Events()`, because the current scheduler event channel has a single UI consumer.
- The extension can pair through `pair_request`, or users can still copy the token shown in Settings.
- The first `create_task` slice ignores the browser-provided `size` / `supportsRange` hints and lets the existing HTTP probe verify server behavior.
- Online merge currently supports only two direct HTTP(S) resources. Blob assembly and richer media probing remain browser/runtime responsibilities.

## Pending

- Extension-side live browser validation against this Go bridge.
- Real-world validation against sites that emit separate audio/video resources.

## Verification

- `go test ./internal/browserbridge`
- `go test ./internal/download/ffmpeg -v`
- `go test ./...`
- `go test -race ./...`
- `go build ./...`
- `npm run typecheck` in `reference/Ghost-Downloader-3/browser_extension/app`
- `npm run build` in `reference/Ghost-Downloader-3/browser_extension/app`

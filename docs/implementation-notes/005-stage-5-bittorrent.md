# Stage 5 BitTorrent / Magnet Notes

## Reference

- `GHOST_DOWNLOADER_GO_WIN32_BLUEPRINT.md`, Stage 5
- `reference/Ghost-Downloader-3/app/downloads/bt/pack.py`
- `reference/Ghost-Downloader-3/app/downloads/bt/loaders.py`
- `reference/Ghost-Downloader-3/app/downloads/bt/task.py`
- `reference/Ghost-Downloader-3/app/downloads/bt/worker.py`
- `reference/Ghost-Downloader-3/app/downloads/bt/trackers.py`

## Implemented

- Local paths, `file://`, HTTP(S) `.torrent` sources and v1 `btih` magnet URIs.
- Metadata work runs outside the Walk UI thread and obeys context cancellation plus a configurable timeout.
- 64 MiB metainfo limit, safe output-path normalization and post-sanitization collision checks.
- Single- and multi-file metainfo parsing with original file indexes retained across padding entries.
- Walk file-selection dialog with select-all, clear, invert, per-file size and selected-size summary.
- Tracker merge and stable de-duplication across metainfo tiers, magnet parameters and settings.
- Anacrolix-based selected-file download with optional sequential reads.
- Per-task Bolt piece-completion checkpoint under `.gd3_bt/<task_id>`.
- Resume storage opens files per operation, so Windows handles close deterministically while verified pieces survive pause/application restart.
- Download progress counts only hash-verified pieces; partial and complete local WebSeed integration tests exercise resume without re-fetching verified data.
- Explicit `seeding` scheduler status after selected content completes. Seeding releases the normal download slot and can still be paused/resumed.
- Seed ratio and seed time use OR semantics: the first enabled limit reached stops the task; zero/zero seeds until paused.
- Durable runtime fields: per-file received/completed, upload/download rates, peers, seeds, uploaded bytes, share ratio, phase and seeding seconds.
- Task detail shows BT phase, peer/seed counts, rates, ratio and elapsed seed time.
- Runtime settings for listen port, metadata timeout, connection limit, independent BT rates, DHT, LSD preference, UPnP, NAT-PMP, sequential mode, seed limits, extra trackers and optional saved magnet metainfo.
- HTTP(S)/SOCKS proxy support for metadata, tracker and WebSeed traffic; persisted request headers are applied to HTTP and WebSocket tracker requests.
- A configured listen-port collision falls back to an ephemeral port instead of failing other concurrent BT tasks.
- BT cleanup handles multi-file roots, single-file outputs, padding/unselected internal paths, piece checkpoints and optionally saved metainfo while guarding the download root.
- Redownload clears BT file progress and seeding statistics while preserving immutable metainfo, tracker settings and file selection.
- Scheduler shutdown waits for workers to close and save their final checkpoint before the SQLite store closes; the UI event pump exits with the scheduler.
- Scheduler event backpressure replaces the oldest event with the newest state so terminal/seeding transitions remain visible.
- Add-flow output de-duplication now checks both scheduled tasks and existing filesystem entries.

## Deliberate Differences

- Unselected files and metainfo padding bytes are stored under task-owned hidden subdirectories when shared boundary pieces require those bytes. They are excluded from selected-file progress and removed with the task output.
- DHT is mapped directly. The current anacrolix API combines default port forwarding rather than exposing separate UPnP/NAT-PMP runtime switches, and does not expose an LSD switch; the user preferences are persisted for a future backend/API revision.
- The global proxy is applied to HTTP metadata, trackers and WebSeeds. Native peer/DHT/UDP tracker sockets continue to use the engine's network stack.
- File selection occurs before scheduler insertion. Changing selection on an already-running task is deferred.

## Pending

- Public-swarm Windows validation for tracker/DHT/peer interoperability and long-running seeding.
- BitTorrent v2-only magnet metadata.
- Runtime file-priority editing after task creation.
- Tracker-list refresh service and `.torrent` Windows file association.
- Native UI rendering benchmark with very large (10,000+ row) torrent file lists.

## Verification

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build -trimpath ./...`
- `go test ./internal/download/bt -run 'TestTorrentTransfer(DownloadsFromLocalWebseedAndResumes|ResumesVerifiedPartialPieces)' -v -count=3`

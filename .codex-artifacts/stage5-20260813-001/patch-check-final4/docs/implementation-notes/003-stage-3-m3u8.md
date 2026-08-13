# Stage 3 M3U8 Runtime Notes

## Reference

- `reference/Ghost-Downloader-3/features/m3u8_pack/pack.py`
- `reference/Ghost-Downloader-3/features/m3u8_pack/task.py`
- `reference/Ghost-Downloader-3/features/m3u8_pack/config.py`

## Implemented

- Manifest source detection for HTTP(S), `file://`, and Windows local `.m3u8` / `.m3u` / `.mpd` paths.
- Manifest type detection for HLS and DASH using URL, `Content-Type`, and body sample.
- Simple live/VOD detection for HLS `#EXT-X-ENDLIST` and DASH `type="dynamic"`.
- Output title inference from `Content-Disposition`, query parameters, and URL path.
- Safe filename normalization and known media suffix stripping.
- N_m3u8DL-RE argument builder matching the original pack's main switches:
  - save directory/name and `.gd3_m3u8/<task_id>` temp directory.
  - thread count, retry count, timeout, concurrency, URL parameter append, binary merge, segment count check, cleanup, subtitles.
  - auto selection, selected video, all audio/subtitle selection.
  - max speed, ad keyword, no-date-info.
  - explicit proxy with `socks5h://` normalized to `socks5://`.
  - FFmpeg binary path.
  - decryption engine, key list, key text file, and external decryption binary.
  - live real-time merge, live segment retention, pipe mux, VTT fix, wait/take count, and record limit.
  - VOD mux-after-done, custom mux, and mux imports.
  - request headers as repeated `-H` arguments.
- N_m3u8DL-RE VOD and live progress line parsers.
- Persistent M3U8 settings fields:
  - FFmpeg install directory.
  - N_m3u8DL-RE install directory.
  - output format.
  - thread count, retry count, and request timeout.
  - concurrent download, segment count check, temp cleanup, all audio/subtitle selection, MP4 real-time decryption, and subtitle format.
- Settings dialog controls and validation for the M3U8 fields above.
- Runtime executable discovery for `N_m3u8DL-RE.exe` / `N_m3u8DL-RE`.
- Runtime version probe using `N_m3u8DL-RE --version` with a timeout.
- FFmpeg executable discovery from `bin/ffmpeg.exe`, `bin/ffmpeg`, `ffmpeg.exe`, or `ffmpeg`.
- FFmpeg version probe using `ffmpeg -version` with a timeout.
- Adapter from persisted app settings to M3U8 runtime options.
- Parse flow:
  - Recognizes M3U8/DASH sources from Add URL before falling back to HTTP.
  - Fetches remote manifests with custom headers and explicit proxy.
  - Reads local manifests with an 8 MiB cap.
  - Rejects local manifests that only contain relative segment paths, matching the original project's safety check.
  - Detects manifest type, live/VOD mode, output title, and output extension.
  - Creates `core.Task` with `packId=m3u8` and persists manifest/runtime metadata in `Stage.State`.
- Application registry includes the M3U8 worker.
- N_m3u8DL-RE worker initial implementation:
  - Finds the configured runtime executable from persisted task state.
  - Resolves configured FFmpeg and passes it to N_m3u8DL-RE when available.
  - Creates output and `.gd3_m3u8/<task_id>` temp directories.
  - Creates a `.ghd` placeholder before the external process starts.
  - Starts N_m3u8DL-RE in a background worker instead of the UI thread.
  - Drains stdout and stderr continuously.
  - Parses VOD/live progress lines into scheduler progress updates.
  - On cancellation, kills the process tree on Windows with `taskkill /T /F /PID`.
  - Removes `.ghd`, finds the final output, and renames fallback `<name>.*` outputs back to the task title when needed.
  - Reports final file size through scheduler progress updates.
- Scheduler progress updates can now carry a discovered file size, which M3U8 uses after external muxing completes.

## Deliberate Differences

- Track enumeration is not implemented yet. The first worker relies on `--auto-select` / `best` and can add selection UI later.
- The app expects users to install/configure N_m3u8DL-RE and FFmpeg paths. It detects and runs those binaries, but does not download runtimes yet.

## Pending

- Real-world validation against installed N_m3u8DL-RE and FFmpeg.
- Richer live recording behavior after cancellation.
- Process tree handling for non-Windows platforms if cross-platform support returns to scope.

## Verification

- `go test ./internal/download/m3u8 -v`
- `go test ./...`
- `go test -race ./...`
- `go build ./...`

# Stage 6 Plugin Protocol Notes

## Implemented

- Plugin discovery from the application data `plugins` directory, with one `manifest.json` per child directory.
- Manifest validation for stable ID, name, version, protocol version, executable and bounded timeout.
- JSON-RPC 2.0 over stdin/stdout using one isolated process per call.
- `manifest`, `matches` and `parse` methods.
- Stable plugin ordering and duplicate-ID rejection.
- Per-call timeout and process termination, bounded response/stderr sizes, crash isolation and clear invalid-JSON/RPC errors.
- Declarative parse results (`http`, `m3u8` or `bt`): plugins rewrite/classify input while the host constructs tasks and retains scheduler ownership.
- Plugin parsing is shared by desktop add-URL and browser-bridge task creation.
- Python URL-rewriter example under `examples/plugins/rewrite-example`.

## Protocol

The host starts `<executable> <args...> --stdio`, writes one newline-delimited JSON-RPC 2.0 request, closes stdin and reads one response. Plugin processes are intentionally disposable in this first implementation.

Methods:

- `manifest({}) -> Manifest`
- `matches({"url": string}) -> {"matched": bool}`
- `parse({"url", "downloadDir", "headers", "proxyUrl"}) -> {"kind", "url", "title?", "headers?"}`

Copy a plugin directory to `%AppData%\GhostDownloaderGo\plugins\<plugin-id>` and restart the application to discover it.

## Pending

- `taskCardHints` and `settingsSchema` optional protocol methods.
- Long-lived process pooling with idle shutdown; the current per-call process model favors isolation and deterministic cleanup.
- Windows Job Object memory limits and plugin management UI.
- Signed/distributable plugin packages.

## Verification

- `go test -count=1 ./...`
- `go vet ./...`
- `go build -trimpath ./...`

# Stage 7 UI and Portable Release Notes

## Implemented

- Build version injection through `internal/buildinfo` and Go linker flags.
- GitHub latest-release checking with bounded responses, request timeout and semantic-version comparison.
- Main-window update action that discovers matching portable ZIP/checksum assets, downloads with size limits, verifies the SHA-256 and every package-manifest entry, then launches the replacement helper.
- Exit-time portable replacement with manifest path validation, backups, failure rollback and application relaunch. Releases without portable assets fall back to the release page.
- Panic recovery at the executable boundary with timestamped crash report and Go stack trace.
- Main-window action that opens/selects the application log file.
- System, light and dark theme setting. Windows title bar and existing native child controls receive the selected Explorer theme.
- Strict task-table comparator ordering, preserving stable order for equal values.
- 10,000-row task-model test and benchmark. The native Walk TableView requests cell values from the model rather than constructing per-row widgets.
- Versioned portable ZIP packaging with GUI and BT runtime binaries, README, user guide, per-file SHA-256 manifest and archive checksum.

## Packaging Decision

The release target is portable ZIP only. No installer project is produced. Interactive updating downloads a verified portable archive, exits, replaces the package files with rollback protection, and relaunches the application.

## Verification

- `go test -count=1 ./...`
- `go test -race -count=1 ./...`
- `go vet ./...`
- `go build -trimpath ./...`
- `go test -run '^$' -bench BenchmarkTaskTableModelTenThousandRows -benchtime=5x ./internal/ui`
- `.\scripts\package-portable.ps1 -Version 0.1.0-stage7`

## Remaining Validation

- Interactive dark-theme inspection on Windows 10 and 11, including settings and torrent-selection dialogs.
- Manual 125%/150% DPI layout inspection and narrow-window inspection.
- Long-duration UI event-flow profiling with 1,000 active/changing tasks; the current 10,000-row benchmark covers model rebuild cost.

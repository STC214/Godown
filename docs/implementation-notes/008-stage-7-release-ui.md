# Stage 7 UI and Portable Release Notes

## Implemented

- Build version injection through `internal/buildinfo` and Go linker flags.
- GitHub latest-release checking with bounded responses, request timeout and semantic-version comparison.
- Main-window update action that reports the current version and opens a newer release page after confirmation.
- Panic recovery at the executable boundary with timestamped crash report and Go stack trace.
- Main-window action that opens/selects the application log file.
- System, light and dark theme setting. Windows title bar and existing native child controls receive the selected Explorer theme.
- Strict task-table comparator ordering, preserving stable order for equal values.
- 10,000-row task-model test and benchmark. The native Walk TableView requests cell values from the model rather than constructing per-row widgets.
- Versioned portable ZIP packaging with GUI and BT runtime binaries, README, user guide, per-file SHA-256 manifest and archive checksum.

## Packaging Decision

The release target is portable ZIP only. No installer project is produced. Updating consists of closing the application and replacing the two sibling executables from a verified portable archive.

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

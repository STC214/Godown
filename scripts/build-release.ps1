param(
    [string]$OutputDir = "dist",
    [string]$Version = "0.0.0-dev"
)
$ErrorActionPreference = "Stop"
$root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$target = if ([IO.Path]::IsPathRooted($OutputDir)) {
    [IO.Path]::GetFullPath($OutputDir)
} else {
    [IO.Path]::GetFullPath((Join-Path $root $OutputDir))
}
[IO.Directory]::CreateDirectory($target) | Out-Null

Push-Location $root
try {
    $versionFlag = "-H=windowsgui -X ghost-downloader-go-win32/internal/buildinfo.Version=$Version"
    # Intentionally keep Go symbol and DWARF information in both executables.
    go build -trimpath -ldflags $versionFlag -o (Join-Path $target "gd3win.exe") ./cmd/gd3win
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    go build -trimpath -o (Join-Path $target "gd3-bt-runtime.exe") ./cmd/gd3-bt-runtime
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
} finally {
    Pop-Location
}

Write-Output "BUILD=PASS"
Write-Output "VERSION=$Version"
Write-Output "GUI=$(Join-Path $target 'gd3win.exe')"
Write-Output "BT_RUNTIME=$(Join-Path $target 'gd3-bt-runtime.exe')"

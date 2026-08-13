param([string]$OutputDir = "dist")
$ErrorActionPreference = "Stop"
$root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$target = [IO.Path]::GetFullPath((Join-Path $root $OutputDir))
[IO.Directory]::CreateDirectory($target) | Out-Null

Push-Location $root
try {
    # Intentionally keep Go symbol and DWARF information in both executables.
    go build -trimpath -o (Join-Path $target "gd3win.exe") ./cmd/gd3win
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    go build -trimpath -o (Join-Path $target "gd3-bt-runtime.exe") ./cmd/gd3-bt-runtime
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
} finally {
    Pop-Location
}

Write-Output "BUILD=PASS"
Write-Output "GUI=$(Join-Path $target 'gd3win.exe')"
Write-Output "BT_RUNTIME=$(Join-Path $target 'gd3-bt-runtime.exe')"

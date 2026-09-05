param(
    [string]$Root = (Join-Path $PSScriptRoot "..")
)

$ErrorActionPreference = "Stop"
$rootPath = [IO.Path]::GetFullPath($Root)
$patchPath = Join-Path $rootPath "docs\implementation-notes\015-stage-20.patch"
$stage20Zip = Join-Path $rootPath "release\GhostDownloader-0.1.13-stage20-windows-x64-portable.zip"
$stage20Hash = $stage20Zip + ".sha256"

Push-Location $rootPath
try {
    git apply --reverse --check -- $patchPath
    if ($LASTEXITCODE -ne 0) { throw "Stage 20 reverse patch check failed" }
    git apply --reverse -- $patchPath
    if ($LASTEXITCODE -ne 0) { throw "Stage 20 reverse patch failed" }
    foreach ($file in @($stage20Zip, $stage20Hash)) {
        if (Test-Path -LiteralPath $file -PathType Leaf) {
            Remove-Item -LiteralPath $file -Force
        }
    }
    & (Join-Path $rootPath "scripts\package-portable.ps1") -Version "0.1.12-stage18"
    if ($LASTEXITCODE -ne 0) { throw "Stage 18 portable package rebuild failed" }
} finally {
    Pop-Location
}

Write-Output "ROLLBACK=PASS"
Write-Output "RESTORED_VERSION=0.1.12-stage18"

param(
    [string]$Root = (Join-Path $PSScriptRoot "..")
)

$ErrorActionPreference = "Stop"
$rootPath = [IO.Path]::GetFullPath($Root)
$patchPath = Join-Path $rootPath "docs\implementation-notes\016-stage-21.patch"
$stage21Zip = Join-Path $rootPath "release\GhostDownloader-0.1.14-stage21-windows-x64-portable.zip"
$stage21Hash = $stage21Zip + ".sha256"

Push-Location $rootPath
try {
    git apply --reverse --check -- $patchPath
    if ($LASTEXITCODE -ne 0) { throw "Stage 21 reverse patch check failed" }
    git apply --reverse -- $patchPath
    if ($LASTEXITCODE -ne 0) { throw "Stage 21 reverse patch failed" }
    foreach ($file in @($stage21Zip, $stage21Hash)) {
        if (Test-Path -LiteralPath $file -PathType Leaf) {
            Remove-Item -LiteralPath $file -Force
        }
    }
    & (Join-Path $rootPath "scripts\package-portable.ps1") -Version "0.1.13-stage20"
    if ($LASTEXITCODE -ne 0) { throw "Stage 20 portable package rebuild failed" }
} finally {
    Pop-Location
}

Write-Output "ROLLBACK=PASS"
Write-Output "RESTORED_VERSION=0.1.13-stage20"

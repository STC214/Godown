param([string]$TargetRoot = (Join-Path $PSScriptRoot ".."))
$ErrorActionPreference = "Stop"
$root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$target = [IO.Path]::GetFullPath($TargetRoot)
$original = Join-Path $root ".codex-artifacts/stage27-modified"
$files = @(
    "internal/download/ftp/ftp.go",
    "internal/download/ftp/ftp_test.go",
    "internal/core/model.go",
    "internal/core/scheduler_test.go"
)
$modifiedHashes = @{
    "internal/download/ftp/ftp.go" = "D02A6014DA40514D9CA2025D0BDBF5D85227658A5BCB59B77664E38D0BF2E5E7"
    "internal/download/ftp/ftp_test.go" = "F2E255EB73A36A566163ADDCC4AD88AF4AE3F6A07E9130D3F38DC74164C6A42E"
    "internal/core/model.go" = "C5C139C2D2EE91A7806B8710181AC493FF41E93DF234911C2B68A7B17837A5FA"
    "internal/core/scheduler_test.go" = "CD089ED90A6CF0576E945671F87BCB3A161BE2FC34524CC86675BB9913B3DBC9"
}
foreach ($file in $files) {
    $currentPath = Join-Path $target $file
    $originalPath = Join-Path $original $file
    $currentHash = (Get-FileHash -LiteralPath $currentPath -Algorithm SHA256).Hash
    $originalHash = (Get-FileHash -LiteralPath $originalPath -Algorithm SHA256).Hash
    if ($currentHash -ne $modifiedHashes[$file] -and $currentHash -ne $originalHash) {
        throw "Later changes detected: $file"
    }
}
foreach ($file in $files) {
    $source = Join-Path $original $file
    $destination = Join-Path $target $file
    Copy-Item -LiteralPath $source -Destination $destination -Force
    if ((Get-FileHash -LiteralPath $source).Hash -ne (Get-FileHash -LiteralPath $destination).Hash) {
        throw "Rollback hash mismatch: $file"
    }
}
Write-Output "ROLLBACK=PASS (Stage 28 FTP behavior restored to Stage 27; user data and packages untouched)"

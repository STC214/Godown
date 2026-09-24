param([string]$TargetRoot = (Join-Path $PSScriptRoot ".."))
$ErrorActionPreference = "Stop"
$root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$target = [IO.Path]::GetFullPath($TargetRoot)
$original = Join-Path $root ".codex-artifacts/stage25-original"
$modified = Join-Path $root ".codex-artifacts/stage25-modified"
$files = @("internal/download/ftp/ftp.go", "internal/download/ftp/ftp_test.go", "internal/ui/app.go", "README.md")
foreach ($file in $files) {
    $before = (Get-FileHash -LiteralPath (Join-Path $original $file)).Hash
    $after = (Get-FileHash -LiteralPath (Join-Path $modified $file)).Hash
    $current = (Get-FileHash -LiteralPath (Join-Path $target $file)).Hash
    if ($current -ne $before -and $current -ne $after) { throw "Later changes detected: $file" }
}
foreach ($file in $files) {
    $source = Join-Path $original $file
    $destination = Join-Path $target $file
    Copy-Item -LiteralPath $source -Destination $destination -Force
    if ((Get-FileHash -LiteralPath $source).Hash -ne (Get-FileHash -LiteralPath $destination).Hash) {
        throw "Rollback hash mismatch: $file"
    }
}
Write-Output "ROLLBACK=PASS (source and README; user data and portable packages untouched)"

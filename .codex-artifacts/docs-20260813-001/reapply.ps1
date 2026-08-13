param([string]$Workspace = "")
$ErrorActionPreference = "Stop"
if ([string]::IsNullOrWhiteSpace($Workspace)) {
    $Workspace = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot "..\.."))
}
$Workspace = [IO.Path]::GetFullPath($Workspace)
$sourceRoot = Join-Path $PSScriptRoot "modified-docs"
$manifest = Get-Content -Raw (Join-Path $PSScriptRoot "modified-sha256.json") | ConvertFrom-Json
foreach ($entry in $manifest) {
    $relative = $entry.Path.Replace('/', '\')
    $source = Join-Path $sourceRoot $relative
    $target = [IO.Path]::GetFullPath((Join-Path $Workspace $relative))
    if (!$target.StartsWith($Workspace + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Target outside workspace: $relative"
    }
    if ((Get-FileHash -Algorithm SHA256 $source).Hash.ToLower() -ne $entry.SHA256) {
        throw "Modified document integrity failure: $relative"
    }
    [IO.Directory]::CreateDirectory((Split-Path $target -Parent)) | Out-Null
    Copy-Item -Force -LiteralPath $source -Destination $target
}
Write-Output "REAPPLY=PASS"
Write-Output "FILES_PROCESSED=$($manifest.Count)"

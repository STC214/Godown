param([string]$Workspace = "")
$ErrorActionPreference = "Stop"
if ([string]::IsNullOrWhiteSpace($Workspace)) {
    $Workspace = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot "..\.."))
}
$Workspace = [IO.Path]::GetFullPath($Workspace)
$state = Get-Content -Raw (Join-Path $PSScriptRoot "baseline-state.json") | ConvertFrom-Json
foreach ($entry in $state) {
    $relative = $entry.Path.Replace('/', '\')
    $target = [IO.Path]::GetFullPath((Join-Path $Workspace $relative))
    if (!$target.StartsWith($Workspace + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Target outside workspace: $relative"
    }
    if (!$entry.Exists) {
        if (Test-Path -LiteralPath $target) { [IO.File]::Delete($target) }
        continue
    }
    $source = Join-Path (Join-Path $PSScriptRoot "baseline") $relative
    if ((Get-FileHash -Algorithm SHA256 $source).Hash.ToLower() -ne $entry.SHA256) {
        throw "Baseline integrity failure: $relative"
    }
    [IO.Directory]::CreateDirectory((Split-Path $target -Parent)) | Out-Null
    Copy-Item -Force -LiteralPath $source -Destination $target
}
Write-Output "ROLLBACK=PASS"
Write-Output "FILES_PROCESSED=$($state.Count)"


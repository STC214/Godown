param([string]$Workspace = "")
$ErrorActionPreference = "Stop"
$artifactRoot = $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($Workspace)) {
    $Workspace = [IO.Path]::GetFullPath((Join-Path $artifactRoot "..\.."))
}
$Workspace = [IO.Path]::GetFullPath($Workspace)
$baseline = Join-Path $artifactRoot "baseline"
$changes = Get-Content -Raw (Join-Path $artifactRoot "changed-manifest.json") | ConvertFrom-Json
$hashes = Get-Content -Raw (Join-Path $artifactRoot "baseline-sha256.json") | ConvertFrom-Json

foreach ($entry in $hashes) {
    $source = Join-Path $baseline ($entry.Path.Replace('/', '\'))
    if (!(Test-Path -LiteralPath $source) -or (Get-FileHash -Algorithm SHA256 $source).Hash.ToLower() -ne $entry.SHA256) {
        throw "Baseline integrity failure: $($entry.Path)"
    }
}

foreach ($entry in $changes) {
    $relative = $entry.Path.Replace('/', '\')
    $target = [IO.Path]::GetFullPath((Join-Path $Workspace $relative))
    if (!$target.StartsWith($Workspace + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Target outside workspace: $relative"
    }
    if ($entry.Status.StartsWith('A')) {
        if (Test-Path -LiteralPath $target) { [IO.File]::Delete($target) }
        continue
    }
    $source = Join-Path $baseline $relative
    $parent = Split-Path -Parent $target
    [IO.Directory]::CreateDirectory($parent) | Out-Null
    Copy-Item -Force -LiteralPath $source -Destination $target
}
Write-Output "ROLLBACK=PASS"
Write-Output "WORKSPACE=$Workspace"
Write-Output "FILES_PROCESSED=$($changes.Count)"

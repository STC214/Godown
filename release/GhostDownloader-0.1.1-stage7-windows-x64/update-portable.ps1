param(
    [Parameter(Mandatory = $true)][string]$Archive,
    [Parameter(Mandatory = $true)][string]$InstallDir,
    [int]$WaitPID = 0,
    [string]$Relaunch = ""
)
$ErrorActionPreference = "Stop"
$archivePath = [IO.Path]::GetFullPath($Archive)
$installPath = [IO.Path]::GetFullPath($InstallDir)
if (-not (Test-Path -LiteralPath $archivePath -PathType Leaf)) { throw "Archive does not exist: $archivePath" }
if (-not (Test-Path -LiteralPath $installPath -PathType Container)) { throw "Install directory does not exist: $installPath" }

if ($WaitPID -gt 0) {
    $process = Get-Process -Id $WaitPID -ErrorAction SilentlyContinue
    if ($process -and -not $process.WaitForExit(120000)) { throw "Application did not exit within 120 seconds" }
}

$work = Join-Path ([IO.Path]::GetTempPath()) ("GhostDownloaderUpdate-" + [guid]::NewGuid().ToString("N"))
$extract = Join-Path $work "extract"
$backup = Join-Path $work "backup"
[IO.Directory]::CreateDirectory($extract) | Out-Null
[IO.Directory]::CreateDirectory($backup) | Out-Null
$installed = New-Object System.Collections.Generic.List[string]
$backedUp = New-Object System.Collections.Generic.HashSet[string]([StringComparer]::OrdinalIgnoreCase)

try {
    Expand-Archive -LiteralPath $archivePath -DestinationPath $extract
    $manifestPath = Join-Path $extract "release-manifest.json"
    if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) { throw "Portable manifest is missing" }
    $manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
    foreach ($file in $manifest.files) {
        $relative = [string]$file.name
        if ([IO.Path]::IsPathRooted($relative) -or $relative.Contains("..")) { throw "Unsafe manifest path: $relative" }
        $source = [IO.Path]::GetFullPath((Join-Path $extract $relative))
        if (-not $source.StartsWith($extract + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw "Manifest path escapes archive: $relative" }
        if (-not (Test-Path -LiteralPath $source -PathType Leaf)) { throw "Manifest file is missing: $relative" }
        $actual = (Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash
        if ($actual -ne [string]$file.sha256) { throw "Manifest hash mismatch: $relative" }
    }

    foreach ($file in $manifest.files) {
        $relative = [string]$file.name
        $source = Join-Path $extract $relative
        $destination = Join-Path $installPath $relative
        New-Item -ItemType Directory -Force -Path (Split-Path $destination) | Out-Null
        if (Test-Path -LiteralPath $destination -PathType Leaf) {
            $backupPath = Join-Path $backup $relative
            New-Item -ItemType Directory -Force -Path (Split-Path $backupPath) | Out-Null
            Copy-Item -LiteralPath $destination -Destination $backupPath -Force
            [void]$backedUp.Add($relative)
        }
        $installed.Add($relative)
        Copy-Item -LiteralPath $source -Destination $destination -Force
    }
    $manifestRelative = "release-manifest.json"
    $manifestDestination = Join-Path $installPath $manifestRelative
    if (Test-Path -LiteralPath $manifestDestination -PathType Leaf) {
        Copy-Item -LiteralPath $manifestDestination -Destination (Join-Path $backup $manifestRelative) -Force
        [void]$backedUp.Add($manifestRelative)
    }
    $installed.Add($manifestRelative)
    Copy-Item -LiteralPath $manifestPath -Destination $manifestDestination -Force
} catch {
    foreach ($relative in $installed) {
        $destination = Join-Path $installPath $relative
        if ($backedUp.Contains($relative)) {
            Copy-Item -LiteralPath (Join-Path $backup $relative) -Destination $destination -Force
        } elseif (Test-Path -LiteralPath $destination -PathType Leaf) {
            Remove-Item -LiteralPath $destination -Force
        }
    }
    throw
} finally {
    if (Test-Path -LiteralPath $work -PathType Container) {
        Remove-Item -LiteralPath $work -Recurse -Force
    }
}

if ($Relaunch) {
    $relaunchPath = [IO.Path]::GetFullPath($Relaunch)
    if ($relaunchPath.StartsWith($installPath + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase) -and (Test-Path -LiteralPath $relaunchPath -PathType Leaf)) {
        Start-Process -FilePath $relaunchPath -WorkingDirectory $installPath
    }
}
Write-Output "UPDATE=PASS"
Write-Output "VERSION=$($manifest.version)"

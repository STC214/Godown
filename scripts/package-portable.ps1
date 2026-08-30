param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$')]
    [string]$Version,
    [string]$OutputDir = "release"
)
$ErrorActionPreference = "Stop"
$root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$releaseRoot = [IO.Path]::GetFullPath((Join-Path $root $OutputDir))
$staging = Join-Path $releaseRoot "GhostDownloader-$Version-windows-x64"
[IO.Directory]::CreateDirectory($releaseRoot) | Out-Null
[IO.Directory]::CreateDirectory($staging) | Out-Null

Push-Location $root
try {
    & (Join-Path $PSScriptRoot "build-release.ps1") -OutputDir $staging -Version $Version
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    Copy-Item -LiteralPath README.md -Destination (Join-Path $staging "README.md") -Force
    Copy-Item -LiteralPath docs/USER_GUIDE.zh-CN.md -Destination (Join-Path $staging "USER_GUIDE.zh-CN.md") -Force
    Copy-Item -LiteralPath scripts/update-portable.ps1 -Destination (Join-Path $staging "update-portable.ps1") -Force

    $files = Get-ChildItem -LiteralPath $staging -File | Where-Object Name -ne "release-manifest.json" | Sort-Object Name
    $manifest = [ordered]@{
        version = $Version
        platform = "windows-x64"
        createdAtUtc = [DateTime]::UtcNow.ToString("o")
        files = @($files | ForEach-Object {
            [ordered]@{
                name = $_.Name
                size = $_.Length
                sha256 = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
            }
        })
    }
    $manifest | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $staging "release-manifest.json") -Encoding utf8

    $zip = Join-Path $releaseRoot "GhostDownloader-$Version-windows-x64-portable.zip"
    Compress-Archive -Path (Join-Path $staging "*") -DestinationPath $zip -CompressionLevel Optimal -Force
    $zipHash = (Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash.ToLowerInvariant()
    "$zipHash  $([IO.Path]::GetFileName($zip))" | Set-Content -LiteralPath ($zip + ".sha256") -Encoding ascii
} finally {
    Pop-Location
}

Write-Output "PACKAGE=PASS"
Write-Output "VERSION=$Version"
Write-Output "PORTABLE_ZIP=$zip"
Write-Output "SHA256=$zipHash"

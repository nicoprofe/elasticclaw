<#
.SYNOPSIS
    Installs the elasticclaw CLI on Windows.

.DESCRIPTION
    Downloads the latest release binary for this machine's architecture, verifies
    it against the release checksum manifest, installs it under LOCALAPPDATA, and
    puts it on the user PATH. No administrator rights required.

    After installation, `elasticclaw upgrade` handles every future update, so this
    script is only needed once per machine.

    NOTE TO MAINTAINERS: keep this file pure ASCII. Windows PowerShell 5.1 decodes
    a BOM-less UTF-8 script as CP1252, where a multi-byte character such as an em
    dash can produce a smart quote - which PowerShell honors as a string delimiter,
    breaking the script in ways that only appear on a user's machine.

.EXAMPLE
    irm https://raw.githubusercontent.com/nicoprofe/elasticclaw/main/scripts/install.ps1 | iex

.EXAMPLE
    # Install a specific version, or from a different repository:
    $env:ELASTICCLAW_VERSION = '2026.7.24'
    $env:ELASTICCLAW_RELEASE_REPO = 'nicoprofe/elasticclaw'
    irm https://raw.githubusercontent.com/nicoprofe/elasticclaw/main/scripts/install.ps1 | iex
#>

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$Repo       = if ($env:ELASTICCLAW_RELEASE_REPO) { $env:ELASTICCLAW_RELEASE_REPO } else { 'nicoprofe/elasticclaw' }
$Version    = $env:ELASTICCLAW_VERSION
$InstallDir = if ($env:ELASTICCLAW_INSTALL_DIR) { $env:ELASTICCLAW_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\elasticclaw' }

# TLS 1.2 is not the default on older Windows PowerShell, and github.com requires it.
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

# --- Architecture ------------------------------------------------------------
$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}
$asset = "elasticclaw-windows-$arch.exe"

# --- Resolve version ---------------------------------------------------------
if (-not $Version) {
    Write-Host 'Finding latest release...' -NoNewline
    try {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers @{ 'User-Agent' = 'elasticclaw-installer' }
        $Version = $release.tag_name
    } catch {
        throw "Could not determine the latest release of $Repo. Set `$env:ELASTICCLAW_VERSION to install a specific version. ($_)"
    }
    Write-Host " $Version"
}

$base = "https://github.com/$Repo/releases/download/$Version"
$tmp  = Join-Path ([System.IO.Path]::GetTempPath()) "elasticclaw-install-$([System.Guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory -Path $tmp -Force | Out-Null

try {
    # --- Download ------------------------------------------------------------
    $binaryPath = Join-Path $tmp $asset
    Write-Host "Downloading $asset ($Version)..."
    Invoke-WebRequest -Uri "$base/$asset" -OutFile $binaryPath -UseBasicParsing

    # --- Verify --------------------------------------------------------------
    # An unverified binary is never executed or installed.
    Write-Host 'Verifying checksum...' -NoNewline
    $manifestPath = Join-Path $tmp 'checksums.txt'
    Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile $manifestPath -UseBasicParsing

    $expected = $null
    foreach ($line in Get-Content $manifestPath) {
        $fields = -split $line.Trim()
        if ($fields.Count -eq 2 -and $fields[1].TrimStart('*') -eq $asset) {
            $expected = $fields[0].ToLower()
            break
        }
    }
    if (-not $expected) { throw "checksums.txt for $Version does not list $asset" }

    $actual = (Get-FileHash -Path $binaryPath -Algorithm SHA256).Hash.ToLower()
    if ($actual -ne $expected) {
        throw "Checksum mismatch for $asset. Expected $expected but got $actual. The download was corrupted or tampered with; nothing was installed."
    }
    Write-Host ' OK'

    # --- Install -------------------------------------------------------------
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    $target = Join-Path $InstallDir 'elasticclaw.exe'
    $backup = "${target}.old"

    # A running elasticclaw holds a lock on its own image; renaming it is still
    # permitted, so displace the old copy rather than failing the install.
    if (Test-Path $target) {
        Remove-Item $backup -Force -ErrorAction SilentlyContinue
        try {
            Rename-Item -Path $target -NewName 'elasticclaw.exe.old' -Force
        } catch {
            throw "Could not replace $target. Close any running elasticclaw processes and retry. ($_)"
        }
    }
    Move-Item -Path $binaryPath -Destination $target -Force
    Remove-Item $backup -Force -ErrorAction SilentlyContinue

    # --- PATH ----------------------------------------------------------------
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if ($userPath -notlike "*$InstallDir*") {
        $newPath = if ([string]::IsNullOrEmpty($userPath)) { $InstallDir } else { "$userPath;$InstallDir" }
        [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
        Write-Host "Added $InstallDir to your user PATH (restart your terminal to pick it up)."
    }
    # Make it usable in this session too.
    if ($env:Path -notlike "*$InstallDir*") { $env:Path = "$env:Path;$InstallDir" }

    $installed = & $target version
    Write-Host ''
    Write-Host "Installed: $installed" -ForegroundColor Green
    Write-Host ''
    Write-Host 'Next steps:'
    Write-Host '  elasticclaw --help     # see available commands'
    Write-Host '  elasticclaw login      # connect to your hub'
    Write-Host '  elasticclaw upgrade    # update in place, any time'
} finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

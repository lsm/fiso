#Requires -Version 5.1
<#
.SYNOPSIS
    Installs the Fiso CLI on Windows.
.DESCRIPTION
    Downloads the latest (or specified) Fiso CLI release and installs it.
.EXAMPLE
    irm https://raw.githubusercontent.com/lsm/fiso/main/install.ps1 | iex
.EXAMPLE
    $env:FISO_VERSION = "v0.5.0"; irm https://raw.githubusercontent.com/lsm/fiso/main/install.ps1 | iex
#>

$ErrorActionPreference = "Stop"

$Repo = "lsm/fiso"
$Version = if ($env:FISO_VERSION) { $env:FISO_VERSION } else { "latest" }
$InstallDir = if ($env:FISO_INSTALL_DIR) { $env:FISO_INSTALL_DIR } else {
    Join-Path $env:LOCALAPPDATA "fiso" "bin"
}

function Get-Architecture {
    $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
    switch ($arch) {
        "X64"   { return "amd64" }
        "Arm64" { return "arm64" }
        default { throw "Unsupported architecture: $arch" }
    }
}

function Resolve-Version {
    param([string]$ver)
    if ($ver -eq "latest") {
        $release = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
        if (-not $release.tag_name) {
            throw "Could not determine latest version"
        }
        return $release.tag_name
    }
    return $ver
}

function Install-Fiso {
    $arch = Get-Architecture
    $resolvedVersion = Resolve-Version $Version

    $zipName = "fiso_windows_${arch}.zip"
    $url = "https://github.com/$Repo/releases/download/$resolvedVersion/$zipName"

    $tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())
    New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null

    try {
        Write-Host "Downloading fiso $resolvedVersion (windows/$arch)..."
        $zipPath = Join-Path $tmpDir $zipName
        Invoke-WebRequest -Uri $url -OutFile $zipPath -UseBasicParsing

        Expand-Archive -Path $zipPath -DestinationPath $tmpDir -Force

        $binary = Join-Path $tmpDir "fiso.exe"
        if (-not (Test-Path $binary)) {
            throw "fiso.exe not found in archive"
        }

        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        Copy-Item $binary (Join-Path $InstallDir "fiso.exe") -Force

        # Add to user PATH if not already present
        $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
        if ($userPath -notlike "*$InstallDir*") {
            [Environment]::SetEnvironmentVariable(
                "Path",
                "$InstallDir;$userPath",
                "User"
            )
            Write-Host ""
            Write-Host "Added $InstallDir to your PATH."
            Write-Host "Restart your terminal for PATH changes to take effect."
        }

        # Verify
        $installed = Join-Path $InstallDir "fiso.exe"
        $null = & $installed --help 2>&1

        Write-Host ""
        Write-Host "fiso $resolvedVersion installed to $installed"
        Write-Host ""
        Write-Host "Get started:"
        Write-Host "  mkdir my-project; cd my-project"
        Write-Host "  fiso init"
        Write-Host "  fiso dev"
    }
    finally {
        Remove-Item -Path $tmpDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}

Install-Fiso

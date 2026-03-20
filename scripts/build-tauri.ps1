# CodeFlow Tauri Build Script for Windows
# Usage: .\scripts\build-tauri.ps1

$ErrorActionPreference = "Stop"

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  CodeFlow Tauri Build Script" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

$ProjectRoot = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$BackendDir = Join-Path $ProjectRoot "backend"
$FrontendDir = Join-Path $ProjectRoot "codeflow_template"
$BinariesDir = Join-Path $FrontendDir "src-tauri\binaries"

function Invoke-FrontendInstall {
    param([Parameter(Mandatory = $true)][string]$FrontendDir)

    if (-not (Test-Path (Join-Path $FrontendDir 'node_modules'))) {
        if (Test-Path (Join-Path $FrontendDir 'package-lock.json')) {
            npm ci
        }
        else {
            npm install
        }

        if ($LASTEXITCODE -ne 0) {
            throw 'Frontend dependency installation failed'
        }
    }
}

function Get-ExecutablePath {
    param([Parameter(Mandatory = $true)][string]$Name)

    $command = Get-Command $Name -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -eq $command) {
        return $null
    }

    return $command.Source
}

function Import-EnvironmentFromBatch {
    param(
        [Parameter(Mandatory = $true)][string]$BatchPath,
        [string[]]$Arguments = @(),
        [string[]]$PrependPathEntries = @()
    )

    $quotedArgs = @(
        $Arguments | ForEach-Object {
            if ($_ -match '\s') {
                '"{0}"' -f $_
            }
            else {
                $_
            }
        }
    ) -join ' '

    $pathPrefix = ''
    $joinedPaths = @(
        $PrependPathEntries | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | Select-Object -Unique
    ) -join ';'
    if (-not [string]::IsNullOrWhiteSpace($joinedPaths)) {
        $pathPrefix = 'set "PATH={0};%PATH%" && ' -f $joinedPaths
    }

    $cmdCommand = if ([string]::IsNullOrWhiteSpace($quotedArgs)) {
        '{0}call "{1}" >nul && set' -f $pathPrefix, $BatchPath
    }
    else {
        '{0}call "{1}" {2} >nul && set' -f $pathPrefix, $BatchPath, $quotedArgs
    }

    $envLines = & cmd.exe /d /s /c $cmdCommand
    if ($LASTEXITCODE -ne 0) {
        return $false
    }

    foreach ($line in $envLines) {
        if ($line -match '^(.*?)=(.*)$') {
            Set-Item -Path ("Env:{0}" -f $matches[1]) -Value $matches[2]
        }
    }

    return $true
}

function Get-VsWhereExecutable {
    $candidates = @(
        (Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\Installer\vswhere.exe'),
        'C:\Program Files (x86)\Microsoft Visual Studio\Installer\vswhere.exe'
    ) | Where-Object { $_ -and (Test-Path $_) } | Select-Object -Unique

    return $candidates | Select-Object -First 1
}

function Get-VisualStudioEnvironmentBatch {
    $vswhere = Get-VsWhereExecutable
    if ($vswhere) {
        $installPath = & $vswhere -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath 2>$null | Select-Object -First 1
        if ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace($installPath)) {
            foreach ($candidate in @(
                (Join-Path $installPath 'VC\Auxiliary\Build\vcvars64.bat'),
                (Join-Path $installPath 'Common7\Tools\VsDevCmd.bat')
            )) {
                if (Test-Path $candidate) {
                    return $candidate
                }
            }
        }
    }

    foreach ($candidate in @(
        'D:\Appliciation\Microsoft\VisualStudio\18\Community\VC\Auxiliary\Build\vcvars64.bat',
        'D:\Appliciation\Microsoft\VisualStudio\18\Community\Common7\Tools\VsDevCmd.bat',
        'C:\Program Files\Microsoft Visual Studio\2022\Community\VC\Auxiliary\Build\vcvars64.bat',
        'C:\Program Files\Microsoft Visual Studio\2022\Community\Common7\Tools\VsDevCmd.bat',
        'C:\Program Files\Microsoft Visual Studio\2022\BuildTools\VC\Auxiliary\Build\vcvars64.bat',
        'C:\Program Files\Microsoft Visual Studio\2022\BuildTools\Common7\Tools\VsDevCmd.bat'
    )) {
        if (Test-Path $candidate) {
            return $candidate
        }
    }

    return $null
}

function Import-VisualStudioBuildEnvironment {
    $batchPath = Get-VisualStudioEnvironmentBatch
    if (-not $batchPath) {
        Write-Host '  [ERROR] Visual Studio C++ build tools were not found.' -ForegroundColor Red
        Write-Host '  Install Desktop development with C++ or run this script from a VS Developer PowerShell.' -ForegroundColor Yellow
        return $false
    }

    $arguments = @()
    if ($batchPath -like '*VsDevCmd.bat') {
        $arguments = @('-arch=x64', '-host_arch=x64')
    }

    $prependPathEntries = @()
    $vswhere = Get-VsWhereExecutable
    if ($vswhere) {
        $prependPathEntries += (Split-Path -Parent $vswhere)
    }

    if (-not (Import-EnvironmentFromBatch -BatchPath $batchPath -Arguments $arguments -PrependPathEntries $prependPathEntries)) {
        Write-Host "  [ERROR] Failed to import Visual Studio build environment from $batchPath" -ForegroundColor Red
        return $false
    }

    $clPath = Get-ExecutablePath -Name 'cl.exe'
    if (-not $clPath) {
        Write-Host "  [ERROR] cl.exe is still unavailable after importing $batchPath" -ForegroundColor Red
        return $false
    }

    $env:CC_x86_64_pc_windows_msvc = 'cl.exe'
    $env:CXX_x86_64_pc_windows_msvc = 'cl.exe'

    Write-Host "  Using MSVC environment from: $batchPath" -ForegroundColor DarkGray
    Write-Host "  cl.exe: $clPath" -ForegroundColor DarkGray
    if ($env:VCINSTALLDIR) {
        Write-Host "  VCINSTALLDIR: $($env:VCINSTALLDIR)" -ForegroundColor DarkGray
    }
    if ($env:VCToolsVersion) {
        Write-Host "  VCToolsVersion: $($env:VCToolsVersion)" -ForegroundColor DarkGray
    }
    if ($env:WindowsSdkDir) {
        Write-Host "  WindowsSdkDir: $($env:WindowsSdkDir)" -ForegroundColor DarkGray
    }

    return $true
}

New-Item -ItemType Directory -Path $BinariesDir -Force | Out-Null

$OriginalLocation = Get-Location
try {
    # Step 1: Build Go Backend
    Write-Host "`n[1/3] Building Go Backend..." -ForegroundColor Yellow
    Set-Location $BackendDir

    $OutputPath = Join-Path $BinariesDir "codeflow-server-x86_64-pc-windows-msvc.exe"
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    $env:CGO_ENABLED = "1"

    Write-Host "  Target: $OutputPath"
    go build -ldflags="-s -w" -o $OutputPath ./cmd/codeflow-server

    if ($LASTEXITCODE -ne 0) {
        Write-Host "  [ERROR] Go build failed!" -ForegroundColor Red
        exit 1
    }
    Write-Host "  [OK] Go backend built successfully" -ForegroundColor Green

    # Step 2: Install frontend dependencies
    Write-Host "`n[2/3] Installing frontend dependencies..." -ForegroundColor Yellow
    Set-Location $FrontendDir
    Invoke-FrontendInstall -FrontendDir $FrontendDir
    Write-Host "  [OK] Frontend dependencies ready" -ForegroundColor Green

    # Step 3: Build Tauri Application
    Write-Host "`n[3/3] Building Tauri Application..." -ForegroundColor Yellow
    if (-not (Import-VisualStudioBuildEnvironment)) {
        exit 1
    }

    npm run tauri:build
    if ($LASTEXITCODE -ne 0) {
        Write-Host "  [ERROR] Tauri build failed!" -ForegroundColor Red
        Write-Host '  If the failure still mentions vswhom-sys or LNK1143, the previous build may have mixed GNU and MSVC artifacts.' -ForegroundColor Yellow
        Write-Host '  Retry after cleaning Rust build artifacts: cargo clean --manifest-path codeflow_template/src-tauri/Cargo.toml' -ForegroundColor Yellow
        exit 1
    }
    Write-Host "  [OK] Tauri application built successfully" -ForegroundColor Green

    # Output results
    Write-Host "`n========================================" -ForegroundColor Cyan
    Write-Host "  Build Complete!" -ForegroundColor Green
    Write-Host "========================================" -ForegroundColor Cyan

    $MsiPath = Join-Path $FrontendDir "src-tauri\target\release\bundle\msi"
    $NsisPath = Join-Path $FrontendDir "src-tauri\target\release\bundle\nsis"
    $ExePath = Join-Path $FrontendDir "src-tauri\target\release\codeflow.exe"

    Write-Host "`nOutput files:"
    if (Test-Path $ExePath) {
        Write-Host "  Executable: $ExePath" -ForegroundColor White
    }
    if (Test-Path $MsiPath) {
        Write-Host "  MSI Installer: $MsiPath" -ForegroundColor White
    }
    if (Test-Path $NsisPath) {
        Write-Host "  NSIS Installer: $NsisPath" -ForegroundColor White
    }
}
finally {
    Set-Location $OriginalLocation
}

exit 0

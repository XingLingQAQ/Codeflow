# CodeFlow Build Script (Windows)
# Builds frontend and embeds it into Go backend

param(
    [string]$OutputDir = ".\dist",
    [switch]$SkipFrontend
)

function Copy-DirectoryContents {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string]$Destination
    )

    if (-not (Test-Path $Source)) {
        throw "Source directory not found: $Source"
    }

    if (Test-Path $Destination) {
        Remove-Item -Recurse -Force $Destination
    }

    New-Item -ItemType Directory -Path $Destination | Out-Null
    Copy-Item -Path (Join-Path $Source '*') -Destination $Destination -Recurse -Force
}

function Invoke-FrontendInstall {
    param(
        [Parameter(Mandatory = $true)][string]$FrontendDir,
        [Parameter(Mandatory = $true)][string]$RootDir
    )

    $workspaceYaml = Join-Path $RootDir 'pnpm-workspace.yaml'
    $rootPkg = Join-Path $RootDir 'package.json'
    $localNodeModules = Join-Path $FrontendDir 'node_modules'
    $rootNodeModules = Join-Path $RootDir 'node_modules'

    # Prefer monorepo pnpm install at repo root for apps/desktop
    if ((Test-Path $workspaceYaml) -and (Test-Path $rootPkg)) {
        if (-not (Test-Path $rootNodeModules)) {
            Push-Location $RootDir
            try {
                Write-Host "Installing monorepo dependencies via pnpm..."
                pnpm install
                if ($LASTEXITCODE -ne 0) {
                    throw 'Frontend dependency installation failed (pnpm install)'
                }
            }
            finally {
                Pop-Location
            }
        }
        return
    }

    if (-not (Test-Path $localNodeModules)) {
        Push-Location $FrontendDir
        try {
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
        finally {
            Pop-Location
        }
    }
}

function Invoke-FrontendBuild {
    param(
        [Parameter(Mandatory = $true)][string]$FrontendDir,
        [Parameter(Mandatory = $true)][string]$RootDir
    )

    $workspaceYaml = Join-Path $RootDir 'pnpm-workspace.yaml'
    if (Test-Path $workspaceYaml) {
        Push-Location $RootDir
        try {
            Write-Host "Building frontend with pnpm --filter @codeflow/desktop..."
            pnpm --filter @codeflow/desktop build
            if ($LASTEXITCODE -ne 0) {
                throw 'Frontend build failed'
            }
        }
        finally {
            Pop-Location
        }
        return
    }

    Push-Location $FrontendDir
    try {
        Write-Host "Building frontend with Vite..."
        npm run build
        if ($LASTEXITCODE -ne 0) {
            throw 'Frontend build failed'
        }
    }
    finally {
        Pop-Location
    }
}

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RootDir = Split-Path -Parent $ScriptDir

Write-Host "=== CodeFlow Build Script ===" -ForegroundColor Cyan
Write-Host "Root directory: $RootDir"

# Step 1: Build Frontend
if (-not $SkipFrontend) {
    Write-Host "`n[1/3] Building frontend..." -ForegroundColor Yellow

    $FrontendDir = Join-Path $RootDir "apps\desktop"
    if (-not (Test-Path $FrontendDir)) {
        Write-Error "Frontend directory not found: $FrontendDir"
        exit 1
    }

    try {
        Write-Host "Installing frontend dependencies if needed..."
        Invoke-FrontendInstall -FrontendDir $FrontendDir -RootDir $RootDir
        Invoke-FrontendBuild -FrontendDir $FrontendDir -RootDir $RootDir

        $FrontendDistDir = Join-Path $FrontendDir "dist"
        $EmbeddedDistDir = Join-Path $RootDir "backend\internal\web\dist"
        Write-Host "Syncing frontend dist to embedded backend assets..."
        Copy-DirectoryContents -Source $FrontendDistDir -Destination $EmbeddedDistDir
    }
    catch {
        Write-Error $_
        exit 1
    }

    Write-Host "Frontend build complete!" -ForegroundColor Green
} else {
    Write-Host "`n[1/3] Skipping frontend build" -ForegroundColor Gray
}

# Step 2: Build Go Backend
Write-Host "`n[2/3] Building Go backend..." -ForegroundColor Yellow

$BackendDir = Join-Path $RootDir "backend"
Push-Location $BackendDir
try {
    # Verify dist directory exists
    $DistDir = Join-Path $BackendDir "internal\web\dist"
    if (-not (Test-Path $DistDir)) {
        Write-Error "Frontend dist not found. Run without -SkipFrontend first."
        exit 1
    }

    # Create output directory
    $OutputPath = Join-Path $RootDir $OutputDir
    if (-not (Test-Path $OutputPath)) {
        New-Item -ItemType Directory -Path $OutputPath | Out-Null
    }

    # Build for Windows
    $OutputFile = Join-Path $OutputPath "codeflow.exe"
    Write-Host "Building: $OutputFile"

    $env:CGO_ENABLED = "1"
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"

    go build -ldflags="-s -w" -o $OutputFile ./cmd/codeflow-server

    if ($LASTEXITCODE -ne 0) {
        Write-Error "Go build failed"
        exit 1
    }
}
finally {
    Pop-Location
}

Write-Host "Backend build complete!" -ForegroundColor Green

# Step 3: Summary
Write-Host "`n[3/3] Build Summary" -ForegroundColor Yellow
$FinalExe = Join-Path (Join-Path $RootDir $OutputDir) "codeflow.exe"
$Size = (Get-Item $FinalExe).Length / 1MB
Write-Host "Output: $FinalExe" -ForegroundColor Cyan
Write-Host "Size: $([math]::Round($Size, 2)) MB" -ForegroundColor Cyan

Write-Host "`n=== Build Complete ===" -ForegroundColor Green
Write-Host "Run with: $FinalExe"
Write-Host "Access at: http://localhost:8080"

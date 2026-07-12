# CodeFlow Tauri Development Script
# Usage: .\scripts\dev-tauri.ps1

$ErrorActionPreference = "Stop"

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  CodeFlow Tauri Dev Mode" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

$ProjectRoot = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$BackendDir = Join-Path $ProjectRoot "backend"
$FrontendDir = Join-Path $ProjectRoot "apps\workbench"
$BinariesDir = Join-Path $FrontendDir "src-tauri\binaries"

# Step 1: Build Go Backend (dev mode)
Write-Host "`n[1/2] Building Go Backend (dev)..." -ForegroundColor Yellow
Set-Location $BackendDir

$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "1"

$OutputPath = Join-Path $BinariesDir "codeflow-server-x86_64-pc-windows-msvc.exe"

go build -o $OutputPath ./cmd/codeflow-server
if ($LASTEXITCODE -ne 0) {
    Write-Host "  [ERROR] Go build failed!" -ForegroundColor Red
    exit 1
}
Write-Host "  [OK] Go backend built" -ForegroundColor Green

# Step 2: Ensure monorepo deps, then start Tauri dev mode
Write-Host "`n[2/2] Starting Tauri dev mode..." -ForegroundColor Yellow
if (Test-Path (Join-Path $ProjectRoot 'pnpm-workspace.yaml')) {
    if (-not (Test-Path (Join-Path $ProjectRoot 'node_modules'))) {
        Set-Location $ProjectRoot
        Write-Host "  Installing monorepo dependencies via pnpm..."
        pnpm install
        if ($LASTEXITCODE -ne 0) {
            Write-Host "  [ERROR] pnpm install failed!" -ForegroundColor Red
            exit 1
        }
    }
    Set-Location $ProjectRoot
    pnpm --filter @codeflow/workbench tauri:dev
}
else {
    Set-Location $FrontendDir
    if (-not (Test-Path (Join-Path $FrontendDir 'node_modules'))) {
        if (Test-Path (Join-Path $FrontendDir 'package-lock.json')) {
            npm ci
        }
        else {
            npm install
        }
        if ($LASTEXITCODE -ne 0) {
            Write-Host "  [ERROR] npm install failed!" -ForegroundColor Red
            exit 1
        }
    }
    npm run tauri dev
}

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

# Step 1: Build Go Backend
Write-Host "`n[1/3] Building Go Backend..." -ForegroundColor Yellow
Set-Location $BackendDir

$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "1"

$OutputPath = Join-Path $BinariesDir "codeflow-server-x86_64-pc-windows-msvc.exe"

Write-Host "  Target: $OutputPath"
go build -ldflags="-s -w" -o $OutputPath ./cmd/codeflow-server

if ($LASTEXITCODE -ne 0) {
    Write-Host "  [ERROR] Go build failed!" -ForegroundColor Red
    exit 1
}
Write-Host "  [OK] Go backend built successfully" -ForegroundColor Green

# Step 2: Install npm dependencies
Write-Host "`n[2/3] Installing npm dependencies..." -ForegroundColor Yellow
Set-Location $FrontendDir

npm install
if ($LASTEXITCODE -ne 0) {
    Write-Host "  [ERROR] npm install failed!" -ForegroundColor Red
    exit 1
}
Write-Host "  [OK] Dependencies installed" -ForegroundColor Green

# Step 3: Build Tauri Application
Write-Host "`n[3/3] Building Tauri Application..." -ForegroundColor Yellow

npm run tauri build
if ($LASTEXITCODE -ne 0) {
    Write-Host "  [ERROR] Tauri build failed!" -ForegroundColor Red
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

Set-Location $ProjectRoot

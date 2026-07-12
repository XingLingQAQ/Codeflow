# CodeFlow M0.3 embed smoke
# Verifies apps/workbench dist sync into backend/internal/web/dist for //go:embed.
# Optional: go build of internal/web or full server when Go is available.

param(
    [switch]$BuildFrontend,
    [switch]$SkipGoBuild,
    [string]$RootDir = ""
)

$ErrorActionPreference = "Stop"

function Resolve-GoExe {
    $candidates = @(
        (Get-Command go -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source),
        "C:\Program Files\Go\bin\go.exe",
        "$env:USERPROFILE\scoop\apps\go\current\bin\go.exe",
        "$env:USERPROFILE\go\bin\go.exe"
    ) | Where-Object { $_ -and (Test-Path $_) }
    return $candidates | Select-Object -First 1
}

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

if (-not $RootDir) {
    $ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
    $RootDir = Split-Path -Parent $ScriptDir
}

$FrontendDir = Join-Path $RootDir "apps\workbench"
$FrontendDistDir = Join-Path $FrontendDir "dist"
$EmbeddedDistDir = Join-Path $RootDir "backend\internal\web\dist"
$StaticGo = Join-Path $RootDir "backend\internal\web\static.go"

Write-Host "=== CodeFlow embed smoke (M0.3) ===" -ForegroundColor Cyan
Write-Host "Root: $RootDir"

if (-not (Test-Path $FrontendDir)) {
    throw "Frontend not found: $FrontendDir"
}
if (-not (Test-Path $StaticGo)) {
    throw "Embed entry missing: $StaticGo (expected static.go with //go:embed all:dist)"
}

$staticContent = Get-Content $StaticGo -Raw
if ($staticContent -notmatch '//go:embed\s+all:dist') {
    throw "static.go does not contain //go:embed all:dist"
}
Write-Host "[ok] static.go embed directive present"

if ($BuildFrontend) {
    Write-Host "[1] Building frontend (@codeflow/workbench)..." -ForegroundColor Yellow
    Push-Location $RootDir
    try {
        pnpm --filter @codeflow/workbench build
        if ($LASTEXITCODE -ne 0) { throw "Frontend build failed" }
    }
    finally {
        Pop-Location
    }
}
else {
    Write-Host "[1] Using existing frontend dist (pass -BuildFrontend to rebuild)" -ForegroundColor Gray
}

if (-not (Test-Path $FrontendDistDir)) {
    throw "Frontend dist missing: $FrontendDistDir (run with -BuildFrontend or pnpm build:workbench)"
}

$srcIndex = Join-Path $FrontendDistDir "index.html"
if (-not (Test-Path $srcIndex)) {
    throw "Frontend dist missing index.html: $srcIndex"
}

Write-Host "[2] Syncing $FrontendDistDir -> $EmbeddedDistDir" -ForegroundColor Yellow
Copy-DirectoryContents -Source $FrontendDistDir -Destination $EmbeddedDistDir

$dstIndex = Join-Path $EmbeddedDistDir "index.html"
if (-not (Test-Path $dstIndex)) {
    throw "Embed dist missing index.html after sync"
}

$indexHtml = Get-Content $dstIndex -Raw
if ($indexHtml -notmatch '<div\s+id=["'']root["'']|id=["'']root["'']') {
    # tolerate either root div or generic script entry
    if ($indexHtml -notmatch '<script') {
        throw "index.html does not look like a Vite SPA entry"
    }
}

$assetFiles = Get-ChildItem (Join-Path $EmbeddedDistDir "assets") -File -ErrorAction SilentlyContinue
if (-not $assetFiles -or $assetFiles.Count -lt 1) {
    throw "Embed dist has no assets/ files"
}

# Spot-check relative references from index.html exist when absolute-ish /assets/ paths used
$assetRefs = [regex]::Matches($indexHtml, '/assets/[^"''\s>]+') | ForEach-Object { $_.Value }
foreach ($ref in $assetRefs) {
    $rel = $ref.TrimStart('/')
    $path = Join-Path $EmbeddedDistDir ($rel -replace '/', [IO.Path]::DirectorySeparatorChar)
    if (-not (Test-Path $path)) {
        throw "index.html references missing file: $ref"
    }
}

Write-Host "[ok] dist synced: index.html + $($assetFiles.Count) asset file(s)"

$goExe = Resolve-GoExe
if ($SkipGoBuild) {
    Write-Host "[3] Skipping Go build (-SkipGoBuild)" -ForegroundColor Gray
}
elseif (-not $goExe) {
    Write-Host "[3] Go not found on PATH; path/sync checks only (install Go for compile smoke)" -ForegroundColor Yellow
}
else {
    Write-Host "[3] Go build package ./internal/web (embed compile smoke)" -ForegroundColor Yellow
    $backendDir = Join-Path $RootDir "backend"
    Push-Location $backendDir
    try {
        & $goExe build -o NUL ./internal/web
        if ($LASTEXITCODE -ne 0) { throw "go build ./internal/web failed" }
        Write-Host "[ok] go build ./internal/web succeeded via $goExe"
    }
    finally {
        Pop-Location
    }
}

Write-Host ""
Write-Host "=== M0.3 embed smoke PASSED ===" -ForegroundColor Green
Write-Host "Embed path: $EmbeddedDistDir"
Write-Host "Entry: $StaticGo"
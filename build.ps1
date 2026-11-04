# Gofoom Build & Packaging Script
# Synchronizes version metadata, generates Windows PE resource, and builds binaries.

param(
    [switch]$Dev,
    [switch]$SkipGenerate
)

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
Set-Location $ScriptDir

# 1. Generate versioninfo.json and resource.syso from pkg/version/version.go
if (-not $SkipGenerate) {
    Write-Host "[Build] Synchronizing version metadata from pkg/version/version.go..." -ForegroundColor Cyan
    go generate ./...
    if ($LASTEXITCODE -ne 0) {
        Write-Error "go generate failed with exit code $LASTEXITCODE"
        exit $LASTEXITCODE
    }
}

# 2. Ensure bin directory exists
if (-not (Test-Path "bin")) {
    New-Item -ItemType Directory -Path "bin" | Out-Null
}

# 3. Build executable
if ($Dev) {
    $outPath = "bin/gofoom_dev.exe"
    Write-Host "[Build] Compiling Gofoom (Development mode with console) -> $outPath..." -ForegroundColor Green
    go build -tags dev -o $outPath ./cmd/gofoom
} else {
    $outPath = "bin/gofoom.exe"
    Write-Host "[Build] Compiling Gofoom (Production GUI mode) -> $outPath..." -ForegroundColor Green
    go build -ldflags "-s -w -H=windowsgui" -o $outPath ./cmd/gofoom
}

if ($LASTEXITCODE -ne 0) {
    Write-Error "go build failed with exit code $LASTEXITCODE"
    exit $LASTEXITCODE
}

# 4. Verify embedded PE version information
$info = (Get-Item $outPath).VersionInfo
Write-Host ""
Write-Host "================ Build Summary ================" -ForegroundColor Cyan
Write-Host " Executable     : $outPath"
Write-Host " Product Name   : $($info.ProductName)"
Write-Host " Product Version: $($info.ProductVersion)"
Write-Host " File Version   : $($info.FileVersion)"
Write-Host " Description    : $($info.FileDescription)"
Write-Host " Copyright      : $($info.LegalCopyright)"
Write-Host " File Size      : $([math]::Round((Get-Item $outPath).Length / 1MB, 2)) MB"
Write-Host "===============================================" -ForegroundColor Cyan
Write-Host "[Build] Done!" -ForegroundColor Green

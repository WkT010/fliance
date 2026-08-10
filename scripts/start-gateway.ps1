# Start the Fliance（梵响） api-gateway as a fully detached background process (no console window).
# Usage: powershell -ExecutionPolicy Bypass -File scripts/start-gateway.ps1
# Stop:   powershell -ExecutionPolicy Bypass -File scripts/stop-gateway.ps1

$ErrorActionPreference = "Stop"

$ProjectRoot = Split-Path -Parent $PSScriptRoot
$ExePath = Join-Path $ProjectRoot ".tools\api-gateway.exe"
$LogDir  = Join-Path $ProjectRoot ".tools\logs"
$Stdout  = Join-Path $LogDir "gateway.log"
$Stderr  = Join-Path $LogDir "gateway.err"

if (-not (Test-Path $ExePath)) {
    Write-Error "api-gateway.exe not found at $ExePath. Build it first: go build -o .tools/api-gateway.exe ./cmd/api-gateway"
}
if (-not (Test-Path $LogDir)) { New-Item -ItemType Directory -Path $LogDir -Force | Out-Null }

# Stop any prior instance bound to :8080
$existing = Get-NetTCPConnection -LocalPort 8080 -State Listen -ErrorAction SilentlyContinue
foreach ($c in $existing) {
    try { Stop-Process -Id $c.OwningProcess -Force -ErrorAction SilentlyContinue } catch {}
}
$proc = Get-Process -Name "api-gateway" -ErrorAction SilentlyContinue
foreach ($p in $proc) { try { Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue } catch {} }
Start-Sleep -Milliseconds 400

# Launch detached with hidden window, log to files.
$p = Start-Process -FilePath $ExePath `
    -WorkingDirectory $ProjectRoot `
    -WindowStyle Hidden `
    -RedirectStandardOutput $Stdout `
    -RedirectStandardError  $Stderr `
    -PassThru

Write-Host "api-gateway started (PID=$($p.Id), hidden window, logs -> $LogDir)"

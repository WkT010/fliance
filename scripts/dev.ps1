# Start/stop dev environment (api-gateway hidden + vite dev server).
# Usage:
#   scripts/dev.ps1 start   - start api-gateway (hidden) + vite dev server
#   scripts/dev.ps1 stop    - stop both
#   scripts/dev.ps1 status  - show running state
#   scripts/dev.ps1 logs    - tail the api-gateway logs

$ErrorActionPreference = "Stop"
$Action = if ($args.Count -gt 0) { $args[0] } else { "status" }

$ProjectRoot = Split-Path -Parent $PSScriptRoot
$ExePath    = Join-Path $ProjectRoot ".tools\api-gateway.exe"
$Frontend   = Join-Path $ProjectRoot "frontend"
$LogDir     = Join-Path $ProjectRoot ".tools\logs"
$ViteLog    = Join-Path $LogDir "vite.log"

function Start-Gateway {
    if (-not (Test-Path $ExePath)) { Write-Error "Build first: go build -o .tools/api-gateway.exe ./cmd/api-gateway" }
    if (-not (Test-Path $LogDir)) { New-Item -ItemType Directory -Path $LogDir -Force | Out-Null }
    powershell -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot "start-gateway.ps1") | Write-Host
}

function Start-Vite {
    if (-not (Test-Path $LogDir)) { New-Item -ItemType Directory -Path $LogDir -Force | Out-Null }
    $existing = Get-NetTCPConnection -LocalPort 3000,3001 -State Listen -ErrorAction SilentlyContinue
    if ($existing) { Write-Host "vite already running on port 3000/3001"; return }
    Set-Location $Frontend
    $p = Start-Process -FilePath "npx.cmd" `
        -ArgumentList "vite","--port","3001","--host","0.0.0.0" `
        -WorkingDirectory $Frontend `
        -WindowStyle Hidden `
        -RedirectStandardOutput $ViteLog `
        -RedirectStandardError  $ViteLog `
        -PassThru
    Write-Host "vite started (PID=$($p.Id), logs -> $ViteLog)"
}

function Stop-Vite {
    $p = Get-Process -Name "node" -ErrorAction SilentlyContinue | Where-Object { $_.MainModule.FileName -like "*\node.exe" -and $_.StartTime }
    $vite = Get-CimInstance Win32_Process -Filter "Name='node.exe'" -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandLine -like "*vite*" }
    foreach ($v in $vite) { try { Stop-Process -Id $v.ProcessId -Force -ErrorAction SilentlyContinue } catch {} }
    Write-Host "vite stopped"
}

function Stop-Gateway {
    powershell -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot "stop-gateway.ps1") | Write-Host
}

switch ($Action) {
    "start"   { Start-Gateway; Start-Vite }
    "stop"    { Stop-Vite; Stop-Gateway }
    "status"  {
        $g = Get-Process -Name "api-gateway" -ErrorAction SilentlyContinue
        $v = Get-NetTCPConnection -LocalPort 3001 -State Listen -ErrorAction SilentlyContinue
        Write-Host "api-gateway: $(if ($g) {'running (PID=' + $g.Id + ')'} else {'stopped'})"
        Write-Host "vite:        $(if ($v) {'running on :' + ($v | Select-Object -First 1).LocalPort} else {'stopped'})"
    }
    "logs"    {
        Get-Content (Join-Path $LogDir "gateway.err") -Wait -Tail 30
    }
    default   { Write-Host "Usage: dev.ps1 {start|stop|status|logs}" }
}

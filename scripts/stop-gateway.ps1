# Stop the Fliance（梵响） api-gateway background process.
$ErrorActionPreference = "Stop"
$proc = Get-Process -Name "api-gateway" -ErrorAction SilentlyContinue
if ($null -eq $proc) {
    Write-Host "api-gateway is not running"
    exit 0
}
foreach ($p in $proc) {
    try { Stop-Process -Id $p.Id -Force } catch {}
}
Write-Host "api-gateway stopped (was PID=$($proc.Id))"

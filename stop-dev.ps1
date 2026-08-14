# ============================================================================
# Fliance（梵响） — Windows 开发环境一键停止脚本
# ----------------------------------------------------------------------------
# 用法（在项目根目录，PowerShell 5.1）：
#   powershell -ExecutionPolicy Bypass -File stop-dev.ps1
#
# 按监听端口结束开发进程：50051/8081（matching-engine）、8082（wallet-service）、
# 8080（api-gateway）、3000（前端 vite）。
# 幂等：无进程运行时仅报告端口空闲，可重复执行。
# 重新启动： powershell -ExecutionPolicy Bypass -File start-dev.ps1
# ============================================================================
$ErrorActionPreference = 'Continue'

$ports = @(50051, 8081, 8082, 8080, 3000)
$killed = @()

foreach ($port in $ports) {
    $conns = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
    if (-not $conns) {
        Write-Host ("[stop] :{0} 空闲" -f $port)
        continue
    }
    foreach ($c in $conns) {
        if ($killed -contains $c.OwningProcess) { continue }
        $pr = Get-Process -Id $c.OwningProcess -ErrorAction SilentlyContinue
        $name = if ($pr) { $pr.ProcessName } else { '?' }
        try {
            Stop-Process -Id $c.OwningProcess -Force -ErrorAction Stop
            $killed += $c.OwningProcess
            Write-Host ("[stop] :{0} <- 已结束 pid={1} ({2})" -f $port, $c.OwningProcess, $name)
        } catch {
            Write-Host ("[stop] :{0} 结束 pid={1} ({2}) 失败：{3}" -f $port, $c.OwningProcess, $name, $_.Exception.Message) -ForegroundColor Yellow
        }
    }
}

# 兜底：按进程名清理（端口已释放但进程仍残留的情况）
foreach ($procName in @('matching-engine', 'wallet-service', 'api-gateway')) {
    foreach ($p in (Get-Process -Name $procName -ErrorAction SilentlyContinue)) {
        try {
            Stop-Process -Id $p.Id -Force -ErrorAction Stop
            Write-Host ("[stop] 残留进程 {0} pid={1} 已结束" -f $procName, $p.Id)
        } catch { }
    }
}

if ($killed.Count -gt 0) { Start-Sleep -Seconds 2 }

# 验证端口全部释放
Write-Host ""
$allFree = $true
foreach ($port in $ports) {
    $still = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
    if ($still) {
        Write-Host ("[verify] :{0} 仍被占用（pid={1}）" -f $port, ($still | Select-Object -First 1).OwningProcess) -ForegroundColor Yellow
        $allFree = $false
    } else {
        Write-Host ("[verify] :{0} 已释放" -f $port)
    }
}

if ($allFree) {
    Write-Host "[stop] 全部开发进程已停止 [OK]" -ForegroundColor Green
} else {
    Write-Host "[stop] 仍有端口被占用，请查看上方告警（可能是其他程序占用同名端口）。" -ForegroundColor Yellow
}

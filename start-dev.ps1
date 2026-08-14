# ============================================================================
# Fliance（梵响） — Windows 开发环境一键启动脚本
# ----------------------------------------------------------------------------
# 用法（在项目根目录，PowerShell 5.1）：
#   powershell -ExecutionPolicy Bypass -File start-dev.ps1
#
# 启动顺序与端口：
#   1. matching-engine  gRPC :50051 / 监控 :8081（WAL: data\engine-wal）
#   2. wallet-service   HTTP :8082（WAL: data\wal）
#   3. api-gateway      HTTP :8080
#   4. frontend vite    HTTP :3000（frontend/ 下 npm run dev）
#
# 幂等：启动前先按端口清理旧进程，可重复执行实现一键重启。
# 前置：二进制位于 .tools\build\；缺失时请先运行 install.ps1。
# 日志：.tools\logs\<服务名>.out.log / .err.log
# 停止：powershell -ExecutionPolicy Bypass -File stop-dev.ps1
# ============================================================================
$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
Set-Location $root

$buildDir = Join-Path $root '.tools\build'
$logDir   = Join-Path $root '.tools\logs'
New-Item -ItemType Directory -Force -Path $logDir | Out-Null

function Fail([string]$msg) {
    Write-Host "[start] ERROR: $msg" -ForegroundColor Red
    exit 1
}

function Stop-PortListeners([int[]]$Ports) {
    $killed = @()
    foreach ($port in $Ports) {
        $conns = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
        foreach ($c in $conns) {
            if ($killed -contains $c.OwningProcess) { continue }
            $pr = Get-Process -Id $c.OwningProcess -ErrorAction SilentlyContinue
            try {
                Stop-Process -Id $c.OwningProcess -Force -ErrorAction Stop
                $killed += $c.OwningProcess
                Write-Host ("[clean] :{0} <- 结束旧进程 pid={1} ({2})" -f $port, $c.OwningProcess, $pr.ProcessName)
            } catch {
                Write-Host ("[clean] :{0} 无法结束 pid={1}：{2}" -f $port, $c.OwningProcess, $_.Exception.Message) -ForegroundColor Yellow
            }
        }
    }
    if ($killed.Count -gt 0) { Start-Sleep -Seconds 2 }
}

function Test-TcpPort([int]$Port) {
    try {
        $c = New-Object Net.Sockets.TcpClient
        $iar = $c.BeginConnect('127.0.0.1', $Port, $null, $null)
        $ok = $iar.AsyncWaitHandle.WaitOne(1500) -and $c.Connected
        $c.Close()
        return $ok
    } catch { return $false }
}

# ── 0. 幂等清理：按端口结束旧实例 ───────────────────────────────────────
Write-Host "[start] 清理端口 50051/8081/8082/8080/3000 上的旧进程 ..."
Stop-PortListeners @(50051, 8081, 8082, 8080, 3000)

# ── 1. 载入 .env ───────────────────────────────────────────────────────
$envFile = Join-Path $root '.env'
if (-not (Test-Path $envFile)) { Fail ".env 不存在，请先运行 install.ps1（会从 .env.example 生成）。" }
foreach ($line in (Get-Content $envFile)) {
    $t = $line.Trim()
    if ($t -eq '' -or $t.StartsWith('#')) { continue }
    $idx = $t.IndexOf('=')
    if ($idx -le 0) { continue }
    [Environment]::SetEnvironmentVariable($t.Substring(0, $idx).Trim(), $t.Substring($idx + 1).Trim(), 'Process')
}
Write-Host "[env] 已载入 .env（ENVIRONMENT=$env:ENVIRONMENT）"

# ── 2. 二进制检查 ──────────────────────────────────────────────────────
foreach ($svc in @('matching-engine', 'wallet-service', 'api-gateway')) {
    if (-not (Test-Path (Join-Path $buildDir "$svc.exe"))) {
        Fail "缺少 $buildDir\$svc.exe，请先运行 install.ps1 构建三服务。"
    }
}

function Start-Service {
    param([string]$Name, [string]$Exe)
    $outLog = Join-Path $logDir "$Name.out.log"
    $errLog = Join-Path $logDir "$Name.err.log"
    $p = Start-Process -FilePath $Exe -WorkingDirectory $root `
        -RedirectStandardOutput $outLog -RedirectStandardError $errLog `
        -WindowStyle Hidden -PassThru
    Write-Host ("[start] {0} pid={1} logs={2}.out.log/.err.log" -f $Name, $p.Id, $Name)
    return $p
}

# ── 3. matching-engine（gRPC :50051, 监控 :8081） ─────────────────────
# 独立引擎使用专属 WAL/快照目录，避免与 gateway 内嵌引擎的 ./data 冲突。
$env:WAL_DIR = '.\data\engine-wal'
$env:SNAPSHOT_DIR = '.\data\engine-snapshots'
$engine = Start-Service 'matching-engine' (Join-Path $buildDir 'matching-engine.exe')
Start-Sleep -Seconds 3
if ($engine.HasExited) { Fail "matching-engine 启动后立即退出（code=$($engine.ExitCode)），查看 $logDir\matching-engine.err.log" }

# ── 4. wallet-service（HTTP :8082；.env 的 LISTEN_ADDR=:8080 留给 gateway） ──
$env:WAL_DIR = '.\data\wal'
$env:SNAPSHOT_DIR = '.\data\snapshots'
$env:LISTEN_ADDR = ':8082'
$wallet = Start-Service 'wallet-service' (Join-Path $buildDir 'wallet-service.exe')
Start-Sleep -Seconds 2
if ($wallet.HasExited) { Fail "wallet-service 启动后立即退出（code=$($wallet.ExitCode)），查看 $logDir\wallet-service.err.log" }

# ── 5. api-gateway（HTTP :8080） ───────────────────────────────────────
$env:LISTEN_ADDR = ':8080'
$gateway = Start-Service 'api-gateway' (Join-Path $buildDir 'api-gateway.exe')
Start-Sleep -Seconds 4
if ($gateway.HasExited) { Fail "api-gateway 启动后立即退出（code=$($gateway.ExitCode)），查看 $logDir\api-gateway.err.log" }

# ── 6. frontend vite（:3000） ──────────────────────────────────────────
$feOut = Join-Path $logDir 'frontend.out.log'
$feErr = Join-Path $logDir 'frontend.err.log'
$fe = Start-Process -FilePath 'cmd.exe' -ArgumentList '/c', 'npm run dev' `
    -WorkingDirectory (Join-Path $root 'frontend') `
    -RedirectStandardOutput $feOut -RedirectStandardError $feErr `
    -WindowStyle Hidden -PassThru
Write-Host "[start] frontend pid=$($fe.Id) logs=frontend.out.log/frontend.err.log"

# ── 7. 健康检查 ────────────────────────────────────────────────────────
Write-Host ""
Write-Host "[health] 等待服务就绪 ..."
Start-Sleep -Seconds 5

function Check-Http([string]$Name, [string]$Url) {
    try {
        $code = (curl.exe -s -o NUL -w '%{http_code}' --max-time 5 $Url)
        if ($code -eq '200') { Write-Host ("  [OK]   {0} {1} -> 200" -f $Name, $Url) -ForegroundColor Green; return $true }
        Write-Host ("  [WARN] {0} {1} -> {2}" -f $Name, $Url, $code) -ForegroundColor Yellow; return $false
    } catch {
        Write-Host ("  [WARN] {0} {1} 不可达" -f $Name, $Url) -ForegroundColor Yellow; return $false
    }
}

$ok = $true
if (-not (Test-TcpPort 50051)) { Write-Host '  [WARN] matching-engine gRPC :50051 未监听' -ForegroundColor Yellow; $ok = $false }
else { Write-Host '  [OK]   matching-engine gRPC :50051 监听中' -ForegroundColor Green }
if (-not (Check-Http 'api-gateway    ' 'http://localhost:8080/health')) { $ok = $false }
if (-not (Check-Http 'wallet-service ' 'http://localhost:8082/health')) { $ok = $false }
if (-not (Test-TcpPort 3000)) { Write-Host '  [WARN] frontend :3000 未就绪（vite 冷启动较慢，可稍等几秒）' -ForegroundColor Yellow; $ok = $false }
else { Write-Host '  [OK]   frontend :3000 监听中' -ForegroundColor Green }

Write-Output ''
Write-Output "PIDS: engine=$($engine.Id) wallet=$($wallet.Id) gateway=$($gateway.Id) frontend=$($fe.Id)"
$engine.Id, $wallet.Id, $gateway.Id, $fe.Id | Set-Content (Join-Path $logDir 'pids.txt')
if ($ok) {
    Write-Host "[start] 全部四服务已启动并通过健康检查 ✓" -ForegroundColor Green
} else {
    Write-Host "[start] 服务已启动，但部分健康检查未通过，请查看 $logDir 下日志。" -ForegroundColor Yellow
}

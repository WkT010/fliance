# ============================================================================
# Fliance（梵响） — Windows 一键安装脚本
# ----------------------------------------------------------------------------
# 用法（在项目根目录，PowerShell 5.1）：
#   powershell -ExecutionPolicy Bypass -File install.ps1
#
# 步骤：
#   1. 检测 go / node / npm（不自动安装；缺失时给出清晰指引后退出）
#      - go 优先取 PATH 中的 go，其次回退到 .tools\go\bin\go.exe（GOTOOLCHAIN=auto）
#   2. 若 .env 不存在，从 .env.example 复制生成（已存在则不动）
#   3. npm install（frontend/）
#   4. go mod download
#   5. 构建三服务到 .tools\build\（matching-engine / wallet-service / api-gateway）
#   6. 应用数据库迁移（go run ./scripts/migrate，读取 .env 的 POSTGRES_DSN）
#
# 可选开关（跳过耗时步骤，便于重复执行/增量安装）：
#   -SkipNpm      跳过 npm install
#   -SkipBuild    跳过三服务构建
#   -SkipMigrate  跳过数据库迁移
#
# 说明（新环境推荐路径）：
#   - 数据库统一使用 Supabase 托管 PostgreSQL（.env 的 POSTGRES_DSN，
#     Session pooler 5432 端口 + sslmode=require），无需 Docker。
#   - Redis 可选，默认 ENABLE_REDIS_RATE_LIMIT=false 未启用，无需安装。
#   - 安装完成后直接： powershell -ExecutionPolicy Bypass -File start-dev.ps1
# ============================================================================
param(
    [switch]$SkipNpm,
    [switch]$SkipBuild,
    [switch]$SkipMigrate
)

$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
Set-Location $root

function Fail([string]$msg) {
    Write-Host ""
    Write-Host "[install] ERROR: $msg" -ForegroundColor Red
    exit 1
}
function Step([string]$msg) {
    Write-Host ""
    Write-Host "[install] $msg" -ForegroundColor Cyan
}

# ── 1. 依赖检测（不自动安装，缺则报错指引） ────────────────────────────
Step "1/6 检测依赖 go / node / npm ..."
$env:GOTOOLCHAIN = 'auto'
$go = (Get-Command go -ErrorAction SilentlyContinue).Source
if (-not $go -and (Test-Path (Join-Path $root '.tools\go\bin\go.exe'))) {
    $go = Join-Path $root '.tools\go\bin\go.exe'
}
if (-not $go) {
    Fail "未检测到 go。请先安装 Go 1.25+（https://go.dev/dl/ 或 choco install golang），或将便携工具链放到 .tools\go\ 后重试。"
}
Write-Host ("  go   -> {0} ({1})" -f $go, (& $go version))

$node = (Get-Command node -ErrorAction SilentlyContinue).Source
$npm  = (Get-Command npm.cmd -ErrorAction SilentlyContinue).Source
if (-not $npm) { $npm = (Get-Command npm -ErrorAction SilentlyContinue).Source }
if (-not $node -or -not $npm) {
    Fail "未检测到 node/npm。请先安装 Node.js 18+（https://nodejs.org/ 或 choco install nodejs）后重试。"
}
Write-Host ("  node -> {0} ({1})" -f $node, (& node -v))

# ── 2. .env（不存在才从 .env.example 复制） ────────────────────────────
Step "2/6 检查 .env ..."
$envFile = Join-Path $root '.env'
if (-not (Test-Path $envFile)) {
    Copy-Item (Join-Path $root '.env.example') $envFile
    Write-Host "  已从 .env.example 生成 .env，请按需修改 POSTGRES_DSN / JWT_SECRET 等。"
} else {
    Write-Host "  .env 已存在，保持不变。"
}

# ── 3. npm install ─────────────────────────────────────────────────────
Step "3/6 npm install（frontend/）..."
if ($SkipNpm) {
    Write-Host "  已跳过（-SkipNpm）"
} else {
    Push-Location (Join-Path $root 'frontend')
    try {
        & $npm install
        if ($LASTEXITCODE -ne 0) { Fail "npm install 失败（exit=$LASTEXITCODE）" }
    } finally { Pop-Location }
    Write-Host "  npm install 完成"
}

# ── 4. go mod download ─────────────────────────────────────────────────
Step "4/6 go mod download ..."
& $go mod download
if ($LASTEXITCODE -ne 0) { Fail "go mod download 失败（exit=$LASTEXITCODE）" }
Write-Host "  依赖下载完成"

# ── 5. 构建三服务 → .tools\build\（start-dev.ps1 使用该目录） ──────────
Step "5/6 构建三服务（matching-engine / wallet-service / api-gateway）..."
if ($SkipBuild) {
    Write-Host "  已跳过（-SkipBuild）"
} else {
    $env:CGO_ENABLED = '0'
    $buildDir = Join-Path $root '.tools\build'
    New-Item -ItemType Directory -Force -Path $buildDir | Out-Null
    foreach ($svc in @('matching-engine', 'wallet-service', 'api-gateway')) {
        Write-Host ("  building {0} ..." -f $svc)
        & $go build -ldflags '-s -w' -o (Join-Path $buildDir "$svc.exe") ".\cmd\$svc"
        if ($LASTEXITCODE -ne 0) { Fail "构建 $svc 失败（exit=$LASTEXITCODE）" }
    }
    Write-Host "  三服务构建完成 -> .tools\build\"
}

# ── 6. 数据库迁移（读取 .env 的 POSTGRES_DSN） ────────────────────────
Step "6/6 应用数据库迁移（scripts/migrate）..."
if ($SkipMigrate) {
    Write-Host "  已跳过（-SkipMigrate）"
} else {
    # 将 .env 载入当前进程，保证 POSTGRES_DSN 生效
    foreach ($line in (Get-Content $envFile)) {
        $t = $line.Trim()
        if ($t -eq '' -or $t.StartsWith('#')) { continue }
        $idx = $t.IndexOf('=')
        if ($idx -le 0) { continue }
        [Environment]::SetEnvironmentVariable($t.Substring(0, $idx).Trim(), $t.Substring($idx + 1).Trim(), 'Process')
    }
    & $go run ./scripts/migrate
    if ($LASTEXITCODE -ne 0) {
        Fail "数据库迁移失败。请检查 .env 的 POSTGRES_DSN 是否为 Supabase Session pooler（5432 端口）连接串且保留 sslmode=require，网络可达后重试。"
    }
    Write-Host "  迁移完成（已应用的版本会自动跳过）"
}

Write-Host ""
Write-Host "[install] 安装完成。启动开发环境： powershell -ExecutionPolicy Bypass -File start-dev.ps1" -ForegroundColor Green

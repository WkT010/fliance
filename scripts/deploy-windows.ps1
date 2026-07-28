#Requires -RunAsAdministrator
# Nexa Exchange v4.0.402 - Windows one-click deploy script
# Run in PowerShell as Administrator

$ErrorActionPreference = "Stop"

$Repo      = "https://github.com/WkT010/nexa-exchange.git"
$Version   = "v4.0.402"
$AppDir    = "C:\nexa-exchange"
$Service   = "NexaExchange"
$DbName    = "nexa"
$DbUser    = "nexa"
$DbPass    = "nexa_" + (-join ((1..24) | ForEach-Object { '{0:x}' -f (Get-Random -Max 16) }))
$JwtSecret = -join ((1..64) | ForEach-Object { '{0:x}' -f (Get-Random -Max 16) })

function Refresh-Path {
    $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
}

Write-Host "[NEXA] Deploying Nexa Exchange $Version on Windows..." -ForegroundColor Cyan

# 1. Install Chocolatey
if (-not (Get-Command choco -ErrorAction SilentlyContinue)) {
    Write-Host "[NEXA] Installing Chocolatey..."
    Set-ExecutionPolicy Bypass -Scope Process -Force
    [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.ServicePointManager]::SecurityProtocol -bor 3072
    Invoke-Expression ((New-Object System.Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))
    Refresh-Path
}

# 2. Install dependencies
Write-Host "[NEXA] Installing Go, Node.js, PostgreSQL, Git, nssm..."
choco install -y git golang nodejs postgresql16 nssm
Refresh-Path

# 3. Clone or update source
if (Test-Path $AppDir) {
    Write-Host "[NEXA] Updating existing source..."
    Set-Location $AppDir
    git fetch --tags
    git checkout $Version
    git pull origin $Version
} else {
    Write-Host "[NEXA] Cloning repository..."
    git clone --depth 1 --branch $Version $Repo $AppDir
    Set-Location $AppDir
}

# 4. Build frontend
Write-Host "[NEXA] Building frontend..."
Set-Location "$AppDir\frontend"
npm install
npm run build

# 5. Build backend
Write-Host "[NEXA] Building backend..."
Set-Location $AppDir
go build -o nexa-api.exe ./cmd/api-gateway

# 6. Setup PostgreSQL
$pgBin = "C:\Program Files\PostgreSQL\16\bin"
if (-not (Test-Path $pgBin)) {
    # Try to locate pg_ctl
    $pgCtl = Get-ChildItem -Path "C:\Program Files\PostgreSQL" -Recurse -Filter pg_ctl.exe -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($pgCtl) { $pgBin = $pgCtl.DirectoryName }
}
$env:Path += ";$pgBin"

Write-Host "[NEXA] Ensuring PostgreSQL is running..."
& "$pgBin\pg_ctl.exe" -D "C:\Program Files\PostgreSQL\16\data" start -w 2>$null

Write-Host "[NEXA] Creating database and user..."
& "$pgBin\psql.exe" -U postgres -c "CREATE USER $DbUser WITH PASSWORD '$DbPass';" 2>$null
& "$pgBin\psql.exe" -U postgres -c "CREATE DATABASE $DbName OWNER $DbUser;" 2>$null

# 7. Environment file
$EnvContent = @"
JWT_SECRET=$JwtSecret
JWT_ISSUER=nexa-exchange
LISTEN_ADDR=:8080
POSTGRES_DSN=postgres://$DbUser`:$DbPass@localhost:5432/$DbName?sslmode=disable
ENVIRONMENT=production
CORS_ALLOW_ORIGINS=*
ALCHEMY_API_KEY=your_alchemy_key_here
STATIC_DIR=$AppDir\frontend\dist
"@
$EnvContent | Set-Content "$AppDir\.env" -Encoding UTF8
Write-Host "[NEXA] Environment written to $AppDir\.env" -ForegroundColor Yellow
Write-Host "[NEXA] IMPORTANT: edit $AppDir\.env and replace 'your_alchemy_key_here' with a real Alchemy API key." -ForegroundColor Yellow

# 8. Install / update Windows service via nssm
Write-Host "[NEXA] Registering Windows service..."
nssm stop $Service 2>$null
nssm remove $Service confirm 2>$null
nssm install $Service "$AppDir\nexa-api.exe"
nssm set $Service AppDirectory $AppDir
nssm set $Service AppEnvironmentExtra "JWT_SECRET=$JwtSecret" "JWT_ISSUER=nexa-exchange" "LISTEN_ADDR=:8080" "POSTGRES_DSN=postgres://$DbUser`:$DbPass@localhost:5432/$DbName?sslmode=disable" "ENVIRONMENT=production" "CORS_ALLOW_ORIGINS=*" "STATIC_DIR=$AppDir\frontend\dist"
nssm set $Service Start SERVICE_AUTO_START
nssm set $Service DisplayName "Nexa Exchange"
nssm set $Service Description "Nexa Exchange API gateway and web frontend"
nssm start $Service

$ip = (Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.IPAddress -notlike '127.*' -and $_.IPAddress -notlike '169.254.*' } | Select-Object -First 1).IPAddress
if (-not $ip) { $ip = "localhost" }

Write-Host ""
Write-Host "[NEXA] Deployment complete!" -ForegroundColor Green
Write-Host "[NEXA] Open http://${ip}:8080 in your browser." -ForegroundColor Green
Write-Host "[NEXA] Service: nssm status $Service / Start-Service $Service" -ForegroundColor Green
Write-Host "[NEXA] Logs:   $AppDir\nexa.log" -ForegroundColor Green

#Requires -RunAsAdministrator
# Nexa Exchange v4.0.402 - Windows one-click deploy script
# Run in PowerShell as Administrator. If the window closes, use deploy-windows.cmd instead.

param(
    [string]$AppDir = "C:\nexa-exchange",
    [string]$Version = "v4.0.402"
)

$ErrorActionPreference = "Stop"
$Repo = "https://github.com/WkT010/nexa-exchange.git"
$Service = "NexaExchange"
$LogFile = "$AppDir\deploy.log"

function Write-Log($msg) {
    $line = "[$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')] $msg"
    Write-Host $line
    if ($AppDir -and (Test-Path $AppDir)) {
        $line | Out-File -Append -FilePath $LogFile -Encoding UTF8
    }
}

try {
    New-Item -ItemType Directory -Force -Path $AppDir | Out-Null
    "" | Set-Content $LogFile -Encoding UTF8
} catch { }

try {
    Write-Log "[NEXA] Deploying Nexa Exchange $Version on Windows..."
    Write-Log "[NEXA] Log file: $LogFile"

    # --- 1. Chocolatey ---
    if (-not (Get-Command choco -ErrorAction SilentlyContinue)) {
        Write-Log "[NEXA] Installing Chocolatey..."
        Set-ExecutionPolicy Bypass -Scope Process -Force
        [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.ServicePointManager]::SecurityProtocol -bor 3072
        Invoke-Expression ((New-Object System.Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))
        refreshenv
    } else {
        Write-Log "[NEXA] Chocolatey already installed."
    }

    # --- 2. Install dependencies ---
    Write-Log "[NEXA] Installing Go, Node.js, PostgreSQL, Git, nssm..."
    choco install -y git golang nodejs postgresql16 nssm
    refreshenv

    # --- 3. Locate tools ---
    $go = (Get-Command go -ErrorAction SilentlyContinue).Source
    if (-not $go) { $go = "C:\Program Files\Go\bin\go.exe" }
    $npm = (Get-Command npm -ErrorAction SilentlyContinue).Source
    if (-not $npm) { $npm = "C:\Program Files\nodejs\npm.cmd" }

    if (-not (Test-Path $go)) { throw "Go not found after installation. Please restart PowerShell and rerun." }
    if (-not (Test-Path $npm)) { throw "npm not found after installation. Please restart PowerShell and rerun." }

    Write-Log "[NEXA] Go: $go"
    Write-Log "[NEXA] npm: $npm"

    # --- 4. Clone or update source ---
    if (Test-Path "$AppDir\.git") {
        Write-Log "[NEXA] Updating existing source..."
        Set-Location $AppDir
        git fetch --all --tags
        git checkout $Version
        git pull origin $Version
    } else {
        Write-Log "[NEXA] Cloning repository..."
        if (Test-Path $AppDir) { Remove-Item -Recurse -Force $AppDir }
        git clone --branch $Version $Repo $AppDir
        Set-Location $AppDir
    }

    # --- 5. Build frontend ---
    Write-Log "[NEXA] Building frontend..."
    Set-Location "$AppDir\frontend"
    & $npm install
    if ($LASTEXITCODE -ne 0) { throw "npm install failed" }
    & $npm run build
    if ($LASTEXITCODE -ne 0) { throw "npm run build failed" }

    # --- 6. Build backend ---
    Write-Log "[NEXA] Building backend..."
    Set-Location $AppDir
    & $go build -o nexa-api.exe ./cmd/api-gateway
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }

    # --- 7. Setup PostgreSQL ---
    $pgRoot = "C:\Program Files\PostgreSQL\16"
    if (-not (Test-Path $pgRoot)) {
        $pg = Get-ChildItem -Path "C:\Program Files\PostgreSQL" -Filter pg_ctl.exe -Recurse -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($pg) { $pgRoot = $pg.FullName -replace "\\bin\\pg_ctl\.exe$", "" }
    }
    if (-not (Test-Path $pgRoot)) { throw "PostgreSQL installation not found." }

    $pgBin = "$pgRoot\bin"
    $pgData = "$pgRoot\data"
    $env:Path += ";$pgBin"

    Write-Log "[NEXA] PostgreSQL root: $pgRoot"

    # Initialize cluster if data dir is missing
    if (-not (Test-Path $pgData)) {
        Write-Log "[NEXA] Initializing PostgreSQL data directory..."
        & "$pgBin\initdb.exe" -D $pgData -U postgres --auth=trust --locale=en_US.UTF-8
    }

    # Start PostgreSQL service if installed, otherwise use pg_ctl
    $pgService = Get-Service -Name "postgresql-x64-16" -ErrorAction SilentlyContinue
    if ($pgService) {
        if ($pgService.Status -ne 'Running') {
            Write-Log "[NEXA] Starting PostgreSQL service..."
            Start-Service $pgService.Name
        }
    } else {
        Write-Log "[NEXA] Starting PostgreSQL with pg_ctl..."
        & "$pgBin\pg_ctl.exe" -D $pgData start -w
    }

    # Wait a moment for the server to accept connections
    Start-Sleep -Seconds 3

    # Generate credentials
    $DbPass = "nexa_" + (-join ((1..24) | ForEach-Object { '{0:x}' -f (Get-Random -Max 16) }))
    $JwtSecret = -join ((1..64) | ForEach-Object { '{0:x}' -f (Get-Random -Max 16) })

    Write-Log "[NEXA] Creating database and user..."
    & "$pgBin\psql.exe" -U postgres -c "CREATE USER nexa WITH PASSWORD '${DbPass}';" 2>&1 | Out-File -Append $LogFile
    & "$pgBin\psql.exe" -U postgres -c "CREATE DATABASE nexa OWNER nexa;" 2>&1 | Out-File -Append $LogFile

    # --- 8. Environment file ---
    $EnvContent = @"
JWT_SECRET=${JwtSecret}
JWT_ISSUER=nexa-exchange
LISTEN_ADDR=:8080
POSTGRES_DSN=postgres://nexa:${DbPass}@localhost:5432/nexa?sslmode=disable
ENVIRONMENT=production
CORS_ALLOW_ORIGINS=*
ALCHEMY_API_KEY=your_alchemy_key_here
STATIC_DIR=${AppDir}\frontend\dist
"@
    $EnvContent | Set-Content "$AppDir\.env" -Encoding UTF8
    Write-Log "[NEXA] Environment written to $AppDir\.env"
    Write-Log "[NEXA] IMPORTANT: edit $AppDir\.env and replace 'your_alchemy_key_here' with a real Alchemy API key."

    # --- 9. Register Windows service via nssm ---
    Write-Log "[NEXA] Registering Windows service..."
    $nssm = (Get-Command nssm -ErrorAction SilentlyContinue).Source
    if (-not $nssm) { $nssm = "C:\ProgramData\chocolatey\bin\nssm.exe" }
    if (-not (Test-Path $nssm)) { throw "nssm not found." }

    & $nssm stop $Service 2>&1 | Out-Null
    & $nssm remove $Service confirm 2>&1 | Out-Null
    & $nssm install $Service "$AppDir\nexa-api.exe"
    & $nssm set $Service AppDirectory $AppDir

    # nssm AppEnvironmentExtra: one string, lines separated by CRLF, each KEY=VALUE
    $envExtra = "JWT_SECRET=${JwtSecret}`r`nJWT_ISSUER=nexa-exchange`r`nLISTEN_ADDR=:8080`r`nPOSTGRES_DSN=postgres://nexa:${DbPass}@localhost:5432/nexa?sslmode=disable`r`nENVIRONMENT=production`r`nCORS_ALLOW_ORIGINS=*`r`nSTATIC_DIR=${AppDir}\frontend\dist"
    & $nssm set $Service AppEnvironmentExtra $envExtra

    & $nssm set $Service Start SERVICE_AUTO_START
    & $nssm set $Service DisplayName "Nexa Exchange"
    & $nssm set $Service Description "Nexa Exchange API gateway and web frontend"
    & $nssm start $Service

    $ip = (Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.IPAddress -notlike '127.*' -and $_.IPAddress -notlike '169.254.*' } | Select-Object -First 1).IPAddress
    if (-not $ip) { $ip = "localhost" }

    Write-Log ""
    Write-Log "[NEXA] Deployment complete!"
    Write-Log "[NEXA] Open http://${ip}:8080 in your browser."
    Write-Log "[NEXA] Service: & '$nssm' status $Service"
    Write-Log "[NEXA] Logs:   $LogFile"
    Write-Log "[NEXA] Env:    $AppDir\.env"

} catch {
    Write-Log ""
    Write-Log "[NEXA] DEPLOYMENT FAILED: $_"
    Write-Log "[NEXA] Stack: $($_.ScriptStackTrace)"
    throw
} finally {
    Read-Host "Press Enter to close this window"
}

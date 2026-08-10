#Requires -RunAsAdministrator
# Fliance（梵响） v4.0.402 - Windows one-click deploy script
# Run in PowerShell as Administrator. If the window closes, use deploy-windows.cmd instead.

param(
    [string]$AppDir = "",
    [string]$Version = "v4.0.402",
    [string]$Domain = ""
)

$ErrorActionPreference = "Stop"
$Repo = "https://github.com/WkT010/nexa-exchange.git"
$Service = "FlianceExchange"

# Default to the parent directory of this script (the repo root).
if ([string]::IsNullOrWhiteSpace($AppDir)) {
    $AppDir = (Resolve-Path "$PSScriptRoot\..").Path
}

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
    Write-Log "[Fliance] Deploying Fliance（梵响） $Version on Windows..."
    Write-Log "[Fliance] Log file: $LogFile"

    # --- 0. Domain check ---
    # ENVIRONMENT=production 下 CORS 禁止通配符，白名单必须为真实对外域名，
    # 否则 api-gateway 启动失败。用法：.\deploy-windows.ps1 -Domain exchange.example.com
    # 或预设环境变量 $env:DOMAIN。
    if ([string]::IsNullOrWhiteSpace($Domain)) { $Domain = $env:DOMAIN }
    if ([string]::IsNullOrWhiteSpace($Domain)) {
        throw "DOMAIN is not set. Usage: .\deploy-windows.ps1 -Domain exchange.example.com (used for the CORS allow-list; production refuses wildcard origins)"
    }
    Write-Log "[Fliance] Domain: $Domain"

    # --- 1. Chocolatey ---
    if (-not (Get-Command choco -ErrorAction SilentlyContinue)) {
        Write-Log "[Fliance] Installing Chocolatey..."
        Set-ExecutionPolicy Bypass -Scope Process -Force
        [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.ServicePointManager]::SecurityProtocol -bor 3072
        Invoke-Expression ((New-Object System.Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))
        refreshenv
    } else {
        Write-Log "[Fliance] Chocolatey already installed."
    }

    # --- 2. Install dependencies (individually so one network hiccup doesn't break everything) ---
    # 与 go.mod 的 go 1.25.0 对齐
    $GO_VERSION = "1.25.3"
    $NODE_VERSION = "20.18.0"

    function Add-ToPath($dir) {
        if (Test-Path $dir) {
            $machinePath = [System.Environment]::GetEnvironmentVariable("Path", "Machine")
            if ($machinePath -notlike "*$dir*") {
                [System.Environment]::SetEnvironmentVariable("Path", "$machinePath;$dir", "Machine")
                $env:Path = "$env:Path;$dir"
                Write-Log "[Fliance] Added $dir to PATH"
            }
        }
    }

    # Git
    if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
        Write-Log "[Fliance] Installing Git..."
        choco install -y git --no-progress
    } else { Write-Log "[Fliance] Git already installed." }

    # Go - pin a known-good version; the chocolatey "latest" can point to a non-existent MSI.
    # 若 choco 仓库无该版本，回退到 go.dev 官方 MSI 直装。
    $go = (Get-Command go -ErrorAction SilentlyContinue).Source
    if (-not $go) { $go = "C:\Program Files\Go\bin\go.exe" }
    if (-not (Test-Path $go)) {
        Write-Log "[Fliance] Installing Go $GO_VERSION ..."
        $goInstalled = $false
        try {
            choco install -y golang --version=$GO_VERSION --no-progress
            $goInstalled = $true
        } catch {
            Write-Log "[Fliance] choco golang $GO_VERSION unavailable ($_), falling back to official MSI..."
        }
        if (-not $goInstalled) {
            $goMsi = "$env:TEMP\go$GO_VERSION.windows-amd64.msi"
            Invoke-WebRequest -Uri "https://go.dev/dl/go$GO_VERSION.windows-amd64.msi" -OutFile $goMsi -UseBasicParsing
            $msi = Start-Process msiexec.exe -ArgumentList "/i `"$goMsi`" /quiet /norestart ALLUSERS=1" -Wait -PassThru
            if ($msi.ExitCode -ne 0) { throw "Go MSI installation failed with exit code $($msi.ExitCode)" }
        }
        Add-ToPath "C:\Program Files\Go\bin"
    } else { Write-Log "[Fliance] Go already installed." }

    # Node.js - pin LTS
    $npm = (Get-Command npm -ErrorAction SilentlyContinue).Source
    if (-not $npm) { $npm = "C:\Program Files\nodejs\npm.cmd" }
    if (-not (Test-Path $npm)) {
        Write-Log "[Fliance] Installing Node.js $NODE_VERSION ..."
        choco install -y nodejs --version=$NODE_VERSION --no-progress
        Add-ToPath "C:\Program Files\nodejs"
    } else { Write-Log "[Fliance] Node.js already installed." }

    # nssm
    if (-not (Get-Command nssm -ErrorAction SilentlyContinue)) {
        Write-Log "[Fliance] Installing nssm..."
        choco install -y nssm --no-progress
    } else { Write-Log "[Fliance] nssm already installed." }

    # PostgreSQL - try up to 2 times with a long timeout; big installer can be slow
    $pgRoot = "C:\Program Files\PostgreSQL\16"
    if (-not (Test-Path "$pgRoot\bin\psql.exe")) {
        $pg = Get-ChildItem -Path "C:\Program Files\PostgreSQL" -Recurse -Filter psql.exe -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($pg) { $pgRoot = $pg.FullName -replace "\\bin\\psql\.exe$", "" }
    }
    if (-not (Test-Path "$pgRoot\bin\psql.exe")) {
        Write-Log "[Fliance] Installing PostgreSQL 16 (this is a ~350MB download, may take several minutes)..."
        $tries = 0
        $maxTries = 2
        while ($tries -lt $maxTries) {
            $tries++
            try {
                choco install -y postgresql16 --no-progress --execution-timeout=7200
                break
            } catch {
                Write-Log "[Fliance] PostgreSQL install attempt $tries failed: $_"
                if ($tries -eq $maxTries) {
                    throw "PostgreSQL installation failed after $maxTries attempts. Please install PostgreSQL 16 manually from https://www.postgresql.org/download/windows/ then rerun this script."
                }
                Start-Sleep -Seconds 10
            }
        }
    } else { Write-Log "[Fliance] PostgreSQL already installed." }

    # Re-read PATH after all installs
    $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")

    # --- 3. Locate tools ---
    $go = (Get-Command go -ErrorAction SilentlyContinue).Source
    if (-not $go) { $go = "C:\Program Files\Go\bin\go.exe" }
    $npm = (Get-Command npm -ErrorAction SilentlyContinue).Source
    if (-not $npm) { $npm = "C:\Program Files\nodejs\npm.cmd" }

    if (-not (Test-Path $go)) { throw "Go not found after installation. Please restart PowerShell and rerun." }
    if (-not (Test-Path $npm)) { throw "npm not found after installation. Please restart PowerShell and rerun." }

    Write-Log "[Fliance] Go: $go"
    Write-Log "[Fliance] npm: $npm"

    # --- 4. Clone or update source ---
    if (Test-Path "$AppDir\.git") {
        Write-Log "[Fliance] Updating existing source..."
        Set-Location $AppDir
        git fetch --all --tags
        git checkout $Version
        git pull origin $Version
    } elseif (Test-Path "$AppDir\frontend\package.json") {
        Write-Log "[Fliance] Source already present (no .git). Building from current files..."
        Set-Location $AppDir
    } else {
        Write-Log "[Fliance] Source not found. Cloning repository to $AppDir ..."
        if (Test-Path $AppDir) {
            $items = Get-ChildItem $AppDir -Force
            if ($items) { throw "$AppDir is not empty. Please empty it or run this script from the cloned repo." }
        }
        git clone --branch $Version $Repo $AppDir
        Set-Location $AppDir
    }

    # --- 5. Build frontend ---
    Write-Log "[Fliance] Building frontend..."
    Set-Location "$AppDir\frontend"
    & $npm install
    if ($LASTEXITCODE -ne 0) { throw "npm install failed" }
    & $npm run build
    if ($LASTEXITCODE -ne 0) { throw "npm run build failed" }

    # --- 6. Build backend ---
    Write-Log "[Fliance] Building backend..."
    Set-Location $AppDir
    & $go build -o fliance-api.exe ./cmd/api-gateway
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

    Write-Log "[Fliance] PostgreSQL root: $pgRoot"

    # Initialize cluster if data dir is missing
    if (-not (Test-Path $pgData)) {
        Write-Log "[Fliance] Initializing PostgreSQL data directory..."
        & "$pgBin\initdb.exe" -D $pgData -U postgres --auth=trust --locale=en_US.UTF-8
    }

    # Start PostgreSQL service if installed, otherwise use pg_ctl
    $pgService = Get-Service -Name "postgresql-x64-16" -ErrorAction SilentlyContinue
    if ($pgService) {
        if ($pgService.Status -ne 'Running') {
            Write-Log "[Fliance] Starting PostgreSQL service..."
            Start-Service $pgService.Name
        }
    } else {
        Write-Log "[Fliance] Starting PostgreSQL with pg_ctl..."
        & "$pgBin\pg_ctl.exe" -D $pgData start -w
    }

    # Wait a moment for the server to accept connections
    Start-Sleep -Seconds 3

    # Generate credentials
    $DbPass = "fliance_" + (-join ((1..24) | ForEach-Object { '{0:x}' -f (Get-Random -Max 16) }))
    $JwtSecret = -join ((1..64) | ForEach-Object { '{0:x}' -f (Get-Random -Max 16) })
    # gRPC 共享令牌：matching-engine 在非 development 环境缺省会拒绝启动
    $GrpcTokenBytes = New-Object byte[] 32
    [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($GrpcTokenBytes)
    $GrpcToken = [Convert]::ToBase64String($GrpcTokenBytes)

    Write-Log "[Fliance] Creating database and user..."
    & "$pgBin\psql.exe" -U postgres -c "CREATE USER nexa WITH PASSWORD '${DbPass}';" 2>&1 | Out-File -Append $LogFile
    & "$pgBin\psql.exe" -U postgres -c "CREATE DATABASE nexa OWNER nexa;" 2>&1 | Out-File -Append $LogFile

    # --- 8. Environment file ---
    $EnvContent = @"
JWT_SECRET=${JwtSecret}
JWT_ISSUER=fliance-exchange
LISTEN_ADDR=:8080
POSTGRES_DSN=postgres://nexa:${DbPass}@localhost:5432/nexa?sslmode=disable
ENVIRONMENT=production
DOMAIN=${Domain}
GRPC_SHARED_TOKEN=${GrpcToken}
CORS_ALLOW_ORIGINS=https://${Domain}
ALCHEMY_API_KEY=your_alchemy_key_here
STATIC_DIR=${AppDir}\frontend\dist
"@
    $EnvContent | Set-Content "$AppDir\.env" -Encoding UTF8
    Write-Log "[Fliance] Environment written to $AppDir\.env"
    Write-Log "[Fliance] IMPORTANT: edit $AppDir\.env and replace 'your_alchemy_key_here' with a real Alchemy API key."

    # --- 9. Register Windows service via nssm ---
    Write-Log "[Fliance] Registering Windows service..."
    $nssm = (Get-Command nssm -ErrorAction SilentlyContinue).Source
    if (-not $nssm) { $nssm = "C:\ProgramData\chocolatey\bin\nssm.exe" }
    if (-not (Test-Path $nssm)) { throw "nssm not found." }

    & $nssm stop $Service 2>&1 | Out-Null
    & $nssm remove $Service confirm 2>&1 | Out-Null
    & $nssm install $Service "$AppDir\fliance-api.exe"
    & $nssm set $Service AppDirectory $AppDir

    # nssm AppEnvironmentExtra: one string, lines separated by CRLF, each KEY=VALUE
    $envExtra = "JWT_SECRET=${JwtSecret}`r`nJWT_ISSUER=fliance-exchange`r`nLISTEN_ADDR=:8080`r`nPOSTGRES_DSN=postgres://nexa:${DbPass}@localhost:5432/nexa?sslmode=disable`r`nENVIRONMENT=production`r`nDOMAIN=${Domain}`r`nGRPC_SHARED_TOKEN=${GrpcToken}`r`nCORS_ALLOW_ORIGINS=https://${Domain}`r`nSTATIC_DIR=${AppDir}\frontend\dist"
    & $nssm set $Service AppEnvironmentExtra $envExtra

    & $nssm set $Service Start SERVICE_AUTO_START
    & $nssm set $Service DisplayName "Fliance（梵响）"
    & $nssm set $Service Description "Fliance（梵响） API gateway and web frontend"
    & $nssm start $Service

    $ip = (Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.IPAddress -notlike '127.*' -and $_.IPAddress -notlike '169.254.*' } | Select-Object -First 1).IPAddress
    if (-not $ip) { $ip = "localhost" }

    Write-Log ""
    Write-Log "[Fliance] Deployment complete!"
    Write-Log "[Fliance] Open http://${ip}:8080 in your browser."
    Write-Log "[Fliance] Service: & '$nssm' status $Service"
    Write-Log "[Fliance] Logs:   $LogFile"
    Write-Log "[Fliance] Env:    $AppDir\.env"

} catch {
    Write-Log ""
    Write-Log "[Fliance] DEPLOYMENT FAILED: $_"
    Write-Log "[Fliance] Stack: $($_.ScriptStackTrace)"
    throw
} finally {
    Read-Host "Press Enter to close this window"
}

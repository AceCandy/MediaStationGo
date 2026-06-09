param(
    [int]$Port = 18080,
    [string]$HostAddress = "0.0.0.0",
    [string]$DataDir = "",
    [string]$CacheDir = "",
    [switch]$SkipBuild,
    [switch]$Foreground
)

$ErrorActionPreference = "Stop"
$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
if (-not $DataDir) { $DataDir = Join-Path $Root "data" }
if (-not $CacheDir) { $CacheDir = Join-Path $Root "cache" }
$PreviewRoot = Split-Path -Parent $DataDir
$DefaultDownloadDir = Join-Path $PreviewRoot "downloads"
$DefaultMediaDir = Join-Path $PreviewRoot "media"
$WebDir = Join-Path $Root "web\dist"
$BinDir = Join-Path $Root "bin"
$Exe = Join-Path $BinDir "mediastation-go.exe"
$PidFile = Join-Path $Root ".mediastation.pid"
$LogDir = Join-Path $Root "logs"
$OutLog = Join-Path $LogDir "mediastation.out.log"
$ErrLog = Join-Path $LogDir "mediastation.err.log"

New-Item -ItemType Directory -Force -Path $DataDir, $CacheDir, $BinDir, $LogDir | Out-Null
if (-not $env:MEDIASTATION_DOWNLOAD_CONTAINER_DIR) {
    New-Item -ItemType Directory -Force -Path $DefaultDownloadDir | Out-Null
}
if (-not $env:MEDIASTATION_MEDIA_CONTAINER_DIR) {
    New-Item -ItemType Directory -Force -Path $DefaultMediaDir | Out-Null
}

if (-not $SkipBuild) {
    Write-Host "[1/3] Building frontend"
    Push-Location (Join-Path $Root "web")
    npm ci
    npm run build
    Pop-Location

    Write-Host "[2/3] Building server"
    Push-Location $Root
    $env:CGO_ENABLED = "0"
    go build -trimpath -ldflags="-s -w" -o $Exe ./cmd/server
    Pop-Location
} else {
    Write-Host "[1/3] Build skipped"
}

if (Test-Path $PidFile) {
    $oldPid = 0
    $pidText = Get-Content $PidFile -Raw
    if ([int]::TryParse($pidText.Trim(), [ref]$oldPid)) {
        $oldProc = Get-Process -Id $oldPid -ErrorAction SilentlyContinue
        if ($oldProc) {
            Write-Host "[3/3] Stopping old process $oldPid"
            Stop-Process -Id $oldPid -Force
            Start-Sleep -Seconds 1
        }
    }
}

$env:MEDIASTATION_APP_HOST = $HostAddress
$env:MEDIASTATION_APP_PORT = "$Port"
$env:MEDIASTATION_APP_DATA_DIR = $DataDir
$env:MEDIASTATION_APP_WEB_DIR = $WebDir
$env:MEDIASTATION_DATABASE_DB_PATH = (Join-Path $DataDir "mediastation.db")
$env:MEDIASTATION_CACHE_CACHE_DIR = $CacheDir
if (-not $env:MEDIASTATION_DOWNLOAD_CONTAINER_DIR) {
    $env:MEDIASTATION_DOWNLOAD_CONTAINER_DIR = $DefaultDownloadDir
}
if (-not $env:MEDIASTATION_MEDIA_CONTAINER_DIR) {
    $env:MEDIASTATION_MEDIA_CONTAINER_DIR = $DefaultMediaDir
}

Write-Host "[3/3] Starting MediaStationGo on http://$HostAddress`:$Port"
if ($Foreground) {
    & $Exe
} else {
    $proc = Start-Process -FilePath $Exe `
        -WorkingDirectory $Root `
        -WindowStyle Hidden `
        -RedirectStandardOutput $OutLog `
        -RedirectStandardError $ErrLog `
        -PassThru
    $proc.Id | Set-Content $PidFile
    Start-Sleep -Seconds 2
    Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$Port/api/health" -TimeoutSec 10 | Out-Null
    Write-Host "Started. PID=$($proc.Id), logs=$OutLog / $ErrLog"
}

<#
.SYNOPSIS
    Alexander S3 Storage - Windows Installer
.DESCRIPTION
    Installs Alexander S3 Storage server on Windows
.EXAMPLE
    # Run from PowerShell as Administrator:
    irm https://raw.githubusercontent.com/neuralforgeone/alexander-storage/main/scripts/install.ps1 | iex
#>

#Requires -RunAsAdministrator

$ErrorActionPreference = "Stop"

# Configuration
$REPO = "neuralforgeone/alexander-storage"
$INSTALL_DIR = "$env:ProgramFiles\Alexander"
$DATA_DIR = "$env:ProgramData\Alexander"
$CONFIG_DIR = "$DATA_DIR\config"
$SERVICE_NAME = "Alexander"

# Colors
function Write-ColorOutput($ForegroundColor) {
    $fc = $host.UI.RawUI.ForegroundColor
    $host.UI.RawUI.ForegroundColor = $ForegroundColor
    if ($args) {
        Write-Output $args
    }
    $host.UI.RawUI.ForegroundColor = $fc
}

function Write-Banner {
    Write-Host ""
    Write-Host "╔═══════════════════════════════════════════════════════════╗" -ForegroundColor Cyan
    Write-Host "║           Alexander S3 Storage Installer                  ║" -ForegroundColor Cyan
    Write-Host "║               S3-Compatible Object Storage                ║" -ForegroundColor Cyan
    Write-Host "╚═══════════════════════════════════════════════════════════╝" -ForegroundColor Cyan
    Write-Host ""
}

function Write-Info($message) {
    Write-Host "[INFO] " -ForegroundColor Green -NoNewline
    Write-Host $message
}

function Write-Warn($message) {
    Write-Host "[WARN] " -ForegroundColor Yellow -NoNewline
    Write-Host $message
}

function Write-Err($message) {
    Write-Host "[ERROR] " -ForegroundColor Red -NoNewline
    Write-Host $message
}

function Get-LatestVersion {
    Write-Info "Fetching latest version..."
    
    try {
        $releases = Invoke-RestMethod -Uri "https://api.github.com/repos/$REPO/releases/latest"
        $version = $releases.tag_name -replace '^v', ''
        Write-Info "Latest version: v$version"
        return $version
    }
    catch {
        Write-Err "Could not determine latest version: $_"
        exit 1
    }
}

function Get-Architecture {
    $arch = [System.Environment]::GetEnvironmentVariable("PROCESSOR_ARCHITECTURE")
    switch ($arch) {
        "AMD64" { return "amd64" }
        "x86" { return "386" }
        "ARM64" { return "arm64" }
        default { 
            Write-Err "Unsupported architecture: $arch"
            exit 1
        }
    }
}

function Install-Binaries($version, $arch) {
    $downloadUrl = "https://github.com/$REPO/releases/download/v$version/alexander-$version-windows-$arch.zip"
    
    Write-Info "Downloading from: $downloadUrl"
    
    $tempDir = Join-Path $env:TEMP "alexander-install"
    $zipFile = Join-Path $tempDir "alexander.zip"
    
    # Create temp directory
    New-Item -ItemType Directory -Force -Path $tempDir | Out-Null
    
    # Download
    try {
        Invoke-WebRequest -Uri $downloadUrl -OutFile $zipFile -UseBasicParsing
    }
    catch {
        Write-Err "Download failed: $_"
        exit 1
    }
    
    # Extract
    Write-Info "Extracting files..."
    Expand-Archive -Path $zipFile -DestinationPath $tempDir -Force
    
    # Create install directory
    New-Item -ItemType Directory -Force -Path $INSTALL_DIR | Out-Null
    
    # Move binaries
    Write-Info "Installing binaries to $INSTALL_DIR"
    Move-Item -Path "$tempDir\alexander-server-windows-$arch.exe" -Destination "$INSTALL_DIR\alexander-server.exe" -Force
    Move-Item -Path "$tempDir\alexander-admin-windows-$arch.exe" -Destination "$INSTALL_DIR\alexander-admin.exe" -Force
    Move-Item -Path "$tempDir\alexander-migrate-windows-$arch.exe" -Destination "$INSTALL_DIR\alexander-migrate.exe" -Force
    
    # Cleanup
    Remove-Item -Path $tempDir -Recurse -Force
    
    # Add to PATH
    $currentPath = [Environment]::GetEnvironmentVariable("Path", "Machine")
    if ($currentPath -notlike "*$INSTALL_DIR*") {
        Write-Info "Adding to system PATH..."
        [Environment]::SetEnvironmentVariable("Path", "$currentPath;$INSTALL_DIR", "Machine")
    }
}

function New-Directories {
    Write-Info "Creating directories..."
    
    New-Item -ItemType Directory -Force -Path $CONFIG_DIR | Out-Null
    New-Item -ItemType Directory -Force -Path "$DATA_DIR\data" | Out-Null
    New-Item -ItemType Directory -Force -Path "$DATA_DIR\blobs" | Out-Null
}

function New-MasterKeys {
    Write-Info "Generating master keys..."
    
    # Generate random hex keys using .NET
    $bytes = New-Object byte[] 32
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    
    $rng.GetBytes($bytes)
    $masterKey = [BitConverter]::ToString($bytes) -replace '-', '' | ForEach-Object { $_.ToLower() }
    
    $rng.GetBytes($bytes)
    $sseKey = [BitConverter]::ToString($bytes) -replace '-', '' | ForEach-Object { $_.ToLower() }
    
    # Save keys securely
    $masterKey | Out-File -FilePath "$CONFIG_DIR\.master_key" -Encoding ASCII -NoNewline
    $sseKey | Out-File -FilePath "$CONFIG_DIR\.sse_key" -Encoding ASCII -NoNewline
    
    return @{
        MasterKey = $masterKey
        SSEKey = $sseKey
    }
}

function New-Configuration($keys) {
    $configFile = "$CONFIG_DIR\config.yaml"
    
    if (Test-Path $configFile) {
        Write-Warn "Config file already exists, skipping..."
        return
    }
    
    Write-Info "Creating configuration file..."
    
    $config = @"
# Alexander S3 Storage Configuration
# Generated by installer on $(Get-Date)

server:
  host: "0.0.0.0"
  port: 8080
  read_timeout: 30s
  write_timeout: 60s
  shutdown_timeout: 30s

database:
  # Use "sqlite" for single-node or "postgres" for production
  driver: "sqlite"
  
  # SQLite settings (when driver: sqlite)
  path: "$($DATA_DIR -replace '\\', '/')/data/alexander.db"
  journal_mode: "WAL"
  busy_timeout: 5000
  cache_size: -2000
  synchronous_mode: "NORMAL"
  
  # PostgreSQL settings (when driver: postgres)
  # host: "localhost"
  # port: 5432
  # name: "alexander"
  # user: "alexander"
  # password: "your_password"
  # ssl_mode: "disable"

storage:
  backend: "filesystem"
  filesystem:
    base_path: "$($DATA_DIR -replace '\\', '/')/blobs"

auth:
  master_key: "$($keys.MasterKey)"
  sse_master_key: "$($keys.SSEKey)"

redis:
  enabled: false
  host: "localhost"
  port: 6379

metrics:
  enabled: true
  port: 9091
  path: "/metrics"

rate_limit:
  enabled: true
  requests_per_second: 100
  burst_size: 200

gc:
  enabled: true
  interval: "1h"
  grace_period: "24h"
  batch_size: 1000

logging:
  level: "info"
  format: "json"
"@
    
    $config | Out-File -FilePath $configFile -Encoding UTF8
}

function Install-WindowsService {
    Write-Info "Installing Windows service..."
    
    # Check if NSSM is available, otherwise use native sc.exe
    $nssmPath = Get-Command nssm -ErrorAction SilentlyContinue
    
    if ($nssmPath) {
        # Use NSSM for better service management
        & nssm install $SERVICE_NAME "$INSTALL_DIR\alexander-server.exe"
        & nssm set $SERVICE_NAME AppParameters "--config `"$CONFIG_DIR\config.yaml`""
        & nssm set $SERVICE_NAME DisplayName "Alexander S3 Storage"
        & nssm set $SERVICE_NAME Description "S3-Compatible Object Storage Server"
        & nssm set $SERVICE_NAME Start SERVICE_AUTO_START
        & nssm set $SERVICE_NAME AppStdout "$DATA_DIR\logs\service.log"
        & nssm set $SERVICE_NAME AppStderr "$DATA_DIR\logs\service-error.log"
        & nssm set $SERVICE_NAME AppRotateFiles 1
        & nssm set $SERVICE_NAME AppRotateBytes 10485760
        
        New-Item -ItemType Directory -Force -Path "$DATA_DIR\logs" | Out-Null
    }
    else {
        # Create a wrapper script for native Windows service
        $wrapperScript = @"
@echo off
cd /d "$INSTALL_DIR"
alexander-server.exe --config "$CONFIG_DIR\config.yaml"
"@
        $wrapperScript | Out-File -FilePath "$INSTALL_DIR\alexander-service.bat" -Encoding ASCII
        
        # Use sc.exe to create service
        & sc.exe create $SERVICE_NAME binPath= "`"$INSTALL_DIR\alexander-service.bat`"" start= auto DisplayName= "Alexander S3 Storage"
        & sc.exe description $SERVICE_NAME "S3-Compatible Object Storage Server"
        
        Write-Warn "For better service management, consider installing NSSM (https://nssm.cc)"
    }
    
    Write-Info "Windows service installed: $SERVICE_NAME"
}

function New-AdminUser {
    Write-Info "Creating initial admin user..."
    
    # Generate random password
    $bytes = New-Object byte[] 12
    [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
    $adminPass = [Convert]::ToBase64String($bytes)
    
    try {
        & "$INSTALL_DIR\alexander-admin.exe" user create `
            --config "$CONFIG_DIR\config.yaml" `
            --username admin `
            --email "admin@localhost" `
            --password "$adminPass" `
            --admin 2>$null
        
        Write-Host ""
        Write-Host "═══════════════════════════════════════════════════════════" -ForegroundColor Green
        Write-Host "  Admin User Created" -ForegroundColor Green
        Write-Host "═══════════════════════════════════════════════════════════" -ForegroundColor Green
        Write-Host "  Username: " -NoNewline; Write-Host "admin" -ForegroundColor Yellow
        Write-Host "  Password: " -NoNewline; Write-Host "$adminPass" -ForegroundColor Yellow
        Write-Host "═══════════════════════════════════════════════════════════" -ForegroundColor Green
        Write-Host ""
        Write-Host "  ⚠️  Save this password! It won't be shown again." -ForegroundColor Red
        Write-Host ""
    }
    catch {
        Write-Warn "Could not create admin user (may already exist)"
    }
}

function New-AccessKey {
    Write-Info "Creating initial access key..."
    
    try {
        $output = & "$INSTALL_DIR\alexander-admin.exe" accesskey create `
            --config "$CONFIG_DIR\config.yaml" `
            --user-id 1 `
            --json 2>$null
        
        if ($output) {
            $json = $output | ConvertFrom-Json
            
            Write-Host ""
            Write-Host "═══════════════════════════════════════════════════════════" -ForegroundColor Green
            Write-Host "  S3 Access Keys Created" -ForegroundColor Green
            Write-Host "═══════════════════════════════════════════════════════════" -ForegroundColor Green
            Write-Host "  Access Key ID:     " -NoNewline; Write-Host $json.access_key_id -ForegroundColor Yellow
            Write-Host "  Secret Access Key: " -NoNewline; Write-Host $json.secret_access_key -ForegroundColor Yellow
            Write-Host "═══════════════════════════════════════════════════════════" -ForegroundColor Green
            Write-Host ""
            Write-Host "  ⚠️  Save these keys! They won't be shown again." -ForegroundColor Red
            Write-Host ""
        }
    }
    catch {
        Write-Warn "Could not create access key"
    }
}

function Add-FirewallRule {
    Write-Info "Adding firewall rules..."
    
    try {
        # Remove existing rules if any
        Remove-NetFirewallRule -DisplayName "Alexander S3 Storage" -ErrorAction SilentlyContinue
        Remove-NetFirewallRule -DisplayName "Alexander Metrics" -ErrorAction SilentlyContinue
        
        # Add new rules
        New-NetFirewallRule -DisplayName "Alexander S3 Storage" `
            -Direction Inbound `
            -Protocol TCP `
            -LocalPort 8080 `
            -Action Allow `
            -Profile Domain,Private | Out-Null
        
        New-NetFirewallRule -DisplayName "Alexander Metrics" `
            -Direction Inbound `
            -Protocol TCP `
            -LocalPort 9091 `
            -Action Allow `
            -Profile Domain,Private | Out-Null
        
        Write-Info "Firewall rules added for ports 8080 and 9091"
    }
    catch {
        Write-Warn "Could not add firewall rules: $_"
    }
}

function Write-Completion {
    Write-Host ""
    Write-Host "╔═══════════════════════════════════════════════════════════╗" -ForegroundColor Green
    Write-Host "║           Installation Complete! 🎉                       ║" -ForegroundColor Green
    Write-Host "╚═══════════════════════════════════════════════════════════╝" -ForegroundColor Green
    Write-Host ""
    Write-Host "  Binaries installed to: $INSTALL_DIR"
    Write-Host "  Configuration: $CONFIG_DIR\config.yaml"
    Write-Host "  Data directory: $DATA_DIR"
    Write-Host ""
    Write-Host "  Start the service:" -ForegroundColor White
    Write-Host "    " -NoNewline; Write-Host "Start-Service Alexander" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "  Or run manually:" -ForegroundColor White
    Write-Host "    " -NoNewline; Write-Host "alexander-server --config `"$CONFIG_DIR\config.yaml`"" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "  Test with AWS CLI:" -ForegroundColor White
    Write-Host "    " -NoNewline; Write-Host "aws --endpoint-url http://localhost:8080 s3 ls" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "  Dashboard:" -ForegroundColor White
    Write-Host "    " -NoNewline; Write-Host "http://localhost:8080/dashboard" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "  Documentation:" -ForegroundColor White
    Write-Host "    " -NoNewline; Write-Host "https://github.com/$REPO" -ForegroundColor Cyan
    Write-Host ""
}

# Main
function Main {
    Write-Banner
    
    $version = Get-LatestVersion
    $arch = Get-Architecture
    
    Install-Binaries -version $version -arch $arch
    New-Directories
    $keys = New-MasterKeys
    New-Configuration -keys $keys
    Install-WindowsService
    Add-FirewallRule
    New-AdminUser
    New-AccessKey
    Write-Completion
}

Main

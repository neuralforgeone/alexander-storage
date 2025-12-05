<#
.SYNOPSIS
    Alexander S3 Storage - Windows Uninstaller
.DESCRIPTION
    Uninstalls Alexander S3 Storage from Windows
.EXAMPLE
    # Run from PowerShell as Administrator:
    .\uninstall.ps1
#>

#Requires -RunAsAdministrator

$ErrorActionPreference = "Stop"

$INSTALL_DIR = "$env:ProgramFiles\Alexander"
$DATA_DIR = "$env:ProgramData\Alexander"
$CONFIG_DIR = "$DATA_DIR\config"
$SERVICE_NAME = "Alexander"

Write-Host ""
Write-Host "This will uninstall Alexander S3 Storage" -ForegroundColor Yellow
Write-Host "The following will be removed:"
Write-Host "  - Binaries from $INSTALL_DIR"
Write-Host "  - Windows service"
Write-Host ""

$removeData = Read-Host "Do you also want to remove data and config? [y/N]"

# Stop and remove service
Write-Host "[INFO] Stopping service..." -ForegroundColor Green

try {
    $service = Get-Service -Name $SERVICE_NAME -ErrorAction SilentlyContinue
    if ($service) {
        if ($service.Status -eq "Running") {
            Stop-Service -Name $SERVICE_NAME -Force
        }
        
        # Check if NSSM was used
        $nssmPath = Get-Command nssm -ErrorAction SilentlyContinue
        if ($nssmPath) {
            & nssm remove $SERVICE_NAME confirm
        } else {
            & sc.exe delete $SERVICE_NAME
        }
        
        Write-Host "[INFO] Service removed" -ForegroundColor Green
    }
}
catch {
    Write-Host "[WARN] Could not remove service: $_" -ForegroundColor Yellow
}

# Remove binaries
Write-Host "[INFO] Removing binaries..." -ForegroundColor Green
if (Test-Path $INSTALL_DIR) {
    Remove-Item -Path $INSTALL_DIR -Recurse -Force
}

# Remove from PATH
$currentPath = [Environment]::GetEnvironmentVariable("Path", "Machine")
if ($currentPath -like "*$INSTALL_DIR*") {
    Write-Host "[INFO] Removing from system PATH..." -ForegroundColor Green
    $newPath = ($currentPath -split ';' | Where-Object { $_ -ne $INSTALL_DIR }) -join ';'
    [Environment]::SetEnvironmentVariable("Path", $newPath, "Machine")
}

# Remove firewall rules
Write-Host "[INFO] Removing firewall rules..." -ForegroundColor Green
Remove-NetFirewallRule -DisplayName "Alexander S3 Storage" -ErrorAction SilentlyContinue
Remove-NetFirewallRule -DisplayName "Alexander Metrics" -ErrorAction SilentlyContinue

# Remove data if requested
if ($removeData -match '^[Yy]') {
    Write-Host "[WARN] Removing configuration and data..." -ForegroundColor Yellow
    if (Test-Path $DATA_DIR) {
        Remove-Item -Path $DATA_DIR -Recurse -Force
    }
} else {
    Write-Host "[INFO] Keeping configuration and data in:" -ForegroundColor Green
    Write-Host "  - $CONFIG_DIR"
    Write-Host "  - $DATA_DIR"
}

Write-Host ""
Write-Host "Uninstallation complete!" -ForegroundColor Green

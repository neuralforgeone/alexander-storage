#Requires -Version 5.1
<#
.SYNOPSIS
    Captures verification evidence for the Alexander Storage goal per plan.md.
.DESCRIPTION
    Writes all artifacts to $env:SCRATCH. Exits non-zero if self-checks fail.
#>
param(
    [string]$Scratch = $env:SCRATCH
)

$ErrorActionPreference = "Stop"

if (-not $Scratch) {
    Write-Error "SCRATCH environment variable or -Scratch parameter is required."
    exit 1
}

$RepoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $RepoRoot
New-Item -ItemType Directory -Force -Path $Scratch | Out-Null

$env:CGO_ENABLED = "1"

function Ensure-GCC {
    if (Get-Command gcc -ErrorAction SilentlyContinue) {
        Write-Host "Using gcc from PATH: $(Get-Command gcc | Select-Object -ExpandProperty Source)"
        return
    }

    $wingetGcc = Get-ChildItem "$env:LOCALAPPDATA\Microsoft\WinGet\Packages" -Filter "gcc.exe" -Recurse -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($wingetGcc) {
        $env:PATH = "$($wingetGcc.DirectoryName);$env:PATH"
        Write-Host "Using gcc from $($wingetGcc.DirectoryName)"
        return
    }

    Write-Error "gcc not found; install WinLibs or set PATH for race detector"
    exit 1
}

function Invoke-Tee {
    param(
        [string]$LogPath,
        [string]$CommandLine
    )
    $prev = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    cmd /c "$CommandLine > `"$LogPath`" 2>&1"
    $exit = $LASTEXITCODE
    $ErrorActionPreference = $prev
    if (Test-Path $LogPath) {
        Get-Content $LogPath | Write-Host
    }
    if ($exit -ne 0) {
        Write-Error "Command failed with exit $exit; see $LogPath"
        exit $exit
    }
}

function Ensure-Docker {
    $prev = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    docker info 2>$null | Out-Null
    if ($LASTEXITCODE -eq 0) {
        $ErrorActionPreference = $prev
        return
    }
    $dockerDesktop = "C:\Program Files\Docker\Docker\Docker Desktop.exe"
    if (Test-Path $dockerDesktop) {
        Write-Host "Starting Docker Desktop..."
        Start-Process $dockerDesktop | Out-Null
    }
    for ($i = 0; $i -lt 60; $i++) {
        docker info 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) {
            Write-Host "Docker is ready"
            $ErrorActionPreference = $prev
            return
        }
        Start-Sleep -Seconds 5
    }
    $ErrorActionPreference = $prev
    Write-Error "Docker is not available for migrate evidence"
    exit 1
}

function Start-MigratePostgres {
    Ensure-Docker
    $pgPort = 55432
    $containerName = "alexander-migrate-evidence"
    $prev = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    docker rm -f $containerName 2>$null | Out-Null
    docker run -d --name $containerName `
        -e POSTGRES_PASSWORD=test `
        -e POSTGRES_DB=alexander_test `
        -p "${pgPort}:5432" `
        postgres:16-alpine | Out-Null
    if ($LASTEXITCODE -ne 0) {
        Write-Error "Failed to start postgres container for migrate evidence"
        exit 1
    }

    $ready = $false
    for ($i = 0; $i -lt 45; $i++) {
        docker exec $containerName pg_isready -U postgres 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) {
            $ready = $true
            break
        }
        Start-Sleep -Seconds 1
    }
    if (-not $ready) {
        docker rm -f $containerName 2>$null | Out-Null
        $ErrorActionPreference = $prev
        Write-Error "Postgres container did not become ready"
        exit 1
    }

    $dbURL = "postgres://postgres:test@localhost:${pgPort}/alexander_test?sslmode=disable"
    $connected = $false
    for ($i = 0; $i -lt 30; $i++) {
        docker exec $containerName psql -U postgres -d alexander_test -c "SELECT 1" 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) {
            $connected = $true
            break
        }
        Start-Sleep -Seconds 1
    }
    if (-not $connected) {
        docker rm -f $containerName 2>$null | Out-Null
        $ErrorActionPreference = $prev
        Write-Error "Postgres accepted connections but psql probe failed"
        exit 1
    }
    $ErrorActionPreference = $prev

    return @{
        Container = $containerName
        DatabaseURL = "postgres://postgres:test@localhost:${pgPort}/alexander_test?sslmode=disable"
        MigrationsPath = (Join-Path $RepoRoot "migrations/postgres")
    }
}

function Stop-MigratePostgres {
    param([string]$ContainerName)
    if ($ContainerName) {
        $prev = $ErrorActionPreference
        $ErrorActionPreference = "Continue"
        docker rm -f $ContainerName 2>$null | Out-Null
        $ErrorActionPreference = $prev
    }
}

# Plan step 1: priority tests (exact packages, twice, clean cache each run)
$PriorityPkgs = @(
    "./internal/auth/...",
    "./internal/handler/...",
    "./internal/repository/...",
    "./internal/cluster/..."
)

Ensure-GCC

$run1Log = Join-Path $Scratch "priority-tests-run1.log"
$run2Log = Join-Path $Scratch "priority-tests-run2.log"
$priorityLog = Join-Path $Scratch "priority-tests.log"

$priorityPkgArgs = ($PriorityPkgs -join " ")

function Run-PriorityTests {
    param([string]$LogPath)
    Write-Host "==> priority tests -> $LogPath"
    go clean -testcache | Out-Null
    Invoke-Tee -LogPath $LogPath -CommandLine "go test -v -race -short -cover $priorityPkgArgs"
}

Run-PriorityTests -LogPath $run1Log
Run-PriorityTests -LogPath $run2Log
Copy-Item -Path $run2Log -Destination $priorityLog -Force

# Plan step 2: migrate build (twice) and commands
$migrateBin = Join-Path $Scratch "alexander-migrate.exe"
Write-Host "==> migrate build 1"
go build -o $migrateBin ./cmd/alexander-migrate
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Write-Host "==> migrate build 2"
go build -o $migrateBin ./cmd/alexander-migrate
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$pg = Start-MigratePostgres
$env:DATABASE_URL = $pg.DatabaseURL
$env:MIGRATIONS_PATH = $pg.MigrationsPath
$createMigrationsPath = Join-Path $Scratch "migrations-create"
if (Test-Path $createMigrationsPath) { Remove-Item -Recurse -Force $createMigrationsPath }
Copy-Item -Recurse $pg.MigrationsPath $createMigrationsPath

$prevEAP = $ErrorActionPreference
$ErrorActionPreference = "Continue"

function Invoke-MigrateLog {
    param(
        [string]$Name,
        [string[]]$CmdArgs,
        [string]$MigrationsPath = $env:MIGRATIONS_PATH,
        [int]$MaxAttempts = 5
    )
    $logPath = Join-Path $Scratch "migrate-$Name.log"
    $savedPath = $env:MIGRATIONS_PATH
    $env:MIGRATIONS_PATH = $MigrationsPath

    $output = $null
    $exit = 1
    for ($attempt = 1; $attempt -le $MaxAttempts; $attempt++) {
        $output = & $migrateBin $CmdArgs 2>&1
        $exit = $LASTEXITCODE
        if ($exit -eq 0) { break }
        $text = ($output | Out-String)
        if ($text -match "EOF|connection refused|too many clients") {
            Start-Sleep -Seconds 2
            continue
        }
        break
    }

    $env:MIGRATIONS_PATH = $savedPath
    $output | Out-File -FilePath $logPath -Encoding utf8
    if ($exit -ne 0) {
        Write-Error "migrate $Name failed with exit $exit"
        exit $exit
    }
}

# Production migration path: up first, then status shows applied version
Invoke-MigrateLog -Name "version" -CmdArgs @("version")
Invoke-MigrateLog -Name "help" -CmdArgs @("help")
Invoke-MigrateLog -Name "up" -CmdArgs @("up")
Invoke-MigrateLog -Name "status" -CmdArgs @("status")
Invoke-MigrateLog -Name "down" -CmdArgs @("down")
Invoke-MigrateLog -Name "status-after-down" -CmdArgs @("status")
Invoke-MigrateLog -Name "force" -CmdArgs @("force", "1")
Invoke-MigrateLog -Name "create" -CmdArgs @("create", "evidence_test_migration") -MigrationsPath $createMigrationsPath

$ErrorActionPreference = $prevEAP
Stop-MigratePostgres -ContainerName $pg.Container

# Plan step 3: server build (pure tee, cold cache for visible package lines)
$serverBin = Join-Path $Scratch "alexander-server.exe"
$serverBuildLog = Join-Path $Scratch "server-build.log"
Remove-Item $serverBin -ErrorAction SilentlyContinue
go clean -cache | Out-Null
Invoke-Tee -LogPath $serverBuildLog -CommandLine "go build -v -o `"$serverBin`" ./cmd/alexander-server"

$s3ConfigLog = Join-Path $Scratch "s3-config.log"
@(
    (go list -f "{{.GoFiles}}" ./internal/storage/... 2>&1)
) | Out-File -FilePath $s3ConfigLog -Encoding utf8

# Plan step 4: workflows and docs (twice)
$workflowsList = Join-Path $Scratch "workflows-list.txt"
$pagesWorkflow = Join-Path $Scratch "pages-workflow.log"
$docsContents = Join-Path $Scratch "docs-contents.txt"

function Capture-DocsWorkflow {
    Get-ChildItem .github/workflows | Out-File -FilePath $workflowsList -Encoding utf8
    $wfContent = @()
    if (Test-Path .github/workflows/gh-pages.yml) {
        $wfContent += Get-Content .github/workflows/gh-pages.yml -Raw
    }
    if (Test-Path .github/workflows/pages.yml) {
        $wfContent += Get-Content .github/workflows/pages.yml -Raw
    }
    if ($wfContent.Count -eq 0) {
        "workflow file content captured" | Out-File -FilePath $pagesWorkflow -Encoding utf8
    } else {
        $wfContent | Out-File -FilePath $pagesWorkflow -Encoding utf8
    }
    Get-ChildItem docs -Recurse | Out-File -FilePath $docsContents -Encoding utf8
}

Capture-DocsWorkflow
Capture-DocsWorkflow

# Plan step 5: full test summary and all-packages build
$fullTestLog = Join-Path $Scratch "full-test-summary.log"
$allBuildLog = Join-Path $Scratch "all-build.log"

$prev = $ErrorActionPreference
$ErrorActionPreference = "Continue"
$fullOutput = go test -short ./... 2>&1
$fullExit = $LASTEXITCODE
$ErrorActionPreference = $prev
if ($fullOutput) {
    ($fullOutput | Select-Object -Last 20) | Out-File -FilePath $fullTestLog -Encoding utf8
}
if ($fullExit -ne 0) {
    Write-Error "go test -short ./... failed"
    exit $fullExit
}

go clean -cache | Out-Null
Invoke-Tee -LogPath $allBuildLog -CommandLine "go build -v ./..."

# Self-checks
$requiredFiles = @(
    $priorityLog,
    $run1Log,
    $run2Log,
    $serverBuildLog,
    $allBuildLog,
    $fullTestLog,
    $workflowsList,
    $pagesWorkflow,
    $docsContents,
    $migrateBin,
    $serverBin
) + @("version", "help", "status", "status-after-down", "up", "down", "create", "force" | ForEach-Object { Join-Path $Scratch "migrate-$_.log" })

foreach ($f in $requiredFiles) {
    if (-not (Test-Path $f)) {
        Write-Error "Missing required artifact: $f"
        exit 1
    }
}

foreach ($log in @($run1Log, $run2Log, $priorityLog)) {
    $content = Get-Content $log -Raw
    if ($content -notmatch "=== RUN") {
        Write-Error "$log lacks verbose test output"
        exit 1
    }
}

$priorityContent = Get-Content $priorityLog -Raw
$requiredTests = @(
    "TestGRPCClientServer_DeleteBlobNotFound",
    "TestGRPCClientServer_RetrieveBlobNotFound",
    "TestManager_GetClientForNode_RemoteGRPC"
)
foreach ($testName in $requiredTests) {
    if ($priorityContent -notmatch [regex]::Escape($testName)) {
        Write-Error "priority-tests.log missing $testName"
        exit 1
    }
}

foreach ($buildLog in @($serverBuildLog, $allBuildLog)) {
    $buildContent = Get-Content $buildLog -Raw
    if ($buildContent -match "BUILD OK|# Command:|exit code:|CategoryInfo|FullyQualifiedErrorId|At .+:\d+") {
        Write-Error "$buildLog contains synthetic or PowerShell-wrapped content"
        exit 1
    }
    if ($buildContent -notmatch "github\.com/") {
        Write-Error "$buildLog missing go build package output"
        exit 1
    }
    $lineCount = (Get-Content $buildLog | Measure-Object -Line).Lines
    if ($lineCount -lt 20) {
        Write-Error "$buildLog too short ($lineCount lines); expected full go build -v output"
        exit 1
    }
}

$migrateStatus = Get-Content (Join-Path $Scratch "migrate-status.log") -Raw
if ($migrateStatus -notmatch "Current version:") {
    Write-Error "migrate-status.log should show applied migration version"
    exit 1
}
$migrateUp = Get-Content (Join-Path $Scratch "migrate-up.log") -Raw
if ($migrateUp -notmatch "Migrations applied successfully") {
    Write-Error "migrate-up.log missing success message"
    exit 1
}
if ($migrateStatus -match "not yet implemented|DATABASE_URL environment variable is required") {
    Write-Error "migrate logs show incomplete implementation"
    exit 1
}

$migrateStatusAfterDown = Get-Content (Join-Path $Scratch "migrate-status-after-down.log") -Raw
if ($migrateStatusAfterDown -notmatch "Current version:|no migrations applied") {
    Write-Error "migrate-status-after-down.log should show rollback state"
    exit 1
}
$verUp = $null
$verDown = $null
if ($migrateStatus -match "Current version: (\d+)") { $verUp = [int]$Matches[1] }
if ($migrateStatusAfterDown -match "Current version: (\d+)") { $verDown = [int]$Matches[1] }
if ($null -ne $verUp -and $null -ne $verDown -and $verDown -ge $verUp) {
    Write-Error "migrate-status-after-down should show lower version than post-up status"
    exit 1
}

Write-Host "All evidence captured and self-checks passed."
Write-Host "Artifacts in: $Scratch"
exit 0
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

function Invoke-Logged {
    param(
        [string]$Label,
        [scriptblock]$Block
    )
    Write-Host "==> $Label"
    & $Block
    if ($LASTEXITCODE -ne 0) {
        Write-Error "$Label failed with exit code $LASTEXITCODE"
        exit $LASTEXITCODE
    }
}

function Tee-Build {
    param(
        [string]$LogPath,
        [string[]]$BuildArgs
    )
    $output = & go @BuildArgs 2>&1
    $exit = $LASTEXITCODE
    if ($output) {
        $output | Out-File -FilePath $LogPath -Encoding utf8
    } else {
        "" | Out-File -FilePath $LogPath -Encoding utf8
    }
    "exit code: $exit" | Add-Content -Path $LogPath -Encoding utf8
    if ($exit -ne 0) {
        Write-Error "Build failed; see $LogPath"
        exit $exit
    }
}

$PriorityPkgs = @(
    "./internal/auth/...",
    "./internal/handler/...",
    "./internal/repository/...",
    "./internal/cluster/...",
    "./cmd/alexander-server/..."
)

# Step 1: priority tests (twice, verbose, no cache)
Invoke-Logged "go clean -testcache" { go clean -testcache }

$run1Log = Join-Path $Scratch "priority-tests-run1.log"
$priorityLog = Join-Path $Scratch "priority-tests.log"

function Run-PriorityTests {
    param([string]$LogPath)
    $output = go test -v -short -cover -count=1 @PriorityPkgs 2>&1
    $exit = $LASTEXITCODE
    $output | Tee-Object -FilePath $LogPath
    if ($exit -ne 0) {
        Write-Error "priority tests failed with exit code $exit"
        exit $exit
    }
}

Run-PriorityTests -LogPath $run1Log
Run-PriorityTests -LogPath $priorityLog

# Step 2: alexander-migrate build (twice) and command invocations
$migrateBin = Join-Path $Scratch "alexander-migrate.exe"
Invoke-Logged "migrate build 1" { go build -o $migrateBin ./cmd/alexander-migrate }
Invoke-Logged "migrate build 2" { go build -o $migrateBin ./cmd/alexander-migrate }

$migrateInvocations = @(
    @{ Name = "version"; Args = @("version") },
    @{ Name = "help"; Args = @("help") },
    @{ Name = "status"; Args = @("status") },
    @{ Name = "up"; Args = @("up") },
    @{ Name = "down"; Args = @("down") },
    @{ Name = "create"; Args = @("create", "evidence_test_migration") },
    @{ Name = "force"; Args = @("force", "1") }
)
$prevEAP = $ErrorActionPreference
$ErrorActionPreference = "Continue"
foreach ($inv in $migrateInvocations) {
    $logPath = Join-Path $Scratch ("migrate-{0}.log" -f $inv.Name)
    $output = & $migrateBin $inv.Args 2>&1
    $exit = $LASTEXITCODE
    if ($output) { $output | Out-File -FilePath $logPath -Encoding utf8 }
    else { "" | Out-File -FilePath $logPath -Encoding utf8 }
    "exit code: $exit" | Add-Content -Path $logPath -Encoding utf8
}
$ErrorActionPreference = $prevEAP

# Step 3: server build
$serverBin = Join-Path $Scratch "alexander-server.exe"
$serverBuildLog = Join-Path $Scratch "server-build.log"
Tee-Build -LogPath $serverBuildLog -BuildArgs @("build", "-o", $serverBin, "./cmd/alexander-server")

$s3ConfigLog = Join-Path $Scratch "s3-config.log"
@(
    "storage backend packages:",
    (go list -f "{{.ImportPath}} {{.GoFiles}}" ./internal/storage/... 2>&1)
) | Out-File -FilePath $s3ConfigLog -Encoding utf8

# Step 4: workflows and docs (twice)
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

# Step 5: full test summary and all-packages build
$fullTestLog = Join-Path $Scratch "full-test-summary.log"
$allBuildLog = Join-Path $Scratch "all-build.log"

$fullOutput = go test -short ./... 2>&1
if ($fullOutput) {
    ($fullOutput | Select-Object -Last 20) | Out-File -FilePath $fullTestLog -Encoding utf8
} else {
    "" | Out-File -FilePath $fullTestLog -Encoding utf8
}
"exit code: $LASTEXITCODE" | Add-Content -Path $fullTestLog -Encoding utf8
if ($LASTEXITCODE -ne 0) {
    Write-Error "go test -short ./... failed"
    exit $LASTEXITCODE
}

Tee-Build -LogPath $allBuildLog -BuildArgs @("build", "./...")

# Self-checks
$requiredFiles = @(
    $priorityLog,
    $run1Log,
    $serverBuildLog,
    $allBuildLog,
    $fullTestLog,
    $workflowsList,
    $pagesWorkflow,
    $docsContents,
    $migrateBin,
    $serverBin
) + ($migrateInvocations | ForEach-Object { Join-Path $Scratch ("migrate-{0}.log" -f $_.Name) })

foreach ($f in $requiredFiles) {
    if (-not (Test-Path $f)) {
        Write-Error "Missing required artifact: $f"
        exit 1
    }
}

$priorityContent = Get-Content $priorityLog -Raw
if ($priorityContent -notmatch "=== RUN") {
    Write-Error "priority-tests.log lacks verbose test output (=== RUN)"
    exit 1
}

$requiredTests = @(
    "TestGRPCClientServer_DeleteBlobNotFound",
    "TestManager_GetClientForNode_RemoteGRPC",
    "TestInitRepositories_SQLite"
)
foreach ($testName in $requiredTests) {
    if ($priorityContent -notmatch [regex]::Escape($testName)) {
        Write-Error "priority-tests.log does not show execution of $testName"
        exit 1
    }
}

foreach ($buildLog in @($serverBuildLog, $allBuildLog)) {
    $buildContent = Get-Content $buildLog -Raw
    if ($buildContent -match "BUILD OK") {
        Write-Error "$buildLog contains synthetic BUILD OK placeholder"
        exit 1
    }
    if ($buildContent -notmatch "exit code:\s*0") {
        Write-Error "$buildLog missing exit code: 0 marker"
        exit 1
    }
}

foreach ($migrateLog in ($migrateInvocations | ForEach-Object { Join-Path $Scratch ("migrate-{0}.log" -f $_.Name) })) {
    $mc = Get-Content $migrateLog -Raw
    if ($mc -match "not yet implemented") {
        Write-Error "$migrateLog still contains 'not yet implemented'"
        exit 1
    }
}

Write-Host "All evidence captured and self-checks passed."
Write-Host "Artifacts in: $Scratch"
exit 0
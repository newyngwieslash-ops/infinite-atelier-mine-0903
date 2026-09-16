$ErrorActionPreference = "Stop"

$RootDir = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$WebDir = Join-Path $RootDir "web"
$EmbedPlaceholder = Join-Path $WebDir "dist\.gitkeep"

function Restore-EmbedPlaceholder {
    $EmbedDir = Split-Path -Parent $EmbedPlaceholder
    New-Item -ItemType Directory -Path $EmbedDir -Force | Out-Null
    [System.IO.File]::WriteAllBytes($EmbedPlaceholder, [byte[]]@())
}

trap {
    Restore-EmbedPlaceholder
    throw $_
}

function Invoke-Step {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][scriptblock]$Command
    )

    Write-Host "`n==> $Name"
    & $Command
    if ($LASTEXITCODE -ne 0) {
        throw "$Name failed with exit code $LASTEXITCODE"
    }
}

if (-not (Get-Command node -ErrorAction SilentlyContinue)) { throw "node is required" }
if (-not (Get-Command npm.cmd -ErrorAction SilentlyContinue)) { throw "npm is required" }
if (-not (Test-Path (Join-Path $WebDir "package.json"))) { throw "web/package.json is missing" }
if (-not (Test-Path (Join-Path $WebDir "package-lock.json"))) { throw "web/package-lock.json is missing" }
if (-not (Test-Path (Join-Path $WebDir "node_modules"))) {
    throw "web dependencies are missing; follow README installation instructions first"
}

Write-Host "workspace: $RootDir"
Write-Host "node: $(node --version)"
Write-Host "npm: $(npm.cmd --version)"

Push-Location $WebDir
try {
    $Scripts = (Get-Content "package.json" -Raw | ConvertFrom-Json).scripts

    Invoke-Step "frontend typecheck" { npm.cmd run typecheck }

    if ($Scripts.PSObject.Properties.Name -contains "test") {
        Invoke-Step "frontend tests" { npm.cmd test }
    } else {
        Write-Host "`nSKIP: frontend tests — web/package.json has no test script."
    }

    if ($Scripts.PSObject.Properties.Name -contains "lint") {
        Invoke-Step "frontend lint" { npm.cmd run lint }
    } else {
        Write-Host "SKIP: frontend lint — web/package.json has no lint script."
    }

    Invoke-Step "frontend production build" { npm.cmd run build }

    $MonoformDir = Join-Path $WebDir "monoform-studio"
    if ((Test-Path (Join-Path $MonoformDir "package.json")) -and (Test-Path (Join-Path $MonoformDir "node_modules"))) {
        Invoke-Step "MONOFORM source build" { npm.cmd --prefix $MonoformDir run build }
    } else {
        Write-Host "SKIP: MONOFORM source build — its separate node_modules is absent; the main build uses tracked web/public/monoform output."
    }
} finally {
    Pop-Location
}

if (Test-Path (Join-Path $RootDir "go.mod")) {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw "go.mod exists but go is unavailable" }
    Push-Location $RootDir
    try {
        Invoke-Step "Go tests" { go test ./... -count=1 }
        Invoke-Step "Go vet" { go vet ./... }
    } finally {
        Pop-Location
    }
} else {
    Write-Host "SKIP: Go tests and vet — go.mod is unavailable."
}

if (Get-Command wails -ErrorAction SilentlyContinue) {
    Push-Location $RootDir
    try {
        Invoke-Step "Wails production build" { wails build }
    } finally {
        Pop-Location
    }
} else {
    Write-Host "SKIP: Wails production build — install pinned CLI v2.15.0 with: go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0. Production build evidence is required for the desktop acceptance gates."
}

$SecurityScan = Join-Path $RootDir "scripts\security-scan.mjs"
if (Test-Path $SecurityScan) {
    Push-Location $RootDir
    try {
        Invoke-Step "security scans (dynamic execution, secrets, persistence, direct calls)" { node scripts/security-scan.mjs }
    } finally {
        Pop-Location
    }
} else {
    Write-Host "SKIP: security scans — scripts/security-scan.mjs is missing."
}

Restore-EmbedPlaceholder
Write-Host "`nPASS: available verification gates completed."

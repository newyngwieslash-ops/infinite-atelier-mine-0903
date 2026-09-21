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

    # The canvas regression (AC-CANVAS-004) runs against the dev server and needs a
    # browser Playwright can launch. It is skipped with an explicit reason when
    # either is missing, never silently passed.
    if ($Scripts.PSObject.Properties.Name -contains "test:e2e") {
        if (Test-Path "node_modules/@playwright/test") {
            Invoke-Step "canvas regression (AC-CANVAS-004)" { npm.cmd run test:e2e }
        } else {
            Write-Host "SKIP: canvas regression - @playwright/test is absent from node_modules; run npm install."
        }
    } else {
        Write-Host "SKIP: canvas regression - web/package.json has no test:e2e script."
    }

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

# Both fixture generators can regenerate what they produced, so a fixture that
# drifted from its generator — or a generator edited without regenerating — is
# detectable. They support --check for exactly this, and without a gate that check
# exists and never runs, which is how a generated fixture silently stops matching
# the code that reads it.
$CanaryGen = Join-Path $RootDir "scripts\gen-canary-fixture.mjs"
if (Test-Path $CanaryGen) {
    Push-Location $RootDir
    try {
        Invoke-Step "canary fixture is current" { node scripts/gen-canary-fixture.mjs --check }
    } finally {
        Pop-Location
    }
} else {
    Write-Host "SKIP: canary fixture check — the generator is missing."
}

$HostileGen = Join-Path $RootDir "scripts\gen-malicious-fixtures.mjs"
if (Test-Path $HostileGen) {
    Push-Location $RootDir
    try {
        Invoke-Step "hostile-input fixtures are current" { node scripts/gen-malicious-fixtures.mjs --check }
    } finally {
        Pop-Location
    }
} else {
    Write-Host "SKIP: hostile-input fixture check — the generator is missing."
}

$ToolSchemaGen = Join-Path $RootDir "scripts\gen-tool-schemas.mjs"
if (Test-Path $ToolSchemaGen) {
    Push-Location $RootDir
    try {
        Invoke-Step "tool schemas are current" { node scripts/gen-tool-schemas.mjs --check }
    } finally {
        Pop-Location
    }
} else {
    Write-Host "SKIP: tool schema check — the generator is missing."
}

# The skill packs' check has two halves since WP-08: a MANIFEST is generated and must match the table
# byte for byte, while a skill DOCUMENT is authored and must carry section 4.3's thirteen headings. The
# distinction is the generator's own — see its comment. Without this step a manifest edited by hand
# would silently diverge from the key list the build loads.
$SkillPackGen = Join-Path $RootDir "scripts\gen-skill-packs.mjs"
if (Test-Path $SkillPackGen) {
    Push-Location $RootDir
    try {
        Invoke-Step "skill packs are current" { node scripts/gen-skill-packs.mjs --check }
    } finally {
        Pop-Location
    }
} else {
    Write-Host "SKIP: skill pack check — the generator is missing."
}

Restore-EmbedPlaceholder
Write-Host "`nPASS: available verification gates completed."

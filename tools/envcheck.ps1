# DecisionCourt env check (ASCII-only output)
# Checks .env:
#   1. No duplicate keys
#   2. Required keys all non-empty
#   3. DATABASE_URL uses docker service host (not localhost)
#   4. SEARCH_PROVIDER in whitelist
#   5. BOCHA_API_KEY set when SEARCH_PROVIDER=bocha
#
# Usage: powershell -ExecutionPolicy Bypass -File tools\envcheck.ps1
#
# Exit codes:
#   0 = pass
#   1 = missing/invalid required key
#   2 = duplicate keys
#   3 = DATABASE_URL uses localhost

$ErrorActionPreference = "Stop"
$envFile = Join-Path $PSScriptRoot "..\.env"
$envFile = (Resolve-Path $envFile).Path

if (-not (Test-Path $envFile)) {
    Write-Host "[FAIL] .env not found: $envFile"
    exit 1
}

$required = @(
    "LLM_API_KEY",
    "BOCHA_API_KEY",
    "JWT_SECRET",
    "POSTGRES_PASSWORD",
    "DATABASE_URL",
    "REDIS_URL",
    "SEARCH_PROVIDER"
)
$placeholder = @{
    "LLM_API_KEY"   = @("sk-xxx", "sk-CHANGEME", "")
    "BOCHA_API_KEY" = @("sk-bocha-xxx", "")
    "JWT_SECRET"    = @("", "CHANGEME")
}

$values = [ordered]@{}
$seen   = @{}
$dupes  = @()

Get-Content $envFile | ForEach-Object {
    $line = $_.Trim()
    if ($line -eq "" -or $line -like "#*") { return }
    if ($line -like 'export *') { $line = $line.Substring(7).Trim() }
    $idx = $line.IndexOf('=')
    if ($idx -lt 0) { return }
    $key = $line.Substring(0, $idx).Trim()
    $val = $line.Substring($idx + 1).Trim()
    if ($val.Length -ge 2) {
        if (($val.StartsWith('"') -and $val.EndsWith('"')) -or `
            ($val.StartsWith("'") -and $val.EndsWith("'"))) {
            $val = $val.Substring(1, $val.Length - 2)
        }
    }
    if ($seen.ContainsKey($key)) {
        $dupes += $key
    } else {
        $seen[$key] = $true
    }
    $values[$key] = $val
}

$exitCode = 0

if ($dupes.Count -gt 0) {
    Write-Host "[FAIL] Duplicate keys (docker compose reads first one, overrides are silently dropped):"
    $dupes | ForEach-Object { Write-Host "        $_" }
    $exitCode = 2
} else {
    Write-Host "[OK]   No duplicate keys"
}

foreach ($k in $required) {
    $ok = $true
    $reason = ""
    if (-not $values.Contains($k)) {
        $ok = $false; $reason = "missing"
    } elseif ([string]::IsNullOrWhiteSpace($values[$k])) {
        $ok = $false; $reason = "empty"
    } elseif ($placeholder.ContainsKey($k) -and $placeholder[$k] -contains $values[$k]) {
        $ok = $false; $reason = "placeholder"
    }
    if (-not $ok) {
        Write-Host "[FAIL] $k = <$reason>"
        $exitCode = 1
    } else {
        $preview = $values[$k].Substring(0, [Math]::Min(10, $values[$k].Length)) + "..."
        Write-Host "[OK]   $k = $preview"
    }
}

if ($values.Contains("DATABASE_URL")) {
    $du = $values["DATABASE_URL"]
    if ($du -match "@(localhost|127\.0\.0\.1)[:/]") {
        Write-Host "[WARN] DATABASE_URL uses localhost — backend container cannot reach host localhost"
        if ($exitCode -eq 0) { $exitCode = 3 }
    } elseif ($du -notmatch "@postgres[:/]") {
        Write-Host "[WARN] DATABASE_URL host is not docker service 'postgres' (dev / sqlite?)"
    } else {
        Write-Host "[OK]   DATABASE_URL uses docker service 'postgres'"
    }
}

$allowedProviders = @("mock", "bocha", "tavily")
if ($values.Contains("SEARCH_PROVIDER")) {
    $sp = $values["SEARCH_PROVIDER"].ToLower()
    if ($allowedProviders -notcontains $sp) {
        Write-Host "[FAIL] SEARCH_PROVIDER='$sp' not in whitelist ($($allowedProviders -join ', '))"
        $exitCode = 1
    } else {
        Write-Host "[OK]   SEARCH_PROVIDER=$sp"
    }
}

if ($values.Contains("SEARCH_PROVIDER") -and $values["SEARCH_PROVIDER"].ToLower() -eq "bocha") {
    if ($values.Contains("BOCHA_API_KEY") -and -not [string]::IsNullOrWhiteSpace($values["BOCHA_API_KEY"])) {
        Write-Host "[OK]   SEARCH_PROVIDER=bocha + BOCHA_API_KEY set"
    } else {
        Write-Host "[FAIL] SEARCH_PROVIDER=bocha but BOCHA_API_KEY is missing/empty"
        $exitCode = 1
    }
}

if ($exitCode -eq 0) {
    Write-Host ""
    Write-Host "[PASS] .env validation passed"
} else {
    Write-Host ""
    Write-Host "[FAIL] .env validation failed (exit code $exitCode)"
}
exit $exitCode

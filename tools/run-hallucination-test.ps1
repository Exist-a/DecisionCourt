# v0.10.1 (ADR 0021) Hallucination rate verification test
# Runs N real trials, waits for opening, computes hallucination rate
#
# Usage: powershell -ExecutionPolicy Bypass -File tools/run-hallucination-test.ps1 -N 5

param(
    [int]$N = 5,
    [int]$WaitSeconds = 30
)

$ErrorActionPreference = "Stop"

# 1. Get token
$anonResp = Invoke-RestMethod -Uri "http://localhost:8080/api/v1/auth/anon" `
    -Method POST -Headers @{"Content-Type"="application/json"; "Origin"="http://localhost:3000"} `
    -Body '{"user_id":"halluc-test-001"}'
$token = $anonResp.data.token

Write-Host "==== Running $N hallucination test sessions ====" -ForegroundColor Cyan

# 2. Different cases (diverse)
$cases = @(
    @{
        title = "Contract-refund"
        option_a = "Refund"
        option_b = "Continue"
        context = "Plaintiff signed up for IT training (30000 yuan), wants refund due to course mismatch. Defendant refuses."
    },
    @{
        title = "Divorce-property"
        option_a = "Agreement divorce"
        option_b = "Litigation divorce"
        context = "Married 8 years with 6yo child. Joint property: house (5M) + savings 1M."
    },
    @{
        title = "Labor-firing"
        option_a = "Wrongful firing, compensation"
        option_b = "Lawful firing"
        context = "Employee Li worked 3 years at 15000/month. Suddenly fired for 'serious violation', no details given."
    },
    @{
        title = "Traffic-accident"
        option_a = "Full liability, 500k"
        option_b = "Main-secondary, 300k"
        context = "Car A hit Car B. Police says A is fully liable. A argues B had fault (no safe distance)."
    },
    @{
        title = "IP-infringement"
        option_a = "Infringement, pay 2M"
        option_b = "No infringement"
        context = "Company A claims B copied its software UI, demands 2M. B says independent development."
    }
)

$results = @()
$hallucinationCount = 0
$totalMessages = 0

for ($i = 0; $i -lt $N; $i++) {
    $case = $cases[$i % $cases.Count]
    Write-Host ""
    Write-Host "[$($i+1)/$N] $($case.title) ..." -ForegroundColor Yellow

    try {
        # Create courtroom
        $body = ConvertTo-Json -Depth 10 $case
        $createResp = Invoke-RestMethod -Uri "http://localhost:8080/api/v1/courtrooms" `
            -Method POST -Headers @{"Content-Type"="application/json"; "Origin"="http://localhost:3000"; "Authorization"="Bearer $token"} `
            -Body $body
        $session = $createResp.data.session_uuid
        Write-Host "  session: $session"

        # Start trial
        Invoke-RestMethod -Uri "http://localhost:8080/api/v1/courtrooms/$session/start" `
            -Method POST -Headers @{"Content-Type"="application/json"; "Origin"="http://localhost:3000"; "Authorization"="Bearer $token"} `
            -Body '{}' | Out-Null

        Write-Host "  started, waiting ${WaitSeconds}s for opening..." -ForegroundColor Gray
        Start-Sleep -Seconds $WaitSeconds

        # Get messages
        $resp = Invoke-RestMethod -Uri "http://localhost:8080/api/v1/courtrooms/$session/messages" `
            -Method GET -Headers @{"Origin"="http://localhost:3000"; "Authorization"="Bearer $token"}

        $msgs = $resp.data.messages
        $msgCount = $msgs.Count
        Write-Host "  message count: $msgCount" -ForegroundColor Gray

        # Detect hallucination
        $hallucinated = 0
        foreach ($m in $msgs) {
            $content = $m.content
            $refs = $m.evidence_refs

            # Patterns (ASCII version of chinese ones via unicode escapes)
            # Use simpler regex that works for ASCII input
            $hasEvidenceRef = $content -match "evidence_ref|附件|Evidence[ ]*\d+"
            $hasCaseNum = $content -match "\(\d{4}\)\S*\d+|[\(\uff08]\d{4}[\)\uff09]\S*\d+\u53f7"
            $hasPercent = $content -match "\d+(\.\d+)?\s*%"
            $hasMoney = $content -match "\d+(\.\d+)?\s*(yuan|wan yuan|yuan yuan|10K|M yuan)"

            # Chinese pattern detection via direct substring (more reliable)
            $hasZhEvidence = $content -match "\u8bc1\u636e[\u4e00\u4e8c\u4e09\u56db\u4e94\u516d\u4e03\u516b\u4e5d\u5341\d]+|\u9644\u4ef6\s*\d+"
            $hasZhCase = $content -match "[\uff08\(]\d{4}[\uff09\)]\S*\d+\u53f7"
            $hasZhPercent = $content -match "\d+(\.\d+)?\s*%"
            $hasZhMoney = $content -match "\d+(\.\d+)?\s*(\u5143|\u4e07\u5143|\u4ebf\u5143|\u5757\u94b1|\u767e\u4e07\u5143)"

            $isHallucination = $false
            $reasons = @()
            if ($refs.Count -eq 0) {
                if ($hasZhEvidence) { $isHallucination = $true; $reasons += "evidence_ref_empty_with_citation" }
                if ($hasZhCase) { $isHallucination = $true; $reasons += "evidence_ref_empty_with_case_num" }
                if ($hasZhPercent) { $isHallucination = $true; $reasons += "evidence_ref_empty_with_stats" }
                if ($hasZhMoney) { $isHallucination = $true; $reasons += "evidence_ref_empty_with_stats" }
            }

            if ($isHallucination) {
                $hallucinated++
                Write-Host "    HALLUCINATION in $($m.role): $($reasons -join ', ')" -ForegroundColor Red
                $preview = if ($content.Length -gt 80) { $content.Substring(0, 80) + "..." } else { $content }
                Write-Host "    preview: $preview" -ForegroundColor Red
            }
        }

        $totalMessages += $msgCount
        $hallucinationCount += $hallucinated

        $results += [PSCustomObject]@{
            Session = $session
            Title = $case.title
            MsgCount = $msgCount
            Hallucinated = $hallucinated
        }
    } catch {
        Write-Host "  ERROR: $_" -ForegroundColor Red
    }
}

Write-Host ""
Write-Host "==== Summary ====" -ForegroundColor Cyan
Write-Host "Total sessions: $N"
Write-Host "Total messages: $totalMessages"
Write-Host "Hallucinated: $hallucinationCount"
$rate = if ($totalMessages -gt 0) { [Math]::Round(($hallucinationCount / $totalMessages) * 100, 1) } else { 0 }
$color = if ($rate -lt 30) { "Green" } elseif ($rate -lt 60) { "Yellow" } else { "Red" }
Write-Host "Hallucination rate: $rate%" -ForegroundColor $color

Write-Host ""
Write-Host "==== Per-session breakdown ===="
$results | Format-Table -AutoSize
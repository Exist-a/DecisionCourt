# Extended hallucination test: 2 sessions x 4 min each, drives past opening
# into cross_exam phase where LLM has higher pressure to hallucinate

param(
    [int]$N = 2,
    [int]$WaitSeconds = 240
)

$ErrorActionPreference = "Stop"

$token = (Invoke-RestMethod -Uri "http://localhost:8080/api/v1/auth/anon" `
    -Method POST -Headers @{"Content-Type"="application/json"; "Origin"="http://localhost:3000"} `
    -Body '{"user_id":"halluc-test-002"}').data.token

Write-Host "==== Extended hallucination test: $N sessions x ${WaitSeconds}s ====" -ForegroundColor Cyan

$cases = @(
    @{
        title = "Contract-refund-EX"
        option_a = "Full refund"
        option_b = "No refund"
        context = "Plaintiff paid 50000 yuan for online training course. After 1 month realized course content does not match promotional materials. Asks for full refund. Defendant refuses, claims course quality is acceptable."
    },
    @{
        title = "Property-dispute-EX"
        option_a = "Plaintiff owns"
        option_b = "Defendant owns"
        context = "House purchased under both names, total 3M yuan. Plaintiff contributed 2.5M, defendant 500K. After breakup, plaintiff claims sole ownership based on contribution."
    }
)

$totalMessages = 0
$totalHallucinated = 0
$perSessionResults = @()

for ($i = 0; $i -lt $N; $i++) {
    $case = $cases[$i % $cases.Count]
    Write-Host ""
    Write-Host "[$($i+1)/$N] $($case.title) - waiting ${WaitSeconds}s ..." -ForegroundColor Yellow

    $session = (Invoke-RestMethod -Uri "http://localhost:8080/api/v1/courtrooms" `
        -Method POST -Headers @{"Content-Type"="application/json"; "Origin"="http://localhost:3000"; "Authorization"="Bearer $token"} `
        -Body (ConvertTo-Json -Depth 5 $case)).data.session_uuid
    Write-Host "  session: $session"

    Invoke-RestMethod -Uri "http://localhost:8080/api/v1/courtrooms/$session/start" `
        -Method POST -Headers @{"Content-Type"="application/json"; "Origin"="http://localhost:3000"; "Authorization"="Bearer $token"} `
        -Body '{}' | Out-Null

    # 分段等待,期间不打扰
    Start-Sleep -Seconds $WaitSeconds

    # Pull all messages
    $resp = Invoke-RestMethod -Uri "http://localhost:8080/api/v1/courtrooms/$session/messages" `
        -Method GET -Headers @{"Origin"="http://localhost:3000"; "Authorization"="Bearer $token"}

    $msgs = $resp.data.messages
    $msgCount = $msgs.Count
    Write-Host "  message count: $msgCount"

    $hallucinated = 0
    $perPhase = @{}
    foreach ($m in $msgs) {
        $content = $m.content
        $refs = $m.evidence_refs
        $phase = $m.phase

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
            $phaseKey = "$phase"
            if (-not $perPhase.ContainsKey($phaseKey)) { $perPhase[$phaseKey] = 0 }
            $perPhase[$phaseKey]++

            $preview = if ($content.Length -gt 100) { $content.Substring(0, 100) + "..." } else { $content }
            Write-Host "    [HALLU] phase=$phase role=$($m.role): $($reasons -join ',')" -ForegroundColor Red
            Write-Host "      $preview" -ForegroundColor Red
        }
    }

    $totalMessages += $msgCount
    $totalHallucinated += $hallucinated

    $perSessionResults += [PSCustomObject]@{
        Session = $session
        Title = $case.title
        MsgCount = $msgCount
        Hallucinated = $hallucinated
        Phases = ($perPhase.Keys | ForEach-Object { "$_=$($perPhase[$_])" }) -join "; "
    }
}

Write-Host ""
Write-Host "==== Summary ====" -ForegroundColor Cyan
Write-Host "Total sessions: $N"
Write-Host "Total messages: $totalMessages"
Write-Host "Hallucinated: $totalHallucinated"
$rate = if ($totalMessages -gt 0) { [Math]::Round(($totalHallucinated / $totalMessages) * 100, 1) } else { 0 }
$color = if ($rate -lt 10) { "Green" } elseif ($rate -lt 30) { "Yellow" } else { "Red" }
Write-Host "Hallucination rate: $rate%" -ForegroundColor $color

Write-Host ""
Write-Host "==== Per-session ===="
$perSessionResults | Format-Table -AutoSize
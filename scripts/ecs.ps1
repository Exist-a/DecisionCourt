# DecisionCourt v0.9.2 ECS Docker context 快速切换 (PowerShell)
#
# 用法:
#   .\scripts\ecs.ps1              # 看当前 context + ECS 容器状态
#   .\scripts\ecs.ps1 ecs          # 切到 ECS context
#   .\scripts\ecs.ps1 local        # 切回本地 context
#   .\scripts\ecs.ps1 logs [name]  # 看 ECS 上容器 stdout 日志(默认 backend,走 docker logs)
#   .\scripts\ecs.ps1 logs-file [-Date YYYY-MM-DD] [-Lines N] [-Follow]
#                                  # 看宿主机的 file_logger 详细 JSON 日志(/opt/DecisionCourt/logs/backend/)
#                                  # 含 35+ 字段(token/cost/compression/throttle 等),适合深度分析
#   .\scripts\ecs.ps1 shell [name] # 进 ECS 上指定容器(默认 backend)
#   .\scripts\ecs.ps1 status       # 看 ECS 容器状态 + 健康
#   .\scripts\ecs.ps1 deploy       # 在 ECS 上拉+重启(等价 docker compose pull && up -d)
#   .\scripts\ecs.ps1 help         # 帮助

[CmdletBinding()]
param(
    [string]$Action = "",
    [string]$Name = "",
    [string]$Date = "",            # logs-file: 指定日期 (默认今天)
    [int]$Lines = 0,               # logs-file: 看最近 N 行 (默认实时跟踪)
    [switch]$Follow                # logs-file: 强制实时跟踪(覆盖 -Lines)
)

$ErrorActionPreference = "Continue"
$ECS_HOST = "47.239.152.177"   # 你的 ECS 公网 IP

function Write-Step($msg) { Write-Host "`n==> $msg" -ForegroundColor Cyan }
function Write-OK($msg)   { Write-Host "    OK $msg" -ForegroundColor Green }
function Write-Warn($msg) { Write-Host "    WARN $msg" -ForegroundColor Yellow }
function Write-Err($msg)  { Write-Host "    ERROR $msg" -ForegroundColor Red }

function Get-CurrentContext {
    $ctx = (docker context ls --format "{{.Name}}|{{.Current}}" | Where-Object { $_ -match "true$" }) -replace "\|true$", ""
    return $ctx
}

function Use-ECS {
    $current = Get-CurrentContext
    if ($current -eq "ecs") {
        Write-Warn "已经在 ECS context"
        return
    }
    docker context use ecs 2>&1 | Out-Null
    if ($LASTEXITCODE -eq 0) {
        Write-OK "已切换到 ECS context"
        Write-Host "    提示:所有 docker 命令现在走 ECS" -ForegroundColor Gray
        Write-Host "    切回本地: .\scripts\ecs.ps1 local" -ForegroundColor Gray
    } else {
        Write-Err "切换失败,先跑: docker context create ecs --docker `"host=ssh://admin@$ECS_HOST`""
    }
}

function Use-Local {
    $current = Get-CurrentContext
    if ($current -eq "default") {
        Write-Warn "已经在本地 context"
        return
    }
    docker context use default 2>&1 | Out-Null
    if ($LASTEXITCODE -eq 0) {
        Write-OK "已切换回本地 context"
    } else {
        Write-Err "切换失败"
    }
}

function Show-Status {
    Write-Step "当前 Docker context"
    docker context ls | Out-Host

    $current = Get-CurrentContext
    if ($current -eq "ecs") {
        Write-Step "ECS 容器状态"
        docker ps --format "table {{.Names}}`t{{.Image}}`t{{.Status}}`t{{.Ports}}" | Out-Host
    } elseif ($current -eq "default") {
        Write-Step "本地容器状态"
        docker ps --format "table {{.Names}}`t{{.Image}}`t{{.Status}}`t{{.Ports}}" | Out-Host
    }
}

function Show-Logs {
    Use-ECS  # 自动切到 ECS
    if (-not $Name) { $Name = "backend" }
    $container = "dc_$Name"
    Write-Step "实时跟踪 $container 日志(按 Ctrl+C 退出)"
    Write-Host "    过滤 LLM: ./scripts/ecs.ps1 logs | Select-String [LLM]" -ForegroundColor Gray
    docker logs $container -f
}

# v0.9.2 新增: 看宿主机的 file_logger 文件日志(/opt/DecisionCourt/logs/backend/)
#  vs Show-Logs: Show-Logs 看 docker stdout (slog 简短输出)
#                 Show-FileLogs 看 file_logger 详细 JSON (35+ 字段,token/cost/compression/throttle)
function Show-FileLogs {
    Use-ECS
    $date = if ($Date) { $Date } else { (Get-Date -Format "yyyy-MM-dd") }
    $logPath = "/opt/DecisionCourt/logs/backend/agent_gateway_${date}.log"

    # 先检查文件是否存在
    $check = ssh admin@$ECS_HOST "test -f $logPath && echo EXISTS || echo MISSING"
    if ($check -match "MISSING") {
        Write-Warn "$logPath 不存在"
        Write-Host "    可能: 今天还没有 trial 触发 LLM 调用,或宿主机 logs 目录未创建" -ForegroundColor Gray
        Write-Host "    备选: 改看 stdout 日志  ./scripts/ecs.ps1 logs backend" -ForegroundColor Gray
        return
    }

    Write-Step "实时跟踪 $logPath(按 Ctrl+C 退出)"
    Write-Host "    看某次 trial: 加 -Date 2026-07-05  -Lines 200" -ForegroundColor Gray
    Write-Host "    只看 LLM:    ./scripts/ecs.ps1 logs-file | Select-String provider" -ForegroundColor Gray
    Write-Host "    只看 token:  ./scripts/ecs.ps1 logs-file | Select-String tokens" -ForegroundColor Gray

    if ($Follow -or (-not $PSBoundParameters.ContainsKey('Lines'))) {
        # 默认实时跟踪
        ssh admin@$ECS_HOST "tail -f $logPath"
    } else {
        # 看最近 N 行(非实时)
        ssh admin@$ECS_HOST "tail -n $Lines $logPath"
    }
}

function Enter-Shell {
    Use-ECS
    if (-not $Name) { $Name = "backend" }
    $container = "dc_$Name"
    Write-Step "进入 $container 容器(输 exit 退出)"
    docker exec -it $container sh
}

function Deploy-ECS {
    [CmdletBinding()]
    param(
        [switch]$SkipImagePull   # 跳过 docker compose pull(只改 compose 时用)
    )
    Use-ECS
    Write-Step "scp docker-compose.yml 到 ECS + 重启"

    # 1) 备份 ECS 上现有 compose
    ssh admin@$ECS_HOST "cp -p /opt/DecisionCourt/docker-compose.yml /opt/DecisionCourt/docker-compose.yml.bak" 2>&1 | Out-Null

    # 2) scp 本地 compose 到 ECS(只这一个文件)
    scp "d:\源码\FullStack\DecisionCourt\docker-compose.yml" "admin@${ECS_HOST}:/opt/DecisionCourt/docker-compose.yml"
    if ($LASTEXITCODE -ne 0) { Write-Err "scp 失败"; return }

    # 3) (可选)拉最新镜像 — 改了 backend/frontend 代码时跑这个
    if (-not $SkipImagePull) {
        Write-Step "拉最新镜像"
        ssh admin@$ECS_HOST "cd /opt/DecisionCourt && docker compose pull"
        if ($LASTEXITCODE -ne 0) { Write-Err "pull 失败"; return }
    }

    # 4) 重启所有变化的容器(detected by docker compose 自动对比 image digest)
    #    改用不带 service name 的 up -d,docker 会自动检测哪些服务需要 recreate
    #    (postgres/redis/caddy 这些基础服务没重新 pull,不会重启)
    Write-Step "重启所有变化的容器"
    ssh admin@$ECS_HOST "cd /opt/DecisionCourt && docker compose up -d"
    if ($LASTEXITCODE -ne 0) { Write-Err "重启失败"; return }

    Write-OK "部署完成"
    Write-Host "    看状态:    .\scripts\ecs.ps1 status" -ForegroundColor Gray
    Write-Host "    看 stdout:  .\scripts\ecs.ps1 logs backend" -ForegroundColor Gray
    Write-Host "    看 LLM 文件日志(JSON 详细):  .\scripts\ecs.ps1 logs-file" -ForegroundColor Gray
}

# ===== 主逻辑 =====
switch ($Action.ToLower()) {
    "" { Show-Status }
    "ecs" { Use-ECS }
    "local" { Use-Local }
    "logs" { Show-Logs }
    "log" { Show-Logs }
    "logs-file" { Show-FileLogs }
    "logfile" { Show-FileLogs }
    "llm-logs" { Show-FileLogs }
    "shell" { Enter-Shell }
    "sh" { Enter-Shell }
    "status" { Show-Status }
    "deploy" { Deploy-ECS }
    "help" {
        Get-Content $PSCommandPath | Select-String "^# " | ForEach-Object { $_ -replace "^# ?", "" }
    }
    default {
        Write-Err "未知操作: $Action"
        Write-Host "    用 .\scripts\ecs.ps1 help 看用法"
    }
}
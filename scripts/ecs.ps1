# DecisionCourt v0.9.2 ECS Docker context 快速切换 (PowerShell)
#
# 用法:
#   .\scripts\ecs.ps1              # 看当前 context + ECS 容器状态
#   .\scripts\ecs.ps1 ecs          # 切到 ECS context
#   .\scripts\ecs.ps1 local        # 切回本地 context
#   .\scripts\ecs.ps1 logs [name]  # 看 ECS 上指定容器日志(默认 backend)
#   .\scripts\ecs.ps1 shell [name] # 进 ECS 上指定容器(默认 backend)
#   .\scripts\ecs.ps1 status       # 看 ECS 容器状态 + 健康
#   .\scripts\ecs.ps1 deploy       # 在 ECS 上拉+重启(等价 docker compose pull && up -d)
#   .\scripts\ecs.ps1 help         # 帮助

[CmdletBinding()]
param(
    [string]$Action = "",
    [string]$Name = ""
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

function Enter-Shell {
    Use-ECS
    if (-not $Name) { $Name = "backend" }
    $container = "dc_$Name"
    Write-Step "进入 $container 容器(输 exit 退出)"
    docker exec -it $container sh
}

function Deploy-ECS {
    Use-ECS
    Write-Step "在 ECS 上拉新镜像 + 重启容器"
    docker compose -f /opt/DecisionCourt/docker-compose.yml pull
    if ($LASTEXITCODE -ne 0) { Write-Err "pull 失败"; return }
    docker compose -f /opt/DecisionCourt/docker-compose.yml up -d
    if ($LASTEXITCODE -ne 0) { Write-Err "up 失败"; return }
    Write-OK "部署完成"
    Write-Host "    看状态: .\scripts\ecs.ps1 status" -ForegroundColor Gray
}

# ===== 主逻辑 =====
switch ($Action.ToLower()) {
    "" { Show-Status }
    "ecs" { Use-ECS }
    "local" { Use-Local }
    "logs" { Show-Logs }
    "log" { Show-Logs }
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
# DecisionCourt v0.9.2 一键部署到 ECS (PowerShell)
#
# 用法:
#   .\scripts\deploy-to-ecs.ps1                       # 部署(默认 SSH 到 ecs,目录 /opt/decisioncourt)
#   .\scripts\deploy-to-ecs.ps1 -SshHost 1.2.3.4      # 直接用 IP
#   .\scripts\deploy-to-ecs.ps1 -RemotePath /home/dc/app
#   .\scripts\deploy-to-ecs.ps1 -TailLogs 30          # 部署完看最近 30 行日志
#   .\scripts\deploy-to-ecs.ps1 -DryRun               # 只打印要执行的命令,不实际跑
#
# 前置:
#   - 已经 ssh-keygen + ssh-copy-id 配置过免密登录(或 ssh-agent 里加了 key)
#   - ECS 上 /opt/decisioncourt 已 clone 仓库(或至少 docker-compose.yml 在那里)
#   - 已用 .\scripts\push-to-acr.ps1 把镜像推到 ACR
#
# 推荐 ssh 别名配置(可选,放 ~/.ssh/config):
#   Host ecs
#       HostName <你的 ECS 公网或内网 IP>
#       User root
#       IdentityFile ~/.ssh/your-key.pem

[CmdletBinding()]
param(
    [string]$SshHost = "ecs",                 # ~/.ssh/config 里的别名,或 IP
    [string]$RemotePath = "/opt/decisioncourt",
    [int]$TailLogs = 0,                       # 部署完想看几行日志(0 = 不看)
    [switch]$DryRun
)

$ErrorActionPreference = "Continue"

function Write-Step($msg) { Write-Host "`n==> $msg" -ForegroundColor Cyan }
function Write-OK($msg)   { Write-Host "    OK $msg" -ForegroundColor Green }
function Write-Warn($msg) { Write-Host "    WARN $msg" -ForegroundColor Yellow }
function Write-Err($msg)  { Write-Host "    ERROR $msg" -ForegroundColor Red }

# ============== 1. 前置检查 ==============
Write-Step "前置检查"
$sshTest = ssh -o BatchMode=yes -o ConnectTimeout=5 $SshHost "echo OK" 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Err "SSH 失败: $sshTest"
    Write-Host "    检查 ~/.ssh/config 里的 $SshHost 别名,或用 -SshHost 指定 IP" -ForegroundColor Yellow
    exit 1
}
Write-OK "SSH 到 $SshHost 通了"

$remoteFiles = ssh $SshHost "ls $RemotePath/docker-compose.yml 2>/dev/null && echo HAS_COMPOSE"
if ($LASTEXITCODE -ne 0 -or $remoteFiles -notmatch "HAS_COMPOSE") {
    Write-Err "ECS 上 $RemotePath 没有 docker-compose.yml"
    Write-Host "    确认目录对不对(用 -RemotePath 改)" -ForegroundColor Yellow
    exit 1
}
Write-OK "ECS 上找到 compose 文件"

# ============== 2. 检查 ACR 上 :latest 是不是新版本 ==============
Write-Step "对比本地和 ACR 的 :latest"
$localDigest = docker images --digests --format "{{.Repository}}:{{.Tag}}  {{.Digest}}" |
    Where-Object { $_ -match "decision-court-(backend|frontend):latest" } |
    ForEach-Object { ($_ -split '\s+')[1] }
Write-Host "    本地 backend digest:  $localDigest"

$remoteDigest = ssh $SshHost "docker images --digests --format '{{.Repository}}:{{.Tag}}  {{.Digest}}' | grep decision-court" 2>&1
Write-Host "    ECS 当前 digest:"
$remoteDigest | ForEach-Object { Write-Host "      $_" }

# ============== 3. 拉镜像 ==============
$composePull = "cd $RemotePath && docker compose pull"
Write-Step "在 ECS 上拉新镜像"
Write-Host "    ssh $SshHost '$composePull'" -ForegroundColor Gray
if ($DryRun) { exit 0 }

ssh $SshHost $composePull
if ($LASTEXITCODE -ne 0) { Write-Err "docker compose pull 失败"; exit 1 }
Write-OK "镜像已拉到 ECS"

# ============== 4. 重启容器 ==============
$composeUp = "cd $RemotePath && docker compose up -d"
Write-Step "在 ECS 上重启容器(只重启镜像或配置变了的)"
Write-Host "    ssh $SshHost '$composeUp'" -ForegroundColor Gray
ssh $SshHost $composeUp
if ($LASTEXITCODE -ne 0) { Write-Err "docker compose up -d 失败"; exit 1 }
Write-OK "容器已重启"

# ============== 5. 显示状态 ==============
Write-Step "部署结果"
$psOut = ssh $SshHost "cd $RemotePath && docker compose ps --format 'table {{.Name}}\t{{.Image}}\t{{.Status}}'" 2>&1
$psOut | ForEach-Object { Write-Host "    $_" }

# ============== 6. 可选:看日志 ==============
if ($TailLogs -gt 0) {
    Write-Step "最近 $TailLogs 行 backend / frontend 日志"
    ssh $SshHost "cd $RemotePath && (docker compose logs --tail=$TailLogs backend 2>&1; echo '---'; docker compose logs --tail=$TailLogs frontend 2>&1)" | ForEach-Object { Write-Host "    $_" }
}

Write-Host ""
Write-Host "================================================================" -ForegroundColor Green
Write-Host "  部署完成!" -ForegroundColor Green
Write-Host "================================================================" -ForegroundColor Green
Write-Host ""
Write-Host "下次部署只跑:" -ForegroundColor Yellow
Write-Host "  .\scripts\deploy-to-ecs.ps1" -ForegroundColor White
Write-Host ""
Write-Host "如果想"改完代码一次到位":用组合命令" -ForegroundColor Yellow
Write-Host "  .\scripts\push-to-acr.ps1; .\scripts\deploy-to-ecs.ps1" -ForegroundColor White
Write-Host ""
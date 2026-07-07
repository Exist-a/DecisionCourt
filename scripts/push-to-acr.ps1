# DecisionCourt v0.9.2 一键构建 + 推送到阿里云 ACR (PowerShell)
#
# 用法(方案 A: 只用 :latest tag,最简单):
#   .\scripts\push-to-acr.ps1                            # 推 :latest (默认用 docker login 缓存)
#   .\scripts\push-to-acr.ps1 -BuildOnly                # 只构建,不推送 (本地调试用)
#   .\scripts\push-to-acr.ps1 -UseVPC                   # 用 VPC 内网地址 (ECS 上推送用)
#   .\scripts\push-to-acr.ps1 -NoLogin                  # 跳过登录 (假设已 docker login)
#   .\scripts\push-to-acr.ps1 -Password xxx             # 用命令行密码(不进文件)
#   .\scripts\push-to-acr.ps1 -Tag v0.9.2               # 指定 tag (紧急回滚用,默认 latest)
#
# 前置:
#   - Docker Desktop 在跑
#   - 当前目录是项目根(能看见 docker-compose.yml)
#   - .env 文件存在(注入 frontend build args 用)
#     至少要包含 NEXT_PUBLIC_API_URL / NEXT_PUBLIC_WS_URL
#
# ECS 端部署命令(不变):
#   ssh ecs 'cd /opt/decisioncourt; docker compose pull; docker compose up -d'
#
# ====================================================================
# ⚠️ 密码管理
# 默认行为:走 `docker login` 缓存的凭证(~/.docker/config.json)。
#   - 第一次手动登录后,后续不会再问
#   - docker logout / 凭证过期会失效,重新跑 docker login 即可
#
# 想完全不输入密码,有三种方式(安全性递减):
#   1. -Password xxx 命令行参数 (密码不进文件,推荐)
#   2. 改下面 $Password = "" 在引号里填密码(明文落盘,慎用)
#   3. 用 git 排除(把 $Password 那行 stash / commit 时跳过)
# ====================================================================

# ===== 在这里填密码可以免输入(明文,慎用) =====
# 取消下面这行注释 + 改成你的 ACR 访问凭证密码即可:
# $ScriptPassword = "your-acr-password-here"

[CmdletBinding()]
param(
    [string]$Tag = "latest",
    [string]$Registry = "crpi-rnawo8jx69bslvbx.cn-hongkong.personal.cr.aliyuncs.com",
    [string]$Username = "Exist-a",
    [string]$Password = "",
    [switch]$UseVPC,
    [switch]$BuildOnly,
    [switch]$NoLogin
)

$ErrorActionPreference = "Continue"  # Docker 把进度写到 stderr,PowerShell 会当成错误;靠 $LASTEXITCODE 判断

# 把脚本里的 $ScriptPassword 也合并进来(用户填了就用,没填就空)
if (-not $Password -and (Get-Variable -Name ScriptPassword -Scope Script -ErrorAction SilentlyContinue)) {
    $Password = $ScriptPassword
}

# ============== 颜色输出 ==============
function Write-Step($msg) { Write-Host "`n==> $msg" -ForegroundColor Cyan }
function Write-OK($msg)   { Write-Host "    OK $msg" -ForegroundColor Green }
function Write-Warn($msg) { Write-Host "    WARN $msg" -ForegroundColor Yellow }
function Write-Err($msg)  { Write-Host "    ERROR $msg" -ForegroundColor Red }

# ============== 0. 强制切到本地 context(关键!) ==============
# 背景: docker context 和 docker buildx 是两套独立系统
#   - `docker context use ecs` 后,docker build 也走 ECS,ECS 拉不到 alpine:3.20
#   - 这里强制切回 default context,确保 build 在本地跑
#   - 必须在第 1 步之前做,否则 docker build 失败
$curCtx = (docker context ls --format "{{.Name}}|{{.Current}}" | Where-Object { $_ -match "true$" }) -replace "\|true$", ""
if ($curCtx -ne "default") {
    Write-Step "检测到当前 context = $curCtx,切回 default(避免 build 走 ECS 拉不到 alpine)"
    docker context use default 2>&1 | Out-Null
    docker --context default buildx use default 2>&1 | Out-Null
}

# ============== 1. VPC 替换 ==============
if ($UseVPC) {
    $Registry = $Registry -replace "crpi-rnawo8jx69bslvbx", "crpi-rnawo8jx69bslvbx-vpc"
    Write-Step "使用 VPC 内网地址: $Registry"
}

$BackendImage  = "$Registry/decision-court/decision-court-backend"
$FrontendImage = "$Registry/decision-court/decision-court-frontend"

# ============== 2. Tag (方案 A: 默认 latest) ==============
Write-Step "Tag: $Tag (方案 A: 只用 latest,ECS 端不需要任何改动)"

# ============== 3. 前置检查 ==============
Write-Step "前置检查"
if (-not (Test-Path "docker-compose.yml")) {
    Write-Err "当前目录没有 docker-compose.yml,看起来不在项目根目录"
    exit 1
}
Write-OK "项目根目录确认"

try {
    docker info | Out-Null
    if ($LASTEXITCODE -ne 0) { throw }
    Write-OK "Docker 在跑"
} catch {
    Write-Err "Docker 没启动,请先启 Docker Desktop"
    exit 1
}

# ============== 4. 加载 .env (build args 用) ==============
Write-Step "加载 .env"
if (Test-Path ".env") {
    $count = 0
    Get-Content ".env" | ForEach-Object {
        $line = $_.Trim()
        if (-not $line -or $line.StartsWith("#")) { return }
        # PowerShell 5.1 解析 [A-Za-z_] 会当 type literal,用 split 代替 regex
        $kv = $line -split '=', 2
        if ($kv.Count -eq 2 -and [regex]::IsMatch($kv[0], '^[A-Za-z_][A-Za-z0-9_]*$')) {
            $key = $kv[0]
            $val = $kv[1].Trim() -replace '^"', '' -replace '"$', '' -replace "^'", '' -replace "'$", ''
            [Environment]::SetEnvironmentVariable($key, $val, "Process")
            $count++
        }
    }
    Write-OK "已加载 $count 个变量"

    # 检查 frontend 必需的 build args
    if (-not $env:NEXT_PUBLIC_API_URL) {
        Write-Warn ".env 里没设 NEXT_PUBLIC_API_URL,frontend build 会用 Dockerfile 默认 (http://backend:8080)"
        Write-Warn "生产部署必须改为你自己的域名,例如: NEXT_PUBLIC_API_URL=https://yourdomain.com"
    } elseif ($env:NEXT_PUBLIC_API_URL -match "localhost|127\.0\.0\.1") {
        Write-Warn "NEXT_PUBLIC_API_URL 是 localhost,生产环境浏览器连不上!"
        Write-Warn "    推送前请把 .env 里的 NEXT_PUBLIC_API_URL 改成你的域名,例如 https://yourdomain.com"
    }
} else {
    Write-Warn ".env 不存在,frontend build args 走 Dockerfile 默认值 (生产部署会出错!)"
}

# ============== 5. 凭证检查 + (可选) 登录 ==============
#
# 不主动调用 docker login,理由:
#   - `docker login` 在 non-TTY 环境(IDE/PowerShell ISE/SSH 脚本)会强制报错
#     即使凭证其实能用也会报 "cannot perform an interactive login from a non-TTY device"
#   - `docker push/pull` 会自动用 credential helper (Windows Credential Manager)
#     这是更可靠的方式,我们验证过能用
#
# 如果凭证缺失或失效,docker push 会返回 401 unauthorized,届时再排查

# 检查凭证是否在 Windows Credential Manager 里 (便于快速诊断)
Write-Step "检查凭证"
$hasCred = $false
if ($IsWindows -or $env:OS -match "Windows") {
    try {
        $credList = cmdkey /list 2>$null
        if ($credList | Select-String -Pattern $Registry -SimpleMatch -CaseSensitive:$false) {
            Write-OK "凭证已在 Windows Credential Manager 里(自动用于 docker push)"
            $hasCred = $true
        } else {
            Write-Warn "Windows Credential Manager 里没找到 $Registry 的凭证"
            Write-Warn "需要先 docker login(在能输密码的真终端里)再重跑"
        }
    } catch {
        Write-Warn "无法检查凭证 ($_),跳过检查,让 docker push 自己判断"
    }
} else {
    Write-Warn "非 Windows 环境,跳过凭证预检查"
}

# 如果用户明确要求强制登录 (如刚改了密码),走 --password-stdin
if ($NoLogin -or $BuildOnly) {
    Write-Host "    跳过登录(BuildOnly 或 NoLogin)" -ForegroundColor Gray
} elseif ($Password -and -not $hasCred) {
    Write-Step "登录 $Registry (使用 -Password 强制写入凭证)"
    $Password | docker login --username=$Username --password-stdin $Registry
    if ($LASTEXITCODE -ne 0) { Write-Err "登录失败,密码可能错了"; exit 1 }
    $Password = $null
    Write-OK "登录成功"
} elseif (-not $hasCred) {
    Write-Err "凭证缺失,docker push 会失败"
    Write-Host "    解决方法:在你的真 PowerShell 终端(非 IDE 内)跑:" -ForegroundColor Yellow
    Write-Host "      docker login --username=$Username $Registry" -ForegroundColor Yellow
    exit 1
}

# ============== 6. 构建 backend ==============
Write-Step "构建 backend 镜像"
# 显式 --builder default:避开 docker buildx 默认 builder 可能是 ECS 的坑
# (docker context 和 docker buildx 是两套独立系统,context 切回 default 不影响 buildx)
docker build --builder default -t "${BackendImage}:${Tag}" ./backend
if ($LASTEXITCODE -ne 0) { Write-Err "backend build 失败"; exit 1 }
Write-OK "backend 镜像已构建"

# ============== 7. 构建 frontend ==============
Write-Step "构建 frontend 镜像"
$buildArgs = @()
if ($env:NEXT_PUBLIC_API_URL) {
    $buildArgs += "--build-arg"; $buildArgs += "NEXT_PUBLIC_API_URL=$env:NEXT_PUBLIC_API_URL"
}
if ($env:NEXT_PUBLIC_WS_URL) {
    $buildArgs += "--build-arg"; $buildArgs += "NEXT_PUBLIC_WS_URL=$env:NEXT_PUBLIC_WS_URL"
}
if ($env:NEXT_PUBLIC_USE_MOCK) {
    $buildArgs += "--build-arg"; $buildArgs += "NEXT_PUBLIC_USE_MOCK=$env:NEXT_PUBLIC_USE_MOCK"
}

docker build --builder default `
    $buildArgs `
    -t "${FrontendImage}:${Tag}" `
    ./frontend

if ($LASTEXITCODE -ne 0) { Write-Err "frontend build 失败"; exit 1 }
Write-OK "frontend 镜像已构建"
if ($buildArgs.Count -gt 0) {
    Write-Host "    build args: $($buildArgs -join ' ')" -ForegroundColor Gray
} else {
    Write-Warn "frontend build 没传 NEXT_PUBLIC_*,使用 Dockerfile 默认值"
}

# ============== 8. 推送 ==============
if ($BuildOnly) {
    Write-Step "BuildOnly 模式,跳过推送"
    Write-Host "`n本地镜像:" -ForegroundColor Yellow
    docker images | Select-String "decision-court" | ForEach-Object { Write-Host "    $_" }
    exit 0
}

Write-Step "推送 backend:$Tag"
docker push "${BackendImage}:${Tag}"
if ($LASTEXITCODE -ne 0) { Write-Err "backend push 失败"; exit 1 }
Write-OK "backend 推送成功"

Write-Step "推送 frontend:$Tag"
docker push "${FrontendImage}:${Tag}"
if ($LASTEXITCODE -ne 0) { Write-Err "frontend push 失败"; exit 1 }
Write-OK "frontend 推送成功"

# ============== 9. 完成 ==============
Write-Host ""
Write-Host "================================================================" -ForegroundColor Green
Write-Host "  推送完成!" -ForegroundColor Green
Write-Host "================================================================" -ForegroundColor Green
Write-Host ""
Write-Host "  backend:  ${BackendImage}:${Tag}" -ForegroundColor White
Write-Host "  frontend: ${FrontendImage}:${Tag}" -ForegroundColor White
Write-Host ""
Write-Host "下一步 - SSH 到 ECS 执行:" -ForegroundColor Yellow
Write-Host "  cd /opt/decisioncourt" -ForegroundColor White
Write-Host "  docker compose pull" -ForegroundColor White
Write-Host "  docker compose up -d" -ForegroundColor White
Write-Host ""
Write-Host "或者一行命令:" -ForegroundColor Yellow
$q = [char]39
Write-Host ("  ssh ecs " + $q + "cd /opt/decisioncourt" + [char]59 + " docker compose pull" + [char]59 + " docker compose up -d" + $q) -ForegroundColor White
Write-Host ""
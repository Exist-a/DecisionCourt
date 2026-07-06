# DecisionCourt v0.9.2 一键 commit + push(分 3 个 commit)
# 不动 .env (AGENTS.md 红线)

$ErrorActionPreference = "Continue"
Set-Location "d:\源码\FullStack\DecisionCourt"

# 安全检查: .env 不能进 git
Write-Host "==> 安全检查: .env 是否被忽略" -ForegroundColor Cyan
$envIgnored = git check-ignore .env 2>&1
if ($envIgnored -notmatch "\.env") {
    Write-Host "  ERROR: .env 没被忽略! 不能 commit" -ForegroundColor Red
    exit 1
}
Write-Host "  OK .env 已被忽略" -ForegroundColor Green

# ============================================================
# Commit 1: feat(deploy) - v0.9.2 dev/prod 分离 + 工作流脚本
# ============================================================
Write-Host "`n==> Commit 1/3: feat(deploy) - dev/prod 分离" -ForegroundColor Cyan
git add docker-compose.dev.yml backend/Dockerfile.dev frontend/Dockerfile.dev scripts/ docker-compose.yml
git status --short

$msg1 = @"
feat(deploy): v0.9.2 dev/prod 分离 + 一键工作流脚本

变更:
- docker-compose.dev.yml (new): bind mount + Dozzle + 弱密码 dev 配置
- Dockerfile.dev x2 (new): pnpm dev / Go air 热重载
- scripts/push-to-acr.ps1 (new): 本地一键 build + push 到阿里云 ACR
- scripts/deploy-on-ecs.sh (new): ECS 一键 pull + restart
- scripts/ecs.ps1 (new): 本地 Docker context 切换 + 看日志
- docker-compose.yml: backend 加 ./logs/backend:/app/logs volume(修复 v0.9.2 坑 13)
"@
git commit -m $msg1
if ($LASTEXITCODE -ne 0) { Write-Host "Commit 1 失败" -ForegroundColor Red; exit 1 }

# ============================================================
# Commit 2: fix - 部署后真测 bug 修复
# ============================================================
Write-Host "`n==> Commit 2/3: fix - 部署后真测新发现的 bug" -ForegroundColor Cyan
git add frontend/app/page.tsx frontend/next.config.mjs docs/security-audit-v0.8.3.md backend/internal/api/websocket.go docs/adr/0017-websocket-uuid-credential.md
git status --short

$msg2 = @"
fix: 部署后真测新发现的 bug + websocket UUID 凭证

修复:
1. frontend CSP #425/#418/#423 错误(部署后浏览器真测发现):
   - next.config.mjs: connect-src 改用 process.env 动态注入 NEXT_PUBLIC_WS_URL
   - app/page.tsx: 时区不一致 hydration 加 suppressHydrationWarning
   - security-audit-v0.8.3.md: 追加 §3.1 部署后真测章节
2. backend WebSocket UUID 凭证 bug(ADR 0017):
   - websocket.go: SessionUUID 用作"房间钥匙"避免越权访问
   - adr/0017-websocket-uuid-credential.md: 完整记录设计 + 取舍
"@
git commit -m $msg2
if ($LASTEXITCODE -ne 0) { Write-Host "Commit 2 失败" -ForegroundColor Red; exit 1 }

# ============================================================
# Commit 3: docs - v0.9.2 工作流文档 + ADR 坑 13 + 同步更新
# ============================================================
Write-Host "`n==> Commit 3/3: docs - 工作流文档 + ADR + 同步" -ForegroundColor Cyan
git add docs/dev-deploy-workflow.md docs/OBSERVABILITY.md docs/adr/0016-deployment-lessons-learned.md docs/decisioncourt-prd.md docs/decisioncourt-tech-spec.md docs/deployment/CHECKLIST.md README.md backend/cmd/server/main.go backend/internal/api/handler.go frontend/components/courtroom/CourtroomScene.tsx
git status --short

$msg3 = @"
docs: v0.9.2 工作流文档 + ADR 坑 13 + PRD/CHECKLIST 同步

新增:
- docs/dev-deploy-workflow.md: 日常操作手册(dev + deploy + 8 坑)
- docs/OBSERVABILITY.md: 运维速查(10 个常用查询命令)

更新:
- ADR 0016: 追加"坑 13" (backend read_only + file_logger 静默失败)
            + 2 条行动项(架构变更要做全代码搜索 / 错误吞掉至少打 WARN)
- ADR 0017: websocket UUID 凭证设计决策
- PRD / 技术规范 / CHECKLIST: v0.9.2 状态同步
- README.md: 文档索引更新
"@
git commit -m $msg3
if ($LASTEXITCODE -ne 0) { Write-Host "Commit 3 失败" -ForegroundColor Red; exit 1 }

# ============================================================
# Push 到 origin
# ============================================================
Write-Host "`n==> Push 到 origin" -ForegroundColor Cyan
git push origin main
if ($LASTEXITCODE -ne 0) {
    Write-Host "Push 失败 - 可能需要 Personal Access Token" -ForegroundColor Red
    exit 1
}

Write-Host "`n==> 全部完成" -ForegroundColor Green
git log --oneline -5
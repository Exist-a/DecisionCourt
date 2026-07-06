#!/bin/bash
# DecisionCourt v0.9.2 在 ECS 上的一键部署脚本
#
# 用法(在 ECS 上跑,deploy.sh 放在项目根目录):
#   ./deploy.sh                                # 部署 + 健康检查
#   ./deploy.sh --logs 30                      # 部署完看 backend/frontend 最近 30 行日志
#   ./deploy.sh --no-pull                      # 不拉镜像,只重启(本地缓存)
#   ./deploy.sh --yes                          # 跳过确认倒计时(脚本/CI 用)
#   ./deploy.sh --dir /some/other/path         # 指定项目目录
#   ./deploy.sh --health-url http://x:8080/health   # 自定义健康检查 URL
#   ./deploy.sh --help                         # 帮助
#
# 工作流:
#   1. 你在本地:  .\scripts\push-to-acr.ps1          (build + push 到 ACR)
#   2. 你在 ECS:  ./deploy.sh                        (从 ACR pull + 重启 + 健康检查)
#
# 这个脚本要在 ECS 上保存,WorkBench 里跑。不要在本地跑。
#
# 回滚:
#   - 自动:./deploy.sh 第二次跑会用上次没问题的镜像重启
#     (前提:没改 docker-compose.yml,只改了 image 内容)
#   - 手动:把 docker-compose.yml 里的 image tag 改回上一个,然后 ./deploy.sh
#   - 紧急:$DC up -d <service> --force-recreate 强制重建

set -e

# ===== 参数默认值 =====
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="${PROJECT_DIR:-$SCRIPT_DIR}"   # 默认 = 脚本所在目录
TAIL_LOGS=0
DO_PULL=true
SKIP_CONFIRM=false
HEALTH_URL=""                                # 自动推断
LOG_FILE="$SCRIPT_DIR/deploy.log"

# ===== 颜色 =====
GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

step() { echo -e "\n${CYAN}==> $1${NC}" | tee -a "$LOG_FILE"; }
ok()   { echo -e "    ${GREEN}OK $1${NC}" | tee -a "$LOG_FILE"; }
warn() { echo -e "    ${YELLOW}WARN $1${NC}" | tee -a "$LOG_FILE"; }
err()  { echo -e "    ${RED}ERROR $1${NC}" | tee -a "$LOG_FILE"; }

# ===== 初始化日志 =====
echo "" > "$LOG_FILE"
echo "=== DecisionCourt 部署日志 $(date '+%Y-%m-%d %H:%M:%S %Z') ===" | tee -a "$LOG_FILE"

# ===== 参数解析 =====
while [[ $# -gt 0 ]]; do
    case $1 in
        --logs)         TAIL_LOGS="$2"; shift 2 ;;
        --no-pull)      DO_PULL=false; shift ;;
        --dir)          PROJECT_DIR="$2"; shift 2 ;;
        --yes|-y)       SKIP_CONFIRM=true; shift ;;
        --health-url)   HEALTH_URL="$2"; shift 2 ;;
        --help|-h)
            grep '^#' "$0" | sed 's/^# *//' | sed 's/^$//'
            exit 0
            ;;
        *) err "未知参数: $1 (用 --help 看用法)"; exit 1 ;;
    esac
done

# ===== 检测 docker compose 版本 =====
if docker compose version > /dev/null 2>&1; then
    DC="docker compose"
    DC_VERSION=$(docker compose version --short 2>/dev/null)
elif command -v docker-compose > /dev/null 2>&1 && docker-compose --version > /dev/null 2>&1; then
    DC="docker-compose"
    DC_VERSION=$(docker-compose --version --short 2>/dev/null)
else
    err "找不到 'docker compose' (v2 plugin) 或 'docker-compose' (v1 二进制)"
    echo "    装 v2 plugin:"
    echo "      sudo apt-get update && sudo apt-get install -y docker-compose-plugin"
    echo "    或装 v1:"
    echo "      sudo apt-get install -y docker-compose"
    exit 1
fi
step "检测到 ${GREEN}$DC${NC} ($DC_VERSION)"

# ===== 1. 前置检查 =====
step "前置检查"
if [ ! -f "$PROJECT_DIR/docker-compose.yml" ]; then
    err "目录里没有 docker-compose.yml: $PROJECT_DIR"
    exit 1
fi
cd "$PROJECT_DIR"
ok "项目目录: $PROJECT_DIR"

if ! docker info > /dev/null 2>&1; then
    err "Docker 没在跑"
    exit 1
fi
ok "Docker 在跑"

# 记录当前在跑的镜像(用于回滚参考)
PRE_IMAGES=$($DC ps --format "{{.Image}}" 2>/dev/null | sort -u)
PRE_COUNT=$(echo "$PRE_IMAGES" | grep -c . || true)
ok "当前运行着 $PRE_COUNT 个服务的镜像"

# ===== 2. 确认 =====
if ! $SKIP_CONFIRM; then
    step "确认部署"
    echo "    当前时间: $(date '+%Y-%m-%d %H:%M:%S %Z')"
    echo "    项目目录: $PROJECT_DIR"
    echo "    工具: $DC ($DC_VERSION)"
    if $DO_PULL; then
        echo "    动作: 拉新镜像 + 重启容器 + 健康检查"
    else
        echo "    动作: 重启容器 + 健康检查(不拉新镜像)"
    fi
    echo ""
    echo -n "    5 秒后开始部署,按 Ctrl+C 取消... "
    for i in 5 4 3 2 1; do
        echo -n "$i "
        sleep 1
    done
    echo ""
fi

# ===== 3. 拉镜像 =====
if $DO_PULL; then
    step "拉新镜像(从阿里云 ACR)"
    $DC pull 2>&1 | tee -a "$LOG_FILE" | grep -E "Pulling|Downloaded|up to date|Error" || true
    ok "镜像已拉到 ECS"
else
    step "跳过拉镜像(--no-pull),用本地缓存"
fi

# ===== 4. 重启容器 =====
step "重启容器(自动替换旧镜像)"
$DC up -d 2>&1 | tee -a "$LOG_FILE" | grep -vE "^$" | tail -20 || true
ok "容器已重启"

# ===== 5. 健康检查 =====
step "健康检查"

# 自动推断健康检查 URL
if [ -z "$HEALTH_URL" ]; then
    # 尝试读 .env 里的 BACKEND_PORT,默认 8080
    if [ -f .env ]; then
        BACKEND_PORT=$(grep -E "^BACKEND_PORT=" .env | cut -d= -f2 | tr -d '"' || echo "8080")
    else
        BACKEND_PORT="8080"
    fi
    HEALTH_URL="http://localhost:${BACKEND_PORT}/health"
fi

HEALTH_OK=false
for i in $(seq 1 15); do
    if curl -sf -m 3 "$HEALTH_URL" > /dev/null 2>&1; then
        HEALTH_OK=true
        ok "backend 健康检查通过 (尝试 $i/15): $HEALTH_URL"
        break
    fi
    sleep 2
done

if ! $HEALTH_OK; then
    err "backend 健康检查失败 (15 次 × 2 秒 = 30 秒)"
    warn "可能服务还在启动,或者真出问题了"
    echo ""
    echo "    手动检查: docker logs dc_backend --tail 50"
    echo "    或:       $DC logs --tail=50 backend"
    echo ""
    warn "是否要回滚?如果需要,把 docker-compose.yml 里 image 改回上一个,再跑 ./deploy.sh"
    warn "或强制重启: $DC up -d backend --force-recreate"
fi

# ===== 6. 服务状态 =====
step "服务状态"
$DC ps --format "table {{.Name}}\t{{.Image}}\t{{.Status}}" | tee -a "$LOG_FILE"

# 显示新的镜像(对比 pre)
NEW_IMAGES=$($DC ps --format "{{.Image}}" 2>/dev/null | sort -u)
CHANGED=$(diff <(echo "$PRE_IMAGES") <(echo "$NEW_IMAGES") || true)
if [ -n "$CHANGED" ]; then
    step "镜像变化"
    echo "$CHANGED" | sed 's/^/    /' | tee -a "$LOG_FILE"
else
    ok "镜像没变(可能 ACR 上 :latest 没更新,本地 push 后再跑一次)"
fi

# ===== 7. 可选:看日志 =====
if [ "$TAIL_LOGS" -gt 0 ]; then
    step "最近 $TAIL_LOGS 行 backend 日志"
    $DC logs --tail=$TAIL_LOGS backend 2>&1 | sed 's/^/    /' | tee -a "$LOG_FILE"
fi

# ===== 8. 收尾 =====
echo ""
echo -e "${GREEN}================================================================${NC}" | tee -a "$LOG_FILE"
echo -e "${GREEN}  部署完成!${NC}" | tee -a "$LOG_FILE"
echo -e "${GREEN}================================================================${NC}" | tee -a "$LOG_FILE"
echo ""

if $HEALTH_OK; then
    echo -e "  ${GREEN}✓ backend 健康${NC}"
else
    echo -e "  ${YELLOW}! backend 健康检查失败,请查日志${NC}"
fi
echo "  日志: $LOG_FILE"

echo ""
echo -e "${YELLOW}常用命令:${NC}"
echo "  看实时日志:   $DC logs -f backend"
echo "  看所有容器:   $DC ps"
echo "  进 backend:   $DC exec backend sh"
echo "  回滚:         修改 docker-compose.yml image tag,然后 ./deploy.sh"
echo "  健康检查:     curl -i $HEALTH_URL"
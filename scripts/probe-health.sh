#!/usr/bin/env bash
# DecisionCourt 存活/就绪拨测（Ops P1 —— "能主动叫你"的最便宜实现）
#
# 触发: docs/OPS-DASHBOARD-PLAN.md §4 P1（外部拨测）+ §10.4（仍未做清单）
# 用户 2026-10-11 拍板：**只做拨测，不接通知渠道** —— 异常打印可操作 [ALERT]
# 行并非零退出，靠 crontab 的 MAILTO（或外部工具捕获 stderr）来通知。
#
# 为什么需要它（与 /metrics 面板互补）：
#   面板解决"能看"，但没人会一直盯着面板。"能被告知"才是告警的价值。
#   本脚本回答一个问题：**服务现在还能不能提供服务**。
#
# 关键设计（踩坑沉淀，勿删）：
#   1. `/api/v1/health/llm` **在未配置 LLM key 时也返回 HTTP 200**，只在 body 里
#      写 `"configured":false`。所以拨测**必须校验 body**，只看状态码会漏判
#      —— 这正是 AGENTS.md「开关是 on ≠ 数据拿到了」的同族陷阱。
#   2. `/health` 也必须校验 body（`"status":"ok"`），不能只看 200。
#
# 安装（ECS 上，admin 用户 crontab，每 5 分钟一次）：
#   crontab -e
#   */5 * * * * DC_BASE_URL=http://127.0.0.1:8080 /opt/DecisionCourt/scripts/probe-health.sh >> /opt/DecisionCourt/logs/probe.log 2>&1
#
# 环境变量：
#   DC_BASE_URL       后端基址（默认 http://127.0.0.1:8080）
#   DC_FRONTEND_URL   可选：前端基址（设置则一并探活）
#   DC_METRICS_TOKEN  可选：/metrics 访问令牌（生产默认收紧，需要才探）
#   DC_PROBE_TIMEOUT  curl 超时秒数（默认 10）
#
# 退出码：0 = 全部通过；1 = 至少一项异常。

set -uo pipefail

BASE_URL="${DC_BASE_URL:-http://127.0.0.1:8080}"
FRONTEND_URL="${DC_FRONTEND_URL:-}"
METRICS_TOKEN="${DC_METRICS_TOKEN:-}"
TIMEOUT="${DC_PROBE_TIMEOUT:-10}"

ALERTS=()
NOTES=()

# probe <label> <url> [extra curl args...] —— 返回 "code|body"
probe() {
  local url="$1"; shift
  local out
  out="$(curl -s -S -m "$TIMEOUT" -o - -w $'\n%{http_code}' "$@" "$url" 2>/dev/null)" || {
    echo "-1|"; return
  }
  local code body
  code="${out##*$'\n'}"
  body="${out%$'\n'*}"
  echo "${code}|${body}"
}

# ---------- 1) 后端存活 + body 校验 ----------
HEALTH="$(probe "${BASE_URL}/health")"
HEALTH_CODE="${HEALTH%%|*}"
HEALTH_BODY="${HEALTH#*|}"
if [ "$HEALTH_CODE" != "200" ]; then
  ALERTS+=("BACKEND_DOWN: GET ${BASE_URL}/health -> HTTP ${HEALTH_CODE}（后端进程挂了 / 网络不通 / 反代异常）")
elif ! printf '%s' "$HEALTH_BODY" | grep -q '"status":"ok"'; then
  ALERTS+=("BACKEND_UNHEALTHY: /health 返回 200 但 body 非 {\"status\":\"ok\"} —— body=${HEALTH_BODY}")
else
  NOTES+=("health=ok")
fi

# ---------- 2) LLM 就绪（必须校验 configured，不能只看 200） ----------
LLMH="$(probe "${BASE_URL}/api/v1/health/llm")"
LLMH_CODE="${LLMH%%|*}"
LLMH_BODY="${LLMH#*|}"
if [ "$LLMH_CODE" != "200" ]; then
  ALERTS+=("LLM_HEALTH_UNREACHABLE: GET ${BASE_URL}/api/v1/health/llm -> HTTP ${LLMH_CODE}")
elif ! printf '%s' "$LLMH_BODY" | grep -q '"configured":true'; then
  ALERTS+=("LLM_NOT_CONFIGURED: /api/v1/health/llm 返回 200 但 configured!=true —— LLM_API_KEY 缺失/失效，所有庭审都会失败。body=${LLMH_BODY}")
else
  NOTES+=("llm=configured")
fi

# ---------- 3) 可选：/metrics 可达性（生产默认收紧，需 token） ----------
if [ -n "$METRICS_TOKEN" ]; then
  M_CODE="$(curl -s -m "$TIMEOUT" -o /dev/null -w '%{http_code}' \
    -H "Authorization: Bearer ${METRICS_TOKEN}" "${BASE_URL}/metrics" 2>/dev/null || echo -1)"
  if [ "$M_CODE" != "200" ]; then
    ALERTS+=("METRICS_UNREACHABLE: /metrics 带 token 仍 -> HTTP ${M_CODE}（token 不匹配 / 中间件未放行）")
  else
    NOTES+=("metrics=ok")
  fi
fi

# ---------- 4) 可选：前端探活 ----------
if [ -n "$FRONTEND_URL" ]; then
  F_CODE="$(curl -s -m "$TIMEOUT" -o /dev/null -w '%{http_code}' "$FRONTEND_URL" 2>/dev/null || echo -1)"
  if [ "$F_CODE" != "200" ]; then
    ALERTS+=("FRONTEND_DOWN: GET ${FRONTEND_URL} -> HTTP ${F_CODE}")
  else
    NOTES+=("frontend=ok")
  fi
fi

# ---------- 输出 ----------
TS="$(date -Iseconds 2>/dev/null || date +%Y-%m-%dT%H:%M:%S%z)"
if [ ${#ALERTS[@]} -gt 0 ]; then
  echo "[ALERT] ${TS} DecisionCourt probe FAILED (${#ALERTS[@]} 项):"
  for a in "${ALERTS[@]}"; do
    echo "  - $a"
  done
  echo "  处置提示: 后端容器状态 \`docker compose ps\`；日志 \`docker compose logs --tail=100 backend\`；"
  echo "            LLM key 在服务器 .env（不要回显）；/metrics 401 检查 OPS_METRICS_TOKEN。"
  exit 1
fi

echo "[OK] ${TS} DecisionCourt probe passed: ${NOTES[*]}"
exit 0

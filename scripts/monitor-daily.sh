#!/bin/bash
# DecisionCourt 临时监控脚本（PR-E 配套，crontab 兜底）
#
# 触发: production-retrospective-2026-08-05.md §4 + ACTION-ITEMS-ECS-EXPIRY-2026-08.md §C3
# Phase C Prometheus 之前的兜底方案，每 6 小时跑一次。
#
# 安装（ECS 上，admin 用户 crontab）:
#   crontab -e
#   0 */6 * * * /opt/DecisionCourt/scripts/monitor-daily.sh >> /opt/DecisionCourt/logs/monitor.log 2>&1
#
# 输出: stderr 一行 JSON，stdout 详细日志
# 阈值超限: stderr 输出告警行（Phase D 接通知时使用）

set -euo pipefail

# ====== 阈值（可调） ======
ERROR_RATE_THRESHOLD=0.05      # 5% 错误率告警（24h 窗口）
LLM_P99_THRESHOLD_SEC=20       # LLM P99 > 20s 告警（24h 窗口）
DISK_USAGE_THRESHOLD=80        # 磁盘 > 80% 告警

# ====== 数据库查询 ======
PG_CONTAINER="dc_postgres"

# 1. 24h decision_events 错误率
ERROR_RATE=$(docker exec "$PG_CONTAINER" psql -U decisioncourt -d decisioncourt -tA -c "
  SELECT
    CASE WHEN count(*) = 0 THEN 0
    ELSE round(
      sum(CASE WHEN status='error' THEN 1 ELSE 0 END)::numeric / count(*),
      4
    )
  END as error_rate
  FROM decision_events
  WHERE created_at > NOW() - INTERVAL '24 hours';
" 2>/dev/null || echo "-1")

# 2. 24h LLM P99 latency（毫秒 → 秒）
LLM_P99_MS=$(docker exec "$PG_CONTAINER" psql -U decisioncourt -d decisioncourt -tA -c "
  SELECT COALESCE(
    percentile_cont(0.99) WITHIN GROUP (ORDER BY duration_ms),
    0
  )
  FROM llm_calls
  WHERE created_at > NOW() - INTERVAL '24 hours';
" 2>/dev/null || echo "-1")

LLM_P99_SEC=$(echo "scale=3; $LLM_P99_MS / 1000" | bc 2>/dev/null || echo "0")

# 3. 磁盘使用率
DISK_USAGE=$(df /opt/DecisionCourt | tail -1 | awk '{print $5}' | sed 's/%//')

# ====== 输出 JSON（stderr 给 crontab 监控使用） ======
TIMESTAMP=$(date -Iseconds 2>/dev/null || date +%Y-%m-%dT%H:%M:%S%z)
JSON="{\"ts\":\"$TIMESTAMP\",\"error_rate_24h\":$ERROR_RATE,\"llm_p99_sec_24h\":$LLM_P99_SEC,\"disk_usage_pct\":$DISK_USAGE}"

echo "$JSON" >&2

# ====== 告警判断 ======
ALERTS=()

# 错误率告警
if [ "$ERROR_RATE" != "-1" ]; then
  COMPARE_ERR=$(echo "$ERROR_RATE > $ERROR_RATE_THRESHOLD" | bc 2>/dev/null || echo 0)
  if [ "$COMPARE_ERR" = "1" ]; then
    ALERTS+=("HIGH_ERROR_RATE: ${ERROR_RATE} > ${ERROR_RATE_THRESHOLD} (24h)")
  fi
fi

# LLM P99 告警
if [ "$LLM_P99_SEC" != "0" ] && [ "$LLM_P99_SEC" != "-1" ]; then
  COMPARE_P99=$(echo "$LLM_P99_SEC > $LLM_P99_THRESHOLD_SEC" | bc 2>/dev/null || echo 0)
  if [ "$COMPARE_P99" = "1" ]; then
    ALERTS+=("SLOW_LLM_P99: ${LLM_P99_SEC}s > ${LLM_P99_THRESHOLD_SEC}s (24h)")
  fi
fi

# 磁盘告警
if [ -n "$DISK_USAGE" ] && [ "$DISK_USAGE" -gt "$DISK_USAGE_THRESHOLD" ]; then
  ALERTS+=("HIGH_DISK_USAGE: ${DISK_USAGE}% > ${DISK_USAGE_THRESHOLD}%")
fi

# 输出告警
if [ ${#ALERTS[@]} -gt 0 ]; then
  echo "[ALERT] $(date):"
  for alert in "${ALERTS[@]}"; do
    echo "  - $alert"
    # TODO Phase D: 接通知（邮件 / Slack / webhook）
    # 当前 stderr 输出，可被外部监控工具（dozzle / uptime-kuma）捕获
  done
  exit 1
else
  echo "[OK] $(date): error_rate=$ERROR_RATE, llm_p99=${LLM_P99_SEC}s, disk=${DISK_USAGE}%"
  exit 0
fi
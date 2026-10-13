#!/bin/sh
# Prometheus 启动包装（生产）：把抓取配置里 credentials 的 ${OPS_METRICS_TOKEN} 展开成
# 真实 token 再 exec。
#
# 为什么要这一层：Prometheus **不会**在 scrape_configs 里做环境变量展开 —— 它只在
# external_labels 段展开，而且还要 --enable-feature=expand-external-labels 才开。
# 于是 prometheus.prod.yml 里 `authorization.credentials: ${OPS_METRICS_TOKEN}` 会被当成
# **字面量**发出去（2026-10-13 实测抓包 = `Authorization: Bearer ${OPS_METRICS_TOKEN}`），
# 生产 backend（OPS_METRICS_PUBLIC=false）对 /metrics 一律回 401 → 抓取目标 up=0 →
# Grafana「总览」看板全 "No data"。
#
# dev 不受影响：dev 的 prometheus 配置不带 token，backend 走 OPS_METRICS_PUBLIC=true。
#
# token 为空时原样拷贝：生产必设 token，为空只说明配置缺失；此时 Prometheus 照样能起来，
# 只是抓取 401 —— 保持既有取舍「观测栈起不来不该把整份 compose 拖死」（见 compose 注释）。
set -eu

SRC="/etc/prometheus/prometheus.yml"
DST="/tmp/prometheus.yml"

if [ -n "${OPS_METRICS_TOKEN:-}" ]; then
  # 用 | 作分隔符；转义 replacement 里的 & \ |，否则含这些字符的 token 会被 sed 语义破坏
  # （& = 整个匹配串、\ = 转义引导、| = 分隔符）。
  esc=$(printf '%s' "$OPS_METRICS_TOKEN" | sed 's/[&|\\]/\\&/g')
  # 只替换 credentials 行的值，**不要**全局替换 —— 配置文件注释里也写着
  # ${OPS_METRICS_TOKEN} 这个字样，全局替换会把 token 泄露进注释文本。
  sed "s|^\([[:space:]]*credentials:[[:space:]]*\)\${OPS_METRICS_TOKEN}[[:space:]]*\$|\1$esc|" \
    "$SRC" > "$DST"

  # 静默降级护栏：替换没命中（配置被改格式/改了写法）时必须出声，否则又是"开关是 on、
  # 数据没拿到"那一类，排查成本极高。
  if grep -v '^[[:space:]]*#' "$DST" | grep -qF '${OPS_METRICS_TOKEN}'; then
    echo "WARN prometheus-entrypoint: 非注释行里仍有未展开的 \${OPS_METRICS_TOKEN} —— /metrics 抓取会 401，总览看板将无数据" >&2
  fi
else
  cp "$SRC" "$DST"
fi

exec /bin/prometheus \
  --config.file="$DST" \
  --storage.tsdb.retention.time=15d \
  --storage.tsdb.retention.size=512MB

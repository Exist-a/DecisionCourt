package observability

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// RenderPrometheus 把内存指标快照渲染成 Prometheus 文本暴露格式
// （`text/plain; version=0.0.4`）。
//
// 为什么需要（2026-10-10 第 3 轮 = OPS-DASHBOARD-PLAN P2）：`/metrics` 原本只返回
// JSON，Prometheus 抓不了 → 接不上 Grafana / 告警。指标接口从 v0.8 起就按"可替换
// adapter"设计（metrics.go 注释："未来可替换为 Prometheus client_golang adapter"），
// 这里就是那个导出面。
//
// 实现注意：
//   - 本包 histogram 的 bucketCounts 在 Observe 时对 "value <= bound" 全部自增，
//     因此**已经是 Prometheus 要的 le 累积语义**，可直接输出；末尾补一条 +Inf = 总计数。
//   - 名字与 label 值按 Prometheus 规范转义（反斜杠 / 引号 / 换行）。
//   - 输出按指标名 + label 排序，保证同一进程状态下文本稳定（便于 diff / 快照测试）。
func RenderPrometheus(snap MetricSnapshot) string {
	var b strings.Builder

	counterNames := sortedNames(snap.Counters)
	for _, name := range counterNames {
		fmt.Fprintf(&b, "# TYPE %s counter\n", name)
		for _, s := range snap.Counters[name] {
			fmt.Fprintf(&b, "%s%s %s\n", name, labelsText(s.Labels), formatFloat(s.Value))
		}
	}

	gaugeNames := sortedNames(snap.Gauges)
	for _, name := range gaugeNames {
		fmt.Fprintf(&b, "# TYPE %s gauge\n", name)
		for _, s := range snap.Gauges[name] {
			fmt.Fprintf(&b, "%s%s %s\n", name, labelsText(s.Labels), formatFloat(s.Value))
		}
	}

	histNames := sortedNames(snap.Histograms)
	for _, name := range histNames {
		fmt.Fprintf(&b, "# TYPE %s histogram\n", name)
		for _, s := range snap.Histograms[name] {
			for _, bucket := range s.Buckets {
				// Prometheus 的 le 是累积上限，与本包的 bucketCounts 语义一致。
				fmt.Fprintf(&b, "%s_bucket%s %d\n",
					name, labelsText(withLabel(s.Labels, "le", formatFloat(bucket.UpperBound))), bucket.Count)
			}
			fmt.Fprintf(&b, "%s_bucket%s %d\n", name, labelsText(withLabel(s.Labels, "le", "+Inf")), s.Count)
			fmt.Fprintf(&b, "%s_sum%s %s\n", name, labelsText(s.Labels), formatFloat(s.Sum))
			fmt.Fprintf(&b, "%s_count%s %d\n", name, labelsText(s.Labels), s.Count)
		}
	}

	return b.String()
}

// labelsText 把 labels map 渲染成 `{k="v",...}`；空 map 返回空串。
func labelsText(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `%s="%s"`, promNameEscape(k), promValueEscape(labels[k]))
	}
	b.WriteByte('}')
	return b.String()
}

// withLabel 返回带一个额外 label 的副本（不改动入参 map）。
func withLabel(labels map[string]string, k, v string) map[string]string {
	out := make(map[string]string, len(labels)+1)
	for key, val := range labels {
		out[key] = val
	}
	out[k] = v
	return out
}

// promValueEscape 按 Prometheus 规范转义 label 值。
func promValueEscape(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return r.Replace(v)
}

// promNameEscape 让 label 名尽量合法（我们的 label 名都是固定枚举，正常无需转义）。
func promNameEscape(k string) string {
	var b strings.Builder
	for i, r := range k {
		ok := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// formatFloat 用最短往返表示输出浮点（避免 1.1000000000000001 之类噪声）。
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// sortedNames 返回 map 的 key 排序结果。
func sortedNames(m map[string][]MetricSample) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Package util 提供跨包共用的小工具函数。
//
// ip.go — IP 脱敏工具。
//
// 背景:
//   ECS 30 天生产沉淀(production-retrospective-2026-08-05.md §4 P3-1)发现
//   CreateCourtroom / SubmitEvidence / writeAudit 等多处 slog.Warn + audit_logs.IP
//   字段都是明文 c.ClientIP(),外部 IP probe 时会把攻击者 IP 直接写进日志和
//   audit_logs 表。
//
// 设计:
//   - IPv4 (a.b.c.d) → "a.b.*.*"(保留前两段用于区分网段,后两段脱敏)
//   - IPv6 → 仅保留前 2 段(冒号分隔),其余置 *
//   - 空串 / localhost / 内网 IP → 原样返回(本地开发友好)
//   - 非 IP 字符串 → 原样返回(防御性,gin 在某些 proxy 后可能返回奇怪字符串)
//
// 选择保留 IPv4 前两段的理由:中国大陆 ISP 分配通常是 a.b.c.d /16 或 /24 前缀,
//保留 a.b 可区分 ISP / 区域但不暴露具体主机,足够审计定位攻击源 IP 段。
package util

import (
	"net"
	"strings"
)

// TruncateIP 返回脱敏后的 IP 字符串。
//
// 行为表:
//
//	输入                   输出
//	""                     ""                (空串原样)
//	"127.0.0.1"            "127.0.0.1"       (localhost 原样)
//	"10.0.0.5"            "10.0.0.5"        (RFC1918 内网原样)
//	"172.16.0.1"           "172.16.0.1"      (RFC1918 内网原样)
//	"192.168.1.1"          "192.168.1.1"     (RFC1918 内网原样)
//	"112.80.30.194"        "112.80.*.*"      (公网 IPv4 脱敏)
//	"8.8.8.8"              "8.8.*.*"         (公网 IPv4 脱敏)
//	"::1"                  "::1"             (IPv6 localhost 原样)
//	"2001:db8::1"          "2001:db8::*"     (IPv6 脱敏)
//	"abc"                  "abc"             (非 IP 原样)
//
// 设计权衡:
//   - 内网 IP 不脱敏:开发环境 + 本地部署 + 内网跨机部署,审计时需要看完整 IP 定位问题
//   - 公网 IPv4 保留前两段:足够审计"哪个 ISP/区域的攻击者",但不暴露具体主机
//   - 公网 IPv6 保留前两段:IPv6 前 48 位通常是 ISP / 区域标识,够审计
func TruncateIP(ip string) string {
	if ip == "" {
		return ""
	}

	// 用 net.ParseIP 校验合法性;失败则原样返回(防御性)
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ip
	}

	// IPv4 处理(包括 IPv4-mapped IPv6)
	if v4 := parsed.To4(); v4 != nil {
		// 内网 / loopback 原样
		if v4[0] == 127 || // 127.0.0.0/8 loopback
			v4[0] == 10 || // 10.0.0.0/8 RFC1918
			(v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31) || // 172.16.0.0/12
			(v4[0] == 192 && v4[1] == 168) || // 192.168.0.0/16
			v4.IsLinkLocalUnicast() || // 169.254.0.0/16
			v4.IsMulticast() { // 224.0.0.0/4
			return ip
		}
		// 公网 IPv4 → a.b.*.*
		return joinIPv4Masked(v4[0], v4[1])
	}

	// IPv6 处理
	if parsed.IsLoopback() || // ::1
		parsed.IsLinkLocalUnicast() || // fe80::/10
		parsed.IsLinkLocalMulticast() || // ff02::/16
		parsed.IsPrivate() { // fc00::/7 ULA
		return ip
	}
	// 公网 IPv6 → 前 2 段保留
	parts := strings.Split(ip, ":")
	if len(parts) < 2 {
		return ip
	}
	// 处理 :: 缩写(简化:取前 2 个非空段)
	out := []string{parts[0], parts[1]}
	for i := 2; i < len(parts); i++ {
		if parts[i] != "" {
			out = append(out, "*")
			break
		}
	}
	return strings.Join(out, ":")
}

// joinIPv4Masked 把 IPv4 前两段拼成 "a.b.*.*"。
func joinIPv4Masked(a, b byte) string {
	// 用 strconv.Itoa 等价但避免 import 抖动
	return itoa(int(a)) + "." + itoa(int(b)) + ".*.*"
}

// itoa 是 strconv.Itoa 的简化版,避免本文件 import strconv。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	const digits = "0123456789"
	negative := n < 0
	if negative {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
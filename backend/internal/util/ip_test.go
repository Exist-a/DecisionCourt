package util

import (
	"testing"
)

// TestTruncateIP 覆盖 P3-1 IP 脱敏的全部场景。
//
// 触发背景:ECS 30 天生产沉淀 production-retrospective-2026-08-05.md §4 P3-1
// "创建庭审被外部 IP 持续 probe" — 外部 IP (112.80.30.194 / 111.198.56.206)
// 直接进 slog.Warn + audit_logs.IP,日志里 IP 没脱敏。v1.0.0 修复:
// backend/internal/util/ip.go::TruncateIP 统一脱敏,handler.go 全量接入。
func TestTruncateIP(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// 公网 IPv4 — 必须脱敏
		{
			name: "ECS P3-1 公网 IPv4 脱敏",
			in:   "112.80.30.194",
			want: "112.80.*.*",
		},
		{
			name: "ECS P3-1 第二个 probe IP 脱敏",
			in:   "111.198.56.206",
			want: "111.198.*.*",
		},
		{
			name: "公网 DNS IP 脱敏",
			in:   "8.8.8.8",
			want: "8.8.*.*",
		},
		{
			name: "公网 IP 单字节前缀",
			in:   "1.1.1.1",
			want: "1.1.*.*",
		},

		// 内网 / localhost — 不脱敏(开发 + 内网部署需要完整 IP)
		{
			name: "localhost IPv4 原样",
			in:   "127.0.0.1",
			want: "127.0.0.1",
		},
		{
			name: "RFC1918 10.0.0.0/8 原样",
			in:   "10.0.0.5",
			want: "10.0.0.5",
		},
		{
			name: "RFC1918 172.16/12 原样",
			in:   "172.16.0.1",
			want: "172.16.0.1",
		},
		{
			name: "RFC1918 192.168/16 原样",
			in:   "192.168.1.1",
			want: "192.168.1.1",
		},

		// IPv6
		{
			name: "IPv6 localhost 原样",
			in:   "::1",
			want: "::1",
		},
		{
			name: "公网 IPv6 脱敏",
			in:   "2001:db8:85a3::8a2e:370:7334",
			want: "2001:db8:*",
		},

		// 边界
		{
			name: "空串原样",
			in:   "",
			want: "",
		},
		{
			name: "非 IP 字符串原样(防御性)",
			in:   "abc",
			want: "abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateIP(tt.in)
			if got != tt.want {
				t.Errorf("TruncateIP(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
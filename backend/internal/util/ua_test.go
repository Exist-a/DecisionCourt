package util

import (
	"strings"
	"testing"
)

func TestTruncateUA_Empty(t *testing.T) {
	if got := TruncateUA(""); got != "" {
		t.Errorf("TruncateUA(\"\") = %q, want \"\"", got)
	}
}

func TestTruncateUA_Short_AsIs(t *testing.T) {
	in := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/120.0"
	if got := TruncateUA(in); got != in {
		t.Errorf("TruncateUA(short) = %q, want unchanged %q", got, in)
	}
}

func TestTruncateUA_ExceedsByteLimit(t *testing.T) {
	// 实际生产中遇到的微信 WeChat UA 约 450 字符
	long := strings.Repeat("Mozilla/5.0 ", 50) // 550 字节 / 550 rune
	out := TruncateUA(long)

	if rl := len([]rune(out)); rl != UAMaxLen {
		t.Errorf("TruncateUA(long) rune len = %d, want %d", rl, UAMaxLen)
	}
	if len(out) > 200 {
		// byte 边界:多字节字符不被切
		t.Errorf("TruncateUA(long) byte len = %d, want <= 200", len(out))
	}
}

func TestTruncateUA_MultibyteNotSplit(t *testing.T) {
	// 100 个汉字 + 200 个汉字,确保不会被切到汉字中间
	cn := strings.Repeat("决", UAMaxLen+10) // 210 个汉字(每个 3 字节 UTF-8)
	out := TruncateUA(cn)

	if rl := len([]rune(out)); rl != UAMaxLen {
		t.Errorf("TruncateUA(cn) rune len = %d, want %d", rl, UAMaxLen)
	}
	// 不能出现 broken rune
	if !strings.HasPrefix(out, strings.Repeat("决", UAMaxLen)) {
		t.Errorf("TruncateUA(cn) should be %d '决', got %q", UAMaxLen, out[:min(50, len(out))])
	}
}

func TestTruncateUA_ControlCharsReplaced(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"abc\x00def", "abc def"},
		{"ab\tcd\x07ef", "ab\tcd ef"},
		{"\x1B[31mred\x1B[0m", " [31mred [0m"},
		{"clean\x7fstring", "clean string"},
		{"with\nnewline", "with\nnewline"}, // \n 保留
		{"with\ttab", "with\ttab"},           // \t 保留
	}
	for _, tc := range cases {
		got := TruncateUA(tc.in)
		if got != tc.want {
			t.Errorf("TruncateUA(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

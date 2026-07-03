package search

import (
	"fmt"
	"strings"
	"unicode"
)

// QueryMaxLen 限制 user-controlled search query 的最大长度。
//
// v0.8.3 安全(P3-2):防止恶意 user 用 100KB 的 query 撑爆 Bocha API quota
// 或让 LLM prompt 异常长。同时也是 prompt injection 攻击的边界(短 query
// 更容易审核)。
const QueryMaxLen = 200

// SanitizeQuery 校验并清洗 user-controlled search query。
//
// 规则:
//   - 长度 1-200(utf-8 rune count)
//   - 去除前后空白
//   - 删除 ASCII 控制字符(\x00-\x08, \x0B, \x0C, \x0E-\x1F, \x7F)
//     保留 \t \n \r\n
//   - 拒绝所有控制字符的 query(返回 error)
//
// 返回清洗后的 query,或 error。
//
// 用法:
//
//	query, err := search.SanitizeQuery(rawQuery)
//	if err != nil { return nil, err }
//	results, err := provider.Search(ctx, query)
func SanitizeQuery(raw string) (string, error) {
	q := strings.TrimSpace(raw)
	if q == "" {
		return "", fmt.Errorf("search: query is empty")
	}
	// 计算 rune 数(multi-byte 安全)
	runes := []rune(q)
	if len(runes) > QueryMaxLen {
		return "", fmt.Errorf("search: query too long (%d > %d chars)", len(runes), QueryMaxLen)
	}
	// 过滤控制字符
	cleaned := make([]rune, 0, len(runes))
	for _, r := range runes {
		if isAllowedRune(r) {
			cleaned = append(cleaned, r)
		}
	}
	if len(cleaned) == 0 {
		return "", fmt.Errorf("search: query contains only control characters")
	}
	return string(cleaned), nil
}

// isAllowedRune 返回 true 表示 rune 是允许出现在 query 中的字符。
// 拒绝 ASCII 控制字符(0x00-0x08, 0x0B, 0x0C, 0x0E-0x1F, 0x7F)但保留
// 空白字符(0x09 \t, 0x0A \n, 0x0D \r)和所有可打印 unicode。
func isAllowedRune(r rune) bool {
	if r == 0x09 || r == 0x0A || r == 0x0D {
		return true
	}
	if unicode.IsControl(r) {
		return false
	}
	return true
}

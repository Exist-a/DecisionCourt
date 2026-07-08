// Package util 提供跨包共用的小工具函数。
//
// 当前包含 UA(User-Agent)截断,被 backend/cmd/server 用于写入 users.last_ua
// 和 audit_logs.ua 之前。理由:这些 PG 列定义为 varchar(200),而移动浏览器 /
// 微信内置浏览器 / 部分企业代理的 UA 经常超过 200 字符,直接 INSERT 会触发
// SQLSTATE 22001(value too long for type character varying(200))。我们在
// 写入前截断以保证表写入与审计 trail 完整。
package util

// UAMaxLen 与 model.User.LastUA / model.AuditLog.UA 的 PG 列类型
// varchar(200) 保持一致。任何超过这个长度的 UA 会被截断到恰好这个长度。
const UAMaxLen = 200

// TruncateUA 返回不超过 UAMaxLen 字符(UTF-8 rune 计数)的 UA 字符串。
//
// 行为:
//   - 空串 → 原样返回
//   - rune 数 <= UAMaxLen → 原样返回
//   - rune 数 > UAMaxLen → 截断到前 UAMaxLen 个 rune(不是字节!)
//   - 含不可打印 ASCII 控制字符 → 替换为空格(避免原样存储奇怪字符)
//
// 选 rune-级而非 byte-级截断是为了不切断 UTF-8 多字节字符中间,导致写入 PG
// 的字符串出现 broken rune。200 字节可能要截到 ~50 个汉字才安全,影响数据
// 完整性。
func TruncateUA(s string) string {
	if s == "" {
		return ""
	}

	// rune-级清洗:把不可打印的 ASCII 控制字符替换为空格(保留 \t \n \r)
	cleaned := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\t' || r == '\n' || r == '\r' {
			cleaned = append(cleaned, r)
			continue
		}
		if r < 0x20 || r == 0x7F {
			cleaned = append(cleaned, ' ')
			continue
		}
		cleaned = append(cleaned, r)
	}

	if len(cleaned) <= UAMaxLen {
		return string(cleaned)
	}
	return string(cleaned[:UAMaxLen])
}

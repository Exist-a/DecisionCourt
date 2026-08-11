package util

import "strings"

// BagOfWords 把字符串切成 token set, 中英文混排友好。
// - 中文每个 CJK 字符 (BMP 0x4e00-0x9fff) 算一个独立 token
// - 拉丁字母 + 数字连写成一个 token
// - 大小写无关 (统一 lowercase)
// - 标点 / 空格作为分隔符
//
// v0.10.23 候选 2: 从 belief/convergence.go:201 抽出, 让 agent/react_runner.go
// 复用同一份 token 化逻辑做新意度 Jaccard 检查 (避免不同实现导致同一文本
// 算出不同 Jaccard)。
//
// 用法:
//   a := BagOfWords("Hello 你好")
//   // a = {"hello", "你", "好"}
func BagOfWords(s string) map[string]struct{} {
	out := map[string]struct{}{}
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		out[strings.ToLower(b.String())] = struct{}{}
		b.Reset()
	}
	for _, r := range s {
		switch {
		case r >= 0x4e00 && r <= 0x9fff: // CJK Unified Ideographs
			flush()
			out[string(r)] = struct{}{}
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	return out
}

// JaccardSimilarity 计算两个 token set 的 Jaccard 相似度 = |A ∩ B| / |A ∪ B|。
// 空集 / nil 安全: 若任一空 → 返回 0 (避免 panic)。
//
// v0.10.23 候选 2: 从 belief/convergence.go:172-194 抽出, 让 react_runner.go
// 复用同一份 Jaccard 计算逻辑。
func JaccardSimilarity(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	union := 0
	for k := range a {
		if _, ok := b[k]; ok {
			intersection++
		}
		union++
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}
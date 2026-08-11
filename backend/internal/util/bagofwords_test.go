package util

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// v0.10.23 候选 2: BagOfWords + JaccardSimilarity 单测

// --- BagOfWords ---

func TestBagOfWords_EmptyString(t *testing.T) {
	require.Equal(t, map[string]struct{}{}, BagOfWords(""))
}

func TestBagOfWords_English(t *testing.T) {
	tokens := BagOfWords("Hello world hello")
	// hello 出现 2 次但 set 去重, world 1 次, 大小写归一
	require.Len(t, tokens, 2)
	require.Contains(t, tokens, "hello")
	require.Contains(t, tokens, "world")
}

func TestBagOfWords_ChineseSeparated(t *testing.T) {
	tokens := BagOfWords("你好世界")
	require.Len(t, tokens, 4)
	require.Contains(t, tokens, "你")
	require.Contains(t, tokens, "好")
	require.Contains(t, tokens, "世")
	require.Contains(t, tokens, "界")
}

func TestBagOfWords_MixedCJKAndLatin(t *testing.T) {
	tokens := BagOfWords("Hello 你好世界")
	require.Contains(t, tokens, "hello")
	require.Contains(t, tokens, "你")
	require.Contains(t, tokens, "好")
	require.Contains(t, tokens, "世")
	require.Contains(t, tokens, "界")
}

func TestBagOfWords_PunctuationAsSeparator(t *testing.T) {
	tokens := BagOfWords("hello,world.hello;world")
	require.Len(t, tokens, 2)
	require.Contains(t, tokens, "hello")
	require.Contains(t, tokens, "world")
}

func TestBagOfWords_AllSeparators(t *testing.T) {
	tokens := BagOfWords("!@#$%^&*()")
	require.Equal(t, map[string]struct{}{}, tokens)
}

func TestBagOfWords_Numbers(t *testing.T) {
	tokens := BagOfWords("abc 123 def456")
	// abc / def456 是字母+数字混合 token, 123 单独数字 token
	require.Contains(t, tokens, "abc")
	require.Contains(t, tokens, "123")
	require.Contains(t, tokens, "def456")
}

// --- JaccardSimilarity ---

func TestJaccardSimilarity_Identical(t *testing.T) {
	a := BagOfWords("hello world")
	b := BagOfWords("hello world")
	require.Equal(t, 1.0, JaccardSimilarity(a, b))
}

func TestJaccardSimilarity_Disjoint(t *testing.T) {
	a := BagOfWords("hello world")
	b := BagOfWords("foo bar")
	require.Equal(t, 0.0, JaccardSimilarity(a, b))
}

func TestJaccardSimilarity_PartialOverlap(t *testing.T) {
	a := BagOfWords("hello world foo")
	b := BagOfWords("hello world bar")
	// 交集 {hello, world} = 2, 并集 {hello, world, foo, bar} = 4
	require.Equal(t, 0.5, JaccardSimilarity(a, b))
}

func TestJaccardSimilarity_EmptyA(t *testing.T) {
	a := map[string]struct{}{}
	b := BagOfWords("hello")
	require.Equal(t, 0.0, JaccardSimilarity(a, b))
}

func TestJaccardSimilarity_EmptyB(t *testing.T) {
	a := BagOfWords("hello")
	b := map[string]struct{}{}
	require.Equal(t, 0.0, JaccardSimilarity(a, b))
}

func TestJaccardSimilarity_BothEmpty(t *testing.T) {
	a := map[string]struct{}{}
	b := map[string]struct{}{}
	require.Equal(t, 0.0, JaccardSimilarity(a, b))
}

func TestJaccardSimilarity_CJKPartial(t *testing.T) {
	a := BagOfWords("你好世界")
	b := BagOfWords("你好世界不同")
	// 交集 {你, 好, 世, 界} = 4, 并集 {你, 好, 世, 界, 不, 同} = 6
	require.Equal(t, 4.0/6.0, JaccardSimilarity(a, b))
}

func TestJaccardSimilarity_CaseInsensitive(t *testing.T) {
	a := BagOfWords("Hello World")
	b := BagOfWords("hello world")
	require.Equal(t, 1.0, JaccardSimilarity(a, b))
}

// 防回归: BagOfWords 与 JaccardSimilarity 行为稳定 (确保 v0.10.23 后
// belief/convergence.go:172-194 与 agent/react_runner.go 算出同一 jaccard)
func TestBagOfWordsAndJaccard_DeterministicAcrossRuns(t *testing.T) {
	input := strings.Repeat("你好 ", 10) + strings.Repeat("Hello ", 5)
	a := BagOfWords(input)
	b := BagOfWords(input)
	j1 := JaccardSimilarity(a, b)
	require.Equal(t, 1.0, j1)
}
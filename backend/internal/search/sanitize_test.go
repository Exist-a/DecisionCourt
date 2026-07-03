package search

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeQuery_Empty(t *testing.T) {
	_, err := SanitizeQuery("")
	assert.Error(t, err)
	_, err = SanitizeQuery("   \t\n  ")
	assert.Error(t, err, "whitespace-only must be rejected")
}

func TestSanitizeQuery_TrimsWhitespace(t *testing.T) {
	got, err := SanitizeQuery("  hello world  \n")
	require.NoError(t, err)
	assert.Equal(t, "hello world", got)
}

func TestSanitizeQuery_MaxLength(t *testing.T) {
	// 201 chars → reject
	q := strings.Repeat("a", QueryMaxLen+1)
	_, err := SanitizeQuery(q)
	assert.Error(t, err, "query longer than max must be rejected")
	// 200 chars → accept
	q = strings.Repeat("a", QueryMaxLen)
	got, err := SanitizeQuery(q)
	require.NoError(t, err)
	assert.Equal(t, QueryMaxLen, len([]rune(got)))
}

func TestSanitizeQuery_StripsControlChars(t *testing.T) {
	got, err := SanitizeQuery("hello\x00\x01\x02 world\x7F")
	require.NoError(t, err)
	assert.Equal(t, "hello world", got, "ASCII control chars must be stripped")
}

func TestSanitizeQuery_PreservesNewlinesAndTabs(t *testing.T) {
	// \t \n \r 在 query 中合法(用户可能搜 multi-line 文本)
	got, err := SanitizeQuery("line1\nline2\tend")
	require.NoError(t, err)
	assert.Equal(t, "line1\nline2\tend", got)
}

func TestSanitizeQuery_AllControlChars(t *testing.T) {
	_, err := SanitizeQuery("\x00\x01\x02\x03")
	assert.Error(t, err, "pure-control query must be rejected")
}

func TestSanitizeQuery_Unicode(t *testing.T) {
	// 中文 utf-8 字符应当保留
	got, err := SanitizeQuery("  决策法庭 v0.8.3 上线  ")
	require.NoError(t, err)
	assert.Equal(t, "决策法庭 v0.8.3 上线", got)
}

func TestSanitizeQuery_MultibyteRuneCount(t *testing.T) {
	// 200 个中文 rune(=600 bytes)→ accept
	q := strings.Repeat("你", QueryMaxLen)
	got, err := SanitizeQuery(q)
	require.NoError(t, err)
	assert.Equal(t, QueryMaxLen, len([]rune(got)))
	// 201 个中文 rune → reject
	q = strings.Repeat("你", QueryMaxLen+1)
	_, err = SanitizeQuery(q)
	assert.Error(t, err)
}

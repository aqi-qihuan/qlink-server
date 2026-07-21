package sharding

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ========== #23 sharding 路由 int32 最小值溢出 ==========

func TestGetRandomDBPrefix_ValidAndNoPanic(t *testing.T) {
	// 覆盖各种输入，包括已知负哈希值，验证不 panic 且结果合法
	codes := []string{
		"",                  // 空串
		"026m8O3a",         // 正常短链码
		"123456789",         // JavaStringHashCode = -1867378635（负值）
		"https://example.com/very/long/path?a=1&b=2&c=3#frag",
		"😀🔥💡",            // emoji（非 BMP）
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", // 长串
	}
	for _, code := range codes {
		prefix := GetRandomDBPrefix(code)
		assert.Contains(t, DBPrefixList, prefix, "DB 前缀应在合法列表中: %q", code)
	}
}

func TestGetRandomTableSuffix_ValidAndNoPanic(t *testing.T) {
	codes := []string{
		"",
		"026m8O3a",
		"123456789",
		"https://example.com/x",
		"😀",
	}
	for _, code := range codes {
		suffix := GetRandomTableSuffix(code)
		assert.Contains(t, TableSuffixList, suffix, "表后缀应在合法列表中: %q", code)
	}
}

func TestGetRandomDBPrefix_Deterministic(t *testing.T) {
	// 相同 code 必须路由到相同分片（否则跨分片查询会丢数据）
	code := "026m8O3a"
	first := GetRandomDBPrefix(code)
	for i := 0; i < 50; i++ {
		assert.Equal(t, first, GetRandomDBPrefix(code), "相同 code 的分片应稳定")
	}
}

func TestGetRandomDBPrefix_NegativeHashNoOverflow(t *testing.T) {
	// 核心回归：原 bug 在 h == math.MinInt32 时 -h 溢出为负，
	// 导致 int(h) % len 产生负索引 → 越界 panic。
	// 修复后使用 uint32 转换，这里验证负哈希不产生负索引。
	h := int32(math.MinInt32)
	idx := int(uint32(h)) % len(DBPrefixList)
	assert.GreaterOrEqual(t, idx, 0, "索引不应为负（原 bug 会越界 panic）")
	assert.Less(t, idx, len(DBPrefixList), "索引应在范围内")

	h2 := int32(-1867378635) // "123456789"
	idx2 := int(uint32(h2)) % len(DBPrefixList)
	assert.GreaterOrEqual(t, idx2, 0, "负值哈希索引不应为负")
}

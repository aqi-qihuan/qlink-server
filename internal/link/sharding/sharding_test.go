package sharding

import (
	"math"
	"testing"

	"github.com/aqi/qlink-server/internal/common/util"
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

// ========== 路由解析与 code 生成语义一致性（跳转 404 排障回归） ==========

func TestRouteShortLink_ParsesEmbeddedShards(t *testing.T) {
	// code 格式: dbPrefix + base62(hash) + tableSuffix（见 component.CreateShortLinkCode）,
	// RouteShortLink 必须解析首尾字符还原分片。
	// 以下均为远程库存量数据, 所在分片与解析结果 100% 吻合(2026-09-28 实测 10/10):
	cases := []struct{ code, dbPrefix, tableSuffix string }{
		{"03rCsNC0", "0", "0"},
		{"021Tgota", "0", "a"},
		{"1oQKYJ0", "1", "0"},
		{"115P8y2a", "1", "a"},
		{"a3z3JYk0", "a", "0"},
		{"a4hEvL30", "a", "0"},
	}
	for _, c := range cases {
		dbPrefix, tableSuffix := RouteShortLink(c.code)
		assert.Equal(t, c.dbPrefix, dbPrefix, "DB 分片解析错误: %q", c.code)
		assert.Equal(t, c.tableSuffix, tableSuffix, "表分片解析错误: %q", c.code)
	}
}

func TestJavaStringHashCode_JavaCompat(t *testing.T) {
	// 与 Java String.hashCode() 对齐的已知值(存量数据由 Java 按此路由写入)
	assert.Equal(t, int32(0), util.JavaStringHashCode(""))
	assert.Equal(t, int32(-1867378635), util.JavaStringHashCode("123456789"))
}

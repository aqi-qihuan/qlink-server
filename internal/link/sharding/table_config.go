package sharding

import "github.com/aqi/qlink-server/internal/common/util"

// TableSuffixList contains the active table shard suffixes.
// Matches Java's ShardingTableConfig.tableSuffixList: ["0", "a"]
var TableSuffixList = []string{"0", "a"}

// GetRandomTableSuffix returns a deterministic table suffix based on the base62 code.
// Uses Java's String.hashCode() algorithm for compatibility.
func GetRandomTableSuffix(code string) string {
	h := util.JavaStringHashCode(code)
	// Use unsigned conversion to avoid negative index from int32 overflow.
	idx := int(uint32(h)) % len(TableSuffixList)
	return TableSuffixList[idx]
}

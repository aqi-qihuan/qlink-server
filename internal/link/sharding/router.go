package sharding

// RouteShortLink returns the DB prefix and table suffix for a given short link code.
// The code format is: dbPrefix + base62(hash) + tableSuffix (see component.CreateShortLinkCode),
// so the shard information is embedded in the code itself: first character selects the DB,
// last character selects the table. This matches the Java implementation and MUST be kept
// in sync with component.CreateShortLinkCode — do NOT replace it with hash-based routing,
// which caused a read/write shard mismatch (lookups scanned a different table than writes).
func RouteShortLink(code string) (dbPrefix string, tableSuffix string) {
	if len(code) == 0 {
		return "0", "0"
	}
	dbPrefix = string(code[0])
	tableSuffix = string(code[len(code)-1])
	return
}

// RouteGroupCodeMapping returns the DB index and table index for group_code_mapping.
// DB shard: account_no % 2 (0 or 1)
// Table shard: group_id % 2 (0 or 1)
func RouteGroupCodeMapping(accountNo int64, groupId int64) (dbIndex int, tableIndex int) {
	dbIndex = int(accountNo%2) & 0x7FFFFFFF // ensure non-negative
	tableIndex = int(groupId%2) & 0x7FFFFFFF
	return
}

// RouteLinkGroup returns the DB index for link_group.
// DB shard: account_no % 2
func RouteLinkGroup(accountNo int64) int {
	return int(accountNo%2) & 0x7FFFFFFF
}

// GetTableName returns the physical table name by appending the suffix.
func GetTableName(logicName string, suffix string) string {
	return logicName + "_" + suffix
}

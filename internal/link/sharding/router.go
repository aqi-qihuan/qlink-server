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

// IsValidShortLinkCode reports whether the code can be routed to an existing
// physical shard. The system only creates short_link_<suffix> tables for the
// suffixes in TableSuffixList, and only opens the databases in DBPrefixList.
// A code whose first/last character falls outside those lists (e.g. a random
// string typed by a crawler) would route to a non-existent table/database and
// surface a 500. Callers on public endpoints must reject it as 404 before any
// DB query. Note this validates routing feasibility, NOT existence in the DB.
func IsValidShortLinkCode(code string) bool {
	if len(code) < 3 { // need at least dbPrefix + one hash char + tableSuffix
		return false
	}
	if !contains(DBPrefixList, string(code[0])) {
		return false
	}
	return contains(TableSuffixList, string(code[len(code)-1]))
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
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

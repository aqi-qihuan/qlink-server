package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/aqi/qlink-server/internal/ai/llm"
)

// AnalyticsAgent converts natural language questions into ClickHouse SQL queries.
type AnalyticsAgent struct {
	chatModel model.ChatModel
}

func NewAnalyticsAgent(cm model.ChatModel) *AnalyticsAgent {
	return &AnalyticsAgent{
		chatModel: cm,
	}
}

// AnalyticsResult holds the generated SQL and explanation.
type AnalyticsResult struct {
	SQL         string `json:"sql"`
	Explanation string `json:"explanation"`
}

const clickHouseSchema = `Table: visit_stats (ClickHouse MergeTree)
Columns:
  code          String    -- 短链码
  referer       String    -- 来源页面
  is_new        String    -- 是否新访问 ('0'/'1')
  account_no    UInt64    -- 账号编号
  province      String    -- 省份
  city          String    -- 城市
  ip            String    -- 访客IP
  browser_name  String    -- 浏览器
  os            String    -- 操作系统
  device_type   String    -- 设备类型 (PC/Mobile/Tablet)
  pv            UInt64    -- 页面浏览量
  uv            UInt64    -- 独立访客数
  start_time    DateTime  -- 访问开始时间
  end_time      DateTime  -- 访问结束时间
  ts            UInt64    -- 访问时间戳(毫秒)

ClickHouse specific functions: toYYYYMMDD(), toHour(), toMinute(), toYYYYMMDDhhmmss()`

const analyticsSystemPrompt = `You are a ClickHouse SQL expert. Given the table schema below, generate a SQL query to answer the user's question.

%s

Rules:
1. Always filter by account_no and code when a short link code is provided.
2. Use ClickHouse-specific functions (toYYYYMMDD, toHour, toMinute) for date operations.
3. Return a JSON object with "sql" and "explanation" fields.
4. "explanation" should be a brief Chinese description of what the query does.
5. Return ONLY valid JSON, no other text.

Example response:
{"sql": "SELECT count() FROM visit_stats WHERE code = 'abc'", "explanation": "查询短链abc的总访问量"}`

// Query converts a natural language question into a ClickHouse SQL query.
func (a *AnalyticsAgent) Query(ctx context.Context, question string, shortLinkCode string) (*AnalyticsResult, error) {
	systemPrompt := fmt.Sprintf(analyticsSystemPrompt, clickHouseSchema)

	userMsg := fmt.Sprintf("Question: %s", question)
	if shortLinkCode != "" {
		userMsg = fmt.Sprintf("Short link code: %s\nQuestion: %s", shortLinkCode, question)
	}

	messages := []*schema.Message{
		schema.SystemMessage(systemPrompt),
		schema.UserMessage(userMsg),
	}

	resp, err := a.chatModel.Generate(ctx, messages,
		model.WithMaxTokens(4096),
		model.WithTemperature(0.3),
	)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	jsonStr := llm.ExtractJSON(resp.Content)

	var result AnalyticsResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		// Fallback: return raw response as explanation
		return &AnalyticsResult{
			Explanation: resp.Content,
		}, nil
	}

	// Validate SQL safety before returning
	if result.SQL != "" {
		if err := ValidateSQL(result.SQL); err != nil {
			return nil, err
		}
	}
	return &result, nil
}

// dangerousSQLKeywords are SQL keywords that modify or delete data.
var dangerousSQLKeywords = []string{
	"DROP", "DELETE", "UPDATE", "INSERT", "ALTER", "TRUNCATE",
	"CREATE", "REPLACE", "RENAME", "GRANT", "REVOKE",
}

// ValidateSQL checks that the generated SQL is a safe SELECT-only query.
func ValidateSQL(sql string) error {
	upper := strings.TrimSpace(strings.ToUpper(sql))

	// Must start with SELECT or WITH (CTE)
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return fmt.Errorf("SQL must be a SELECT query, got: %s", upper[:min(len(upper), 20)])
	}

	// Check for dangerous keywords as whole words
	for _, keyword := range dangerousSQLKeywords {
		// Use word boundary check: keyword must be preceded by space/start and followed by space/end
		idx := 0
		for {
			pos := strings.Index(upper[idx:], keyword)
			if pos == -1 {
				break
			}
			pos += idx
			end := pos + len(keyword)
			// Check word boundaries
			beforeOK := pos == 0 || upper[pos-1] == ' ' || upper[pos-1] == '\n' || upper[pos-1] == '\t' || upper[pos-1] == '(' || upper[pos-1] == ';'
			afterOK := end >= len(upper) || upper[end] == ' ' || upper[end] == '\n' || upper[end] == '\t' || upper[end] == '(' || upper[end] == ')' || upper[end] == ';'
			if beforeOK && afterOK {
				return fmt.Errorf("SQL contains forbidden keyword: %s", keyword)
			}
			idx = end
		}
	}

	// Reject multiple statements (semicolons outside of quotes)
	if strings.Count(sql, ";") > 1 {
		return fmt.Errorf("SQL must not contain multiple statements")
	}

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

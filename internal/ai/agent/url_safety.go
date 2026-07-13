package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/aqi/qlink-server/internal/ai/llm"
)

// URLSafetyAgent detects malicious URLs before creating short links.
type URLSafetyAgent struct {
	chatModel model.ChatModel
}

func NewURLSafetyAgent(cm model.ChatModel) *URLSafetyAgent {
	return &URLSafetyAgent{
		chatModel: cm,
	}
}

// SafetyResult holds the URL safety analysis.
type SafetyResult struct {
	Safe   bool     `json:"safe"`
	Reason string   `json:"reason,omitempty"`
	Tags   []string `json:"tags,omitempty"` // phishing, malware, scam, gambling, etc.
	Score  float64  `json:"score"`          // 0.0 (safe) to 1.0 (dangerous)
}

const safetySystemPrompt = `You are a URL security analyst. Analyze the given URL for safety concerns.
Check for: phishing, malware distribution, scam patterns, gambling, adult content, fraud.

Return a JSON object with:
- "safe": boolean (true if the URL appears safe)
- "reason": brief Chinese explanation of the verdict
- "tags": array of risk category tags (e.g., "钓鱼", "恶意软件", "诈骗", "赌博")
- "score": risk score from 0.0 (completely safe) to 1.0 (extremely dangerous)

Return ONLY valid JSON, no other text.`

// Analyze checks if a URL is potentially malicious.
func (a *URLSafetyAgent) Analyze(ctx context.Context, url string) (*SafetyResult, error) {
	messages := []*schema.Message{
		schema.SystemMessage(safetySystemPrompt),
		schema.UserMessage(fmt.Sprintf("Analyze this URL: %s", url)),
	}

	resp, err := a.chatModel.Generate(ctx, messages,
		model.WithMaxTokens(2048),
		model.WithTemperature(0.3),
	)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	jsonStr := llm.ExtractJSON(resp.Content)

	var result SafetyResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		// Fallback: assume safe if LLM response can't be parsed
		return &SafetyResult{
			Safe:   true,
			Score:  0.0,
			Reason: resp.Content,
		}, nil
	}
	return &result, nil
}

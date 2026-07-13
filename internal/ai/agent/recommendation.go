package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/aqi/qlink-server/internal/ai/llm"
)

// RecommendationAgent provides smart short link recommendations.
type RecommendationAgent struct {
	chatModel model.ChatModel
}

func NewRecommendationAgent(cm model.ChatModel) *RecommendationAgent {
	return &RecommendationAgent{
		chatModel: cm,
	}
}

// RecommendationResult holds the AI-generated suggestions.
type RecommendationResult struct {
	Title        string   `json:"title"`
	GroupSuggest string   `json:"group_suggest"`
	Tags         []string `json:"tags"`
	Summary      string   `json:"summary"`
}

const recommendationSystemPrompt = `You are a URL analysis assistant. Analyze the given URL and return a JSON object with these fields:
- "title": a concise Chinese title (max 20 chars)
- "group_suggest": a suggested group/category name in Chinese
- "tags": an array of 2-4 relevant tags in Chinese
- "summary": a one-sentence Chinese summary of what the URL links to

Return ONLY valid JSON, no other text.`

// Recommend analyzes a URL and returns suggested title/group/tags.
func (a *RecommendationAgent) Recommend(ctx context.Context, url string) (*RecommendationResult, error) {
	messages := []*schema.Message{
		schema.SystemMessage(recommendationSystemPrompt),
		schema.UserMessage(fmt.Sprintf("Analyze this URL: %s", url)),
	}

	resp, err := a.chatModel.Generate(ctx, messages,
		model.WithMaxTokens(2048),
		model.WithTemperature(0.7),
	)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	jsonStr := llm.ExtractJSON(resp.Content)

	var result RecommendationResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		// Fallback: return raw response as summary
		return &RecommendationResult{
			Title:   "",
			Summary: resp.Content,
		}, nil
	}
	return &result, nil
}

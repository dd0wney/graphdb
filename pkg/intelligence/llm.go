package intelligence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type LLMClient struct {
	APIKey string
	Model  string
	BaseURL string
}

func NewLLMClient(model string) *LLMClient {
	return &LLMClient{
		APIKey:  os.Getenv("ANTHROPIC_API_KEY"),
		Model:   model,
		BaseURL: "https://api.anthropic.com/v1/messages",
	}
}

func (c *LLMClient) Generate(ctx context.Context, prompt string) (string, error) {
	if c.APIKey == "" {
		// Mock response for development/testing if no API key is provided
		return fmt.Sprintf("[MOCK RESPONSE for model %s]: You asked: %s", c.Model, prompt), nil
	}

	payload := map[string]any{
		"model": c.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens": 1024,
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", c.BaseURL, bytes.NewBuffer(body))
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("LLM API error %d: %s", resp.StatusCode, string(b))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty response from LLM")
	}

	return result.Content[0].Text, nil
}

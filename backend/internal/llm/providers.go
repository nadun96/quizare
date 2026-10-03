package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Provider marks one answer. Implementations must honour ctx cancellation,
// because River can only cancel jobs through the context (ADR-06).
type Provider interface {
	Grade(ctx context.Context, apiKey, model string, r GradeRequest) (GradeResult, error)
}

// PermanentError means retrying cannot help: an invalid key, exhausted quota
// or an unknown model. The answer falls back to manual marking (UC-04 exception).
type PermanentError struct{ Reason string }

func (e *PermanentError) Error() string { return e.Reason }

func classify(status int, body string) error {
	switch {
	case status == 401 || status == 403:
		return &PermanentError{"the API key was rejected by the provider"}
	case status == 402:
		return &PermanentError{"the provider account has no credit or quota left"}
	case status == 404 || status == 400 || status == 422:
		return &PermanentError{fmt.Sprintf("the provider rejected the request (%d): %s", status, truncate(body, 200))}
	case status == 429 && strings.Contains(strings.ToLower(body), "quota"):
		return &PermanentError{"the provider quota is exhausted"}
	default: // 429 rate limit, 5xx, 529 overloaded: retry with backoff
		return fmt.Errorf("provider returned %d: %s", status, truncate(body, 200))
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func decodeResult(text string) (GradeResult, error) {
	var r GradeResult
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(strings.TrimSuffix(strings.TrimPrefix(text, "```json"), "```"), "```")
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &r); err != nil {
		return r, fmt.Errorf("provider did not return the expected JSON: %w", err)
	}
	return r, nil
}

// Default models when a key is registered without one.
var DefaultModels = map[string]string{
	"anthropic": "claude-opus-5-5",
}

// ---------- Anthropic (official Go SDK) ----------

type Anthropic struct{ BaseURL string } // BaseURL overrides the API host in tests

func (p Anthropic) Grade(ctx context.Context, apiKey, model string, r GradeRequest) (GradeResult, error) {
	opts := []option.RequestOption{option.WithAPIKey(apiKey), option.WithMaxRetries(0)} // River owns retries
	if p.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(p.BaseURL))
	}
	client := anthropic.NewClient(opts...)
	resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: 16000,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt(r)))},
		OutputConfig: anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffort("medium"),
			Format: anthropic.JSONOutputFormatParam{Schema: schema()},
		},
	})
	if err != nil {
		var apierr *anthropic.Error
		if errors.As(err, &apierr) {
			return GradeResult{}, classify(apierr.StatusCode, apierr.Error())
		}
		return GradeResult{}, err
	}
	if resp.StopReason == anthropic.StopReasonRefusal {
		return GradeResult{}, &PermanentError{"the model declined to mark this answer"}
	}
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			return decodeResult(t.Text)
		}
	}
	return GradeResult{}, errors.New("the model returned no text")
}

// ---------- OpenAI (Chat Completions with JSON-schema output) ----------

type OpenAI struct {
	BaseURL string
	Client  *http.Client
}

func (p OpenAI) Grade(ctx context.Context, apiKey, model string, r GradeRequest) (GradeResult, error) {
	base := p.BaseURL
	if base == "" {
		base = "https://api.openai.com"
	}
	body := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt(r)},
		},
		"response_format": map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": "grade", "strict": true, "schema": schema()},
		},
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := postJSON(ctx, p.Client, base+"/v1/chat/completions", map[string]string{"Authorization": "Bearer " + apiKey}, body, &out); err != nil {
		return GradeResult{}, err
	}
	if len(out.Choices) == 0 {
		return GradeResult{}, errors.New("the model returned no choices")
	}
	if out.Choices[0].Message.Refusal != "" {
		return GradeResult{}, &PermanentError{"the model declined to mark this answer"}
	}
	return decodeResult(out.Choices[0].Message.Content)
}

// ---------- Google Gemini (generateContent with a response schema) ----------

type Google struct {
	BaseURL string
	Client  *http.Client
}

func (p Google) Grade(ctx context.Context, apiKey, model string, r GradeRequest) (GradeResult, error) {
	base := p.BaseURL
	if base == "" {
		base = "https://generativelanguage.googleapis.com"
	}
	sch := schema()
	delete(sch, "additionalProperties") // not part of Gemini's schema dialect
	body := map[string]any{
		"systemInstruction": map[string]any{"parts": []map[string]string{{"text": systemPrompt}}},
		"contents":          []map[string]any{{"role": "user", "parts": []map[string]string{{"text": userPrompt(r)}}}},
		"generationConfig":  map[string]any{"responseMimeType": "application/json", "responseSchema": sch},
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
	}
	// The key goes in a header, never the URL, so it cannot leak into logs.
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent", base, model)
	if err := postJSON(ctx, p.Client, url, map[string]string{"x-goog-api-key": apiKey}, body, &out); err != nil {
		return GradeResult{}, err
	}
	if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return GradeResult{}, &PermanentError{"the model returned no answer (possibly blocked)"}
	}
	return decodeResult(out.Candidates[0].Content.Parts[0].Text)
}

var defaultHTTP = &http.Client{Timeout: 100 * time.Second}

func postJSON(ctx context.Context, c *http.Client, url string, headers map[string]string, body, out any) error {
	if c == nil {
		c = defaultHTTP
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return err // network errors are retryable
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return classify(resp.StatusCode, string(data))
	}
	return json.Unmarshal(data, out)
}

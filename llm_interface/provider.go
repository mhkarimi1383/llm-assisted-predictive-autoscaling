// Chat providers for the LLM Interface layer.
//
// The OpenAI provider talks to any OpenAI-compatible
// chat-completions API (Ollama, vLLM, OpenAI, Groq, ...). Request
// scoped endpoint, API key, and model overrides (from the
// LLMAutoscaler spec) take precedence over the service defaults.
package llminterface

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

	llminterfacepb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/llminterface"
)

// maxChatAttempts bounds retries for retryable failures. The total
// time is additionally bounded by the context deadline (Algorithm 3:
// retry delays and attempt budgets come out of the cycle deadline).
const maxChatAttempts = 3

// retryDelay is the fixed pause between attempts; the caller deadline
// bounds the overall budget.
const retryDelay = 500 * time.Millisecond

// ChatProvider produces one assistant message for a chat request.
type ChatProvider interface {
	Chat(ctx context.Context, req *llminterfacepb.ChatRequest) (*llminterfacepb.ChatResponse, error)
}

// ChatConfig is the service-level default provider configuration.
type ChatConfig struct {
	// Endpoint is an OpenAI-compatible base URL such as
	// "http://ollama:11434/v1", or "mock://" for the built-in
	// deterministic mock provider.
	Endpoint string
	// APIKey is the default bearer token (may be empty).
	APIKey string
	// Model is the default model name (may be empty).
	Model string
	// Timeout bounds a single chat attempt.
	Timeout time.Duration
	// ExtraJSON, when set, is parsed as a JSON object and merged
	// under the request payload — provider-specific options that the
	// generic ChatRequest does not model (e.g. Z.AI's
	// {"thinking":{"type":"disabled"}}). Payload fields always win.
	ExtraJSON string
}

// chatRequestPayload is the OpenAI chat-completions request body.
type chatRequestPayload struct {
	Model          string              `json:"model"`
	Messages       []chatMessageProp   `json:"messages"`
	Temperature    float64             `json:"temperature,omitempty"`
	MaxTokens      int32               `json:"max_tokens,omitempty"`
	ResponseFormat *responseFormatProp `json:"response_format,omitempty"`
	Stream         bool                `json:"stream"`
}

type chatMessageProp struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormatProp struct {
	Type string `json:"type"`
}

// chatResponsePayload is the OpenAI chat-completions response body.
type chatResponsePayload struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int32 `json:"prompt_tokens"`
		CompletionTokens int32 `json:"completion_tokens"`
	} `json:"usage"`
	Model string `json:"model"`
}

// openAIProvider implements ChatProvider against an
// OpenAI-compatible chat-completions API.
type openAIProvider struct {
	defaultEndpoint string
	defaultAPIKey   string
	defaultModel    string
	extraJSON       string
	client          *http.Client
}

func newOpenAIProvider(cfg ChatConfig) *openAIProvider {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &openAIProvider{
		defaultEndpoint: strings.TrimRight(cfg.Endpoint, "/"),
		defaultAPIKey:   cfg.APIKey,
		defaultModel:    cfg.Model,
		extraJSON:       cfg.ExtraJSON,
		client:          &http.Client{Timeout: timeout},
	}
}

// Chat performs a chat-completions call with bounded retries.
func (p *openAIProvider) Chat(ctx context.Context, req *llminterfacepb.ChatRequest) (*llminterfacepb.ChatResponse, error) {
	if len(req.GetMessages()) == 0 {
		return nil, fmt.Errorf("chat request has no messages")
	}

	endpoint := p.defaultEndpoint
	if req.GetEndpoint() != "" {
		endpoint = strings.TrimRight(req.GetEndpoint(), "/")
	}
	if endpoint == "" {
		return nil, fmt.Errorf("no LLM endpoint configured")
	}
	apiKey := p.defaultAPIKey
	if req.GetApiKey() != "" {
		apiKey = req.GetApiKey()
	}
	model := p.defaultModel
	if req.GetModel() != "" {
		model = req.GetModel()
	}
	if model == "" {
		return nil, fmt.Errorf("no LLM model configured")
	}

	payload := chatRequestPayload{
		Model:       model,
		Messages:    make([]chatMessageProp, 0, len(req.GetMessages())),
		Temperature: req.GetTemperature(),
		MaxTokens:   req.GetMaxTokens(),
		Stream:      false,
	}
	for _, m := range req.GetMessages() {
		payload.Messages = append(payload.Messages, chatMessageProp{Role: m.GetRole(), Content: m.GetContent()})
	}
	if req.GetJsonMode() {
		payload.ResponseFormat = &responseFormatProp{Type: "json_object"}
	}

	var body []byte
	if p.extraJSON != "" {
		merged := map[string]any{}
		if err := json.Unmarshal([]byte(p.extraJSON), &merged); err == nil {
			base, _ := json.Marshal(payload)
			if json.Unmarshal(base, &merged) == nil {
				body, err = json.Marshal(merged)
				if err != nil {
					return nil, fmt.Errorf("failed to encode chat request: %w", err)
				}
			}
		}
	}
	if body == nil {
		var mErr error
		body, mErr = json.Marshal(payload)
		if mErr != nil {
			return nil, fmt.Errorf("failed to encode chat request: %w", mErr)
		}
	}

	var lastErr error
	for attempt := 1; attempt <= maxChatAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("chat cancelled before attempt %d: %w", attempt, err)
		}

		resp, err := p.attempt(ctx, endpoint, apiKey, body)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		// Retry only transient failures while the deadline allows it.
		if !isRetryable(err) || attempt == maxChatAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("chat cancelled during retry wait: %w", ctx.Err())
		case <-time.After(retryDelay):
		}
	}
	return nil, lastErr
}

func (p *openAIProvider) attempt(ctx context.Context, endpoint, apiKey string, body []byte) (*llminterfacepb.ChatResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build chat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, &retryableError{cause: fmt.Errorf("LLM request failed: %w", err)}
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, maxResponseBytes))
	if err != nil {
		return nil, &retryableError{cause: fmt.Errorf("failed to read LLM response: %w", err)}
	}

	if httpResp.StatusCode != http.StatusOK {
		if httpResp.StatusCode == http.StatusTooManyRequests || httpResp.StatusCode >= 500 {
			return nil, &retryableError{cause: fmt.Errorf("LLM API returned %d: %s", httpResp.StatusCode, truncateForLog(respBody))}
		}
		return nil, fmt.Errorf("LLM API returned %d: %s", httpResp.StatusCode, truncateForLog(respBody))
	}

	var parsed chatResponsePayload
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode LLM response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("LLM response contains no choices")
	}

	return &llminterfacepb.ChatResponse{
		Content:          parsed.Choices[0].Message.Content,
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		FinishReason:     parsed.Choices[0].FinishReason,
		Model:            parsed.Model,
	}, nil
}

// maxResponseBytes caps how much of a response body is read.
const maxResponseBytes = 4 << 20 // 4 MiB

// retryableError marks failures that are worth one more attempt.
type retryableError struct{ cause error }

func (e *retryableError) Error() string { return e.cause.Error() }
func (e *retryableError) Unwrap() error { return e.cause }

func isRetryable(err error) bool {
	var r *retryableError
	return errors.As(err, &r)
}

func truncateForLog(b []byte) string {
	s := string(b)
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}

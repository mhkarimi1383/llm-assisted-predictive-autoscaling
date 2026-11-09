package llminterface

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	llminterfacepb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/llminterface"
)

func chatRequest(content string) *llminterfacepb.ChatRequest {
	return &llminterfacepb.ChatRequest{
		Messages: []*llminterfacepb.ChatMessage{
			{Role: "system", Content: "You are an analyst."},
			{Role: "user", Content: content},
		},
		RequestId: "req-1",
	}
}

func TestMockProviderDecisions(t *testing.T) {
	p := newMockProvider()
	ctx := context.Background()

	cases := []struct {
		name       string
		prompt     string
		wantAction string
		wantTarget int32
	}{
		{
			name:       "high cpu scales up",
			prompt:     `{"role":"forecast","desired_replicas":4,"ready_replicas":4,"cpu_utilization":0.85}`,
			wantAction: "SCALE_UP",
			wantTarget: 5, // 4 + ceil(4*0.25)=1
		},
		{
			name:       "ready below desired scales up",
			prompt:     `{"role":"status","desired_replicas":4,"ready_replicas":2,"cpu_utilization":0.4}`,
			wantAction: "SCALE_UP",
			wantTarget: 5,
		},
		{
			name:       "low cpu scales down",
			prompt:     `{"role":"forecast","desired_replicas":4,"ready_replicas":4,"cpu_utilization":0.2}`,
			wantAction: "SCALE_DOWN",
			wantTarget: 3, // 4 - floor(4*0.25)=1
		},
		{
			// A genuinely idle workload (cpu exactly 0) must scale
			// down, not be mistaken for an unknown measurement.
			name:       "zero cpu scales down",
			prompt:     `{"role":"forecast","desired_replicas":8,"ready_replicas":8,"cpu_utilization":0.0}`,
			wantAction: "SCALE_DOWN",
			wantTarget: 6, // 8 - floor(8*0.25)=2
		},
		{
			name:       "moderate cpu holds",
			prompt:     `{"role":"forecast","desired_replicas":4,"ready_replicas":4,"cpu_utilization":0.5}`,
			wantAction: "HOLD",
			wantTarget: 4,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := p.Chat(ctx, chatRequest(tc.prompt))
			if err != nil {
				t.Fatalf("Chat failed: %v", err)
			}
			if resp.GetError() != "" {
				t.Fatalf("unexpected in-band error: %s", resp.GetError())
			}

			var got struct {
				Action string `json:"action"`
				Target int32  `json:"target"`
			}
			if err := json.Unmarshal([]byte(resp.GetContent()), &got); err != nil {
				t.Fatalf("mock returned non-JSON content %q: %v", resp.GetContent(), err)
			}
			if got.Action != tc.wantAction || got.Target != tc.wantTarget {
				t.Fatalf("got %s/%d, want %s/%d", got.Action, got.Target, tc.wantAction, tc.wantTarget)
			}
		})
	}
}

func TestMockProviderHistoryAbstains(t *testing.T) {
	p := newMockProvider()
	resp, err := p.Chat(context.Background(), chatRequest(`{"role":"history","desired_replicas":3}`))
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if !strings.Contains(resp.GetContent(), "abstention_reason") {
		t.Fatalf("history analyst should abstain without history, got: %s", resp.GetContent())
	}
}

func TestMockProviderHistoryOscillation(t *testing.T) {
	p := newMockProvider()

	// One action only: insufficient evidence -> abstain.
	resp, err := p.Chat(context.Background(), chatRequest(
		`{"role":"history","desired_replicas":5,"recent_actions":[{"action":"SCALE_UP","from_replicas":3,"to_replicas":5}]}`))
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if !strings.Contains(resp.GetContent(), "abstention_reason") {
		t.Fatalf("single action should abstain, got: %s", resp.GetContent())
	}

	// Two same-direction actions -> abstain (neutral).
	resp, err = p.Chat(context.Background(), chatRequest(
		`{"role":"history","desired_replicas":5,"recent_actions":[
			{"action":"SCALE_UP","from_replicas":2,"to_replicas":3},
			{"action":"SCALE_UP","from_replicas":3,"to_replicas":5}]}`))
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if !strings.Contains(resp.GetContent(), "abstention_reason") {
		t.Fatalf("same-direction history should abstain, got: %s", resp.GetContent())
	}

	// Direction reversal -> HOLD vote to dampen thrashing.
	resp, err = p.Chat(context.Background(), chatRequest(
		`{"role":"history","desired_replicas":5,"recent_actions":[
			{"action":"SCALE_UP","from_replicas":3,"to_replicas":5},
			{"action":"SCALE_DOWN","from_replicas":5,"to_replicas":4}]}`))
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if !strings.Contains(resp.GetContent(), `"action":"HOLD"`) {
		t.Fatalf("reversal should produce a HOLD vote, got: %s", resp.GetContent())
	}
}

func TestMockProviderDeterministic(t *testing.T) {
	p := newMockProvider()
	req := chatRequest(`{"role":"forecast","desired_replicas":4,"ready_replicas":4,"cpu_utilization":0.9}`)
	first, err := p.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	second, err := p.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if first.GetContent() != second.GetContent() {
		t.Fatalf("mock provider must be deterministic: %q vs %q", first.GetContent(), second.GetContent())
	}
}

func TestOpenAIProviderSuccess(t *testing.T) {
	var gotAuth string
	var gotPayload chatRequestPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `{"action":"HOLD"}`}, "finish_reason": "stop"},
			},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
			"model": "test-model",
		})
	}))
	defer srv.Close()

	p := newOpenAIProvider(ChatConfig{Endpoint: srv.URL + "/v1", APIKey: "secret", Model: "default-model", Timeout: 5 * time.Second})
	resp, err := p.Chat(context.Background(), chatRequest(`{"role":"forecast","desired_replicas":3}`))
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if resp.GetContent() != `{"action":"HOLD"}` || resp.GetFinishReason() != "stop" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("missing bearer token: %q", gotAuth)
	}
	if gotPayload.Model != "default-model" {
		t.Fatalf("model not sent: %q", gotPayload.Model)
	}
}

func TestOpenAIProviderJSONMode(t *testing.T) {
	var gotPayload chatRequestPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "{}"}, "finish_reason": "stop"}},
		})
	}))
	defer srv.Close()

	p := newOpenAIProvider(ChatConfig{Endpoint: srv.URL, Model: "m", Timeout: 5 * time.Second})
	req := chatRequest(`{"role":"status"}`)
	req.JsonMode = true
	if _, err := p.Chat(context.Background(), req); err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if gotPayload.ResponseFormat == nil || gotPayload.ResponseFormat.Type != "json_object" {
		t.Fatalf("json_mode must set response_format, got %+v", gotPayload.ResponseFormat)
	}
}

func TestOpenAIProviderRetriesThenSucceeds(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "ok"}, "finish_reason": "stop"}},
		})
	}))
	defer srv.Close()

	p := newOpenAIProvider(ChatConfig{Endpoint: srv.URL, Model: "m", Timeout: 5 * time.Second})
	resp, err := p.Chat(context.Background(), chatRequest(`{"role":"status"}`))
	if err != nil {
		t.Fatalf("Chat failed after retries: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if resp.GetContent() != "ok" {
		t.Fatalf("unexpected content %q", resp.GetContent())
	}
}

func TestOpenAIProviderPermanentErrorNotRetried(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	p := newOpenAIProvider(ChatConfig{Endpoint: srv.URL, Model: "m", Timeout: 5 * time.Second})
	_, err := p.Chat(context.Background(), chatRequest(`{"role":"status"}`))
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if attempts != 1 {
		t.Fatalf("400 must not be retried, got %d attempts", attempts)
	}
}

func TestOpenAIProviderRequestScopedOverrides(t *testing.T) {	var gotAuth string
	var gotPath string
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "hi"}, "finish_reason": "stop"}},
		})
	}))
	defer secondary.Close()

	p := newOpenAIProvider(ChatConfig{Endpoint: "http://127.0.0.1:1/unreachable", Model: "default", Timeout: 2 * time.Second})
	req := chatRequest(`{"role":"status"}`)
	req.Endpoint = secondary.URL + "/api/v1"
	req.ApiKey = "per-request-key"
	resp, err := p.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if resp.GetContent() != "hi" {
		t.Fatalf("unexpected content %q", resp.GetContent())
	}
	if gotPath != "/api/v1/chat/completions" {
		t.Fatalf("request endpoint override ignored, path: %s", gotPath)
	}
	if gotAuth != "Bearer per-request-key" {
		t.Fatalf("request api key override ignored: %q", gotAuth)
	}
}

// --- per-request provider routing (service default + CR overrides) ---

func TestProviderRouting(t *testing.T) {
	s := &llmInterfaceServer{
		chat:           newMockProvider(),
		openAIProvider: newOpenAIProvider(ChatConfig{Model: "m", Timeout: 5 * time.Second}),
		mockProvider:   newMockProvider(),
	}

	// No override -> service default.
	if got := s.providerFor(chatRequest("{}")); got != s.chat {
		t.Fatal("empty endpoint override must route to the service default")
	}
	// mock:// override -> mock provider.
	req := chatRequest("{}")
	req.Endpoint = "mock://"
	if got := s.providerFor(req); got != s.mockProvider {
		t.Fatal("mock:// override must route to the mock provider")
	}
	// Real URL override -> openAI provider.
	req2 := chatRequest("{}")
	req2.Endpoint = "http://real.example/v1"
	if got := s.providerFor(req2); got != s.openAIProvider {
		t.Fatal("URL override must route to the OpenAI provider")
	}
}

func TestChatEndpointOverrideBeatsMockDefault(t *testing.T) {
	// The service starts with mock:// (as in the demo manifests) but the
	// LLMAutoscaler spec carries a real endpoint: the request must reach
	// that endpoint, not the mock.
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "from real api"}, "finish_reason": "stop"}},
		})
	}))
	defer srv.Close()

	s := &llmInterfaceServer{
		chat:           newMockProvider(),
		openAIProvider: newOpenAIProvider(ChatConfig{Model: "m", Timeout: 5 * time.Second}),
		mockProvider:   newMockProvider(),
	}

	req := chatRequest(`{"role":"forecast","desired_replicas":3}`)
	req.Endpoint = srv.URL
	resp, err := s.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if resp.GetError() != "" {
		t.Fatalf("unexpected in-band error: %s", resp.GetError())
	}
	if !hit || resp.GetContent() != "from real api" {
		t.Fatalf("CR endpoint override ignored (hit=%v content=%q)", hit, resp.GetContent())
	}
}

func TestChatMockOverrideBeatsRealDefault(t *testing.T) {
	// Conversely, a CR with llm.endpoint mock:// gets the mock even when
	// the service default is a real endpoint that is unreachable.
	s := &llmInterfaceServer{
		chat:           newOpenAIProvider(ChatConfig{Endpoint: "http://127.0.0.1:1/unreachable", Model: "m", Timeout: 2 * time.Second}),
		openAIProvider: newOpenAIProvider(ChatConfig{Model: "m", Timeout: 5 * time.Second}),
		mockProvider:   newMockProvider(),
	}

	req := chatRequest(`{"role":"forecast","desired_replicas":3,"cpu_utilization":0.9}`)
	req.Endpoint = "mock://"
	resp, err := s.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if resp.GetError() != "" || !strings.Contains(resp.GetContent(), "SCALE_UP") {
		t.Fatalf("mock override not honored: content=%q error=%q", resp.GetContent(), resp.GetError())
	}
}

func TestMockProviderRespectsConfiguredRange(t *testing.T) {
	p := newMockProvider()
	// At the configured floor an idle workload must HOLD, not propose
	// below min replicas.
	prompt := "## Decision context\n- allowed replica range: [2, 10]\n" +
		`{"role":"forecast","desired_replicas":2,"ready_replicas":2,"cpu_utilization":0.0}`
	resp, err := p.Chat(context.Background(), chatRequest(prompt))
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	var got struct {
		Action string `json:"action"`
		Target int32  `json:"target"`
	}
	if err := json.Unmarshal([]byte(resp.GetContent()), &got); err != nil {
		t.Fatalf("non-JSON content %q: %v", resp.GetContent(), err)
	}
	if got.Action != "HOLD" || got.Target != 2 {
		t.Fatalf("at min replicas want HOLD@2, got %s@%d", got.Action, got.Target)
	}
}

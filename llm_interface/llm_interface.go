// LLM Interface Layer - OpenAI-compatible interface for LLM communication
//
// The layer exposes the Chat RPC consumed by the multi-agent reasoning
// engine (Algorithm 3). Requests are served by the configured chat
// provider: an OpenAI-compatible chat-completions API or the
// deterministic mock provider. The layer dials no other services.
package llminterface

import (
	"context"
	"fmt"
	"log"
	"strings"

	"google.golang.org/grpc"

	llminterfacepb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/llminterface"

	helpers "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/helpers"
)

// llmInterfaceServer implements the LLMInterfaceService service
type llmInterfaceServer struct {
	llminterfacepb.UnimplementedLLMInterfaceServiceServer

	// chat is the service-default provider (from the endpoint flag);
	// openAIProvider and mockProvider serve requests that carry a
	// per-request endpoint override. All set once in Start.
	chat           ChatProvider
	openAIProvider ChatProvider
	mockProvider   ChatProvider
}

// Start initializes and starts the LLM Interface Layer.
func Start(ctx context.Context, listenPort int, chatCfg ChatConfig) error {
	// Two provider layers work together:
	//
	//   1. Service default (--llm-provider-endpoint): "mock://" selects
	//      the deterministic mock; anything else is an OpenAI-compatible
	//      endpoint. Used for requests that carry no endpoint override.
	//   2. Per-request override: the LLMAutoscaler spec's llm section is
	//      forwarded by the operator through the reasoning engine into
	//      ChatRequest.endpoint/api_key/model, so one service instance
	//      serves workloads with different LLM backends.
	//
	// An openAIProvider is always available so requests with an
	// endpoint override work even when the service default is mock.
	var defaultProvider ChatProvider
	if strings.HasPrefix(strings.ToLower(chatCfg.Endpoint), "mock://") || chatCfg.Endpoint == "" {
		log.Println("Default chat provider: deterministic mock (no external LLM)")
		defaultProvider = newMockProvider()
	} else {
		log.Printf("Default chat provider: OpenAI-compatible endpoint %s (model: %s)", chatCfg.Endpoint, chatCfg.Model)
		defaultProvider = newOpenAIProvider(chatCfg)
	}

	server := &llmInterfaceServer{
		chat:           defaultProvider,
		openAIProvider: newOpenAIProvider(chatCfg),
		mockProvider:   newMockProvider(),
	}
	return helpers.ServeGRPC(ctx, listenPort, "LLM Interface Layer", func(s *grpc.Server) {
		llminterfacepb.RegisterLLMInterfaceServiceServer(s, server)
	})
}

// providerFor routes a chat request to the right provider: an empty
// endpoint override uses the service default; "mock://" forces the
// deterministic mock; anything else goes to the OpenAI provider with
// the request-scoped configuration.
func (s *llmInterfaceServer) providerFor(req *llminterfacepb.ChatRequest) ChatProvider {
	endpoint := req.GetEndpoint()
	switch {
	case endpoint == "":
		return s.chat
	case strings.HasPrefix(strings.ToLower(endpoint), "mock://"):
		return s.mockProvider
	default:
		return s.openAIProvider
	}
}

// Chat implements the LLMInterfaceService.Chat method (Algorithm 3).
func (s *llmInterfaceServer) Chat(ctx context.Context, req *llminterfacepb.ChatRequest) (*llminterfacepb.ChatResponse, error) {
	log.Printf("Received Chat request (id: %s, model: %q, endpoint: %q, messages: %d, json_mode: %t)",
		req.GetRequestId(), req.GetModel(), req.GetEndpoint(), len(req.GetMessages()), req.GetJsonMode())

	resp, err := s.providerFor(req).Chat(ctx, req)
	if err != nil {
		log.Printf("Chat request %s failed: %v", req.GetRequestId(), err)
		// Surface the failure in-band so callers can treat it as an
		// analyst failure instead of a transport error.
		return &llminterfacepb.ChatResponse{Error: err.Error()}, nil
	}

	log.Printf("Chat request %s completed (finish_reason: %s, completion_tokens: %d)",
		req.GetRequestId(), resp.GetFinishReason(), resp.GetCompletionTokens())
	return resp, nil
}

// GenerateText implements the LLMInterfaceService.GenerateText method
func (s *llmInterfaceServer) GenerateText(ctx context.Context, req *llminterfacepb.GenerateTextRequest) (*llminterfacepb.GenerateTextResponse, error) {
	log.Printf("Received GenerateText request: %v", req)

	// Validate request
	if req.GetPrompt() == "" {
		return &llminterfacepb.GenerateTextResponse{
			Text:         "",
			TokenCount:   0,
			FinishReason: 1, // Length
		}, nil
	}

	// Serve GenerateText through the chat provider as a single-turn
	// conversation so all LLM traffic shares one code path.
	chatResp, err := s.chat.Chat(ctx, &llminterfacepb.ChatRequest{
		Messages: []*llminterfacepb.ChatMessage{
			{Role: "user", Content: req.GetPrompt()},
		},
		Model:        "",
		Temperature:  req.GetTemperature(),
		MaxTokens:    req.GetMaxTokens(),
	})
	if err != nil || chatResp.GetError() != "" {
		reason := ""
		if err != nil {
			reason = err.Error()
		} else {
			reason = chatResp.GetError()
		}
		log.Printf("GenerateText backend failure: %s", reason)
		return &llminterfacepb.GenerateTextResponse{
			Text:         "",
			TokenCount:   0,
			FinishReason: 2, // Error/content filter
		}, nil
	}

	return &llminterfacepb.GenerateTextResponse{
		Text:         chatResp.GetContent(),
		TokenCount:   chatResp.GetCompletionTokens(),
		FinishReason: 0, // Stop
	}, nil
}

// GenerateTextStream implements the LLMInterfaceService.GenerateTextStream method (server-streaming)
func (s *llmInterfaceServer) GenerateTextStream(req *llminterfacepb.GenerateTextRequest, stream llminterfacepb.LLMInterfaceService_GenerateTextStreamServer) error {
	log.Printf("Received GenerateTextStream request: %v", req)

	// Context for cancellation
	ctx := stream.Context()

	// Validate request
	if req.GetPrompt() == "" {
		return stream.Send(&llminterfacepb.GenerateTextResponse{
			Text:         "",
			TokenCount:   0,
			FinishReason: 1, // Length
		})
	}

	// Produce the full completion through the chat provider, then
	// stream it back word by word to emulate a streaming API.
	chatResp, err := s.chat.Chat(ctx, &llminterfacepb.ChatRequest{
		Messages: []*llminterfacepb.ChatMessage{
			{Role: "user", Content: req.GetPrompt()},
		},
		Temperature: req.GetTemperature(),
		MaxTokens:   req.GetMaxTokens(),
	})
	if err != nil || chatResp.GetError() != "" {
		return stream.Send(&llminterfacepb.GenerateTextResponse{
			Text:         "",
			TokenCount:   0,
			FinishReason: 2, // Error/content filter
		})
	}

	words := strings.Fields(chatResp.GetContent())

	// Send each cumulative prefix as a separate response to simulate streaming
	for i := 0; i < len(words); i++ {
		// Check if client cancelled
		select {
		case <-ctx.Done():
			log.Println("GenerateTextStream cancelled by client")
			return ctx.Err()
		default:
			// Continue
		}

		partialText := strings.Join(words[:i+1], " ")

		var finishReason int32
		if i == len(words)-1 {
			finishReason = 0 // Stop
		} else {
			finishReason = 2 // Token (continuing)
		}

		if err := stream.Send(&llminterfacepb.GenerateTextResponse{
			Text:         partialText,
			TokenCount:   int32(len(partialText) / 5), // Rough estimate
			FinishReason: finishReason,
		}); err != nil {
			log.Printf("Error sending streaming response: %v", err)
			return err
		}
	}

	return nil
}

// GetEmbeddings implements the LLMInterfaceService.GetEmbeddings method
func (s *llmInterfaceServer) GetEmbeddings(ctx context.Context, req *llminterfacepb.EmbeddingsRequest) (*llminterfacepb.EmbeddingsResponse, error) {
	log.Printf("Received GetEmbeddings request: %v", req)

	// Validate request
	if req.GetText() == "" {
		return &llminterfacepb.EmbeddingsResponse{
			Embedding: []float32{},
		}, nil
	}

	// v1 has no embedding endpoint; return a deterministic vector so
	// callers can exercise the RPC without a real embedding model.
	embedding := make([]float32, 384)
	for i := range embedding {
		embedding[i] = float32((hashString(req.GetText()+fmt.Sprintf("%d", i))%1000)/500.0 - 1.0)
	}

	return &llminterfacepb.EmbeddingsResponse{
		Embedding: embedding,
	}, nil
}

// CountTokens implements the LLMInterfaceService.CountTokens method
func (s *llmInterfaceServer) CountTokens(ctx context.Context, req *llminterfacepb.TokenCountRequest) (*llminterfacepb.TokenCountResponse, error) {
	log.Printf("Received CountTokens request: %v", req)

	// Validate request
	if req.GetText() == "" {
		return &llminterfacepb.TokenCountResponse{
			TokenCount: 0,
			Tokens:     []string{},
		}, nil
	}

	// Approximate token counting by whitespace/punctuation split.
	text := req.GetText()
	tokens := []string{}
	tokenCount := 0

	var currentToken string
	for _, char := range text {
		if char == ' ' || char == '\t' || char == '\n' || char == ',' || char == '.' || char == '!' || char == '?' {
			if currentToken != "" {
				tokens = append(tokens, currentToken)
				tokenCount++
				currentToken = ""
			}
		} else {
			currentToken += string(char)
		}
	}
	if currentToken != "" {
		tokens = append(tokens, currentToken)
		tokenCount++
	}

	return &llminterfacepb.TokenCountResponse{
		TokenCount: int32(tokenCount),
		Tokens:     tokens,
	}, nil
}

// Simple hash function for deterministic embeddings
func hashString(s string) int {
	hash := 0
	for i := 0; i < len(s); i++ {
		hash = 31*hash + int(s[i])
	}
	return hash
}

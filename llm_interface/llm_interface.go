// LLM Interface Layer - OpenAPI-compatible interface for LLM communication
package llminterface

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"

	"google.golang.org/grpc"

	// Import generated protobuf packages
	datacqpb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/dataacquisition"
	llminterfacepb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/llminterface"
	multianalreasoningpb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/multianalreasoning"
	safetypolicypb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/safetypolicy"
	scalingexecutorpb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/scalingexecutor"

	// Import config package
	config "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/config"

	// Helper functions
	helpers "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/helpers"
)

// Use imports to satisfy compiler (reference constants from generated code)
var _ = llminterfacepb.LLMInterfaceService_GenerateText_FullMethodName
var _ = safetypolicypb.SafetyPolicyService_CheckSafety_FullMethodName
var _ = scalingexecutorpb.ScalingExecutorService_ExecuteScaling_FullMethodName
var _ = multianalreasoningpb.MultiAgentReasoningService_Reason_FullMethodName
var _ = datacqpb.DataAcquisitionService_GetWorkloadPrediction_FullMethodName

// Client connections for outgoing gRPC calls
var (
	dataAcquisitionClient     *grpc.ClientConn
	safetyPolicyClient        *grpc.ClientConn
	scalingExecutorClient     *grpc.ClientConn
	multiAgentReasoningClient *grpc.ClientConn
)

// llmInterfaceServer implements the LLMInterfaceService service
type llmInterfaceServer struct {
	llminterfacepb.UnimplementedLLMInterfaceServiceServer
	mu          sync.Mutex
	server      *grpc.Server
	isRunning   bool
	stopChannel chan struct{}
	wg          sync.WaitGroup
}

// Start initializes and starts the LLM Interface Layer
func Start(ctx context.Context, cfg config.Config, listenPort int) error {
	log.Println("Starting LLM Interface Layer...")

	// Initialize gRPC connection to Data Acquisition service
	var err error
	dataAcquisitionClient, err = helpers.DialHelper(ctx, cfg.DataAcquisitionHost, cfg.DataAcquisitionPort, "Data Acquisition")
	if err != nil {
		return fmt.Errorf("failed to connect to Data Acquisition service: %w", err)
	}

	// Initialize gRPC connection to Safety Policy service
	safetyPolicyClient, err = helpers.DialHelper(ctx, cfg.SafetyPolicyHost, cfg.SafetyPolicyPort, "Safety Policy")
	if err != nil {
		return fmt.Errorf("failed to connect to Safety Policy service: %w", err)
	}

	// Initialize gRPC connection to Scaling Executor service
	scalingExecutorClient, err = helpers.DialHelper(ctx, cfg.ScalingExecutorHost, cfg.ScalingExecutorPort, "Scaling Executor")
	if err != nil {
		return fmt.Errorf("failed to connect to Scaling Executor service: %w", err)
	}

	// Initialize gRPC connection to Multi Agent Reasoning service
	multiAgentReasoningClient, err = helpers.DialHelper(ctx, cfg.MultiAgentReasoningHost, cfg.MultiAgentReasoningPort, "Multi Agent Reasoning")
	if err != nil {
		return fmt.Errorf("failed to connect to Multi Agent Reasoning service: %w", err)
	}

	
	// Initialize the server
	server := &llmInterfaceServer{
		isRunning:   true,
		stopChannel: make(chan struct{}),
	}

	// Set up gRPC listener
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", listenPort))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", listenPort, err)
	}

	// Create gRPC server
	server.server = grpc.NewServer()
	llminterfacepb.RegisterLLMInterfaceServiceServer(server.server, server)

	// Start the server in a goroutine
	server.wg.Add(1)
	go func() {
		defer server.wg.Done()
		log.Printf("Starting gRPC server on port %d", listenPort)
		if err := server.server.Serve(lis); err != nil {
			if server.isRunning {
				log.Printf("gRPC server error: %v", err)
			}
		}
	}()

	// Wait for stop signal or context cancellation
	select {
	case <-ctx.Done():
		log.Println("Received context cancellation, shutting down...")
	case <-server.stopChannel:
		log.Println("Received stop signal, shutting down...")
	}

	// Stop the server gracefully
	server.Stop()
	return nil
}

// Stop gracefully shuts down the gRPC server
func (s *llmInterfaceServer) Stop() {
	log.Println("Stopping LLM Interface Layer...")
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning {
		return
	}
	s.isRunning = false

	// Stop accepting new connections
	s.server.GracefulStop()

	// Close stop channel to signal goroutines
	close(s.stopChannel)

	// Wait for goroutines to finish
	s.wg.Wait()

	// Close client connections
	if dataAcquisitionClient != nil {
		dataAcquisitionClient.Close()
	}
	if safetyPolicyClient != nil {
		safetyPolicyClient.Close()
	}
	if scalingExecutorClient != nil {
		scalingExecutorClient.Close()
	}
	if multiAgentReasoningClient != nil {
		multiAgentReasoningClient.Close()
	}

	log.Println("LLM Interface Layer stopped")
}

// GenerateText implements the LLMInterfaceService.GenerateText method
func (s *llmInterfaceServer) GenerateText(ctx context.Context, req *llminterfacepb.GenerateTextRequest) (*llminterfacepb.GenerateTextResponse, error) {
	log.Printf("Received GenerateText request: %v", req)

	// Validate request
	if req.GetPrompt() == "" {
		return &llminterfacepb.GenerateTextResponse{
			Text:         "",
			TokenCount:   0,
			FinishReason: 1, // Length (or could be 2 for token limit)
		}, nil
	}

	// In a real implementation, we would:
	// 1. Send the prompt to an actual LLM service
	// 2. Get the generated text back
	// 3. Calculate token count
	// 4. Determine finish reason

	// For now, simulate a response
	generatedText := fmt.Sprintf("This is a simulated response to: \"%s\"", req.GetPrompt())
	tokenCount := int32(len(generatedText) / 5) // Rough estimate

	return &llminterfacepb.GenerateTextResponse{
		Text:         generatedText,
		TokenCount:   tokenCount,
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

	// In a real implementation, we would:
	// 1. Send the prompt to an actual LLM service with streaming enabled
	// 2. Receive chunks of generated text as they become available
	// 3. Send each chunk to the client via the stream

	// For now, simulate streaming by sending the response in chunks
	words := []string{"This", "is", "a", "simulated", "streaming", "response", "to:", "\"" + req.GetPrompt() + "\""}

	// Send each word as a separate response to simulate streaming
	for i := 0; i < len(words); i++ {
		// Check if client cancelled
		select {
		case <-ctx.Done():
			log.Println("GenerateTextStream cancelled by client")
			return ctx.Err()
		default:
			// Continue
		}

		// Build response incrementally
		var partialText string
		for j := 0; j <= i; j++ {
			if j > 0 {
				partialText += " "
			}
			partialText += words[j]
		}

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

	// In a real implementation, we would:
	// 1. Send the text to an embedding model service
	// 2. Get the embedding vector back
	// 3. Return the embedding

	// For now, simulate an embedding vector (384 dimensions, common for many embedding models)
	embedding := make([]float32, 384)
	for i := range embedding {
		// Generate deterministic but varied values based on the text
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

	// In a real implementation, we would:
	// 1. Use a tokenizer to split the text into tokens
	// 2. Count the tokens
	// 3. Optionally return the actual tokens

	// For now, simulate token counting (simple whitespace split)
	text := req.GetText()
	tokens := []string{}
	tokenCount := 0

	if text != "" {
		// Simple tokenization by whitespace and punctuation
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

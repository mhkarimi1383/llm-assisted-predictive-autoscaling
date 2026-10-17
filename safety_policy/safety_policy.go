// Safety Policy Engine - Enforces hard safety constraints on LLM outputs
package safetypolicy

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"

	"google.golang.org/grpc"

	// Import generated protobuf packages
	datacqpb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/dataacquisition"
	llminterfacepb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/llminterface"
	safetypolicypb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/safetypolicy"
	scalingexecutorpb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/scalingexecutor"
	multianalreasoningpb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/multianalreasoning"

	// Import config package
	config "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/config"

	// Helper functions
	helpers "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/helpers"
)

// Use imports to satisfy compiler (reference constants from generated code)
var _ = datacqpb.DataAcquisitionService_GetWorkloadPrediction_FullMethodName
var _ = llminterfacepb.LLMInterfaceService_GenerateText_FullMethodName
var _ = safetypolicypb.SafetyPolicyService_CheckSafety_FullMethodName
var _ = scalingexecutorpb.ScalingExecutorService_ExecuteScaling_FullMethodName
var _ = multianalreasoningpb.MultiAgentReasoningService_Reason_FullMethodName

// Client connections for outgoing gRPC calls
var (
	dataAcquisitionClient    *grpc.ClientConn
	llmInterfaceClient       *grpc.ClientConn
	scalingExecutorClient    *grpc.ClientConn
	multiAgentReasoningClient *grpc.ClientConn
)

// safetyPolicyServer implements the SafetyPolicyService service
type safetyPolicyServer struct {
	safetypolicypb.UnimplementedSafetyPolicyServiceServer
	mu          sync.Mutex
	server      *grpc.Server
	isRunning   bool
	stopChannel chan struct{}
	wg          sync.WaitGroup
}

// Start initializes and starts the Safety Policy Engine
func Start(ctx context.Context, cfg config.Config, listenPort int) error {
	log.Println("Starting Safety Policy Engine...")

	// Initialize gRPC connection to Data Acquisition service
	var err error
	dataAcquisitionClient, err = helpers.DialHelper(ctx, cfg.DataAcquisitionHost, cfg.DataAcquisitionPort, "Data Acquisition")
	if err != nil {
		return fmt.Errorf("failed to connect to Data Acquisition service: %w", err)
	}

	// Initialize gRPC connection to LLM Interface service
	llmInterfaceClient, err = helpers.DialHelper(ctx, cfg.LLMInterfaceHost, cfg.LLMInterfacePort, "LLM Interface")
	if err != nil {
		return fmt.Errorf("failed to connect to LLM Interface service: %w", err)
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
	server := &safetyPolicyServer{
		isRunning:     true,
		stopChannel:   make(chan struct{}),
	}

	// Set up gRPC listener
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", listenPort))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", listenPort, err)
	}

	// Create gRPC server
	server.server = grpc.NewServer()
	safetypolicypb.RegisterSafetyPolicyServiceServer(server.server, server)

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
func (s *safetyPolicyServer) Stop() {
	log.Println("Stopping Safety Policy Engine...")
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
	if llmInterfaceClient != nil {
		llmInterfaceClient.Close()
	}
	if scalingExecutorClient != nil {
		scalingExecutorClient.Close()
	}
	if multiAgentReasoningClient != nil {
		multiAgentReasoningClient.Close()
	}

	log.Println("Safety Policy Engine stopped")
}

// CheckSafety implements the SafetyPolicyService.CheckSafety method
func (s *safetyPolicyServer) CheckSafety(ctx context.Context, req *safetypolicypb.SafetyCheckRequest) (*safetypolicypb.SafetyCheckResponse, error) {
	log.Printf("Received CheckSafety request: %v", req)

	// Validate request - check if both prompt and generated_text are empty
	if req.GetPrompt() == "" && req.GetGeneratedText() == "" {
		return &safetypolicypb.SafetyCheckResponse{
			IsSafe:     false,
			Violations: []string{"Input cannot be empty"},
			Confidence: 1.0,
		}, nil
	}

	// In a real implementation, we would:
	// 1. Analyze the text for safety violations
	// 2. Check against safety policies
	// 3. Return appropriate safety assessment

	// For now, simulate a simple check (always safe for demo)
	isSafe := true
	var violations []string
	confidence := 0.95

	// Simple demo logic: flag texts with certain words as unsafe
	unsafeWords := []string{"hate", "violence", "illegal"}
	textToCheck := req.GetPrompt()
	if textToCheck == "" {
		textToCheck = req.GetGeneratedText()
	}
	lowerText := strings.ToLower(textToCheck)
	for _, word := range unsafeWords {
		if strings.Contains(lowerText, word) {
			isSafe = false
			violations = append(violations, fmt.Sprintf("Contains potentially unsafe word: %s", word))
			confidence = 0.9
			break
		}
	}

	safeText := ""
	if !isSafe {
		// Apply basic filtering for demo
		safeText = textToCheck
		for _, word := range unsafeWords {
			safeText = strings.ReplaceAll(safeText, word, "[FILTERED]")
			safeText = strings.ReplaceAll(safeText, strings.ToUpper(word), "[FILTERED]")
		}
	}

	return &safetypolicypb.SafetyCheckResponse{
		IsSafe:     isSafe,
		Violations: violations,
		SafeText:   safeText,
		Confidence: confidence,
	}, nil
}

// ApplySafetyPolicy implements the SafetyPolicyService.ApplySafetyPolicy method
func (s *safetyPolicyServer) ApplySafetyPolicy(ctx context.Context, req *safetypolicypb.SafetyApplyRequest) (*safetypolicypb.SafetyApplyResponse, error) {
	log.Printf("Received ApplySafetyPolicy request: %v", req)

	// Validate request
	if req.GetGeneratedText() == "" {
		return &safetypolicypb.SafetyApplyResponse{
			IsSafe:                 false,
			SafeText:               "",
			TransformationsApplied: []string{},
		}, nil
	}

	// In a real implementation, we would:
	// 1. Apply safety transformations to the text
	// 2. Filter, sanitize, or modify content as needed
	// 3. Return the processed text

	// For now, simulate applying a safety policy (just return the text unchanged for demo)
	processedText := req.GetGeneratedText()

	// Simple demo logic: replace unsafe words with [FILTERED]
	unsafeWords := []string{"hate", "violence", "illegal"}
	var transformations []string
	for _, word := range unsafeWords {
		if strings.Contains(processedText, word) {
			processedText = strings.ReplaceAll(processedText, word, "[FILTERED]")
			transformations = append(transformations, fmt.Sprintf("Replaced '%s' with '[FILTERED]'", word))
		}
		if strings.Contains(processedText, strings.ToUpper(word)) {
			processedText = strings.ReplaceAll(processedText, strings.ToUpper(word), "[FILTERED]")
			transformations = append(transformations, fmt.Sprintf("Replaced '%s' with '[FILTERED]'", strings.ToUpper(word)))
		}
	}

	// Determine if text is safe after transformations
	isSafe := len(transformations) == 0

	return &safetypolicypb.SafetyApplyResponse{
		IsSafe:                 isSafe,
		SafeText:               processedText,
		TransformationsApplied: transformations,
	}, nil
}

// GetSafetyPolicies implements the SafetyPolicyService.GetSafetyPolicies method
func (s *safetyPolicyServer) GetSafetyPolicies(ctx context.Context, req *safetypolicypb.GetSafetyPoliciesRequest) (*safetypolicypb.GetSafetyPoliciesResponse, error) {
	log.Printf("Received GetSafetyPolicies request: %v", req)

	// In a real implementation, we would:
	// 1. Retrieve safety policies from a database or configuration
	// 2. Filter by policy IDs if specified
	// 3. Return the list of policies

	// For now, return some example policies
	examplePolicies := []*safetypolicypb.SafetyPolicy{
		{
			Id:          "policy_001",
			Name:        "Content Safety Policy",
			Description: "Basic content safety guidelines",
			Type:        safetypolicypb.PolicyType_KEYWORD,
			Pattern:     "hate|violence|illegal",
			Action:      safetypolicypb.Action_TRANSFORM,
		},
		{
			Id:          "policy_002",
			Name:        "Profanity Filter",
			Description: "Filter profane language",
			Type:        safetypolicypb.PolicyType_KEYWORD,
			Pattern:     "damn|hell|shit",
			Action:      safetypolicypb.Action_TRANSFORM,
		},
	}

	return &safetypolicypb.GetSafetyPoliciesResponse{
		Policies: examplePolicies,
	}, nil
}

// UpdateSafetyPolicy implements the SafetyPolicyService.UpdateSafetyPolicy method
func (s *safetyPolicyServer) UpdateSafetyPolicy(ctx context.Context, req *safetypolicypb.UpdateSafetyPolicyRequest) (*safetypolicypb.UpdateSafetyPolicyResponse, error) {
	log.Printf("Received UpdateSafetyPolicy request: %v", req)

	// Validate request
	if req.GetPolicy() == nil {
		return &safetypolicypb.UpdateSafetyPolicyResponse{
			Success: false,
			Message: "Policy cannot be empty",
		}, nil
	}

	// In a real implementation, we would:
	// 1. Validate the policy
	// 2. Update the policy in storage/database
	// 3. Notify relevant services of the change
	// 4. Return success/failure status

	// For now, simulate updating a policy
	return &safetypolicypb.UpdateSafetyPolicyResponse{
		Success: true,
		Message: "Safety policy updated successfully",
	}, nil
}
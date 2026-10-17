// Scaling Executor - Implements scaling decisions in Kubernetes
package scalingexecutor

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

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
	safetyPolicyClient       *grpc.ClientConn
	scalingExecutorClient    *grpc.ClientConn
	multiAgentReasoningClient *grpc.ClientConn
)

// scalingExecutorServer implements the ScalingExecutorService service
type scalingExecutorServer struct {
	scalingexecutorpb.UnimplementedScalingExecutorServiceServer
	mu          sync.Mutex
	resources   map[string]*scalingexecutorpb.ScalableResource
	server      *grpc.Server
	isRunning   bool
	stopChannel chan struct{}
	wg          sync.WaitGroup
}

// Start initializes and starts the Scaling Executor
func Start(ctx context.Context, cfg config.Config, listenPort int) error {
	log.Println("Starting Scaling Executor...")

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

	// Initialize gRPC connection to Safety Policy service
	safetyPolicyClient, err = helpers.DialHelper(ctx, cfg.SafetyPolicyHost, cfg.SafetyPolicyPort, "Safety Policy")
	if err != nil {
		return fmt.Errorf("failed to connect to Safety Policy service: %w", err)
	}

	// Initialize gRPC connection to Multi Agent Reasoning service
	multiAgentReasoningClient, err = helpers.DialHelper(ctx, cfg.MultiAgentReasoningHost, cfg.MultiAgentReasoningPort, "Multi Agent Reasoning")
	if err != nil {
		return fmt.Errorf("failed to connect to Multi Agent Reasoning service: %w", err)
	}

	
	// Initialize the server
	server := &scalingExecutorServer{
		resources:     make(map[string]*scalingexecutorpb.ScalableResource),
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
	scalingexecutorpb.RegisterScalingExecutorServiceServer(server.server, server)

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

	// Initialize some default resources for demonstration
	server.initializeDefaultResources()

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
func (s *scalingExecutorServer) Stop() {
	log.Println("Stopping Scaling Executor...")
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
	if safetyPolicyClient != nil {
		safetyPolicyClient.Close()
	}
	if scalingExecutorClient != nil {
		scalingExecutorClient.Close()
	}
	if multiAgentReasoningClient != nil {
		multiAgentReasoningClient.Close()
	}

	log.Println("Scaling Executor stopped")
}

// initializeDefaultResources sets up some example scalable resources
func (s *scalingExecutorServer) initializeDefaultResources() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Add some example resources
	defaultResources := []*scalingexecutorpb.ScalableResource{
		{
			ResourceType: "deployment",
			ResourceName: "nginx-deployment",
			Namespace:    "default",
			CurrentReplicas: 3,
			MinReplicas:    1,
			MaxReplicas:    10,
		},
		{
			ResourceType: "statefulset",
			ResourceName: "redis",
			Namespace:    "default",
			CurrentReplicas: 1,
			MinReplicas:    1,
			MaxReplicas:    5,
		},
		{
			ResourceType: "deployment",
			ResourceName: "frontend",
			Namespace:    "production",
			CurrentReplicas: 5,
			MinReplicas:    2,
			MaxReplicas:    20,
		},
	}

	for _, res := range defaultResources {
		key := fmt.Sprintf("%s/%s/%s", res.ResourceType, res.Namespace, res.ResourceName)
		s.resources[key] = res
	}
}

// ExecuteScaling implements the ScalingExecutorService.ExecuteScaling method
func (s *scalingExecutorServer) ExecuteScaling(ctx context.Context, req *scalingexecutorpb.ScalingDecision) (*scalingexecutorpb.ScalingResult, error) {
	log.Printf("Received ExecuteScaling request: %v", req)

	// Validate request
	if req.GetResourceType() == "" || req.GetResourceName() == "" || req.GetNamespace() == "" {
		return &scalingexecutorpb.ScalingResult{
			Success: false,
			Message: "Resource type, name, and namespace are required",
		}, nil
	}

	if req.GetReplicaCount() < 0 {
		return &scalingexecutorpb.ScalingResult{
			Success: false,
			Message: "Replica count must be non-negative",
		}, nil
	}

	// Generate resource key
	key := fmt.Sprintf("%s/%s/%s", req.GetResourceType(), req.GetNamespace(), req.GetResourceName())

	// Update or create resource
	s.mu.Lock()
	resource, exists := s.resources[key]
	if !exists {
		resource = &scalingexecutorpb.ScalableResource{
			ResourceType: req.GetResourceType(),
			ResourceName: req.GetResourceName(),
			Namespace:    req.GetNamespace(),
			CurrentReplicas: 0, // Will be updated below
			MinReplicas:    0,
			MaxReplicas:    0,
		}
		s.resources[key] = resource
	}

	// Update current replicas (in a real implementation, this would call Kubernetes API)
	resource.CurrentReplicas = req.GetReplicaCount()
	s.mu.Unlock()

	// In a real implementation, we would:
	// 1. Use a Kubernetes client to scale the resource
	// 2. Wait for the scaling operation to complete
	// 3. Return appropriate success/failure based on the result

	// For now, simulate success
	log.Printf("Scaling %s/%s/%s to %d replicas", req.GetResourceType(), req.GetNamespace(), req.GetResourceName(), req.GetReplicaCount())

	return &scalingexecutorpb.ScalingResult{
		Success:   true,
		Message:   fmt.Sprintf("Successfully scaled %s/%s/%s to %d replicas", req.GetResourceType(), req.GetNamespace(), req.GetResourceName(), req.GetReplicaCount()),
		Timestamp: time.Now().Unix(),
	}, nil
}

// GetScalingStatus implements the ScalingExecutorService.GetScalingStatus method
func (s *scalingExecutorServer) GetScalingStatus(ctx context.Context, req *scalingexecutorpb.ScalingStatusRequest) (*scalingexecutorpb.ScalingStatusResponse, error) {
	log.Printf("Received GetScalingStatus request: %v", req)

	// Validate request
	if req.GetResourceType() == "" || req.GetResourceName() == "" || req.GetNamespace() == "" {
		return &scalingexecutorpb.ScalingStatusResponse{}, fmt.Errorf("resource type, name, and namespace are required")
	}

	// Generate resource key
	key := fmt.Sprintf("%s/%s/%s", req.GetResourceType(), req.GetNamespace(), req.GetResourceName())

	// Look up resource
	s.mu.Lock()
	resource, exists := s.resources[key]
	s.mu.Unlock()

	if !exists {
		return &scalingexecutorpb.ScalingStatusResponse{}, fmt.Errorf("resource %s/%s/%s not found", req.GetResourceType(), req.GetNamespace(), req.GetResourceName())
	}

	// Determine status based on current state (in real implementation, this would check actual K8s status)
	status := "Ready"
	if resource.GetCurrentReplicas() == 0 {
		status = "ScaledDown"
	} else {
		status = "Ready"
	}

	return &scalingexecutorpb.ScalingStatusResponse{
		ResourceType:   resource.GetResourceType(),
		ResourceName:   resource.GetResourceName(),
		Namespace:      resource.GetNamespace(),
		CurrentReplicas: resource.GetCurrentReplicas(),
		DesiredReplicas: resource.GetCurrentReplicas(), // In real implementation, this might differ during scaling
		LastUpdated:    time.Now().Unix(),
		Status:         status,
	}, nil
}

// WatchScalingEvents implements the ScalingExecutorService.WatchScalingEvents method (server-streaming)
func (s *scalingExecutorServer) WatchScalingEvents(req *scalingexecutorpb.WatchScalingEventsRequest, stream scalingexecutorpb.ScalingExecutorService_WatchScalingEventsServer) error {
	log.Printf("Received WatchScalingEvents request: %v", req)

	// Context for cancellation
	ctx := stream.Context()

	// Create a ticker to periodically send events (in real implementation, this would watch actual K8s events)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Send initial events or just keep connection alive
	for {
		select {
		case <-ctx.Done():
			log.Println("WatchScalingEvents stream cancelled by client")
			return ctx.Err()
		case <-ticker.C:
			// In a real implementation, we would check for actual scaling events
			// For now, send a heartbeat event
			event := &scalingexecutorpb.ScalingEvent{
				ResourceType: "heartbeat",
				ResourceName: "scaling-executor",
				Namespace:    "default",
				Timestamp:    time.Now().Unix(),
				Type:         scalingexecutorpb.EventType_UNKNOWN,
				FromReplicas: 0,
				ToReplicas:   0,
				Message:      "Heartbeat from scaling executor",
			}

			if err := stream.Send(event); err != nil {
				log.Printf("Error sending scaling event: %v", err)
				return err
			}
		}
	}
}

// ListScalableResources implements the ScalingExecutorService.ListScalableResources method
func (s *scalingExecutorServer) ListScalableResources(ctx context.Context, req *scalingexecutorpb.ListScalableResourcesRequest) (*scalingexecutorpb.ListScalableResourcesResponse, error) {
	log.Printf("Received ListScalableResources request: %v", req)

	var resources []*scalingexecutorpb.ScalableResource

	s.mu.Lock()
	for _, resource := range s.resources {
		// Filter by namespace if specified
		if req.GetNamespace() != "" && resource.GetNamespace() != req.GetNamespace() {
			continue
		}
		resources = append(resources, resource)
	}
	s.mu.Unlock()

	return &scalingexecutorpb.ListScalableResourcesResponse{
		Resources: resources,
	}, nil
}
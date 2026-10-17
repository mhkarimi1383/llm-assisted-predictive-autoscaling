// Data Acquisition Layer - Collects workload predictions and cluster status
package dataacquisition

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"

	// Import generated protobuf packages
	dataacpb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/dataacquisition"
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
var _ = llminterfacepb.LLMInterfaceService_GenerateText_FullMethodName
var _ = safetypolicypb.SafetyPolicyService_CheckSafety_FullMethodName
var _ = scalingexecutorpb.ScalingExecutorService_ExecuteScaling_FullMethodName
var _ = multianalreasoningpb.MultiAgentReasoningService_Reason_FullMethodName

// Client connections for outgoing gRPC calls
var (
	llmInterfaceClient         *grpc.ClientConn
	safetyPolicyClient         *grpc.ClientConn
	scalingExecutorClient      *grpc.ClientConn
	multiAgentReasoningClient  *grpc.ClientConn
)

// dataAcquisitionServer implements the DataAcquisitionService service
type dataAcquisitionServer struct {
	dataacpb.UnimplementedDataAcquisitionServiceServer
	mu          sync.Mutex
	server      *grpc.Server
	isRunning   bool
	stopChannel chan struct{}
	wg          sync.WaitGroup
}

// Start initializes and starts the Data Acquisition Layer
func Start(ctx context.Context, cfg config.Config, listenPort int) error {
	log.Println("Starting Data Acquisition Layer...")

	// Initialize ALL outgoing gRPC connections
	var err error

	// LLM Interface connection
	llmInterfaceClient, err = helpers.DialHelper(ctx, cfg.LLMInterfaceHost, cfg.LLMInterfacePort, "LLM Interface")
	if err != nil {
		return fmt.Errorf("failed to connect to LLM Interface service: %w", err)
	}

	// Safety Policy connection
	safetyPolicyClient, err = helpers.DialHelper(ctx, cfg.SafetyPolicyHost, cfg.SafetyPolicyPort, "Safety Policy")
	if err != nil {
		return fmt.Errorf("failed to connect to Safety Policy service: %w", err)
	}

	// Scaling Executor connection
	scalingExecutorClient, err = helpers.DialHelper(ctx, cfg.ScalingExecutorHost, cfg.ScalingExecutorPort, "Scaling Executor")
	if err != nil {
		return fmt.Errorf("failed to connect to Scaling Executor service: %w", err)
	}

	// Multi Agent Reasoning connection
	multiAgentReasoningClient, err = helpers.DialHelper(ctx, cfg.MultiAgentReasoningHost, cfg.MultiAgentReasoningPort, "Multi Agent Reasoning")
	if err != nil {
		return fmt.Errorf("failed to connect to Multi Agent Reasoning service: %w", err)
	}

	// Initialize the server
	server := &dataAcquisitionServer{
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
	dataacpb.RegisterDataAcquisitionServiceServer(server.server, server)

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
func (s *dataAcquisitionServer) Stop() {
	log.Println("Stopping Data Acquisition Layer...")
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

	log.Println("Data Acquisition Layer stopped")
}

// GetWorkloadPrediction implements the DataAcquisitionService.GetWorkloadPrediction method
func (s *dataAcquisitionServer) GetWorkloadPrediction(ctx context.Context, req *dataacpb.WorkloadPredictionRequest) (*dataacpb.WorkloadPredictionResponse, error) {
	log.Printf("Received GetWorkloadPrediction request: %v", req)

	// Validate request
	if req.GetWorkloadId() == "" {
		return &dataacpb.WorkloadPredictionResponse{}, fmt.Errorf("workload ID cannot be empty")
	}

	// In a real implementation, we would:
	// 1. Query a database or ML model for workload predictions
	// 2. Return the predicted values

	// For now, simulate a response
	prediction := &dataacpb.WorkloadPredictionResponse{
		WorkloadId:                   req.GetWorkloadId(),
		Timestamp:                    0, // Current timestamp would be set in real implementation
		PredictedCpuUtilization:      65.3,
		PredictedMemoryUtilization:   42.1,
		PredictedNetworkIn:           10.5,
		PredictedNetworkOut:          8.2,
	}

	return prediction, nil
}

// GetClusterMetrics implements the DataAcquisitionService.GetClusterMetrics method
func (s *dataAcquisitionServer) GetClusterMetrics(ctx context.Context, req *dataacpb.ClusterMetricsRequest) (*dataacpb.ClusterMetricsResponse, error) {
	log.Printf("Received GetClusterMetrics request: %v", req)

	// In a real implementation, we would:
	// 1. Query monitoring systems (Prometheus, etc.) for current cluster metrics
	// 2. Return the current values

	// For now, simulate a response
	metrics := &dataacpb.ClusterMetricsResponse{
		Timestamp:             0, // Current timestamp would be set in real implementation
		CpuUtilization:        45.7,
		MemoryUtilization:     62.3,
		DiskUtilization:       23.8,
		PodCount:              15,
		NodeCount:             3,
	}

	return metrics, nil
}

// SubscribeWorkloadPredictions implements the DataAcquisitionService.SubscribeWorkloadPredictions method (server-streaming)
func (s *dataAcquisitionServer) SubscribeWorkloadPredictions(req *dataacpb.WorkloadPredictionSubscriptionRequest, stream dataacpb.DataAcquisitionService_SubscribeWorkloadPredictionsServer) error {
	log.Printf("Received SubscribeWorkloadPredictions request: %v", req)

	// Context for cancellation
	ctx := stream.Context()

	// Validate request
	if len(req.GetWorkloadIds()) == 0 {
		return stream.Send(&dataacpb.WorkloadPredictionResponse{})
	}

	// In a real implementation, we would:
	// 1. Set up a stream of workload predictions for the specified workload IDs
	// 2. Send updates as new predictions become available

	// For now, simulate sending periodic updates
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("SubscribeWorkloadPredictions stream cancelled by client")
			return ctx.Err()
		case <-ticker.C:
			// Send a simulated prediction for each requested workload ID
			for _, workloadID := range req.GetWorkloadIds() {
				prediction := &dataacpb.WorkloadPredictionResponse{
					WorkloadId:                   workloadID,
					Timestamp:                    0, // Would be current timestamp
					PredictedCpuUtilization:      60.0 + float64(len(workloadID)%10), // Vary slightly
					PredictedMemoryUtilization:   40.0 + float64(len(workloadID)%15),
					PredictedNetworkIn:           5.0 + float64(len(workloadID)%5),
					PredictedNetworkOut:          3.0 + float64(len(workloadID)%3),
				}

				if err := stream.Send(prediction); err != nil {
					log.Printf("Error sending workload prediction: %v", err)
					return err
				}
			}
		}
	}
}

// SubscribeClusterMetrics implements the DataAcquisitionService.SubscribeClusterMetrics method (server-streaming)
func (s *dataAcquisitionServer) SubscribeClusterMetrics(req *dataacpb.ClusterMetricsSubscriptionRequest, stream dataacpb.DataAcquisitionService_SubscribeClusterMetricsServer) error {
	log.Printf("Received SubscribeClusterMetrics request: %v", req)

	// Context for cancellation
	ctx := stream.Context()

	// In a real implementation, we would:
	// 1. Set up a stream of cluster metrics
	// 2 - Send updates as cluster metrics change

	// For now, simulate sending periodic updates
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("SubscribeClusterMetrics stream cancelled by client")
			return ctx.Err()
		case <-ticker.C:
			// Send simulated cluster metrics
			metrics := &dataacpb.ClusterMetricsResponse{
				Timestamp:     0, // Would be current timestamp
				CpuUtilization: 50.0,
				MemoryUtilization: 55.0,
				DiskUtilization: 25.0,
				PodCount:      12,
				NodeCount:     2,
			}

			if err := stream.Send(metrics); err != nil {
				log.Printf("Error sending cluster metrics: %v", err)
				return err
			}
		}
	}
}
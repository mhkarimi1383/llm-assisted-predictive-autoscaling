// Shared service client registry.
//
// One place dials every backing service and hands out typed clients,
// so components and the operator never repeat connection wiring.
// Uses DialHelper's connection cache underneath; gRPC client
// connections are lazy, so dialing all services up front is cheap and
// components simply use the accessors they need.
package helpers

import (
	"context"
	"fmt"

	"google.golang.org/grpc"

	dataacpb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/dataacquisition"
	llminterfacepb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/llminterface"
	multianalreasoningpb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/multianalreasoning"
	safetypolicypb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/safetypolicy"
	scalingexecutorpb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/scalingexecutor"

	config "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/config"
)

// ServiceClients aggregates typed gRPC clients for the five backing
// services.
type ServiceClients struct {
	DataAcquisition     dataacpb.DataAcquisitionServiceClient
	LLMInterface        llminterfacepb.LLMInterfaceServiceClient
	MultiAgentReasoning multianalreasoningpb.MultiAgentReasoningServiceClient
	SafetyPolicy        safetypolicypb.SafetyPolicyServiceClient
	ScalingExecutor     scalingexecutorpb.ScalingExecutorServiceClient

	conns []*grpc.ClientConn
}

// NewServiceClients dials every backing service using the connection
// config and returns the registry. Call Close when done.
func NewServiceClients(ctx context.Context, cfg config.Config) (*ServiceClients, error) {
	c := &ServiceClients{}

	dials := []struct {
		name string
		host string
		port int
	}{
		{"Data Acquisition", cfg.DataAcquisitionHost, cfg.DataAcquisitionPort},
		{"LLM Interface", cfg.LLMInterfaceHost, cfg.LLMInterfacePort},
		{"Multi Agent Reasoning", cfg.MultiAgentReasoningHost, cfg.MultiAgentReasoningPort},
		{"Safety Policy", cfg.SafetyPolicyHost, cfg.SafetyPolicyPort},
		{"Scaling Executor", cfg.ScalingExecutorHost, cfg.ScalingExecutorPort},
	}

	for _, d := range dials {
		conn, err := DialHelper(ctx, d.host, d.port, d.name)
		if err != nil {
			c.Close()
			return nil, fmt.Errorf("failed to connect to %s service: %w", d.name, err)
		}
		c.conns = append(c.conns, conn)
	}

	c.DataAcquisition = dataacpb.NewDataAcquisitionServiceClient(c.conns[0])
	c.LLMInterface = llminterfacepb.NewLLMInterfaceServiceClient(c.conns[1])
	c.MultiAgentReasoning = multianalreasoningpb.NewMultiAgentReasoningServiceClient(c.conns[2])
	c.SafetyPolicy = safetypolicypb.NewSafetyPolicyServiceClient(c.conns[3])
	c.ScalingExecutor = scalingexecutorpb.NewScalingExecutorServiceClient(c.conns[4])
	return c, nil
}

// Close releases every connection.
func (c *ServiceClients) Close() {
	for _, conn := range c.conns {
		if conn != nil {
			_ = conn.Close()
		}
	}
	c.conns = nil
}

// main.go - Entry point for the LLM-Based Intelligent Autoscaling System
// This file implements a Cobra CLI to run individual components

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	// Import component packages (Go implementations)
	dataacquisition "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/data_acquisition"
	llminterface "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/llm_interface"
	operator "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/operator"
	safetypolicy "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/safety_policy"
	scalingexecutor "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/scaling_executor"

	// Import config package
	config "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/config"

	"github.com/spf13/cobra"
)

// service ports (default values)
const (
	defaultDataAcquisitionPort     = 50051
	defaultLLMInterfacePort        = 50052
	defaultSafetyPolicyPort        = 50053
	defaultScalingExecutorPort     = 50054
	defaultMultiAgentReasoningPort = 50055
)

// Global variables to hold connection parameters (for flag binding)
var (
	llmInterfaceHost        string
	llmInterfacePort        int
	dataAcquisitionHost     string
	dataAcquisitionPort     int
	safetyPolicyHost        string
	safetyPolicyPort        int
	scalingExecutorHost     string
	scalingExecutorPort     int
	multiAgentReasoningHost string
	multiAgentReasoningPort int

	// LLM provider defaults used by the LLM Interface service.
	// Per-request overrides come from the LLMAutoscaler spec.
	llmProviderEndpoint string
	llmProviderAPIKey   string
	llmProviderModel    string
	llmProviderTimeout  int

	// Operator options.
	operatorMetricsAddr    string
	operatorProbeAddr      string
	operatorWatchNamespace string
	operatorRequeueSeconds int
)

// Global configuration instance
var cfg config.Config

// Root command
var rootCmd = &cobra.Command{
	Use:   "llm-autoscaler",
	Short: "LLM-Based Intelligent Autoscaling System",
	Long:  `A modular autoscaling system that uses LLMs for predictive scaling decisions.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// This ensures that persistent flags are initialized before any command runs
	},
}

// dataacquisition command
var dataacquisitionCmd = &cobra.Command{
	Use:   "dataacquisition",
	Short: "Start the Data Acquisition component",
	Long:  `Starts the Data Acquisition layer which collects workload predictions and cluster status.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Create context that can be cancelled
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		log.Println("Starting Data Acquisition component...")
		if err := dataacquisition.Start(ctx, dataAcquisitionPort); err != nil {
			return fmt.Errorf("failed to start Data Acquisition component: %w", err)
		}

		// Wait for interrupt signal to gracefully shutdown
		<-ctx.Done()
		log.Println("Data Acquisition component stopped.")
		return nil
	},
}

// llminterface command
var llminterfaceCmd = &cobra.Command{
	Use:   "llminterface",
	Short: "Start the LLM Interface component",
	Long:  `Starts the LLM Interface layer which provides an OpenAI-compatible chat interface for LLM communication. Use --llm-provider-endpoint mock:// for the deterministic mock provider.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Create context that can be cancelled
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		chatCfg := llminterface.ChatConfig{
			Endpoint: llmProviderEndpoint,
			APIKey:   llmProviderAPIKey,
			Model:    llmProviderModel,
			Timeout:  time.Duration(llmProviderTimeout) * time.Second,
		}

		log.Println("Starting LLM Interface component...")
		if err := llminterface.Start(ctx, llmInterfacePort, chatCfg); err != nil {
			return fmt.Errorf("failed to start LLM Interface component: %w", err)
		}

		// Wait for interrupt signal to gracefully shutdown
		<-ctx.Done()
		log.Println("LLM Interface component stopped.")
		return nil
	},
}

// safetypolicy command
var safetypolicyCmd = &cobra.Command{
	Use:   "safetypolicy",
	Short: "Start the Safety Policy component",
	Long:  `Starts the Safety Policy engine which enforces hard safety constraints on LLM outputs.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Create context that can be cancelled
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		log.Println("Starting Safety Policy component...")
		if err := safetypolicy.Start(ctx, safetyPolicyPort); err != nil {
			return fmt.Errorf("failed to start Safety Policy component: %w", err)
		}

		// Wait for interrupt signal to gracefully shutdown
		<-ctx.Done()
		log.Println("Safety Policy component stopped.")
		return nil
	},
}

// scalingexecutor command
var scalingexecutorCmd = &cobra.Command{
	Use:   "scalingexecutor",
	Short: "Start the Scaling Executor component",
	Long:  `Starts the Scaling Executor which implements scaling decisions in Kubernetes.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Create context that can be cancelled
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		log.Println("Starting Scaling Executor component...")
		if err := scalingexecutor.Start(ctx, scalingExecutorPort); err != nil {
			return fmt.Errorf("failed to start Scaling Executor component: %w", err)
		}

		// Wait for interrupt signal to gracefully shutdown
		<-ctx.Done()
		log.Println("Scaling Executor component stopped.")
		return nil
	},
}

// operator command
var operatorCmd = &cobra.Command{
	Use:   "operator",
	Short: "Start the LLMAutoscaler operator",
	Long:  `Starts the controller-runtime operator that watches LLMAutoscaler CRs and drives the decision cycle across the backing services.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		log.Println("Starting LLMAutoscaler operator...")

		cfg = config.Config{
			LLMInterfaceHost:        llmInterfaceHost,
			LLMInterfacePort:        llmInterfacePort,
			DataAcquisitionHost:     dataAcquisitionHost,
			DataAcquisitionPort:     dataAcquisitionPort,
			SafetyPolicyHost:        safetyPolicyHost,
			SafetyPolicyPort:        safetyPolicyPort,
			ScalingExecutorHost:     scalingExecutorHost,
			ScalingExecutorPort:     scalingExecutorPort,
			MultiAgentReasoningHost: multiAgentReasoningHost,
			MultiAgentReasoningPort: multiAgentReasoningPort,
		}

		opts := operator.Options{
			MetricsAddr:    operatorMetricsAddr,
			ProbeAddr:      operatorProbeAddr,
			WatchNamespace: operatorWatchNamespace,
			RequeueAfter:   time.Duration(operatorRequeueSeconds) * time.Second,
		}

		if err := operator.Start(ctx, cfg, opts); err != nil {
			return fmt.Errorf("failed to start operator: %w", err)
		}
		log.Println("Operator stopped.")
		return nil
	},
}

func init() {
	// Define global/persistent flags for connection parameters
	rootCmd.PersistentFlags().StringVar(&llmInterfaceHost, "llm-interface-host", "localhost", "Hostname of the LLM Interface service")
	rootCmd.PersistentFlags().IntVar(&llmInterfacePort, "llm-interface-port", defaultLLMInterfacePort, "Port of the LLM Interface service")
	rootCmd.PersistentFlags().StringVar(&dataAcquisitionHost, "data-acquisition-host", "localhost", "Hostname of the Data Acquisition service")
	rootCmd.PersistentFlags().IntVar(&dataAcquisitionPort, "data-acquisition-port", defaultDataAcquisitionPort, "Port of the Data Acquisition service")
	rootCmd.PersistentFlags().StringVar(&safetyPolicyHost, "safety-policy-host", "localhost", "Hostname of the Safety Policy service")
	rootCmd.PersistentFlags().IntVar(&safetyPolicyPort, "safety-policy-port", defaultSafetyPolicyPort, "Port of the Safety Policy service")
	rootCmd.PersistentFlags().StringVar(&scalingExecutorHost, "scaling-executor-host", "localhost", "Hostname of the Scaling Executor service")
	rootCmd.PersistentFlags().IntVar(&scalingExecutorPort, "scaling-executor-port", defaultScalingExecutorPort, "Port of the Scaling Executor service")
	rootCmd.PersistentFlags().StringVar(&multiAgentReasoningHost, "multi-agent-reasoning-host", "localhost", "Hostname of the Multi Agent Reasoning service")
	rootCmd.PersistentFlags().IntVar(&multiAgentReasoningPort, "multi-agent-reasoning-port", defaultMultiAgentReasoningPort, "Port of the Multi Agent Reasoning service")

	// LLM provider defaults for the LLM Interface service
	rootCmd.PersistentFlags().StringVar(&llmProviderEndpoint, "llm-provider-endpoint", "mock://", "Default LLM provider endpoint (OpenAI-compatible base URL, or mock://)")
	rootCmd.PersistentFlags().StringVar(&llmProviderAPIKey, "llm-provider-api-key", "", "Default LLM provider API key (overrides: LLMPROVIDER_API_KEY env)")
	rootCmd.PersistentFlags().StringVar(&llmProviderModel, "llm-provider-model", "", "Default LLM provider model name")
	rootCmd.PersistentFlags().IntVar(&llmProviderTimeout, "llm-provider-timeout", 30, "Per-request LLM timeout in seconds")

	// Allow env fallback for the API key so secrets stay out of argv
	if v := os.Getenv("LLMPROVIDER_API_KEY"); v != "" && llmProviderAPIKey == "" {
		llmProviderAPIKey = v
	}

	// Operator flags
	rootCmd.PersistentFlags().StringVar(&operatorMetricsAddr, "operator-metrics-addr", ":8080", "Metrics endpoint bind address (0 disables)")
	rootCmd.PersistentFlags().StringVar(&operatorProbeAddr, "operator-probe-addr", ":8081", "Health probe bind address")
	rootCmd.PersistentFlags().StringVar(&operatorWatchNamespace, "operator-watch-namespace", "", "Namespace to watch (empty = all namespaces)")
	rootCmd.PersistentFlags().IntVar(&operatorRequeueSeconds, "operator-requeue-seconds", 60, "Control period between decision cycles in seconds")

	// Update global config when flags are parsed
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		cfg = config.Config{
			LLMInterfaceHost:        llmInterfaceHost,
			LLMInterfacePort:        llmInterfacePort,
			DataAcquisitionHost:     dataAcquisitionHost,
			DataAcquisitionPort:     dataAcquisitionPort,
			SafetyPolicyHost:        safetyPolicyHost,
			SafetyPolicyPort:        safetyPolicyPort,
			ScalingExecutorHost:     scalingExecutorHost,
			ScalingExecutorPort:     scalingExecutorPort,
			MultiAgentReasoningHost: multiAgentReasoningHost,
			MultiAgentReasoningPort: multiAgentReasoningPort,
		}
	}

	// Add commands to root
	rootCmd.AddCommand(dataacquisitionCmd)
	rootCmd.AddCommand(llminterfaceCmd)
	rootCmd.AddCommand(safetypolicyCmd)
	rootCmd.AddCommand(scalingexecutorCmd)
	rootCmd.AddCommand(operatorCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

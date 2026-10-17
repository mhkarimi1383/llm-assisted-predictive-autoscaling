// main.go - Entry point for the LLM-Based Intelligent Autoscaling System
// This file implements a Cobra CLI to run individual components

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	// Import component packages (Go implementations)
	dataacquisition "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/data_acquisition"
	llminterface "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/llm_interface"
	safetypolicy "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/safety_policy"
	scalingexecutor "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/scaling_executor"

	// Import config package
	config "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/config"

	"github.com/spf13/cobra"
)

// service ports (default values)
const (
	defaultDataAcquisitionPort    = 50051
	defaultLLMInterfacePort       = 50052
	defaultSafetyPolicyPort       = 50053
	defaultScalingExecutorPort    = 50054
	defaultMultiAgentReasoningPort = 50055
)

// Global variables to hold connection parameters (for flag binding)
var (
	llmInterfaceHost     string
	llmInterfacePort     int
	dataAcquisitionHost  string
	dataAcquisitionPort  int
	safetyPolicyHost     string
	safetyPolicyPort     int
	scalingExecutorHost  string
	scalingExecutorPort  int
	multiAgentReasoningHost string
	multiAgentReasoningPort int
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
		if err := dataacquisition.Start(ctx, cfg, dataAcquisitionPort); err != nil {
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
	Long:  `Starts the LLM Interface layer which provides OpenAPI-compatible interface for LLM communication.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Create context that can be cancelled
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		log.Println("Starting LLM Interface component...")
		if err := llminterface.Start(ctx, cfg, llmInterfacePort); err != nil {
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
		if err := safetypolicy.Start(ctx, cfg, safetyPolicyPort); err != nil {
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
		if err := scalingexecutor.Start(ctx, cfg, scalingExecutorPort); err != nil {
			return fmt.Errorf("failed to start Scaling Executor component: %w", err)
		}

		// Wait for interrupt signal to gracefully shutdown
		<-ctx.Done()
		log.Println("Scaling Executor component stopped.")
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

	// Update global config when flags are parsed
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		cfg = config.Config{
			LLMInterfaceHost:     llmInterfaceHost,
			LLMInterfacePort:     llmInterfacePort,
			DataAcquisitionHost:  dataAcquisitionHost,
			DataAcquisitionPort:  dataAcquisitionPort,
			SafetyPolicyHost:     safetyPolicyHost,
			SafetyPolicyPort:     safetyPolicyPort,
			ScalingExecutorHost:  scalingExecutorHost,
			ScalingExecutorPort:  scalingExecutorPort,
			MultiAgentReasoningHost: multiAgentReasoningHost,
			MultiAgentReasoningPort: multiAgentReasoningPort,
		}
	}

	// Add commands to root
	rootCmd.AddCommand(dataacquisitionCmd)
	rootCmd.AddCommand(llminterfaceCmd)
	rootCmd.AddCommand(safetypolicyCmd)
	rootCmd.AddCommand(scalingexecutorCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
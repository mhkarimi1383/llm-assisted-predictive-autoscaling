// main.go - Entry point for the LLM-Based Intelligent Autoscaling System
// This file initializes and runs all components of the system.

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	// Import component packages (Go implementations)
	"github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/data_acquisition"
	"github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/llm_interface"
	"github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/safety_policy"
	"github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/scaling_executor"
)

func main() {
	log.Println("Starting LLM-Based Intelligent Autoscaling System...")

	// Create a context that can be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Received shutdown signal, initiating graceful shutdown...")
		cancel()
	}()

	// Initialize and start all Go components
	if err := startComponents(ctx); err != nil {
		log.Fatalf("Failed to start components: %v", err)
	}

	// Wait for shutdown signal
	<-ctx.Done()
	log.Println("Shutdown complete.")
}

// startComponents initializes and starts all Go components
func startComponents(ctx context.Context) error {
	// Start Data Acquisition Layer (Go part)
	if err := dataacquisition.Start(ctx); err != nil {
		return err
	}

	// Start LLM Interface Layer (Go part)
	if err := llminterface.Start(ctx); err != nil {
		return err
	}

	// Start Safety Policy Engine (Go part)
	if err := safetypolicy.Start(ctx); err != nil {
		return err
	}

	// Start Scaling Executor (Go part)
	if err := scalingexecutor.Start(ctx); err != nil {
		return err
	}

	return nil
}
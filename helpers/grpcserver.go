// Shared gRPC server lifecycle.
//
// Every component's Start function reduces to a listener, a
// registered servicer, and a graceful stop on context cancellation.
// This helper owns that boilerplate once.
package helpers

import (
	"context"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"
)

// ServeGRPC listens on the port, registers the servicer through
// registerFn, serves until ctx is cancelled, and shuts down
// gracefully. It blocks until shutdown completes.
func ServeGRPC(ctx context.Context, listenPort int, serviceName string, registerFn func(s *grpc.Server)) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", listenPort))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", listenPort, err)
	}

	server := grpc.NewServer()
	registerFn(server)

	log.Printf("Starting %s gRPC server on port %d", serviceName, listenPort)
	serveErr := make(chan error, 1)
	go func() {
		if err := server.Serve(lis); err != nil {
			serveErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Printf("%s shutting down...", serviceName)
	case err := <-serveErr:
		log.Printf("%s gRPC server error: %v", serviceName, err)
		return fmt.Errorf("%s gRPC server failed: %w", serviceName, err)
	}

	server.GracefulStop()
	log.Printf("%s stopped", serviceName)
	return nil
}

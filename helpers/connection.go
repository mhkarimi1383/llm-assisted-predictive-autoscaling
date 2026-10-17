package helpers

import (
	"context"
	"fmt"
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var connectionCache = make(map[string]*grpc.ClientConn)

// DialHelper creates a gRPC connection and returns the client connection and any error.
// It encapsulates the common pattern of creating a gRPC connection with logging and error handling.
func DialHelper(ctx context.Context, host string, port int, serviceName string) (*grpc.ClientConn, error) {
	if conn, ok := connectionCache[fmt.Sprintf("%s:%d", host, port)]; ok {
		log.Printf("Using cached connection to %s service at %s:%d", serviceName, host, port)
		return conn, nil
	}
	conn, err := grpc.NewClient(
		fmt.Sprintf("%s:%d", host, port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s service: %w", serviceName, err)
	}
	connectionCache[fmt.Sprintf("%s:%d", host, port)] = conn
	log.Printf("Connected to %s service at %s:%d", serviceName, host, port)
	return conn, nil
}

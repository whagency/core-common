package services

import "google.golang.org/grpc"

// GRPCClientBase is a common contract for gRPC-based service clients.
// It exposes connection access and lifecycle management.
type GRPCClientBase interface {
	// GetConnection returns a reusable grpc connection.
	GetConnection() (*grpc.ClientConn, error)

	// Close closes the underlying gRPC connection (if any).
	Close() error

	// Reset forces the next call to re-dial (rarely needed; mostly for tests).
	Reset() error
}

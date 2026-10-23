package grpc

import (
	"crypto/tls"
	"time"

	grpcCommon "github.com/whagency/core-common/grpc"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

const _defaultTimeoutExtCall = 15 * time.Second

type Options struct {
	grpcTimeout          time.Duration
	transportCredentials credentials.TransportCredentials
}

type Option func(*Options)

type Service struct {
	mgr *grpcCommon.ClientConnManager
}

// WithTimeoutExtCall sets the timeout for external calls to the merchant service.
func WithTimeoutExtCall(timeout time.Duration) Option {
	return func(o *Options) {
		o.grpcTimeout = timeout
	}
}

// WithTransportCredentials sets custom transport credentials for gRPC client connection.
func WithTransportCredentials(creds credentials.TransportCredentials) Option {
	return func(o *Options) {
		if creds != nil {
			o.transportCredentials = creds
		}
	}
}

// WithInsecureTransport enables plaintext gRPC transport.
// Use only in trusted internal networks when TLS is not available.
func WithInsecureTransport() Option {
	return func(o *Options) {
		o.transportCredentials = insecure.NewCredentials()
	}
}

// defaultServiceOptions provides the default options for the merchant service.
func defaultServiceOptions() *Options {
	return &Options{
		grpcTimeout:          _defaultTimeoutExtCall,
		transportCredentials: credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12}),
	}
}

// NewMerchantService creates a new instance of Service.
func NewMerchantService(
	merchantServiceGrpcURL string,
	opts ...Option,
) *Service {
	options := defaultServiceOptions()

	for _, opt := range opts {
		opt(options)
	}

	mgr := grpcCommon.NewClientConnManager(
		merchantServiceGrpcURL,
		grpc.WithTransportCredentials(options.transportCredentials),
		grpc.WithChainUnaryInterceptor(
			grpcCommon.UnaryClientTimeoutInterceptor(options.grpcTimeout),
			grpcCommon.ObservabilityUnaryClientInterceptor(),
		),
	)

	return &Service{
		mgr: mgr,
	}
}

// GetConnection returns the grpc connection.
func (s *Service) GetConnection() (*grpc.ClientConn, error) {
	return s.mgr.GetConnection()
}

// Close closes the underlying gRPC connection (if any).
func (s *Service) Close() error {
	if s == nil || s.mgr == nil {
		return nil
	}
	return s.mgr.Close()
}

// Reset forces the next call to re-dial (rarely needed; mostly for tests).
func (s *Service) Reset() error {
	if s == nil || s.mgr == nil {
		return nil
	}
	return s.mgr.Reset()
}

package extproc

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"

	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
)

// contextKey is a type for context keys to avoid collisions
type contextKey string

// streamContextKey is used to store the gRPC stream in the HTTP request context
const streamContextKey contextKey = "extproc-stream"

const (
	defaultTimeout    = 5 * time.Second
	defaultMaxMsgSize = 4 * 1024 * 1024 // 4MB
)

// StreamWrapper wraps the gRPC stream with context management
type StreamWrapper struct {
	stream extprocv3.ExternalProcessor_ProcessClient
	cancel context.CancelFunc
}

// Send sends a processing request on the stream
func (s *StreamWrapper) Send(req *extprocv3.ProcessingRequest) error {
	return s.stream.Send(req)
}

// Recv receives a processing response from the stream
func (s *StreamWrapper) Recv() (*extprocv3.ProcessingResponse, error) {
	return s.stream.Recv()
}

// Close closes the stream and cancels the context
func (s *StreamWrapper) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	return s.stream.CloseSend()
}

// getStreamFromContext retrieves the gRPC stream from the HTTP request context
func getStreamFromContext(ctx context.Context) (*StreamWrapper, bool) {
	stream, ok := ctx.Value(streamContextKey).(*StreamWrapper)
	return stream, ok
}

// storeStreamInContext stores the gRPC stream in the HTTP request context
func storeStreamInContext(ctx context.Context, stream *StreamWrapper) context.Context {
	return context.WithValue(ctx, streamContextKey, stream)
}

// ExtProcClient defines the interface for external processing client.
type ExtProcClient interface {
	CreateStream(ctx context.Context) (*StreamWrapper, error)
	ProcessHeaders(ctx context.Context, req *extprocv3.ProcessingRequest) (*extprocv3.ProcessingResponse, error)
	Close() error
}

// ClientConfig holds the configuration for the gRPC client.
type ClientConfig struct {
	ServerAddr     string
	Timeout        time.Duration
	InsecureConn   bool
	MaxRecvMsgSize int
	MaxSendMsgSize int
}

// grpcClient implements ExtProcClient using gRPC.
type grpcClient struct {
	conn   *grpc.ClientConn
	client extprocv3.ExternalProcessorClient
	config ClientConfig
}

// NewGRPCClient creates a new gRPC client for external processing.
func NewGRPCClient(config ClientConfig) (ExtProcClient, error) {
	// Set default values
	if config.Timeout == 0 {
		config.Timeout = defaultTimeout
	}
	if config.MaxRecvMsgSize == 0 {
		config.MaxRecvMsgSize = defaultMaxMsgSize
	}
	if config.MaxSendMsgSize == 0 {
		config.MaxSendMsgSize = defaultMaxMsgSize
	}

	// Create gRPC connection options
	var opts []grpc.DialOption

	if config.InsecureConn {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	opts = append(opts,
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(config.MaxRecvMsgSize),
			grpc.MaxCallSendMsgSize(config.MaxSendMsgSize),
		),
		grpc.WithBlock(),
	)

	// Create connection with timeout
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx, config.ServerAddr, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ext-proc server %s: %w", config.ServerAddr, err)
	}

	client := extprocv3.NewExternalProcessorClient(conn)

	// Perform health check
	healthClient := grpc_health_v1.NewHealthClient(conn)
	healthResp, err := healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("health check failed for ext-proc server %s: %w", config.ServerAddr, err)
	}

	if healthResp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		conn.Close()
		return nil, fmt.Errorf("ext-proc server %s is not healthy: %s", config.ServerAddr, healthResp.Status)
	}

	return &grpcClient{
		conn:   conn,
		client: client,
		config: config,
	}, nil
}

// CreateStream creates a new persistent gRPC stream for external processing
func (c *grpcClient) CreateStream(ctx context.Context) (*StreamWrapper, error) {
	// Create a context with timeout for the entire stream lifetime
	streamCtx, cancel := context.WithTimeout(ctx, c.config.Timeout*10) // Extended timeout for persistent stream

	// Create bidirectional stream
	stream, err := c.client.Process(streamCtx)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create processing stream: %w", err)
	}

	return &StreamWrapper{
		stream: stream,
		cancel: cancel,
	}, nil
}

// ProcessHeaders sends a processing request and returns the response using persistent stream.
func (c *grpcClient) ProcessHeaders(ctx context.Context, req *extprocv3.ProcessingRequest) (*extprocv3.ProcessingResponse, error) {
	// Get the persistent stream from context
	stream, ok := getStreamFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("no persistent stream found in context - CreateStream must be called first")
	}

	// Send the processing request
	if err := stream.Send(req); err != nil {
		return nil, fmt.Errorf("failed to send processing request: %w", err)
	}

	// Receive the processing response
	resp, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("failed to receive processing response: %w", err)
	}

	return resp, nil
}

// Close closes the gRPC client connection.
func (c *grpcClient) Close() error {
	return c.conn.Close()
}

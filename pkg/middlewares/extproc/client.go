package extproc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"

	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
)

const (
	defaultTimeout    = 5 * time.Second
	defaultMaxMsgSize = 4 * 1024 * 1024 // 4MB
)

// ExtProcClient defines the interface for external processing client.
type ExtProcClient interface {
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
	mutex  sync.RWMutex
	closed bool
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

// ProcessHeaders sends a processing request and returns the response.
func (c *grpcClient) ProcessHeaders(ctx context.Context, req *extprocv3.ProcessingRequest) (*extprocv3.ProcessingResponse, error) {
	c.mutex.RLock()
	if c.closed {
		c.mutex.RUnlock()
		return nil, ErrClientClosed
	}
	client := c.client
	c.mutex.RUnlock()

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	// Create bidirectional stream
	stream, err := client.Process(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create processing stream: %w", err)
	}
	defer stream.CloseSend()

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
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true
	return c.conn.Close()
}

// Pool manages a pool of gRPC clients for better performance.
type Pool struct {
	config  ClientConfig
	clients chan ExtProcClient
	factory func() (ExtProcClient, error)
	maxSize int
	mutex   sync.RWMutex
	closed  bool
}

// NewPool creates a new client pool.
func NewPool(config ClientConfig, maxSize int) (*Pool, error) {
	if maxSize <= 0 {
		maxSize = 10 // Default pool size
	}

	pool := &Pool{
		config:  config,
		clients: make(chan ExtProcClient, maxSize),
		maxSize: maxSize,
		factory: func() (ExtProcClient, error) {
			return NewGRPCClient(config)
		},
	}

	// Pre-populate the pool with initial connections
	initialSize := maxSize / 2
	if initialSize < 1 {
		initialSize = 1
	}

	for i := 0; i < initialSize; i++ {
		client, err := pool.factory()
		if err != nil {
			pool.Close()
			return nil, fmt.Errorf("failed to create initial client: %w", err)
		}
		pool.clients <- client
	}

	return pool, nil
}

// Get retrieves a client from the pool.
func (p *Pool) Get() (ExtProcClient, error) {
	p.mutex.RLock()
	if p.closed {
		p.mutex.RUnlock()
		return nil, ErrPoolClosed
	}
	p.mutex.RUnlock()

	select {
	case client := <-p.clients:
		return client, nil
	default:
		// Pool is empty, create a new client
		return p.factory()
	}
}

// Put returns a client to the pool.
func (p *Pool) Put(client ExtProcClient) {
	if client == nil {
		return
	}

	p.mutex.RLock()
	if p.closed {
		p.mutex.RUnlock()
		client.Close()
		return
	}
	p.mutex.RUnlock()

	select {
	case p.clients <- client:
		// Client returned to pool
	default:
		// Pool is full, close the client
		client.Close()
	}
}

// Close closes all clients in the pool.
func (p *Pool) Close() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.closed {
		return nil
	}

	p.closed = true
	close(p.clients)

	// Close all clients in the pool
	for client := range p.clients {
		client.Close()
	}

	return nil
}

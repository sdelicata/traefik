package extproc

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog"
	ptypes "github.com/traefik/paerser/types"
	"github.com/traefik/traefik/v3/pkg/config/dynamic"
	"github.com/traefik/traefik/v3/pkg/middlewares"
)

const (
	typeName = "ExtProc"
)

// extProc is the external processing middleware.
type extProc struct {
	name   string
	next   http.Handler
	client ExtProcClient
	config *dynamic.ExtProc
}

// New creates a new external processing middleware instance.
func New(ctx context.Context, next http.Handler, config dynamic.ExtProc, name string) (http.Handler, error) {
	logger := middlewares.GetLogger(ctx, name, typeName)
	logger.Debug().Msg("Creating ext-proc middleware")

	// Validate configuration
	if config.GRPCServer == "" {
		return nil, ErrMissingGRPCServer
	}

	// Create gRPC client
	clientConfig := ClientConfig{
		ServerAddr:     config.GRPCServer,
		Timeout:        getTimeout(config.Timeout),
		InsecureConn:   config.InsecureConn,
		MaxRecvMsgSize: getMaxMsgSize(config.MaxRecvMsgSize, defaultMaxMsgSize),
		MaxSendMsgSize: getMaxMsgSize(config.MaxSendMsgSize, defaultMaxMsgSize),
	}

	client, err := NewGRPCClient(clientConfig)
	if err != nil {
		return nil, err
	}

	return &extProc{
		name:   name,
		next:   next,
		client: client,
		config: &config,
	}, nil
}

// GetTracingInformation returns tracing information for the middleware.
func (e *extProc) GetTracingInformation() (string, string) {
	return e.name, typeName
}

// ServeHTTP handles HTTP requests through external processing.
func (e *extProc) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	logger := middlewares.GetLogger(ctx, e.name, typeName)

	// Check if processing mode requires request header processing
	if shouldProcessRequestHeaders(e.config.ProcessingMode) {
		// Create processing request for request headers
		procReq, err := HTTPRequestToProcessingRequest(req)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to create processing request")
			// Continue without processing on error (fail-open)
			e.next.ServeHTTP(rw, req)
			return
		}

		// Send to external processor
		procResp, err := e.client.ProcessHeaders(ctx, procReq)
		if err != nil {
			logger.Error().Err(err).Msg("External processing failed")
			// Continue without processing on error (fail-open)
			e.next.ServeHTTP(rw, req)
			return
		}

		// Apply request mutations if any
		if err := ApplyRequestMutations(req, procResp); err != nil {
			logger.Error().Err(err).Msg("Failed to apply request mutations")
			// Continue without processing on error
		}

		logger.Debug().Msg("Request headers processed successfully")
	}

	// Check if processing mode requires response header processing
	if shouldProcessResponseHeaders(e.config.ProcessingMode) {
		// Wrap the response writer to intercept response
		wrappedRW := &responseWriter{
			ResponseWriter: rw,
			extProc:        e,
			ctx:            ctx,
			logger:         logger,
			headers:        make(http.Header),
			statusCode:     http.StatusOK, // Default status
		}

		// Continue with the middleware chain
		e.next.ServeHTTP(wrappedRW, req)
	} else {
		// No response processing needed, continue normally
		e.next.ServeHTTP(rw, req)
	}
}

// responseWriter wraps http.ResponseWriter to intercept response headers.
type responseWriter struct {
	http.ResponseWriter
	extProc *extProc
	ctx     context.Context
	logger  *zerolog.Logger

	headers    http.Header
	statusCode int
	written    bool
	mutex      sync.RWMutex
}

// Header returns the header map for the response.
func (rw *responseWriter) Header() http.Header {
	return rw.ResponseWriter.Header()
}

// Write writes the data to the connection as part of an HTTP reply.
func (rw *responseWriter) Write(data []byte) (int, error) {
	if !rw.written {
		rw.processResponseHeaders()
	}
	return rw.ResponseWriter.Write(data)
}

// WriteHeader sends an HTTP response header with the provided status code.
func (rw *responseWriter) WriteHeader(code int) {
	rw.mutex.Lock()
	rw.statusCode = code
	rw.mutex.Unlock()

	if !rw.written {
		rw.processResponseHeaders()
	}
	rw.ResponseWriter.WriteHeader(code)
}

// processResponseHeaders processes response headers through external processor.
func (rw *responseWriter) processResponseHeaders() {
	rw.mutex.Lock()
	defer rw.mutex.Unlock()

	if rw.written {
		return
	}
	rw.written = true

	// Create processing request for response headers
	procReq, err := HTTPResponseToProcessingRequest(rw.ResponseWriter, rw.statusCode)
	if err != nil {
		rw.logger.Error().Err(err).Msg("Failed to create response processing request")
		return
	}

	// Send to external processor
	procResp, err := rw.extProc.client.ProcessHeaders(rw.ctx, procReq)
	if err != nil {
		rw.logger.Error().Err(err).Msg("Response processing failed")
		return
	}

	// Apply response mutations
	if err := ApplyResponseMutations(rw.ResponseWriter, procResp); err != nil {
		rw.logger.Error().Err(err).Msg("Failed to apply response mutations")
		return
	}

	rw.logger.Debug().Msg("Response headers processed successfully")
}

// Helper functions

func getTimeout(timeout *ptypes.Duration) time.Duration {
	if timeout != nil {
		return time.Duration(*timeout)
	}
	return defaultTimeout
}

func getMaxMsgSize(size int, defaultSize int) int {
	if size > 0 {
		return size
	}
	return defaultSize
}

func shouldProcessRequestHeaders(mode *dynamic.ProcessingMode) bool {
	if mode == nil {
		return false
	}
	return mode.RequestHeadersMode == "SEND" || mode.RequestHeadersMode == "STREAMED"
}

func shouldProcessResponseHeaders(mode *dynamic.ProcessingMode) bool {
	if mode == nil {
		return false
	}
	return mode.ResponseHeadersMode == "SEND" || mode.ResponseHeadersMode == "STREAMED"
}

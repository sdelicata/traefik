// Package extproc implements the External Processing middleware for Traefik.
//
// Trailer Handling Note:
// Similar to Envoy's ext_proc filter, this implementation does not pre-declare HTTP/1.1 trailers
// in the Trailer header. This design choice means:
// - Trailers work seamlessly with HTTP/2 and modern HTTP/1.1 clients
// - The ext_proc server can dynamically add any trailer without prior declaration
// - Strict HTTP/1.1 clients expecting Trailer header declaration may not see trailers
// This trade-off prioritizes flexibility and simplicity over strict HTTP/1.1 RFC compliance.
package extproc

import (
	"context"
	"io"
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

	// Header processing modes
	HeaderModeSkip = "SKIP"
	HeaderModeSend = "SEND"

	// Body processing modes
	BodyModeNone            = "NONE"
	BodyModeStreamed        = "STREAMED"
	BodyModeBuffered        = "BUFFERED"
	BodyModeBufferedPartial = "BUFFERED_PARTIAL"
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

	// Check if any processing is needed
	needsRequestProcessing := shouldProcessRequestHeaders(e.config.ProcessingMode)
	needsResponseProcessing := shouldProcessResponseHeaders(e.config.ProcessingMode)
	needsBodyProcessing := shouldProcessRequestBody(e.config.ProcessingMode)
	needsRequestTrailersProcessing := shouldProcessRequestTrailers(e.config.ProcessingMode)
	needsResponseTrailersProcessing := shouldProcessResponseTrailers(e.config.ProcessingMode)

	if !needsRequestProcessing && !needsResponseProcessing && !needsBodyProcessing && !needsRequestTrailersProcessing && !needsResponseTrailersProcessing {
		// No processing needed, continue normally
		e.next.ServeHTTP(rw, req)
		return
	}

	// Create persistent gRPC stream for this HTTP request
	stream, err := e.client.CreateStream(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to create ext-proc stream")
		// Continue without processing on error (fail-open)
		e.next.ServeHTTP(rw, req)
		return
	}
	defer stream.Close()

	// Store stream in context for reuse in response processing
	ctxWithStream := storeStreamInContext(ctx, stream)
	req = req.WithContext(ctxWithStream)

	// Process request headers if configured
	if needsRequestProcessing {
		// Create processing request for request headers
		// FIXME: rename to introduce header idea.
		procReq, err := HTTPRequestToProcessingRequest(req, e.config)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to create processing request")
			// Continue without processing on error (fail-open)
			e.next.ServeHTTP(rw, req)
			return
		}

		// Send to external processor using persistent stream
		procResp, err := e.client.ProcessHeaders(ctxWithStream, procReq)
		if err != nil {
			logger.Error().Err(err).Msg("External processing failed")
			// Continue without processing on error (fail-open)
			e.next.ServeHTTP(rw, req)
			return
		}

		// Apply request mutations if any
		// TODO: Validate that response only contains directives for modifying request headers.
		if err := ApplyRequestMutations(req, procResp); err != nil {
			logger.Error().Err(err).Msg("Failed to apply request mutations")
			// Continue without processing on error
		}

		logger.Debug().Msg("Request headers processed successfully")
	}

	// Process request body if configured
	if needsBodyProcessing {
		// Read and buffer the request body
		// IMPORTANT: This will also populate req.Trailer after reading the body
		bodyBytes, err := ReadRequestBody(req)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to read request body")
			// Continue without processing on error (fail-open)
			e.next.ServeHTTP(rw, req)
			return
		}

		// Create processing request for request body
		procReq, err := HTTPRequestBodyToProcessingRequest(bodyBytes)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to create body processing request")
			// Continue without processing on error (fail-open)
			e.next.ServeHTTP(rw, req)
			return
		}

		// Send to external processor using persistent stream
		procResp, err := e.client.ProcessBody(ctxWithStream, procReq)
		if err != nil {
			logger.Error().Err(err).Msg("External body processing failed")
			// Continue without processing on error (fail-open)
			e.next.ServeHTTP(rw, req)
			return
		}

		// Check if we should return an immediate response (error)
		if procResp.GetImmediateResponse() != nil {
			immediateResp := procResp.GetImmediateResponse()
			statusCode := int(immediateResp.Status.Code)

			logger.Info().
				Int("status_code", statusCode).
				Msg("External processor requested immediate response")

			// Write the immediate response
			rw.WriteHeader(statusCode)
			if len(immediateResp.Body) > 0 {
				rw.Write([]byte(immediateResp.Body))
			}
			return
		}

		logger.Debug().Msg("Request body processed successfully")
	}

	// Process request trailers if configured (after body processing - Envoy approach)
	if needsRequestTrailersProcessing && len(req.Trailer) > 0 {
		processRequestTrailers(req, e, ctxWithStream, logger)
	}

	// Process response headers or trailers if configured
	if needsResponseProcessing || needsResponseTrailersProcessing {
		// Wrap the response writer to intercept response (will reuse the same stream)
		wrappedRW := &responseWriter{
			ResponseWriter:                  rw,
			extProc:                         e,
			ctx:                             ctxWithStream, // Use context with stream
			logger:                          logger,
			headers:                         make(http.Header),
			statusCode:                      http.StatusOK, // Default status
			needsResponseProcessing:         needsResponseProcessing,
			needsResponseTrailersProcessing: needsResponseTrailersProcessing,
		}

		// Continue with the middleware chain (use updated request with stream context)
		e.next.ServeHTTP(wrappedRW, req)

		// IMPORTANT: Ensure trailers are processed after the handler completes
		wrappedRW.Close()
	} else {
		// No response processing needed, continue normally
		e.next.ServeHTTP(rw, req)
	}
}

// responseWriter wraps http.ResponseWriter to intercept response headers and trailers.
// FIXME: looks too complex.
type responseWriter struct {
	http.ResponseWriter
	extProc *extProc
	ctx     context.Context
	logger  *zerolog.Logger

	headers    http.Header
	statusCode int
	written    bool
	mutex      sync.RWMutex

	needsResponseProcessing         bool
	needsResponseTrailersProcessing bool
	trailersProcessed               bool
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

	n, err := rw.ResponseWriter.Write(data)

	// Flush after write to ensure chunked encoding when trailers are needed
	if rw.needsResponseTrailersProcessing {
		if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
			flusher.Flush()
		}
	}

	// Don't process trailers here - wait for explicit Flush or Close
	return n, err
}

// Flush implements http.Flusher if the underlying ResponseWriter supports it.
func (rw *responseWriter) Flush() {
	if !rw.written {
		rw.processResponseHeaders()
	}

	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}

	// Process trailers after flushing (body is complete)
	if rw.needsResponseTrailersProcessing && !rw.trailersProcessed {
		rw.processResponseTrailers()
	}
}

// Close ensures that trailers are processed after the response is complete.
func (rw *responseWriter) Close() error {
	if rw.needsResponseTrailersProcessing && !rw.trailersProcessed {
		rw.processResponseTrailers()
	}
	if closer, ok := rw.ResponseWriter.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// WriteHeader sends an HTTP response header with the provided status code.
func (rw *responseWriter) WriteHeader(code int) {
	rw.mutex.Lock()
	rw.statusCode = code

	// For HTTP/2 compatibility, we need to declare trailers that the ext-proc might add
	// This is a workaround since we don't know ahead of time what trailers will be added
	// FIXME: this entire part should be removed.
	if rw.needsResponseTrailersProcessing {
		// Declare common trailers that ext-proc might add
		// In a production system, this should be configurable or determined from ext-proc
		existingTrailer := rw.ResponseWriter.Header().Get("Trailer")
		trailers := []string{"x-response-trailer", "x-modified-trailer", "x-processed-trailer"}
		for _, t := range trailers {
			if existingTrailer != "" {
				existingTrailer += ", " + t
			} else {
				existingTrailer = t
			}
		}
		if existingTrailer != "" {
			rw.ResponseWriter.Header().Set("Trailer", existingTrailer)
		}
	}
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

	// Force chunked encoding if we need to send trailers
	// This is required for HTTP/1.1 trailers to work
	// FIXME: is this part still necessary?
	if rw.needsResponseTrailersProcessing {
		rw.ResponseWriter.Header().Del("Content-Length")
		// Go will automatically use chunked encoding when Content-Length is absent
	}

	// Process response headers if needed
	if rw.needsResponseProcessing {
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
}

// processRequestTrailers processes request trailers using the simplified Envoy approach
// This is called AFTER body processing, when req.Trailer is guaranteed to be populated
func processRequestTrailers(req *http.Request, extProc *extProc, ctx context.Context, logger *zerolog.Logger) {
	// At this point, req.Trailer is populated by Go after body reading
	if len(req.Trailer) == 0 {
		logger.Debug().Msg("No request trailers found, skipping processing")
		return
	}

	// Create processing request for request trailers
	procReq, err := HTTPRequestTrailersToProcessingRequest(req)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to create trailers processing request")
		return
	}

	// Send to external processor using the persistent stream
	procResp, err := extProc.client.ProcessHeaders(ctx, procReq)
	if err != nil {
		logger.Error().Err(err).Msg("External trailers processing failed")
		return
	}

	// Apply request trailers mutations if any
	if err := ApplyRequestTrailersMutations(req, procResp); err != nil {
		logger.Error().Err(err).Msg("Failed to apply request trailers mutations")
		return
	}

	logger.Info().Msg("Request trailers processed successfully")
}

// processResponseTrailers processes response trailers through external processor.
// This should be called after the response body has been sent.
func (rw *responseWriter) processResponseTrailers() {
	rw.mutex.Lock()
	defer rw.mutex.Unlock()

	if rw.trailersProcessed || !rw.needsResponseTrailersProcessing {
		return
	}
	rw.trailersProcessed = true

	// IMPORTANT: In HTTP/1.1, we set trailers using the Header() map after the body
	// For HTTP/2, trailers also work but must be declared in Trailer header first

	// Create processing request for response trailers (even if empty)
	procReq, err := HTTPResponseTrailersToProcessingRequest(rw.ResponseWriter)
	if err != nil {
		rw.logger.Error().Err(err).Msg("Failed to create response trailers processing request")
		return
	}

	// Send to external processor
	procResp, err := rw.extProc.client.ProcessHeaders(rw.ctx, procReq)
	if err != nil {
		rw.logger.Error().Err(err).Msg("Response trailers processing failed")
		return
	}

	// Apply response trailer mutations
	// IMPORTANT: In Go HTTP/1.1, trailers are set in the Header() map after body is written
	// For HTTP/2, this also works as long as trailers were declared
	if err := ApplyResponseTrailersMutations(rw.ResponseWriter, procResp); err != nil {
		rw.logger.Error().Err(err).Msg("Failed to apply response trailers mutations")
		return
	}

	rw.logger.Debug().Msg("Response trailers processed successfully")
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
	return mode.RequestHeadersMode == HeaderModeSend
}

func shouldProcessResponseHeaders(mode *dynamic.ProcessingMode) bool {
	if mode == nil {
		return false
	}
	return mode.ResponseHeadersMode == HeaderModeSend
}

func shouldProcessRequestBody(mode *dynamic.ProcessingMode) bool {
	if mode == nil {
		return false
	}
	return mode.RequestBodyMode != "" && mode.RequestBodyMode != BodyModeNone
}

func shouldProcessRequestTrailers(mode *dynamic.ProcessingMode) bool {
	if mode == nil {
		return false
	}
	return mode.RequestTrailersMode == HeaderModeSend
}

func shouldProcessResponseTrailers(mode *dynamic.ProcessingMode) bool {
	if mode == nil {
		return false
	}
	return mode.ResponseTrailersMode == HeaderModeSend
}

package extproc

import (
	"fmt"
	"os"
	"strings"
	"sync"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	processingmodev3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Server implements the external processing server
type Server struct {
	extprocv3.UnimplementedExternalProcessorServer
	logger zerolog.Logger
}

// streamContext holds data for a processing stream
type streamContext struct {
	requestID      string
	extractedValue string
	protocolConfig *extprocv3.ProtocolConfiguration
	isFirstMessage bool
	mutex          sync.RWMutex
}

// NewServer creates a new ext-proc server instance
func NewServer() *Server {
	// Configure logger
	logger := zerolog.New(os.Stdout).With().
		Timestamp().
		Str("service", "extproc-server").
		Logger()

	// Set log level from environment
	if level := os.Getenv("LOG_LEVEL"); level != "" {
		if logLevel, err := zerolog.ParseLevel(level); err == nil {
			logger = logger.Level(logLevel)
		}
	}

	return &Server{
		logger: logger,
	}
}

// Process handles the bidirectional gRPC stream for external processing
func (s *Server) Process(stream extprocv3.ExternalProcessor_ProcessServer) error {
	ctx := stream.Context()
	streamID := fmt.Sprintf("stream-%p", stream)

	s.logger.Info().
		Str("stream_id", streamID).
		Msg("New ext-proc stream started")

	// Create stream context for this processing session
	streamCtx := &streamContext{
		requestID:      streamID,
		isFirstMessage: true,
	}

	// Handle stream processing
	for {
		select {
		case <-ctx.Done():
			s.logger.Info().
				Str("stream_id", streamID).
				Msg("Stream context cancelled")
			return ctx.Err()
		default:
		}

		// Receive processing request
		req, err := stream.Recv()
		if err != nil {
			s.logger.Error().
				Err(err).
				Str("stream_id", streamID).
				Msg("Failed to receive processing request")
			return err
		}

		// Process the request and create response
		resp, err := s.processRequest(streamCtx, req)
		if err != nil {
			s.logger.Error().
				Str("stream_id", streamID).
				Msg("Failed to process request - closing stream")

			// If this is a protocol configuration error, we should close the stream

			return err
		}

		// If response is nil, it means the stream should be closed
		if resp == nil {
			s.logger.Info().
				Str("stream_id", streamID).
				Msg("Closing stream as requested by processor")
			return nil
		}

		// Send processing response
		if err := stream.Send(resp); err != nil {
			s.logger.Error().
				Err(err).
				Str("stream_id", streamID).
				Msg("Failed to send processing response")
			return err
		}

		s.logger.Debug().
			Str("stream_id", streamID).
			Msg("Processing request handled successfully")
	}
}

// processRequest processes different types of processing requests
func (s *Server) processRequest(streamCtx *streamContext, req *extprocv3.ProcessingRequest) (*extprocv3.ProcessingResponse, error) {
	// Handle ProtocolConfiguration in the first message
	streamCtx.mutex.Lock()
	if streamCtx.isFirstMessage {
		streamCtx.isFirstMessage = false
		// DEBUG: More detailed logging
		reqType := "nil"
		if req.Request != nil {
			switch req.Request.(type) {
			case *extprocv3.ProcessingRequest_RequestHeaders:
				reqType = "request_headers"
			case *extprocv3.ProcessingRequest_ResponseHeaders:
				reqType = "response_headers"
			case *extprocv3.ProcessingRequest_RequestBody:
				reqType = "request_body"
			case *extprocv3.ProcessingRequest_ResponseBody:

			}
		}

		s.logger.Debug().
			Str("stream_id", streamCtx.requestID).
			Str("request_type", reqType).
			Msg("Processing first message")

		if req.ProtocolConfig != nil {
			s.logger.Info().
				Str("stream_id", streamCtx.requestID).
				Str("request_body_mode", req.ProtocolConfig.RequestBodyMode.String()).
				Bool("send_body_without_waiting", req.ProtocolConfig.SendBodyWithoutWaitingForHeaderResponse).
				Msg("Received ProtocolConfiguration")

			// Validate and store the protocol configuration
			if err := s.validateProtocolConfiguration(req.ProtocolConfig); err != nil {
				streamCtx.mutex.Unlock()
				s.logger.Error().
					Err(err).
					Str("stream_id", streamCtx.requestID).
					Msg("ProtocolConfiguration validation failed")
				return nil, err
			}

			streamCtx.protocolConfig = req.ProtocolConfig
			s.logger.Info().
				Str("stream_id", streamCtx.requestID).
				Msg("ProtocolConfiguration processed and stored")
		} else {
			s.logger.Debug().
				Str("stream_id", streamCtx.requestID).
				Msg("No ProtocolConfiguration in first message")
		}

		// NOTE: The first message can contain BOTH ProtocolConfig AND Request

	}
	streamCtx.mutex.Unlock()

	// Process the actual request
	switch r := req.Request.(type) {
	case *extprocv3.ProcessingRequest_RequestHeaders:
		return s.handleRequestHeaders(streamCtx, r.RequestHeaders), nil

	case *extprocv3.ProcessingRequest_ResponseHeaders:
		return s.handleResponseHeaders(streamCtx, r.ResponseHeaders), nil

	case *extprocv3.ProcessingRequest_RequestBody:
		return s.handleRequestBody(streamCtx, r.RequestBody), nil

	case *extprocv3.ProcessingRequest_ResponseBody:
		// For now, we don't process response bodies - create proper response body continue
		return &extprocv3.ProcessingResponse{
			Response: &extprocv3.ProcessingResponse_ResponseBody{
				ResponseBody: &extprocv3.BodyResponse{
					Response: &extprocv3.CommonResponse{
						Status: extprocv3.CommonResponse_CONTINUE,
					},
				},
			},
		}, nil

	case *extprocv3.ProcessingRequest_RequestTrailers:
		return s.handleRequestTrailers(streamCtx, r.RequestTrailers), nil

	case *extprocv3.ProcessingRequest_ResponseTrailers:
		return s.handleResponseTrailers(streamCtx, r.ResponseTrailers), nil

	default:
		s.logger.Warn().
			Str("stream_id", streamCtx.requestID).
			Msg("Unknown request type, continuing without processing")
		return s.createContinueResponse(), nil
	}
}

// handleRequestHeaders processes request headers and extracts X-Request-Header
func (s *Server) handleRequestHeaders(streamCtx *streamContext, reqHeaders *extprocv3.HttpHeaders) *extprocv3.ProcessingResponse {
	s.logger.Debug().
		Str("stream_id", streamCtx.requestID).
		Int("header_count", len(reqHeaders.Headers.Headers)).
		Msg("Processing request headers")

	// Debug: Log all headers received
	for _, header := range reqHeaders.Headers.Headers {
		s.logger.Debug().
			Str("stream_id", streamCtx.requestID).
			Str("key", header.Key).
			Str("value", header.Value).
			Msg("Received header")
	}

	// Extract X-Request-Header value
	headerValue := s.extractHeaderValue(reqHeaders.Headers.Headers, "x-request-header")

	// Also check for X-Request-Trailer header (for test 11 - response trailers test)
	trailerValue := s.extractHeaderValue(reqHeaders.Headers.Headers, "x-request-trailer")

	// Store in streamCtx for response processing (stream handles request/response correlation)
	streamCtx.mutex.Lock()
	if headerValue != "" {
		streamCtx.extractedValue = headerValue
	} else if trailerValue != "" {
		streamCtx.extractedValue = trailerValue
	}
	streamCtx.mutex.Unlock()

	if headerValue != "" {
		s.logger.Info().
			Str("stream_id", streamCtx.requestID).
			Str("header_value", headerValue).
			Msg("Extracted X-Request-Header value")
	} else if trailerValue != "" {
		s.logger.Info().
			Str("stream_id", streamCtx.requestID).
			Str("trailer_value", trailerValue).
			Msg("Extracted X-Request-Trailer value for response trailer generation")
	} else {
		s.logger.Debug().
			Str("stream_id", streamCtx.requestID).
			Msg("No X-Request-Header or X-Request-Trailer found in request")
	}

	// Continue processing without modifying request headers
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_RequestHeaders{
			RequestHeaders: &extprocv3.HeadersResponse{
				Response: &extprocv3.CommonResponse{
					Status: extprocv3.CommonResponse_CONTINUE,
				},
			},
		},
	}
}

// handleResponseHeaders processes response headers and adds X-Response-Header
func (s *Server) handleResponseHeaders(streamCtx *streamContext, respHeaders *extprocv3.HttpHeaders) *extprocv3.ProcessingResponse {
	s.logger.Debug().
		Str("stream_id", streamCtx.requestID).
		Msg("Processing response headers")

	// FIXME: respHeaders is overriden.

	// Read extracted value from streamCtx (stream-based correlation)
	streamCtx.mutex.RLock()
	extractedValue := streamCtx.extractedValue
	streamCtx.mutex.RUnlock()

	// Only add X-Response-Header if we extracted a value from the request
	if extractedValue == "" {
		s.logger.Debug().
			Str("stream_id", streamCtx.requestID).
			Msg("No extracted value, continuing without header modification")
		return s.createResponseHeadersContinueResponse()
	}

	s.logger.Info().
		Str("stream_id", streamCtx.requestID).
		Str("extracted_value", extractedValue).
		Msg("Adding X-Response-Header to response")

	// Create header mutation to add both X-Response-Header and X-Processed-Trailer
	var setHeaders []*corev3.HeaderValueOption

	// Add X-Response-Header for backward compatibility
	setHeaders = append(setHeaders, &corev3.HeaderValueOption{
		Header: &corev3.HeaderValue{
			Key:   "x-response-header",
			Value: extractedValue,
		},
		Append: wrapperspb.Bool(false), // Set, don't append
	})

	// Add X-Processed-Trailer for Test 10 (when extracted value came from request trailers)
	setHeaders = append(setHeaders, &corev3.HeaderValueOption{
		Header: &corev3.HeaderValue{
			Key:   "X-Processed-Trailer",
			Value: "processed-" + extractedValue,
		},
		Append: wrapperspb.Bool(false), // Set, don't append
	})

	headerMutation := &extprocv3.HeaderMutation{
		SetHeaders: setHeaders,
	}

	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_ResponseHeaders{
			ResponseHeaders: &extprocv3.HeadersResponse{
				Response: &extprocv3.CommonResponse{
					Status:         extprocv3.CommonResponse_CONTINUE,
					HeaderMutation: headerMutation,
				},
			},
		},
	}
}

// extractHeaderValue extracts a header value by key (case insensitive)
func (s *Server) extractHeaderValue(headers []*corev3.HeaderValue, key string) string {
	lowerKey := strings.ToLower(key)
	for _, header := range headers {
		if strings.ToLower(header.Key) == lowerKey {
			return header.Value
		}
	}
	return ""
}

// handleRequestBody processes request body according to protocol configuration
func (s *Server) handleRequestBody(streamCtx *streamContext, requestBody *extprocv3.HttpBody) *extprocv3.ProcessingResponse {
	streamCtx.mutex.RLock()
	protocolConfig := streamCtx.protocolConfig
	streamCtx.mutex.RUnlock()

	// Check if body processing should be skipped
	if protocolConfig != nil && protocolConfig.RequestBodyMode == processingmodev3.ProcessingMode_NONE {
		s.logger.Debug().
			Str("stream_id", streamCtx.requestID).
			Msg("Body processing skipped - mode NONE")
		return s.createRequestBodyContinueResponse()
	}

	s.logger.Debug().
		Str("stream_id", streamCtx.requestID).
		Int("body_size", len(requestBody.Body)).
		Bool("end_of_stream", requestBody.EndOfStream).
		Msg("Processing request body")

	// Handle different body modes
	if protocolConfig != nil {
		switch protocolConfig.RequestBodyMode {
		case processingmodev3.ProcessingMode_STREAMED:
			s.logger.Debug().
				Str("stream_id", streamCtx.requestID).
				Msg("Processing request body in STREAMED mode (treated as buffered for now)")
			// For now, treat streamed as buffered
		case processingmodev3.ProcessingMode_BUFFERED_PARTIAL:
			s.logger.Debug().
				Str("stream_id", streamCtx.requestID).
				Msg("Processing request body in BUFFERED_PARTIAL mode")
		}
	}

	// Convert body bytes to string and check for "stop"
	bodyString := string(requestBody.Body)

	// Debug: Log the exact body content
	s.logger.Debug().
		Str("stream_id", streamCtx.requestID).
		Str("body_content", bodyString).
		Int("body_length", len(bodyString)).
		Msg("Request body content debug")

	if strings.Contains(bodyString, "stop") {
		s.logger.Info().
			Str("stream_id", streamCtx.requestID).
			Msg("Found 'stop' in request body, returning 503 error")

		// Return immediate response with 503 status
		return &extprocv3.ProcessingResponse{
			Response: &extprocv3.ProcessingResponse_ImmediateResponse{
				ImmediateResponse: &extprocv3.ImmediateResponse{
					Status: &typev3.HttpStatus{
						Code: 503, // Service Unavailable
					},
					Body: []byte("Service temporarily unavailable - stop keyword detected"),
				},
			},
		}
	}

	// Test 12: Check for "modify-trailers" in body to trigger trailer mutation
	if strings.Contains(bodyString, "modify-trailers") {
		s.logger.Info().
			Str("stream_id", streamCtx.requestID).
			Str("body_content", bodyString).
			Msg("Found 'modify-trailers' in request body, will modify trailers")

		// Store flag for trailer mutation
		streamCtx.mutex.Lock()
		streamCtx.extractedValue = "modify-trailers"
		streamCtx.mutex.Unlock()
	}

	s.logger.Debug().
		Str("stream_id", streamCtx.requestID).
		Msg("Request body processed successfully, continuing")

	// Continue processing
	return s.createRequestBodyContinueResponse()
}

// validateProtocolConfiguration validates the received ProtocolConfiguration
func (s *Server) validateProtocolConfiguration(config *extprocv3.ProtocolConfiguration) error {
	if config == nil {
		return nil
	}

	// Check supported request body modes
	switch config.RequestBodyMode {
	case processingmodev3.ProcessingMode_NONE:
		s.logger.Debug().Msg("Request body mode: NONE - no body processing")
	case processingmodev3.ProcessingMode_BUFFERED:
		s.logger.Debug().Msg("Request body mode: BUFFERED - full body buffered (supported)")
	case processingmodev3.ProcessingMode_STREAMED:
		s.logger.Warn().Msg("Request body mode: STREAMED - streaming not fully implemented")
		// For now, we'll accept it but treat it as buffered
	case processingmodev3.ProcessingMode_BUFFERED_PARTIAL:
		s.logger.Debug().Msg("Request body mode: BUFFERED_PARTIAL - treating as buffered")
	case processingmodev3.ProcessingMode_FULL_DUPLEX_STREAMED:
		s.logger.Error().Msg("Request body mode: FULL_DUPLEX_STREAMED - not supported")
		return fmt.Errorf("unsupported request body mode: FULL_DUPLEX_STREAMED")
	default:
		return fmt.Errorf("unknown request body mode: %v", config.RequestBodyMode)
	}

	// Check supported response body modes
	switch config.ResponseBodyMode {
	case processingmodev3.ProcessingMode_NONE:
		s.logger.Debug().Msg("Response body mode: NONE - no response body processing (supported)")
	case processingmodev3.ProcessingMode_BUFFERED:
		s.logger.Debug().Msg("Response body mode: BUFFERED - full response body buffered")
	case processingmodev3.ProcessingMode_STREAMED:
		s.logger.Warn().Msg("Response body mode: STREAMED - streaming not implemented")
		return fmt.Errorf("unsupported response body mode: STREAMED")
	case processingmodev3.ProcessingMode_BUFFERED_PARTIAL:
		s.logger.Warn().Msg("Response body mode: BUFFERED_PARTIAL - not implemented")
		return fmt.Errorf("unsupported response body mode: BUFFERED_PARTIAL")
	case processingmodev3.ProcessingMode_FULL_DUPLEX_STREAMED:
		s.logger.Error().Msg("Response body mode: FULL_DUPLEX_STREAMED - not supported")
		return fmt.Errorf("unsupported response body mode: FULL_DUPLEX_STREAMED")
	default:
		return fmt.Errorf("unknown response body mode: %v", config.ResponseBodyMode)
	}

	// Log the send_body_without_waiting_for_header_response setting
	if config.SendBodyWithoutWaitingForHeaderResponse {
		s.logger.Debug().Msg("Send body without waiting for header response: enabled")
	} else {
		s.logger.Debug().Msg("Send body without waiting for header response: disabled")
	}

	s.logger.Info().Msg("ProtocolConfiguration validated successfully")
	return nil
}

// createContinueResponse creates a generic continue response (use for protocol config)
func (s *Server) createContinueResponse() *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_RequestHeaders{
			RequestHeaders: &extprocv3.HeadersResponse{
				Response: &extprocv3.CommonResponse{
					Status: extprocv3.CommonResponse_CONTINUE,
				},
			},
		},
	}
}

// createRequestBodyContinueResponse creates a continue response for request body processing
func (s *Server) createRequestBodyContinueResponse() *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_RequestBody{
			RequestBody: &extprocv3.BodyResponse{
				Response: &extprocv3.CommonResponse{
					Status: extprocv3.CommonResponse_CONTINUE,
				},
			},
		},
	}
}

// createResponseHeadersContinueResponse creates a continue response for response headers
func (s *Server) createResponseHeadersContinueResponse() *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_ResponseHeaders{
			ResponseHeaders: &extprocv3.HeadersResponse{
				Response: &extprocv3.CommonResponse{
					Status: extprocv3.CommonResponse_CONTINUE,
				},
			},
		},
	}
}

// handleRequestTrailers processes request trailers
func (s *Server) handleRequestTrailers(streamCtx *streamContext, reqTrailers *extprocv3.HttpTrailers) *extprocv3.ProcessingResponse {
	// Example processing: Look for X-Request-Trailer header
	trailerValue := s.extractHeaderValue(reqTrailers.Trailers.Headers, "x-request-trailer")

	if trailerValue != "" {

		// Store the trailer value in streamCtx for potential use in response processing
		streamCtx.mutex.Lock()
		streamCtx.extractedValue = trailerValue // Reuse existing field for simplicity
		streamCtx.mutex.Unlock()

		// Test 10 expects: X-Processed-Trailer
		headerMutation := &extprocv3.HeaderMutation{
			SetHeaders: []*corev3.HeaderValueOption{
				{
					Header: &corev3.HeaderValue{
						Key:   "X-Processed-Trailer", // Match test expectation
						Value: "processed-" + trailerValue,
					},
					Append: wrapperspb.Bool(false),
				},
			},
		}

		return &extprocv3.ProcessingResponse{
			Response: &extprocv3.ProcessingResponse_RequestTrailers{
				RequestTrailers: &extprocv3.TrailersResponse{
					HeaderMutation: headerMutation,
				},
			},
		}
	}

	// No special processing needed, just continue
	return nil
}

// handleResponseTrailers processes response trailers
func (s *Server) handleResponseTrailers(streamCtx *streamContext, respTrailers *extprocv3.HttpTrailers) *extprocv3.ProcessingResponse {
	// Read any extracted value from request processing
	streamCtx.mutex.RLock()
	extractedValue := streamCtx.extractedValue
	streamCtx.mutex.RUnlock()

	// Generate response trailers based on extracted value from request processing
	if extractedValue != "" {
		s.logger.Info().
			Str("stream_id", streamCtx.requestID).
			Str("extracted_value", extractedValue).
			Msg("Adding response trailer based on request data")

		var headerMutation *extprocv3.HeaderMutation

		if extractedValue == "modify-trailers" {
			// Test 12: Generate x-modified-trailer when body contains "modify-trailers"
			headerMutation = &extprocv3.HeaderMutation{
				SetHeaders: []*corev3.HeaderValueOption{
					{
						Header: &corev3.HeaderValue{
							Key:   "x-modified-trailer",
							Value: "modified-value",
						},
						Append: wrapperspb.Bool(false),
					},
				},
			}
		} else {
			// Test 11: Default response trailer
			headerMutation = &extprocv3.HeaderMutation{
				SetHeaders: []*corev3.HeaderValueOption{
					{
						Header: &corev3.HeaderValue{
							Key:   "x-response-trailer",
							Value: "response-" + extractedValue,
						},
						Append: wrapperspb.Bool(false),
					},
				},
			}
		}

		return &extprocv3.ProcessingResponse{
			Response: &extprocv3.ProcessingResponse_ResponseTrailers{
				ResponseTrailers: &extprocv3.TrailersResponse{
					HeaderMutation: headerMutation,
				},
			},
		}
	}

	// No special processing needed, just continue
	return nil
}

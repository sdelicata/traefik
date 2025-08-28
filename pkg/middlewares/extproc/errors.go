package extproc

import "errors"

// Error definitions for the ext-proc middleware.
var (
	// Configuration errors
	ErrMissingGRPCServer = errors.New("gRPC server address is required")
	ErrInvalidTimeout    = errors.New("invalid timeout value")
	ErrInvalidMsgSize    = errors.New("invalid message size")

	// Client errors
	ErrClientClosed      = errors.New("gRPC client is closed")
	ErrPoolClosed        = errors.New("client pool is closed")
	ErrConnectionFailed  = errors.New("failed to connect to gRPC server")
	ErrHealthCheckFailed = errors.New("health check failed")

	// Protocol errors
	ErrNilRequest            = errors.New("HTTP request is nil")
	ErrNilResponseWriter     = errors.New("HTTP response writer is nil")
	ErrNilResponse           = errors.New("processing response is nil")
	ErrInvalidResponse       = errors.New("invalid processing response")
	ErrUnknownResponseType   = errors.New("unknown processing response type")
	ErrMissingCommonResponse = errors.New("missing common response")
	ErrInvalidHeaderMutation = errors.New("invalid header mutation")
	ErrEmptyHeaderName       = errors.New("header name cannot be empty")

	// Processing errors
	ErrProcessingTimeout    = errors.New("processing request timed out")
	ErrProcessingFailed     = errors.New("external processing failed")
	ErrStreamCreationFailed = errors.New("failed to create gRPC stream")
	ErrStreamSendFailed     = errors.New("failed to send request over gRPC stream")
	ErrStreamReceiveFailed  = errors.New("failed to receive response from gRPC stream")
)

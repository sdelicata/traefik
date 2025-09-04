package extproc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/traefik/traefik/v3/pkg/config/dynamic"
	corev3 "github.com/traefik/traefik/v3/pkg/proto/envoy/config/core/v3"
	processingmodev3 "github.com/traefik/traefik/v3/pkg/proto/envoy/extensions/filters/http/ext_proc/v3"
	extprocv3 "github.com/traefik/traefik/v3/pkg/proto/envoy/service/ext_proc/v3"
)

// HTTPRequestToProcessingRequest converts an HTTP request to a ProcessingRequest.
func HTTPRequestToProcessingRequest(req *http.Request, config *dynamic.ExtProc) (*extprocv3.ProcessingRequest, error) {
	if req == nil {
		return nil, ErrNilRequest
	}

	// Convert HTTP headers to ext-proc headers
	var headers []*corev3.HeaderValue
	for name, values := range req.Header {
		for _, value := range values {
			headers = append(headers, &corev3.HeaderValue{
				Key:   strings.ToLower(name), // HTTP headers are case-insensitive
				Value: value,
			})
		}
	}

	// Add pseudo-headers that might be useful for processing
	// Following RFC 7540 HTTP/2 specification for pseudo-headers
	headers = append(headers,
		&corev3.HeaderValue{
			Key:   ":method",
			Value: req.Method,
		},
		&corev3.HeaderValue{
			Key:   ":path",
			Value: getPath(req),
		},
		&corev3.HeaderValue{
			Key:   ":scheme",
			Value: getScheme(req),
		},
		&corev3.HeaderValue{
			Key:   ":authority",
			Value: getAuthority(req),
		},
	)

	// Convert configuration to protocol configuration
	var requestBodyMode, responseBodyMode processingmodev3.BodySendMode
	var err error

	if config != nil && config.ProcessingMode != nil {
		requestBodyMode, err = stringToBodySendMode(config.ProcessingMode.RequestBodyMode)
		if err != nil {
			return nil, fmt.Errorf("invalid request body mode: %w", err)
		}

		responseBodyMode, err = stringToBodySendMode(config.ProcessingMode.ResponseBodyMode)
		if err != nil {
			return nil, fmt.Errorf("invalid response body mode: %w", err)
		}
	} else {
		// Default values when no config is provided
		requestBodyMode = processingmodev3.BodySendMode_NONE
		responseBodyMode = processingmodev3.BodySendMode_NONE
	}

	// Create the processing request
	procReq := &extprocv3.ProcessingRequest{
		ProtocolConfig: &extprocv3.ProtocolConfiguration{
			RequestBodyMode:                         requestBodyMode,
			ResponseBodyMode:                        responseBodyMode,
			SendBodyWithoutWaitingForHeaderResponse: false,
		},
		Request: &extprocv3.ProcessingRequest_RequestHeaders{
			RequestHeaders: &extprocv3.HttpHeaders{
				Headers: &corev3.HeaderMap{
					Headers: headers,
				},
				EndOfStream: false, // We're not streaming the body in this POC
			},
		},
	}

	return procReq, nil
}

// HTTPResponseToProcessingRequest converts an HTTP response to a ProcessingRequest.
func HTTPResponseToProcessingRequest(rw http.ResponseWriter, statusCode int) (*extprocv3.ProcessingRequest, error) {
	if rw == nil {
		return nil, ErrNilResponseWriter
	}

	// Convert HTTP response headers to ext-proc headers
	var headers []*corev3.HeaderValue
	for name, values := range rw.Header() {
		for _, value := range values {
			headers = append(headers, &corev3.HeaderValue{
				Key:   strings.ToLower(name),
				Value: value,
			})
		}
	}

	// Add status code as a pseudo-header
	headers = append(headers, &corev3.HeaderValue{
		Key:   ":status",
		Value: fmt.Sprintf("%d", statusCode),
	})

	// Create the processing request for response headers
	procReq := &extprocv3.ProcessingRequest{
		Request: &extprocv3.ProcessingRequest_ResponseHeaders{
			ResponseHeaders: &extprocv3.HttpHeaders{
				Headers: &corev3.HeaderMap{
					Headers: headers,
				},
				EndOfStream: false,
			},
		},
	}

	return procReq, nil
}

// ApplyRequestMutations applies header mutations from ProcessingResponse to the HTTP request.
func ApplyRequestMutations(req *http.Request, resp *extprocv3.ProcessingResponse) error {
	if req == nil || resp == nil {
		return nil
	}

	// Extract header mutations from different response types
	var headerMutation *extprocv3.HeaderMutation

	switch r := resp.Response.(type) {
	case *extprocv3.ProcessingResponse_RequestHeaders:
		if r.RequestHeaders != nil && r.RequestHeaders.Response != nil {
			headerMutation = r.RequestHeaders.Response.HeaderMutation
		}
	default:
		return errors.New("unexpected response type")
	}

	if headerMutation == nil {
		return nil
	}

	// Apply header mutations
	return applyHeaderMutations(req.Header, headerMutation)
}

// ApplyResponseMutations applies header mutations from ProcessingResponse to the HTTP response.
func ApplyResponseMutations(rw http.ResponseWriter, resp *extprocv3.ProcessingResponse) error {
	if rw == nil || resp == nil {
		return nil
	}

	// Extract header mutations from different response types
	var headerMutation *extprocv3.HeaderMutation

	switch r := resp.Response.(type) {
	case *extprocv3.ProcessingResponse_ResponseHeaders:
		if r.ResponseHeaders != nil && r.ResponseHeaders.Response != nil {
			headerMutation = r.ResponseHeaders.Response.HeaderMutation
		}
	default:
		return errors.New("unexpected response type")
	}

	if headerMutation == nil {
		return nil
	}

	// Apply header mutations
	return applyHeaderMutations(rw.Header(), headerMutation)
}

// applyHeaderMutations applies header mutations to an http.Header.
func applyHeaderMutations(headers http.Header, mutation *extprocv3.HeaderMutation) error {
	if headers == nil || mutation == nil {
		return nil
	}

	// Remove headers
	for _, headerName := range mutation.RemoveHeaders {
		headers.Del(headerName)
	}

	// Set headers
	for _, headerOption := range mutation.SetHeaders {
		if headerOption.Header == nil {
			continue
		}

		headerName := headerOption.Header.Key
		headerValue := headerOption.Header.Value

		if headerOption.Append != nil && headerOption.Append.Value {
			// Append to existing header
			headers.Add(headerName, headerValue)
		} else {
			// Set header (replace existing)
			headers.Set(headerName, headerValue)
		}
	}

	return nil
}

// getScheme returns the scheme for the HTTP request.
func getScheme(req *http.Request) string {
	if req.TLS != nil {
		return "https"
	}

	// Check for forwarded protocol headers
	if proto := req.Header.Get("X-Forwarded-Proto"); proto != "" {
		return strings.ToLower(proto)
	}

	if proto := req.Header.Get("X-Forwarded-Protocol"); proto != "" {
		return strings.ToLower(proto)
	}

	// Check for the standard Forwarded header
	if forwarded := req.Header.Get("Forwarded"); forwarded != "" {
		if strings.Contains(strings.ToLower(forwarded), "proto=https") {
			return "https"
		}
	}

	return "http"
}

// getPath returns the path for the HTTP request according to RFC 7540.
// The :path pseudo-header field includes the path and query parts of the target URI.
// According to RFC 7540, this MUST NOT be empty for http or https URIs.
func getPath(req *http.Request) string {
	path := req.URL.Path
	if path == "" {
		path = "/"
	}

	// Include query parameters as per RFC 7540
	if req.URL.RawQuery != "" {
		return path + "?" + req.URL.RawQuery
	}

	return path
}

// getAuthority returns the authority for the HTTP request according to RFC 7540.
// The authority includes the authority portion of the target URI, which replaces
// the Host header field in HTTP/2. According to RFC 7540, the authority MUST NOT
// include the deprecated userinfo subcomponent for http or https schemes.
func getAuthority(req *http.Request) string {
	// Per RFC 7540, clients should use :authority instead of Host header,
	// but in HTTP/1.1 to HTTP/2 conversion, we extract from req.Host first
	// as it's the canonical source in Go's http.Request
	if req.Host != "" {
		// Ensure no userinfo is included (forbidden by RFC 7540)
		if strings.Contains(req.Host, "@") {
			// Strip userinfo if present (though this should be rare in practice)
			if idx := strings.LastIndex(req.Host, "@"); idx != -1 {
				return req.Host[idx+1:]
			}
		}
		return req.Host
	}

	// Fallback to URL.Host if req.Host is empty
	if req.URL != nil && req.URL.Host != "" {
		// Strip userinfo from URL.Host if present
		if strings.Contains(req.URL.Host, "@") {
			if idx := strings.LastIndex(req.URL.Host, "@"); idx != -1 {
				return req.URL.Host[idx+1:]
			}
		}
		return req.URL.Host
	}

	// Return empty if no authority can be determined
	return ""
}

// ReadRequestBody reads and buffers the HTTP request body.
// It restores the body so the request can still be processed by downstream handlers.
func ReadRequestBody(req *http.Request) ([]byte, error) {
	if req.Body == nil {
		return []byte{}, nil
	}

	// Read the body
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}

	// Close the original body
	req.Body.Close()

	// Restore the body for downstream use
	req.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	return bodyBytes, nil
}

// HTTPRequestBodyToProcessingRequest converts request body bytes to a ProcessingRequest.
func HTTPRequestBodyToProcessingRequest(bodyBytes []byte) (*extprocv3.ProcessingRequest, error) {
	// Create the processing request for body
	procReq := &extprocv3.ProcessingRequest{
		Request: &extprocv3.ProcessingRequest_RequestBody{
			RequestBody: &extprocv3.HttpBody{
				Body:        bodyBytes,
				EndOfStream: true, // We're buffering the entire body
			},
		},
	}

	return procReq, nil
}

// stringToBodySendMode converts a string to BodySendMode enum.
// Returns an error for invalid values since config validation should happen upstream.
func stringToBodySendMode(mode string) (processingmodev3.BodySendMode, error) {
	switch mode {
	case "BUFFERED":
		return processingmodev3.BodySendMode_BUFFERED, nil
	case "STREAMED":
		return processingmodev3.BodySendMode_STREAMED, nil
	case "BUFFERED_PARTIAL":
		return processingmodev3.BodySendMode_BUFFERED_PARTIAL, nil
	case "FULL_DUPLEX_STREAMED":
		return processingmodev3.BodySendMode_FULL_DUPLEX_STREAMED, nil
	case "NONE", "":
		return processingmodev3.BodySendMode_NONE, nil
	default:
		return processingmodev3.BodySendMode_NONE, fmt.Errorf("invalid BodySendMode: %s", mode)
	}
}

// validateProcessingResponse validates that a ProcessingResponse is well-formed.
func validateProcessingResponse(resp *extprocv3.ProcessingResponse) error {
	if resp == nil {
		return ErrNilResponse
	}

	switch r := resp.Response.(type) {
	case *extprocv3.ProcessingResponse_RequestHeaders:
		if r.RequestHeaders == nil {
			return ErrInvalidResponse
		}
		return validateHeadersResponse(r.RequestHeaders)

	case *extprocv3.ProcessingResponse_ResponseHeaders:
		if r.ResponseHeaders == nil {
			return ErrInvalidResponse
		}
		return validateHeadersResponse(r.ResponseHeaders)

	case *extprocv3.ProcessingResponse_RequestBody:
		// Body processing not implemented in this POC
		return nil

	case *extprocv3.ProcessingResponse_ResponseBody:
		// Body processing not implemented in this POC
		return nil

	default:
		return ErrUnknownResponseType
	}
}

// validateHeadersResponse validates a HeadersResponse.
func validateHeadersResponse(headersResp *extprocv3.HeadersResponse) error {
	if headersResp == nil {
		return ErrInvalidResponse
	}

	if headersResp.Response == nil {
		return ErrMissingCommonResponse
	}

	// Validate header mutations if present
	if mutation := headersResp.Response.HeaderMutation; mutation != nil {
		for _, headerOption := range mutation.SetHeaders {
			if headerOption.Header == nil {
				return ErrInvalidHeaderMutation
			}
			if headerOption.Header.Key == "" {
				return ErrEmptyHeaderName
			}
		}
	}

	return nil
}

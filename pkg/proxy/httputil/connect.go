package httputil

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
)

// connectTunnelKey is the context key carrying the deferred payload of an in-flight CONNECT request.
type connectTunnelKey struct{}

// connectTunnel holds the payload of a CONNECT request until the backend has accepted the tunnel.
type connectTunnel struct {
	in      *io.PipeWriter
	payload io.Reader
}

// deferConnectPayload detaches the payload of an outgoing CONNECT request so that it can only reach the backend once
// the tunnel is established.
// For a CONNECT, net/http treats the request body as tunnel payload and writes it without any framing (neither
// Content-Length nor chunked, see transfer.go writeBody). Forwarding it before the backend accepted the tunnel
// therefore lets a client smuggle a pipelined request into the backend's HTTP/1 parser, desynchronizing the shared
// keep-alive connection.
// RFC 9110 §9.3.6 states that any response other than a successful one indicates that the tunnel has not been formed.
func deferConnectPayload(pr *httputil.ProxyRequest) {
	if pr.Out.Body == nil {
		return
	}

	pipeReader, pipeWriter := io.Pipe()
	tunnel := &connectTunnel{in: pipeWriter, payload: pr.Out.Body}

	// The Transport blocks on the empty pipe, so only the header section reaches the backend until
	// openConnectTunnel releases the payload.
	pr.Out.Body = pipeReader
	pr.Out.ContentLength = -1
	pr.Out = pr.Out.WithContext(context.WithValue(pr.Out.Context(), connectTunnelKey{}, tunnel))
}

// openConnectTunnel releases the deferred payload of a CONNECT request once the backend has accepted the tunnel.
// RFC 9110 §9.3.6 states that any 2xx response makes the sender switch to tunnel mode immediately after the response
// header section, which is the point where the payload legitimately becomes tunnel data.
func openConnectTunnel(res *http.Response) error {
	tunnel, ok := res.Request.Context().Value(connectTunnelKey{}).(*connectTunnel)
	if !ok {
		return nil
	}

	if res.StatusCode/100 != 2 {
		// The tunnel was refused, so the payload never becomes tunnel data and must not reach the backend.
		if err := tunnel.in.Close(); err != nil {
			return fmt.Errorf("closing refused CONNECT tunnel: %w", err)
		}

		return nil
	}

	go func() {
		_, err := io.Copy(tunnel.in, tunnel.payload)
		// CloseWithError propagates the failure to the Transport reading the outgoing body.
		_ = tunnel.in.CloseWithError(err)
	}()

	return nil
}

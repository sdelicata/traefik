package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

var (
	errTunnelRefused         = errors.New("the backend refused the CONNECT tunnel")
	errResponseHeaderTimeout = responseHeaderTimeoutError{}
)

// responseHeaderTimeoutError is returned when the backend did not send the CONNECT response headers in time.
// It implements net.Error so that the proxy error handler reports a Gateway Timeout.
type responseHeaderTimeoutError struct{}

func (responseHeaderTimeoutError) Error() string {
	return "timeout awaiting the CONNECT response headers"
}

func (responseHeaderTimeoutError) Timeout() bool { return true }

func (responseHeaderTimeoutError) Temporary() bool { return true }

// connectHandler rejects the CONNECT requests coming from a client that cannot tunnel.
type connectHandler struct {
	next http.Handler
}

// newConnectHandler wraps next to reject the CONNECT requests that cannot be tunneled.
func newConnectHandler(next http.Handler) http.Handler {
	return &connectHandler{next: next}
}

func (h *connectHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// Tunneling is only supported for clients speaking HTTP/2 and above.
	if req.Method == http.MethodConnect && req.ProtoMajor == 1 {
		rw.WriteHeader(http.StatusNotImplemented)

		return
	}

	h.next.ServeHTTP(rw, req)
}

// connectRoundTripper defers the payload of a CONNECT request until the backend has accepted the tunnel.
// As described in https://datatracker.ietf.org/doc/html/rfc9931#name-requirements-for-http-conne we must wait for a
// 2xx (Successful) response before forwarding any tunnel data.
//
// The wait is bounded by the response header timeout, as the transport cannot arm its own on a CONNECT request:
// it is armed once the request body has been fully written, and the body of a tunnel is the uplink,
// which never ends before the response.
type connectRoundTripper struct {
	next                  http.RoundTripper
	responseHeaderTimeout time.Duration
}

// newConnectRoundTripper wraps next with the CONNECT payload deferral behavior.
// A responseHeaderTimeout of zero, the default, leaves the deferral unbounded.
func newConnectRoundTripper(next http.RoundTripper, responseHeaderTimeout time.Duration) http.RoundTripper {
	return &connectRoundTripper{next: next, responseHeaderTimeout: responseHeaderTimeout}
}

func (c *connectRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Nothing to defer for a request that is not a CONNECT, or for a CONNECT without body or with a fixed Content-Length.
	if req.Method != http.MethodConnect || req.ContentLength >= 0 || req.Body == nil || req.Body == http.NoBody {
		return c.next.RoundTrip(req)
	}

	deferrer := newBodyDeferrer(req.Context().Done(), req.Body, c.responseHeaderTimeout)

	outReq := *req
	outReq.Body = deferrer

	res, err := c.next.RoundTrip(&outReq)
	if err != nil {
		deferrer.discard()

		return nil, err
	}

	// The tunnel was refused, so the payload never becomes tunnel data and must not reach the backend.
	if res.StatusCode/100 != 2 {
		deferrer.discard()

		return res, nil
	}

	deferrer.release()

	return res, nil
}

// bodyDeferrer holds the payload of a CONNECT request until the backend has accepted the tunnel.
type bodyDeferrer struct {
	body      io.ReadCloser
	doneCh    <-chan struct{}
	timeoutCh <-chan time.Time

	releaseCh   chan struct{}
	releaseOnce func()
	discardCh   chan struct{}
	discardOnce func()
}

func newBodyDeferrer(doneCh <-chan struct{}, body io.ReadCloser, responseHeaderTimeout time.Duration) *bodyDeferrer {
	// A nil channel blocks forever, which is the unbounded deferral.
	var timeoutCh <-chan time.Time
	if responseHeaderTimeout > 0 {
		timeoutCh = time.After(responseHeaderTimeout)
	}

	releaseCh := make(chan struct{})
	discardCh := make(chan struct{})

	return &bodyDeferrer{
		body:        body,
		doneCh:      doneCh,
		timeoutCh:   timeoutCh,
		releaseCh:   releaseCh,
		releaseOnce: sync.OnceFunc(func() { close(releaseCh) }),
		discardCh:   discardCh,
		discardOnce: sync.OnceFunc(func() { close(discardCh) }),
	}
}

func (bd *bodyDeferrer) Read(p []byte) (n int, err error) {
	// Once the tunnel is established the deferral is over, and its bound must not outlive it.
	select {
	case <-bd.releaseCh:
		return bd.body.Read(p)
	default:
	}

	select {
	case <-bd.releaseCh:
		return bd.body.Read(p)
	case <-bd.discardCh:
		return 0, errTunnelRefused
	case <-bd.doneCh:
		return 0, context.Canceled
	case <-bd.timeoutCh:
		return 0, errResponseHeaderTimeout
	}
}

// Close discards the deferred payload, if it has not been forwarded yet, and closes the underlying request body.
func (bd *bodyDeferrer) Close() error {
	bd.discardOnce()

	return bd.body.Close()
}

// release forwards the deferred payload to the backend, and copies the subsequent bytes to the tunnel.
func (bd *bodyDeferrer) release() {
	bd.releaseOnce()
}

// discard makes sure the deferred payload never reaches the backend.
// Closing the underlying body is not enough, as the reverse proxy makes the closure of its request body a no-op.
func (bd *bodyDeferrer) discard() {
	bd.discardOnce()
}

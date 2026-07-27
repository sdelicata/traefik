package service

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnect_HTTP1_Rejected(t *testing.T) {
	var backendCalled bool
	backend := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		backendCalled = true
	}))
	t.Cleanup(backend.Close)

	backendURL, err := url.Parse(backend.URL)
	require.NoError(t, err)

	addr := serveProxy(t, backendURL, 0)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodConnect, addr, nil)
	require.NoError(t, err)

	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })

	assert.Equal(t, http.StatusNotImplemented, res.StatusCode)
	assert.False(t, backendCalled)
}

func TestConnect_HTTP2_TunnelEstablished(t *testing.T) {
	backend := newConnectBackend(t, true)
	addr := serveProxy(t, backend.url, 0)

	pipeReader, pipeWriter := io.Pipe()
	t.Cleanup(func() { _ = pipeWriter.Close() })

	req, err := http.NewRequestWithContext(t.Context(), http.MethodConnect, addr, pipeReader)
	require.NoError(t, err)

	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	transport := http.Transport{Protocols: protocols}

	res, err := transport.RoundTrip(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })

	require.Equal(t, http.StatusOK, res.StatusCode)

	// Payload sent after the tunnel is established must reach the backend and echo back.
	_, err = io.WriteString(pipeWriter, "ping\n")
	require.NoError(t, err)

	echo, err := bufio.NewReader(res.Body).ReadString('\n')
	require.NoError(t, err)

	assert.Equal(t, "PING", strings.TrimSpace(echo))
}

func TestConnect_HTTP2_RefusedTunnelWithContentLength(t *testing.T) {
	backend := newConnectBackend(t, false)
	addr := serveProxy(t, backend.url, 0)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodConnect, addr, strings.NewReader("foo"))
	require.NoError(t, err)

	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	transport := http.Transport{Protocols: protocols}

	res, err := transport.RoundTrip(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })

	assert.Equal(t, http.StatusMethodNotAllowed, res.StatusCode)
	assert.Equal(t, "foo", *backend.payload.Load())
}

func TestConnect_HTTP2_RefusedTunnelDropsPayloadWithoutContentLength(t *testing.T) {
	backend := newConnectBackend(t, false)
	addr := serveProxy(t, backend.url, 0)

	// Wrapping the reader hides its length, so the request is sent without a Content-Length header.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodConnect, addr, io.NopCloser(strings.NewReader("foo")))
	require.NoError(t, err)

	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	transport := http.Transport{Protocols: protocols}

	res, err := transport.RoundTrip(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })

	assert.Equal(t, http.StatusMethodNotAllowed, res.StatusCode)
	assert.Empty(t, *backend.payload.Load())
}

func TestConnect_HTTP2_ResponseHeaderTimeout(t *testing.T) {
	const responseHeaderTimeout = 200 * time.Millisecond

	testCases := []struct {
		desc string
		body func(t *testing.T) io.Reader
	}{
		{
			desc: "client sends no payload",
			body: func(t *testing.T) io.Reader {
				t.Helper()

				pipeReader, pipeWriter := io.Pipe()
				t.Cleanup(func() { _ = pipeWriter.Close() })

				return pipeReader
			},
		},
		{
			desc: "client sends a payload then stalls",
			body: func(t *testing.T) io.Reader {
				t.Helper()

				pipeReader, pipeWriter := io.Pipe()
				t.Cleanup(func() { _ = pipeWriter.Close() })

				// The payload is deferred, so it is never read: the write cannot happen on the test goroutine.
				go func() { _, _ = io.WriteString(pipeWriter, "foo") }()

				return pipeReader
			},
		},
		{
			desc: "client sends a payload with a fixed length",
			body: func(_ *testing.T) io.Reader {
				return strings.NewReader("foo")
			},
		},
	}

	for _, test := range testCases {
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			backendURL := newStalledConnectBackend(t)
			addr := serveProxy(t, backendURL, responseHeaderTimeout)

			req, err := http.NewRequestWithContext(t.Context(), http.MethodConnect, addr, test.body(t))
			require.NoError(t, err)

			protocols := new(http.Protocols)
			protocols.SetUnencryptedHTTP2(true)
			transport := http.Transport{Protocols: protocols}

			type roundTrip struct {
				res *http.Response
				err error
			}
			roundTripCh := make(chan roundTrip, 1)

			go func() {
				res, err := transport.RoundTrip(req)
				roundTripCh <- roundTrip{res: res, err: err}
			}()

			select {
			case got := <-roundTripCh:
				require.NoError(t, got.err)
				t.Cleanup(func() { _ = got.res.Body.Close() })

				assert.Equal(t, http.StatusGatewayTimeout, got.res.StatusCode)

			case <-time.After(20 * responseHeaderTimeout):
				t.Fatal("The CONNECT request outlived the response header timeout")
			}
		})
	}
}

func TestConnect_HTTP2_EstablishedTunnelOutlivesResponseHeaderTimeout(t *testing.T) {
	const responseHeaderTimeout = 200 * time.Millisecond

	backend := newConnectBackend(t, true)
	addr := serveProxy(t, backend.url, responseHeaderTimeout)

	pipeReader, pipeWriter := io.Pipe()
	t.Cleanup(func() { _ = pipeWriter.Close() })

	req, err := http.NewRequestWithContext(t.Context(), http.MethodConnect, addr, pipeReader)
	require.NoError(t, err)

	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	transport := http.Transport{Protocols: protocols}

	res, err := transport.RoundTrip(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })

	require.Equal(t, http.StatusOK, res.StatusCode)

	// The bound only covers the response headers: the tunnel must keep carrying data long past the deadline.
	tunnel := bufio.NewReader(res.Body)
	for i := range 3 {
		time.Sleep(150 * time.Millisecond)

		_, err = fmt.Fprintf(pipeWriter, "ping%d\n", i)
		require.NoError(t, err)

		echo, err := tunnel.ReadString('\n')
		require.NoError(t, err)

		assert.Equal(t, fmt.Sprintf("PING%d", i), strings.TrimSpace(echo))
	}
}

func TestConnect_HTTP2_NoResponseHeaderTimeoutLeavesTheDeferralUnbounded(t *testing.T) {
	backendURL := newStalledConnectBackend(t)
	addr := serveProxy(t, backendURL, 0)

	pipeReader, pipeWriter := io.Pipe()
	t.Cleanup(func() { _ = pipeWriter.Close() })

	req, err := http.NewRequestWithContext(t.Context(), http.MethodConnect, addr, pipeReader)
	require.NoError(t, err)

	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	transport := http.Transport{Protocols: protocols}

	roundTripCh := make(chan struct{})
	go func() {
		defer close(roundTripCh)

		res, err := transport.RoundTrip(req)
		if err == nil {
			_ = res.Body.Close()
		}
	}()

	select {
	case <-roundTripCh:
		t.Fatal("The CONNECT request must stay pending when no response header timeout is configured")

	case <-time.After(500 * time.Millisecond):
	}
}

// connectBackend is an HTTP/1 backend that either accepts CONNECT tunnels and echoes them back,
// or refuses them. It records every payload byte received after the CONNECT header section.
type connectBackend struct {
	url     *url.URL
	payload *atomic.Pointer[string]
}

func newConnectBackend(t *testing.T, accept bool) *connectBackend {
	t.Helper()

	backend := &connectBackend{payload: &atomic.Pointer[string]{}}
	empty := ""
	backend.payload.Store(&empty)

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		// Test that the proxy sets the Connection: close header on CONNECT requests to avoid reusing the connection.
		assert.True(t, req.Close)

		if !accept {
			if req.ContentLength > 0 {
				// A declared body means the proxy forwarded payload; read it to capture what leaked.
				body, _ := io.ReadAll(req.Body)
				got := string(body)
				backend.payload.Store(&got)

				rw.WriteHeader(http.StatusMethodNotAllowed)

				return
			}

			// No declared body: hijack to make sure the proxy did not push anything on the raw connection.
			conn, brw, err := rw.(http.Hijacker).Hijack()
			require.NoError(t, err)
			defer conn.Close()

			var payload strings.Builder
			buf := make([]byte, 1)
			for {
				_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
				n, err := brw.Read(buf)
				if n > 0 {
					payload.Write(buf[:n])
				}
				if err != nil {
					break
				}
			}
			got := payload.String()
			backend.payload.Store(&got)

			_, _ = io.WriteString(conn, "HTTP/1.1 405 Method Not Allowed\r\nContent-Length: 0\r\n\r\n")

			return
		}

		// The tunnel is a raw byte stream, so hijack the connection to bypass the HTTP response machinery.
		// The returned reader already holds any payload buffered alongside the CONNECT header section.
		conn, brw, err := rw.(http.Hijacker).Hijack()
		require.NoError(t, err)
		defer conn.Close()

		_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")

		// Blind relay: echo every line back uppercased.
		var payload strings.Builder
		for {
			line, err := brw.ReadString('\n')
			if len(line) > 0 {
				payload.WriteString(line)
				got := payload.String()
				backend.payload.Store(&got)
				_, _ = io.WriteString(conn, strings.ToUpper(line))
			}
			if err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	backendURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	backend.url = backendURL

	return backend
}

// newStalledConnectBackend is a backend accepting the connection but never sending a response status,
// as an overloaded backend or a plain TCP service sitting behind an HTTP router would do.
func newStalledConnectBackend(t *testing.T) *url.URL {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		conn, _, err := rw.(http.Hijacker).Hijack()
		if !assert.NoError(t, err) {
			return
		}
		defer conn.Close()

		// The test context is canceled before the cleanup functions run,
		// so the hijacked connection does not hold the server shutdown.
		<-t.Context().Done()
	}))
	t.Cleanup(server.Close)

	backendURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	return backendURL
}

// serveProxy exposes the Traefik proxy handler over a httptest server,
// with h2c enabled so that both HTTP/1 and prior-knowledge HTTP/2 clients can reach it.
func serveProxy(t *testing.T, target *url.URL, responseHeaderTimeout time.Duration) string {
	t.Helper()

	// Mirror createRoundTripper, which sets the timeout on both the transport and the CONNECT round tripper:
	// the transport arms it for the requests whose body ends, the CONNECT round tripper for the tunnels.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = responseHeaderTimeout

	proxy, err := buildProxy(new(true), nil, newConnectRoundTripper(transport, responseHeaderTimeout), newBufferPool())
	require.NoError(t, err)

	// The load balancer sets the backend server on the request URL before the proxy runs.
	handler := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		proxy.ServeHTTP(rw, req)
	})

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	srv := httptest.NewUnstartedServer(handler)
	srv.Config.Protocols = protocols
	srv.Start()
	t.Cleanup(srv.Close)

	return srv.URL
}

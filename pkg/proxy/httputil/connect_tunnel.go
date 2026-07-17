package httputil

import (
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// connectHandler establishes HTTP/1 CONNECT tunnels to the backend, and delegates every other request to next.
type connectHandler struct {
	next           http.Handler
	target         *url.URL
	passHostHeader bool
	roundTripper   http.RoundTripper
}

func (h *connectHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// An HTTP/2 CONNECT carries its payload in a live request body, so the ReverseProxy already relays both directions
	// concurrently once deferConnectPayload and openConnectTunnel gated it on the backend response. Hijacking is not an
	// option there anyway, as net/http does not support it for HTTP/2 connections.
	if req.Method != http.MethodConnect || req.ProtoMajor != 1 {
		h.next.ServeHTTP(rw, req)

		return
	}

	h.serveTunnel(rw, req)
}

// serveTunnel establishes an HTTP/1 CONNECT tunnel between the client and the backend.
// An inbound HTTP/1 CONNECT has no request body (RFC 9112 §6.3: a request with neither Content-Length nor chunked
// encoding has no content), so the payload can only be read from the hijacked client connection, and only once the
// backend accepted the tunnel.
func (h *connectHandler) serveTunnel(rw http.ResponseWriter, req *http.Request) {
	logger := log.Ctx(req.Context())

	pipeReader, pipeWriter := io.Pipe()

	outReq := req.Clone(req.Context())
	outReq.Body = pipeReader
	outReq.ContentLength = -1
	outReq.RequestURI = ""
	outReq.URL = &url.URL{Scheme: h.target.Scheme, Host: h.target.Host}
	outReq.Proto = "HTTP/1.1"
	outReq.ProtoMajor = 1
	outReq.ProtoMinor = 1

	// RFC 9110 §9.3.6: CONNECT uses an authority-form request target, which net/http takes from Request.Host.
	if !h.passHostHeader {
		outReq.Host = h.target.Host
	}

	res, err := h.roundTripper.RoundTrip(outReq)
	if err != nil {
		closePipe(pipeWriter, logger)
		ErrorHandler(rw, req, err)

		return
	}
	defer res.Body.Close()

	if res.StatusCode/100 != 2 {
		// The tunnel was refused, so the connection stays a regular HTTP one and the response is relayed as-is.
		closePipe(pipeWriter, logger)

		maps.Copy(rw.Header(), res.Header)
		rw.WriteHeader(res.StatusCode)

		if _, err := io.Copy(rw, res.Body); err != nil {
			logger.Debug().Err(err).Msg("Error while relaying refused CONNECT response")
		}

		return
	}

	conn, brw, err := http.NewResponseController(rw).Hijack()
	if err != nil {
		closePipe(pipeWriter, logger)
		ErrorHandler(rw, req, fmt.Errorf("hijacking client connection: %w", err))

		return
	}
	defer conn.Close()

	// RFC 9110 §9.3.6: "A CONNECT request message does not have content." Bytes buffered before the tunnel existed are
	// therefore a pipelined request rather than tunnel payload, and must not be relayed to the backend.
	if brw.Reader.Buffered() > 0 {
		closePipe(pipeWriter, logger)
		logger.Debug().Msg("Dropping CONNECT request pipelining data ahead of the tunnel")

		return
	}

	// RFC 9110 §9.3.6: "A server MUST NOT send any Transfer-Encoding or Content-Length header fields in a 2xx
	// (Successful) response to CONNECT", and the connection becomes a tunnel right after the header section.
	if _, err := brw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		closePipe(pipeWriter, logger)
		logger.Debug().Err(err).Msg("Error while writing CONNECT tunnel response")

		return
	}

	if err := brw.Flush(); err != nil {
		closePipe(pipeWriter, logger)
		logger.Debug().Err(err).Msg("Error while flushing CONNECT tunnel response")

		return
	}

	errC := make(chan error, 2)

	go func() {
		_, err := io.Copy(pipeWriter, conn)
		errC <- pipeWriter.CloseWithError(err)
	}()

	go func() {
		_, err := io.Copy(conn, res.Body)
		errC <- err
	}()

	if err := <-errC; err != nil {
		logger.Debug().Err(err).Msg("Error while relaying CONNECT tunnel")
	}
}

func closePipe(pipeWriter *io.PipeWriter, logger *zerolog.Logger) {
	if err := pipeWriter.Close(); err != nil {
		logger.Debug().Err(err).Msg("Error while closing CONNECT tunnel payload")
	}
}

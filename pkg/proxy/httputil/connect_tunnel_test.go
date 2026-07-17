package httputil

import (
	"bufio"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnect_HTTP1_TunnelEstablished(t *testing.T) {
	backend := newConnectBackend(t, true)
	addr := serveProxy(t, backend.url)

	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	_, err = io.WriteString(conn, "CONNECT tunnel.example.com:443 HTTP/1.1\r\nHost: tunnel.example.com:443\r\n\r\n")
	require.NoError(t, err)

	br := bufio.NewReader(conn)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))

	statusLine, err := br.ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, "HTTP/1.1 200 Connection Established", strings.TrimSpace(statusLine))

	blank, err := br.ReadString('\n')
	require.NoError(t, err)
	require.Empty(t, strings.TrimSpace(blank))

	// The tunnel must relay bytes in both directions once established.
	_, err = io.WriteString(conn, "ping\n")
	require.NoError(t, err)

	echo, err := br.ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, "PING", strings.TrimSpace(echo))
}

func TestConnect_HTTP1_TunnelRefusedRelaysResponse(t *testing.T) {
	backend := newConnectBackend(t, false)
	addr := serveProxy(t, backend.url)

	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	_, err = io.WriteString(conn, "CONNECT tunnel.example.com:443 HTTP/1.1\r\nHost: tunnel.example.com:443\r\n\r\n")
	require.NoError(t, err)

	br := bufio.NewReader(conn)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))

	statusLine, err := br.ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, "HTTP/1.1 405 Method Not Allowed", strings.TrimSpace(statusLine))

	time.Sleep(500 * time.Millisecond)
	assert.Empty(t, backend.received())
}

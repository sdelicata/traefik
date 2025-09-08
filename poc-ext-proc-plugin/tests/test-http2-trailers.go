package main

import (
	"fmt"
	"net/http"
	"time"
)

func main() {
	// Test server that tries to send trailers without pre-declaration
	http.HandleFunc("/no-declaration", func(w http.ResponseWriter, r *http.Request) {
		// Write response without declaring trailers
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Response body\n"))

		// Try to set trailers after body (won't work without declaration)
		w.Header().Set("X-Test-Trailer", "value-without-declaration")

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	})

	// Test server that properly declares trailers
	http.HandleFunc("/with-declaration", func(w http.ResponseWriter, r *http.Request) {
		// Declare trailers BEFORE writing
		w.Header().Set("Trailer", "X-Test-Trailer")

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Response body\n"))

		// Set trailers after body (will work with declaration)
		w.Header().Set("X-Test-Trailer", "value-with-declaration")

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	})

	// Start server with HTTP/2 support
	server := &http.Server{
		Addr:         ":8082",
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	fmt.Println("Starting HTTP/2 test server on :8082")
	fmt.Println("Test with:")
	fmt.Println("  curl -k --http2 https://localhost:8082/no-declaration -v")
	fmt.Println("  curl -k --http2 https://localhost:8082/with-declaration -v")

	// Use existing certificates
	if err := server.ListenAndServeTLS(
		"/Users/simon/Workspace/traefik/poc-ext-proc-plugin/config/localhost.cert",
		"/Users/simon/Workspace/traefik/poc-ext-proc-plugin/config/localhost.key",
	); err != nil {
		panic(err)
	}
}

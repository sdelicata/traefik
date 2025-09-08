package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Handle different test paths
		switch r.URL.Path {
		case "/response-trailers":
			handleResponseTrailers(w, r)
		case "/trailers":
			handleRequestTrailers(w, r)
		default:
			handleDefault(w, r)
		}
	})

	fmt.Println("Test server listening on :8081")
	http.ListenAndServe(":8081", nil)
}

func handleResponseTrailers(w http.ResponseWriter, r *http.Request) {
	// Set trailers we will send
	w.Header().Set("Trailer", "X-Test-Trailer")

	// Write response
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Response with trailers\n"))

	// HTTP/2 automatically sends trailers; for HTTP/1.1 we need chunked encoding
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	// Set the actual trailer value
	w.Header().Set("X-Test-Trailer", "test-value")
}

func handleRequestTrailers(w http.ResponseWriter, r *http.Request) {
	// Read body to populate trailers
	body, _ := io.ReadAll(r.Body)

	// Check if we received request trailers
	var response string
	if r.Trailer != nil {
		response = fmt.Sprintf("Received request with trailers: %v\nBody: %s\n", r.Trailer, body)
	} else {
		response = fmt.Sprintf("No trailers received\nBody: %s\n", body)
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response))
}

func handleDefault(w http.ResponseWriter, r *http.Request) {
	// Check for special body content
	body, _ := io.ReadAll(r.Body)
	bodyStr := string(body)

	if strings.Contains(bodyStr, "modify-trailers") {
		// Set trailers for mutation test
		w.Header().Set("Trailer", "X-Original-Trailer")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Response for trailer mutation test\n"))

		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		w.Header().Set("X-Original-Trailer", "original-value")
	} else {
		// Normal response
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Normal response\n"))
	}
}

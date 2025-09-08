package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"golang.org/x/net/http2"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: trailer-client-http2 <test-type>")
		fmt.Println("test-type: request-trailers, response-trailers, trailers-mutation")
		os.Exit(1)
	}

	testType := os.Args[1]

	// Create HTTP/2 client with TLS
	client := &http.Client{
		Transport: &http2.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // Accept self-signed certificates
			},
		},
	}

	switch testType {
	case "request-trailers":
		testRequestTrailers(client)
	case "response-trailers":
		testResponseTrailers(client)
	case "trailers-mutation":
		testTrailersMutation(client)
	default:
		fmt.Printf("Unknown test type: %s\n", testType)
		os.Exit(1)
	}
}

func testRequestTrailers(client *http.Client) {
	url := "https://localhost:443/trailers"

	// Create request with body
	body := strings.NewReader("test body")
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		os.Exit(1)
	}

	// Add headers - Host header might be overridden, so also use URL
	req.Host = "whoami.localhost"
	req.Header.Set("X-Enable-Trailers", "true")

	// Set trailers
	req.Trailer = http.Header{
		"X-Request-Trailer": []string{"test-value"},
	}

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error sending request: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	// Read response body
	bodyBytes, _ := io.ReadAll(resp.Body)

	// Check for expected header
	if val := resp.Header.Get("X-Processed-Trailer"); val != "" {
		fmt.Println("SUCCESS: Found expected header 'X-Processed-Trailer'")
	} else {
		fmt.Println("FAILED: Expected header 'X-Processed-Trailer' not found")
		fmt.Printf("Response:\n%s %s\n", resp.Proto, resp.Status)
		for k, v := range resp.Header {
			fmt.Printf("%s: %s\n", k, strings.Join(v, ", "))
		}
		fmt.Printf("\n%s", string(bodyBytes))
		os.Exit(1)
	}
}

func testResponseTrailers(client *http.Client) {
	url := "https://localhost:443/response-trailers"

	// Create request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		os.Exit(1)
	}

	// Add headers - Host header might be overridden, so also use URL
	req.Host = "whoami.localhost"
	req.Header.Set("X-Request-Trailer", "value")

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error sending request: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	// Read response body to get trailers
	bodyBytes, _ := io.ReadAll(resp.Body)

	// Check for expected trailer
	found := false
	if resp.Trailer != nil {
		if val := resp.Trailer.Get("x-response-trailer"); val != "" {
			found = true
		}
	}

	if found {
		fmt.Println("SUCCESS: Found expected trailer 'x-response-trailer'")
	} else {
		fmt.Println("FAILED: Expected trailer 'x-response-trailer' not found")
		fmt.Printf("Response:\n%s %s\n", resp.Proto, resp.Status)
		fmt.Printf("Headers:\n")
		for k, v := range resp.Header {
			fmt.Printf("%s: %s\n", k, strings.Join(v, ", "))
		}
		fmt.Printf("Trailers:\n")
		if resp.Trailer != nil {
			for k, v := range resp.Trailer {
				fmt.Printf("%s: %s\n", k, strings.Join(v, ", "))
			}
		} else {
			fmt.Println("No trailers")
		}
		fmt.Printf("\n%s", string(bodyBytes))
		os.Exit(1)
	}
}

func testTrailersMutation(client *http.Client) {
	url := "https://localhost:443/"

	// Create request with body
	body := strings.NewReader("modify-trailers")
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		os.Exit(1)
	}

	// Add headers - Host header might be overridden, so also use URL
	req.Host = "whoami.localhost"

	// Set trailers
	req.Trailer = http.Header{
		"X-Custom-Trailer": []string{"modify-me"},
	}

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error sending request: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	// Read response body to get trailers
	bodyBytes, _ := io.ReadAll(resp.Body)

	// Check for expected trailer
	found := false
	if resp.Trailer != nil {
		if val := resp.Trailer.Get("x-modified-trailer"); val != "" {
			found = true
		}
	}

	// Also check in headers (might be there)
	if !found {
		if val := resp.Header.Get("x-modified-trailer"); val != "" {
			found = true
		}
	}

	if found {
		fmt.Println("SUCCESS: Found expected trailer 'x-modified-trailer'")
	} else {
		fmt.Println("FAILED: Expected trailer 'x-modified-trailer' not found")
		fmt.Printf("Response:\n%s %s\n", resp.Proto, resp.Status)
		fmt.Printf("Headers:\n")
		for k, v := range resp.Header {
			fmt.Printf("%s: %s\n", k, strings.Join(v, ", "))
		}
		fmt.Printf("Trailers:\n")
		if resp.Trailer != nil {
			for k, v := range resp.Trailer {
				fmt.Printf("%s: %s\n", k, strings.Join(v, ", "))
			}
		} else {
			fmt.Println("No trailers")
		}
		fmt.Printf("\n%s", string(bodyBytes))
		os.Exit(1)
	}
}

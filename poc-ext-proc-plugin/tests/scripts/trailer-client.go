package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: trailer-client <test-type>")
		fmt.Println("test-type: request-trailers, response-trailers, trailers-mutation")
		os.Exit(1)
	}

	testType := os.Args[1]
	host := "whoami.localhost"
	port := "80"

	// Connect to the server
	conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%s", port))
	if err != nil {
		fmt.Printf("Error connecting: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	switch testType {
	case "request-trailers":
		sendRequestWithTrailers(conn, host)
	case "response-trailers":
		sendRequestForResponseTrailers(conn, host)
	case "trailers-mutation":
		sendRequestForTrailersMutation(conn, host)
	default:
		fmt.Printf("Unknown test type: %s\n", testType)
		os.Exit(1)
	}
}

func sendRequestWithTrailers(conn net.Conn, host string) {
	// Send HTTP request with chunked encoding and trailers
	request := fmt.Sprintf("POST /trailers HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Transfer-Encoding: chunked\r\n"+
		"Trailer: X-Request-Trailer\r\n"+
		"X-Enable-Trailers: true\r\n"+
		"\r\n", host)

	// Send the request headers
	_, err := conn.Write([]byte(request))
	if err != nil {
		fmt.Printf("Error sending request: %v\n", err)
		return
	}

	// Send chunked body
	body := "test body"
	chunk := fmt.Sprintf("%x\r\n%s\r\n", len(body), body)
	_, err = conn.Write([]byte(chunk))
	if err != nil {
		fmt.Printf("Error sending chunk: %v\n", err)
		return
	}

	// Send end chunk
	_, err = conn.Write([]byte("0\r\n"))
	if err != nil {
		fmt.Printf("Error sending end chunk: %v\n", err)
		return
	}

	// Send trailers
	trailers := "X-Request-Trailer: trailer-value\r\n\r\n"
	_, err = conn.Write([]byte(trailers))
	if err != nil {
		fmt.Printf("Error sending trailers: %v\n", err)
		return
	}

	// Read response
	readResponse(conn, "X-Processed-Trailer")
}

func sendRequestForResponseTrailers(conn net.Conn, host string) {
	// Send simple request that should trigger response trailers
	request := fmt.Sprintf("GET /response-trailers HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"X-Request-Trailer: value\r\n"+
		"\r\n", host)

	_, err := conn.Write([]byte(request))
	if err != nil {
		fmt.Printf("Error sending request: %v\n", err)
		return
	}

	// Read response
	readResponse(conn, "x-response-trailer")
}

func sendRequestForTrailersMutation(conn net.Conn, host string) {
	// Send request with chunked body to test trailer mutation
	request := fmt.Sprintf("POST / HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Transfer-Encoding: chunked\r\n"+
		"Trailer: X-Custom-Trailer\r\n"+
		"\r\n", host)

	// Send the request headers
	_, err := conn.Write([]byte(request))
	if err != nil {
		fmt.Printf("Error sending request: %v\n", err)
		return
	}

	// Send chunked body
	body := "modify-trailers"
	chunk := fmt.Sprintf("%x\r\n%s\r\n", len(body), body)
	_, err = conn.Write([]byte(chunk))
	if err != nil {
		fmt.Printf("Error sending chunk: %v\n", err)
		return
	}

	// Send end chunk
	_, err = conn.Write([]byte("0\r\n"))
	if err != nil {
		fmt.Printf("Error sending end chunk: %v\n", err)
		return
	}

	// Send trailers
	trailers := "X-Custom-Trailer: modify-me\r\n\r\n"
	_, err = conn.Write([]byte(trailers))
	if err != nil {
		fmt.Printf("Error sending trailers: %v\n", err)
		return
	}

	// Read response
	readResponse(conn, "x-modified-trailer")
}

func readResponse(conn net.Conn, expectedHeader string) {
	// Read the full response
	buffer := make([]byte, 4096)
	n, err := conn.Read(buffer)
	if err != nil && err != io.EOF {
		fmt.Printf("Error reading response: %v\n", err)
		return
	}

	response := string(buffer[:n])

	// Check if the expected header is present
	if strings.Contains(strings.ToLower(response), strings.ToLower(expectedHeader)) {
		fmt.Printf("SUCCESS: Found expected header '%s'\n", expectedHeader)
		os.Exit(0)
	} else {
		fmt.Printf("FAILED: Expected header '%s' not found\n", expectedHeader)
		fmt.Printf("Response:\n%s\n", response)
		os.Exit(1)
	}
}

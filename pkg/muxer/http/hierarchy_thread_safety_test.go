package http

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestConcurrentRouteEvaluation tests basic thread safety of route evaluation
func TestConcurrentRouteEvaluation(t *testing.T) {
	parser, err := NewSyntaxParser()
	require.NoError(t, err)

	mux := NewMuxer(parser)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Add test routes
	routes := []string{
		"PathPrefix(`/api`)",
		"PathPrefix(`/api/v1`)",
		"Path(`/api/v1/users`)",
		"PathPrefix(`/admin`)",
		"Path(`/admin/reports`)",
	}

	for i, rule := range routes {
		err := mux.AddRoute(rule, "v3", i*100, handler)
		require.NoError(t, err)
	}

	// Test concurrent access
	const numGoroutines = 10
	const requestsPerGoroutine = 100
	var wg sync.WaitGroup

	testPaths := []string{
		"/api/v1/users",
		"/admin/reports",
		"/api/test",
		"/nomatch",
	}

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < requestsPerGoroutine; j++ {
				path := testPaths[j%len(testPaths)]
				req := httptest.NewRequest("GET", "http://example.com"+path, nil)
				rec := httptest.NewRecorder()

				// This should not panic or cause data races
				mux.ServeHTTP(rec, req)
			}
		}(i)
	}

	wg.Wait()
}

// TestConcurrentRouteAddition tests thread safety during route addition
func TestConcurrentRouteAddition(t *testing.T) {
	parser, err := NewSyntaxParser()
	require.NoError(t, err)

	mux := NewMuxer(parser)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	const numGoroutines = 5
	var wg sync.WaitGroup

	// Add routes concurrently (this may or may not be supported, but shouldn't crash)
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			// Add routes with unique names to avoid conflicts
			for j := 0; j < 10; j++ {
				rule := "PathPrefix(`/test-" + string(rune('a'+goroutineID)) + "-" + string(rune('0'+j)) + "`)"
				// Note: This might fail if concurrent addition isn't supported, but shouldn't panic
				_ = mux.AddRoute(rule, "v3", goroutineID*100+j, handler)
			}
		}(i)
	}

	wg.Wait()
}

// TestDataRaceDetection runs with race detector to catch data races
func TestDataRaceDetection(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("Race detection requires GOMAXPROCS >= 2")
	}

	parser, err := NewSyntaxParser()
	require.NoError(t, err)

	mux := NewMuxer(parser)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Add initial routes
	err = mux.AddRoute("PathPrefix(`/api`)", "v3", 100, handler)
	require.NoError(t, err)
	err = mux.AddRoute("Path(`/api/users`)", "v3", 200, handler)
	require.NoError(t, err)

	// Stress test with concurrent access
	const duration = 100 // milliseconds
	done := make(chan bool)

	// Concurrent readers
	for i := 0; i < 3; i++ {
		go func() {
			req := httptest.NewRequest("GET", "http://example.com/api/users", nil)
			rec := httptest.NewRecorder()

			for {
				select {
				case <-done:
					return
				default:
					mux.ServeHTTP(rec, req)
				}
			}
		}()
	}

	// Let it run for a short duration
	go func() {
		runtime.Gosched() // Yield to other goroutines
		close(done)
	}()

	// Wait for completion (race detector will catch any issues)
	<-done
}

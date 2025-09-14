package http

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/traefik/traefik/v3/pkg/config/dynamic"
)

// TestConcurrentRouteEvaluation tests thread-safety of concurrent route evaluation (FR-022)
func TestConcurrentRouteEvaluation(t *testing.T) {
	// Setup hierarchical evaluation engine with multiple routers
	parser, err := NewSyntaxParser()
	require.NoError(t, err)
	engine := NewHierarchicalEvaluationEngine(parser)

	// Create test routers with parent-child relationships
	routerConfigs := map[string]*dynamic.Router{
		"api-root": {
			Rule:        "PathPrefix(`/api`)",
			Priority:    1000,
			EntryPoints: []string{"web"}, // Root router needs entry points
		},
		"api-v1": {
			Rule:       "PathPrefix(`/api/v1`)",
			Priority:   900,
			ParentRefs: []string{"api-root"},
			Service:    "api-v1-service",
		},
		"api-users": {
			Rule:       "PathPrefix(`/api/v1/users`)",
			Priority:   800,
			ParentRefs: []string{"api-v1"},
			Service:    "users-service",
		},
		"web-root": {
			Rule:        "PathPrefix(`/web`)",
			Priority:    500,
			EntryPoints: []string{"web"}, // Root router needs entry points
		},
		"web-admin": {
			Rule:       "PathPrefix(`/web/admin`)",
			Priority:   400,
			ParentRefs: []string{"web-root"},
			Service:    "admin-service",
		},
	}

	handlers := map[string]http.Handler{
		"api-root":  http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }),
		"api-v1":    http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }),
		"api-users": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }),
		"web-root":  http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }),
		"web-admin": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }),
	}

	err = engine.BuildHierarchy(routerConfigs, handlers)
	require.NoError(t, err)

	// Test concurrent evaluation with multiple goroutines
	numGoroutines := runtime.NumCPU() * 4 // Use multiple CPU cores
	numRequestsPerGoroutine := 100
	totalRequests := int64(numGoroutines * numRequestsPerGoroutine)

	testRequests := []*http.Request{
		httptest.NewRequest("GET", "/api/v1/users/123", nil),
		httptest.NewRequest("GET", "/api/v1/products", nil),
		httptest.NewRequest("GET", "/web/admin/dashboard", nil),
		httptest.NewRequest("GET", "/api/v1/orders", nil),
		httptest.NewRequest("GET", "/web/public", nil),
	}

	var (
		successCount int64
		errorCount   int64
		wg           sync.WaitGroup
		startTime    = time.Now()
	)

	// Launch concurrent goroutines for route evaluation
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < numRequestsPerGoroutine; j++ {
				req := testRequests[j%len(testRequests)]

				// Evaluate request concurrently
				matchedRouter, found := engine.EvaluateRequest(req)

				if found && matchedRouter != nil {
					atomic.AddInt64(&successCount, 1)
				} else {
					atomic.AddInt64(&errorCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(startTime)

	// Validate results
	processedRequests := atomic.LoadInt64(&successCount) + atomic.LoadInt64(&errorCount)
	assert.Equal(t, totalRequests, processedRequests, "All requests should be processed")

	// Validate performance metrics are consistent
	metrics := engine.GetPerformanceMetrics()
	evaluationCount := metrics["evaluation_counter"].(int64)
	requestCount := metrics["request_count"].(int64)

	assert.True(t, evaluationCount > 0, "Evaluation counter should be incremented")
	assert.Equal(t, totalRequests, requestCount, "Request count should match total requests")

	// Ensure sub-millisecond average performance under load
	avgTimeMs := metrics["avg_request_time_ms"].(float64)
	assert.True(t, avgTimeMs < 1.0, "Average request time should be sub-millisecond under concurrent load")

	t.Logf("Concurrent test completed: %d requests across %d goroutines in %v",
		totalRequests, numGoroutines, duration)
	t.Logf("Success: %d, Errors: %d, Avg time: %.3fms",
		successCount, errorCount, avgTimeMs)
}

// TestConcurrentHierarchyBuildingAndEvaluation tests building hierarchy while evaluating routes
func TestConcurrentHierarchyBuildingAndEvaluation(t *testing.T) {
	parser, err := NewSyntaxParser()
	require.NoError(t, err)
	engine := NewHierarchicalEvaluationEngine(parser)

	// Initial setup
	initialRouters := map[string]*dynamic.Router{
		"root": {
			Rule:        "PathPrefix(`/`)",
			Priority:    100,
			Service:     "root-service",
			EntryPoints: []string{"web"},
		},
	}

	initialHandlers := map[string]http.Handler{
		"root": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }),
	}

	err = engine.BuildHierarchy(initialRouters, initialHandlers)
	require.NoError(t, err)

	var (
		buildCount     int64
		evaluateCount  int64
		wg             sync.WaitGroup
		stopEvaluation int32
	)

	// Start evaluation goroutines
	numEvaluators := runtime.NumCPU()
	for i := 0; i < numEvaluators; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/test/path", nil)

			for atomic.LoadInt32(&stopEvaluation) == 0 {
				_, _ = engine.EvaluateRequest(req)
				atomic.AddInt64(&evaluateCount, 1)
				time.Sleep(time.Microsecond) // Small delay to allow interleaving
			}
		}()
	}

	// Concurrent hierarchy building
	wg.Add(1)
	go func() {
		defer wg.Done()

		for i := 0; i < 10; i++ {
			routerName := fmt.Sprintf("dynamic-router-%d", i)
			newRouters := map[string]*dynamic.Router{
				"root": initialRouters["root"], // Keep existing
				routerName: {
					Rule:     fmt.Sprintf("PathPrefix(`/dynamic/%d`)", i),
					Priority: 50 - i,
					Service:  fmt.Sprintf("dynamic-service-%d", i),
				},
			}

			newHandlers := map[string]http.Handler{
				"root":     initialHandlers["root"],
				routerName: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }),
			}

			err := engine.BuildHierarchy(newRouters, newHandlers)
			if err != nil {
				t.Errorf("Failed to build hierarchy: %v", err)
				return
			}

			atomic.AddInt64(&buildCount, 1)
			time.Sleep(time.Millisecond) // Allow some evaluation time between builds
		}

		atomic.StoreInt32(&stopEvaluation, 1) // Signal evaluation goroutines to stop
	}()

	wg.Wait()

	// Validate no data races or panics occurred
	finalEvaluations := atomic.LoadInt64(&evaluateCount)
	finalBuilds := atomic.LoadInt64(&buildCount)

	assert.True(t, finalEvaluations > 0, "Evaluations should have occurred during concurrent building")
	assert.Equal(t, int64(10), finalBuilds, "All hierarchy builds should complete")

	t.Logf("Concurrent building test: %d builds, %d evaluations", finalBuilds, finalEvaluations)
}

// TestConcurrentPerformanceCounterUpdates tests thread-safety of performance metrics
func TestConcurrentPerformanceCounterUpdates(t *testing.T) {
	parser, err := NewSyntaxParser()
	require.NoError(t, err)
	engine := NewHierarchicalEvaluationEngine(parser)

	// Setup simple hierarchy
	routerConfigs := map[string]*dynamic.Router{
		"test-router": {
			Rule:        "PathPrefix(`/test`)",
			Priority:    100,
			Service:     "test-service",
			EntryPoints: []string{"web"},
		},
	}

	handlers := map[string]http.Handler{
		"test-router": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }),
	}

	err = engine.BuildHierarchy(routerConfigs, handlers)
	require.NoError(t, err)

	// Concurrent counter updates
	numGoroutines := runtime.NumCPU() * 2
	incrementsPerGoroutine := 1000
	expectedTotal := int64(numGoroutines * incrementsPerGoroutine)

	var wg sync.WaitGroup

	// Test evaluation counter updates
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/test/concurrent", nil)

			for j := 0; j < incrementsPerGoroutine; j++ {
				engine.EvaluateRequest(req)
			}
		}()
	}

	wg.Wait()

	// Validate counter consistency
	metrics := engine.GetPerformanceMetrics()
	requestCount := metrics["request_count"].(int64)
	evaluationCount := metrics["evaluation_counter"].(int64)

	assert.Equal(t, expectedTotal, requestCount, "Request count should be accurate under concurrency")
	assert.True(t, evaluationCount > 0, "Evaluation count should be incremented")

	// Test middleware timing updates
	engine.ResetPerformanceCounters()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(routerLevel int) {
			defer wg.Done()

			for j := 0; j < incrementsPerGoroutine; j++ {
				// Simulate middleware execution timing
				executionTime := int64(1000 + j) // Varying execution times
				engine.recordMiddlewareExecutionTiming(executionTime, routerLevel%3)
			}
		}(i)
	}

	wg.Wait()

	// Validate middleware timing metrics
	middlewareMetrics := engine.GetMiddlewareExecutionMetrics()
	middlewareCount := middlewareMetrics["middleware_execution_count"].(int64)

	assert.Equal(t, expectedTotal, middlewareCount, "Middleware execution count should be accurate")

	// Validate timing by level consistency
	timingByLevel := middlewareMetrics["timing_by_level"].(map[string]interface{})
	assert.True(t, len(timingByLevel) > 0, "Timing by level should be recorded")

	t.Logf("Counter test completed: %d total operations across %d goroutines", expectedTotal, numGoroutines)
}

// TestConcurrentConfigurationUpdates tests thread-safety during configuration changes
func TestConcurrentConfigurationUpdates(t *testing.T) {
	parser, err := NewSyntaxParser()
	require.NoError(t, err)
	engine := NewHierarchicalEvaluationEngine(parser)

	// Initial setup
	initialRouters := map[string]*dynamic.Router{
		"stable": {
			Rule:        "PathPrefix(`/stable`)",
			Priority:    1000,
			Service:     "stable-service",
			EntryPoints: []string{"web"},
		},
	}

	initialHandlers := map[string]http.Handler{
		"stable": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }),
	}

	err = engine.BuildHierarchy(initialRouters, initialHandlers)
	require.NoError(t, err)

	var (
		configUpdates   int64
		evaluationCount int64
		validationCount int64
		wg              sync.WaitGroup
		stopOperations  int32
	)

	// Configuration update goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()

		for i := 0; atomic.LoadInt32(&stopOperations) == 0; i++ {
			config := map[string]interface{}{
				"max_depth":                i%10 + 5, // Vary max depth
				"enable_early_termination": i%2 == 0, // Toggle early termination
			}

			engine.SetConfiguration(config)
			atomic.AddInt64(&configUpdates, 1)
			time.Sleep(time.Millisecond)
		}
	}()

	// Evaluation goroutines
	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/stable/test", nil)

			for atomic.LoadInt32(&stopOperations) == 0 {
				_, _ = engine.EvaluateRequest(req)
				atomic.AddInt64(&evaluationCount, 1)
			}
		}()
	}

	// Validation goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()

		for atomic.LoadInt32(&stopOperations) == 0 {
			issues := engine.ValidateHierarchy()
			if len(issues) > 0 {
				t.Errorf("Hierarchy validation failed during concurrent operations: %v", issues)
			}
			atomic.AddInt64(&validationCount, 1)
			time.Sleep(time.Millisecond)
		}
	}()

	// Run for a short duration
	time.Sleep(100 * time.Millisecond)
	atomic.StoreInt32(&stopOperations, 1)

	wg.Wait()

	finalConfigUpdates := atomic.LoadInt64(&configUpdates)
	finalEvaluations := atomic.LoadInt64(&evaluationCount)
	finalValidations := atomic.LoadInt64(&validationCount)

	assert.True(t, finalConfigUpdates > 0, "Configuration updates should occur")
	assert.True(t, finalEvaluations > 0, "Route evaluations should occur during config updates")
	assert.True(t, finalValidations > 0, "Hierarchy validations should occur")

	t.Logf("Concurrent config test: %d updates, %d evaluations, %d validations",
		finalConfigUpdates, finalEvaluations, finalValidations)
}

// TestDataRaceDetection is designed to catch data races with go test -race
func TestDataRaceDetection(t *testing.T) {
	parser, err := NewSyntaxParser()
	require.NoError(t, err)
	engine := NewHierarchicalEvaluationEngine(parser)

	// Complex hierarchy setup
	routerConfigs := map[string]*dynamic.Router{
		"root": {Rule: "PathPrefix(`/`)", Priority: 1000, EntryPoints: []string{"web"}},
		"api":  {Rule: "PathPrefix(`/api`)", Priority: 900, ParentRefs: []string{"root"}},
		"web":  {Rule: "PathPrefix(`/web`)", Priority: 800, ParentRefs: []string{"root"}},
		"v1":   {Rule: "PathPrefix(`/api/v1`)", Priority: 700, ParentRefs: []string{"api"}, Service: "v1-service"},
		"v2":   {Rule: "PathPrefix(`/api/v2`)", Priority: 600, ParentRefs: []string{"api"}, Service: "v2-service"},
	}

	handlers := make(map[string]http.Handler)
	for name := range routerConfigs {
		handlers[name] = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	}

	err = engine.BuildHierarchy(routerConfigs, handlers)
	require.NoError(t, err)

	// Intensive concurrent operations to trigger potential races
	var wg sync.WaitGroup
	duration := 50 * time.Millisecond
	startTime := time.Now()

	// Multiple evaluation goroutines with different access patterns
	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			requests := []*http.Request{
				httptest.NewRequest("GET", "/api/v1/users", nil),
				httptest.NewRequest("GET", "/api/v2/orders", nil),
				httptest.NewRequest("GET", "/web/dashboard", nil),
			}

			for time.Since(startTime) < duration {
				req := requests[id%len(requests)]
				_, _ = engine.EvaluateRequest(req)

				// Access different methods to test all synchronization points
				_ = engine.GetPerformanceMetrics()
				_ = engine.GetRequestTimingMetrics()
				_ = engine.GetMiddlewareExecutionMetrics()
				_ = engine.GetHierarchyInfo()
			}
		}(i)
	}

	// Reset counters goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()

		for time.Since(startTime) < duration {
			engine.ResetPerformanceCounters()
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Configuration changes goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()

		for time.Since(startTime) < duration {
			config := map[string]interface{}{
				"max_depth":                10,
				"enable_early_termination": true,
			}
			engine.SetConfiguration(config)
			time.Sleep(5 * time.Millisecond)
		}
	}()

	wg.Wait()

	// Final consistency check
	metrics := engine.GetPerformanceMetrics()
	assert.NotNil(t, metrics, "Performance metrics should be accessible after concurrent operations")

	t.Log("Data race detection test completed successfully")
}

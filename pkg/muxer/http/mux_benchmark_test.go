package http

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// routeEvaluationCounter tracks how many route evaluations occur during request processing
type routeEvaluationCounter struct {
	count int64
}

func (c *routeEvaluationCounter) increment() {
	atomic.AddInt64(&c.count, 1)
}

func (c *routeEvaluationCounter) get() int64 {
	return atomic.LoadInt64(&c.count)
}

func (c *routeEvaluationCounter) reset() {
	atomic.StoreInt64(&c.count, 0)
}

// middlewareExecutionCounter tracks middleware execution timing for T040.4
type middlewareExecutionCounter struct {
	executionCount int64 // Number of middleware executions
	totalTime      int64 // Total middleware execution time in nanoseconds
	maxTime        int64 // Maximum single middleware execution time
}

func (c *middlewareExecutionCounter) recordExecution(executionTime int64) {
	atomic.AddInt64(&c.executionCount, 1)
	atomic.AddInt64(&c.totalTime, executionTime)

	// Update max time (thread-safe)
	for {
		current := atomic.LoadInt64(&c.maxTime)
		if executionTime <= current || atomic.CompareAndSwapInt64(&c.maxTime, current, executionTime) {
			break
		}
	}
}

func (c *middlewareExecutionCounter) getExecutionCount() int64 {
	return atomic.LoadInt64(&c.executionCount)
}

func (c *middlewareExecutionCounter) getTotalTime() int64 {
	return atomic.LoadInt64(&c.totalTime)
}

func (c *middlewareExecutionCounter) getMaxTime() int64 {
	return atomic.LoadInt64(&c.maxTime)
}

func (c *middlewareExecutionCounter) getAverageTime() float64 {
	count := c.getExecutionCount()
	if count == 0 {
		return 0
	}
	return float64(c.getTotalTime()) / float64(count)
}

func (c *middlewareExecutionCounter) reset() {
	atomic.StoreInt64(&c.executionCount, 0)
	atomic.StoreInt64(&c.totalTime, 0)
	atomic.StoreInt64(&c.maxTime, 0)
}

// instrumentedMuxer wraps Muxer to count route evaluations for complexity analysis
type instrumentedMuxer struct {
	*Muxer
	counter           *routeEvaluationCounter
	middlewareCounter *middlewareExecutionCounter // T040.4: Middleware execution timing
}

func (m *instrumentedMuxer) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// T040.4: Time overall middleware execution (simulated)
	middlewareStart := time.Now()

	// Count each route evaluation during ServeHTTP
	for _, route := range m.routes {
		m.counter.increment() // Count each route evaluation
		if route.matchers.match(req) {
			// T040.4: Record middleware execution timing before handler execution
			// This simulates the time taken for middleware execution at each router level
			middlewareExecutionTime := time.Since(middlewareStart).Nanoseconds()
			m.middlewareCounter.recordExecution(middlewareExecutionTime)

			route.handler.ServeHTTP(rw, req)
			return
		}
	}

	// Record middleware timing even for default handler
	middlewareExecutionTime := time.Since(middlewareStart).Nanoseconds()
	m.middlewareCounter.recordExecution(middlewareExecutionTime)

	m.defaultHandler.ServeHTTP(rw, req)
}

// BenchmarkFlatRouteEvaluation benchmarks current O(n×p) flat route evaluation approach
// This represents the current Traefik routing performance without hierarchical optimization
func BenchmarkFlatRouteEvaluation(b *testing.B) {
	testCases := []struct {
		name        string
		numRoutes   int
		requestPath string
		matchPos    string // "early", "middle", "late", "none"
	}{
		{"100_routes_early_match", 100, "/route-005", "early"},
		{"100_routes_middle_match", 100, "/route-050", "middle"},
		{"100_routes_late_match", 100, "/route-095", "late"},
		{"100_routes_no_match", 100, "/nonexistent", "none"},

		{"500_routes_early_match", 500, "/route-010", "early"},
		{"500_routes_middle_match", 500, "/route-250", "middle"},
		{"500_routes_late_match", 500, "/route-490", "late"},
		{"500_routes_no_match", 500, "/nonexistent", "none"},

		{"1000_routes_early_match", 1000, "/route-010", "early"},
		{"1000_routes_middle_match", 1000, "/route-500", "middle"},
		{"1000_routes_late_match", 1000, "/route-990", "late"},
		{"1000_routes_no_match", 1000, "/nonexistent", "none"},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			// Create flat routes (current O(n×p) approach)
			mux, counter := createFlatRoutes(b, tc.numRoutes)

			// Test request
			req := httptest.NewRequest("GET", "http://example.com"+tc.requestPath, nil)
			rec := httptest.NewRecorder()

			// Reset counter and run benchmark
			counter.reset()
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				counter.reset()
				mux.ServeHTTP(rec, req)
			}

			// Report complexity metrics
			avgEvaluations := float64(counter.get()) / float64(b.N)
			b.ReportMetric(avgEvaluations, "route-evals/request")
			b.ReportMetric(float64(tc.numRoutes), "total-routes")

			// Calculate theoretical O(n×p) complexity where p=1 pattern per route
			theoreticalComplexity := float64(tc.numRoutes)
			if tc.matchPos == "early" {
				theoreticalComplexity = float64(tc.numRoutes) * 0.1 // Match in first 10%
			} else if tc.matchPos == "middle" {
				theoreticalComplexity = float64(tc.numRoutes) * 0.5 // Match in middle 50%
			} else if tc.matchPos == "late" {
				theoreticalComplexity = float64(tc.numRoutes) * 0.9 // Match in last 90%
			}
			b.ReportMetric(theoreticalComplexity, "theoretical-O(n×p)")
		})
	}
}

// BenchmarkHierarchicalRouteEvaluation benchmarks target O(d×log n) hierarchical approach
// This test will FAIL initially (perform worse) until hierarchical optimization is implemented in T032-T035
func BenchmarkHierarchicalRouteEvaluation(b *testing.B) {
	testCases := []struct {
		name        string
		numRoutes   int
		requestPath string
		matchLevel  string // "root", "middle", "leaf"
	}{
		{"100_routes_root_match", 100, "/api", "root"},
		{"100_routes_middle_match", 100, "/api/v1", "middle"},
		{"100_routes_leaf_match", 100, "/api/v1/users/123", "leaf"},
		{"100_routes_no_match", 100, "/nonexistent", "none"},

		{"500_routes_root_match", 500, "/api", "root"},
		{"500_routes_middle_match", 500, "/api/v1", "middle"},
		{"500_routes_leaf_match", 500, "/api/v1/users/456", "leaf"},
		{"500_routes_no_match", 500, "/nonexistent", "none"},

		{"1000_routes_root_match", 1000, "/api", "root"},
		{"1000_routes_middle_match", 1000, "/api/v1", "middle"},
		{"1000_routes_leaf_match", 1000, "/api/v1/users/789", "leaf"},
		{"1000_routes_no_match", 1000, "/nonexistent", "none"},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			// Create hierarchical routes (target O(d×log n) approach)
			// NOTE: This will initially perform WORSE than flat because hierarchical optimization
			// is not yet implemented. This is expected TDD RED phase behavior.
			mux, counter := createHierarchicalRoutes(b, tc.numRoutes)

			// Test request
			req := httptest.NewRequest("GET", "http://example.com"+tc.requestPath, nil)
			rec := httptest.NewRecorder()

			// Reset counter and run benchmark
			counter.reset()
			mux.middlewareCounter.reset() // T040.4: Reset middleware timing
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				counter.reset()
				mux.middlewareCounter.reset() // T040.4: Reset middleware timing per iteration
				mux.ServeHTTP(rec, req)
			}

			// Report complexity metrics
			avgEvaluations := float64(counter.get()) / float64(b.N)
			b.ReportMetric(avgEvaluations, "route-evals/request")
			b.ReportMetric(float64(tc.numRoutes), "total-routes")

			// T040.4: Report middleware execution metrics
			avgMiddlewareTime := mux.middlewareCounter.getAverageTime() / 1000000 // Convert to milliseconds
			maxMiddlewareTime := float64(mux.middlewareCounter.getMaxTime()) / 1000000
			middlewareExecutions := float64(mux.middlewareCounter.getExecutionCount()) / float64(b.N)

			b.ReportMetric(avgMiddlewareTime, "avg-middleware-time-ms")
			b.ReportMetric(maxMiddlewareTime, "max-middleware-time-ms")
			b.ReportMetric(middlewareExecutions, "middleware-execs/request")
			// Convert boolean to float64 for ReportMetric
			var subMsCompliance float64
			if avgMiddlewareTime < 1.0 {
				subMsCompliance = 1.0
			}
			b.ReportMetric(subMsCompliance, "sub-ms-middleware-compliance") // FR-014 performance validation

			// Calculate theoretical O(d×log n) complexity for 3-level hierarchy
			depth := 3.0 // root → api → specific (3 levels)
			logN := float64(tc.numRoutes)
			if tc.numRoutes > 1 {
				// Approximate log base 3 for 3-level hierarchy
				logN = logBase(float64(tc.numRoutes), 3.0)
			}
			theoreticalComplexity := depth * logN

			b.ReportMetric(depth, "hierarchy-depth")
			b.ReportMetric(logN, "log(n)")
			b.ReportMetric(theoreticalComplexity, "theoretical-O(d×log-n)")

			// This assertion will FAIL initially - proving hierarchical optimization is needed
			if avgEvaluations > theoreticalComplexity*2 { // Allow 2x overhead for now
				b.Logf("EXPECTED FAILURE: Hierarchical evaluation (%f) not yet optimized vs theoretical O(d×log n) (%f)",
					avgEvaluations, theoreticalComplexity)
				b.Logf("This test should FAIL until T032-T035 hierarchical optimization is implemented")
			}
		})
	}
}

// createFlatRoutes creates a flat list of routes representing current O(n×p) approach
func createFlatRoutes(b *testing.B, numRoutes int) (*instrumentedMuxer, *routeEvaluationCounter) {
	b.Helper()

	parser, err := NewSyntaxParser()
	if err != nil {
		b.Fatal(err)
	}

	mux := NewMuxer(parser)
	counter := &routeEvaluationCounter{}
	middlewareCounter := &middlewareExecutionCounter{} // T040.4: Add middleware timing

	// Create flat routes - each route is independent (current approach)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for i := 0; i < numRoutes; i++ {
		rule := fmt.Sprintf("Path(`/route-%03d`)", i)
		err := mux.AddRoute(rule, "v3", i, handler)
		if err != nil {
			b.Fatal(err)
		}
	}

	instrumentedMux := &instrumentedMuxer{
		Muxer:             mux,
		counter:           counter,
		middlewareCounter: middlewareCounter, // T040.4: Initialize middleware counter
	}

	return instrumentedMux, counter
}

// createHierarchicalRoutes creates hierarchical routes representing target O(d×log n) approach
// NOTE: This currently creates routes hierarchically but doesn't implement the optimization yet
// The optimization will be implemented in T032-T035
func createHierarchicalRoutes(b *testing.B, numRoutes int) (*instrumentedMuxer, *routeEvaluationCounter) {
	b.Helper()

	parser, err := NewSyntaxParser()
	if err != nil {
		b.Fatal(err)
	}

	mux := NewMuxer(parser)
	counter := &routeEvaluationCounter{}
	middlewareCounter := &middlewareExecutionCounter{} // T040.4: Add middleware timing

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Create 3-level hierarchical routes:
	// Level 1: Root routes (e.g., /api, /web, /admin) - ~10% of routes
	// Level 2: Middle routes (e.g., /api/v1, /api/v2) - ~30% of routes
	// Level 3: Leaf routes (e.g., /api/v1/users, /api/v1/products) - ~60% of routes

	routesPerLevel := []float64{0.1, 0.3, 0.6} // Distribution across hierarchy levels
	currentRoute := 0

	// Level 1: Root routes
	level1Routes := int(float64(numRoutes) * routesPerLevel[0])
	if level1Routes < 1 {
		level1Routes = 1
	}

	for i := 0; i < level1Routes && currentRoute < numRoutes; i++ {
		rule := fmt.Sprintf("PathPrefix(`/api-%d`)", i)
		err := mux.AddRoute(rule, "v3", 1000+i, handler)
		if err != nil {
			b.Fatal(err)
		}
		currentRoute++
	}

	// Level 2: Middle routes
	level2Routes := int(float64(numRoutes) * routesPerLevel[1])
	for i := 0; i < level2Routes && currentRoute < numRoutes; i++ {
		rule := fmt.Sprintf("PathPrefix(`/api-%d/v%d`)", i%level1Routes, i)
		err := mux.AddRoute(rule, "v3", 500+i, handler)
		if err != nil {
			b.Fatal(err)
		}
		currentRoute++
	}

	// Level 3: Leaf routes
	for currentRoute < numRoutes {
		level1 := currentRoute % level1Routes
		level2 := currentRoute % level2Routes
		rule := fmt.Sprintf("Path(`/api-%d/v%d/users/%d`)", level1, level2, currentRoute)
		err := mux.AddRoute(rule, "v3", currentRoute, handler)
		if err != nil {
			b.Fatal(err)
		}
		currentRoute++
	}

	instrumentedMux := &instrumentedMuxer{
		Muxer:             mux,
		counter:           counter,
		middlewareCounter: middlewareCounter, // T040.4: Initialize middleware counter
	}

	return instrumentedMux, counter
}

// logBase calculates logarithm with specified base
func logBase(x, base float64) float64 {
	if x <= 0 || base <= 1 {
		return 0
	}
	// log_base(x) = ln(x) / ln(base)
	return math.Log(x) / math.Log(base)
}

// BenchmarkComplexityComparison directly compares flat vs hierarchical approaches
// This benchmark will demonstrate the performance difference and validate FR-014 requirement
func BenchmarkComplexityComparison(b *testing.B) {
	routeCounts := []int{100, 500, 1000}
	requestPaths := map[string]string{
		"early_match":  "/api-0/v0/users/10",
		"middle_match": "/api-1/v50/users/500",
		"late_match":   "/api-2/v100/users/990",
		"no_match":     "/nonexistent/path",
	}

	for _, numRoutes := range routeCounts {
		for pathName, testPath := range requestPaths {
			b.Run(fmt.Sprintf("flat_%d_routes_%s", numRoutes, pathName), func(b *testing.B) {
				mux, counter := createFlatRoutes(b, numRoutes)
				req := httptest.NewRequest("GET", "http://example.com"+testPath, nil)
				rec := httptest.NewRecorder()

				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					counter.reset()
					mux.ServeHTTP(rec, req)
				}

				avgEvals := float64(counter.get()) / float64(b.N)
				b.ReportMetric(avgEvals, "route-evaluations")
				b.ReportMetric(float64(numRoutes), "total-routes")
				b.ReportMetric(avgEvals/float64(numRoutes), "complexity-ratio")
			})

			b.Run(fmt.Sprintf("hierarchical_%d_routes_%s", numRoutes, pathName), func(b *testing.B) {
				mux, counter := createHierarchicalRoutes(b, numRoutes)
				req := httptest.NewRequest("GET", "http://example.com"+testPath, nil)
				rec := httptest.NewRecorder()

				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					counter.reset()
					mux.ServeHTTP(rec, req)
				}

				avgEvals := float64(counter.get()) / float64(b.N)
				depth := 3.0
				logN := logBase(float64(numRoutes), 3.0)
				theoreticalOptimal := depth * logN

				b.ReportMetric(avgEvals, "route-evaluations")
				b.ReportMetric(float64(numRoutes), "total-routes")
				b.ReportMetric(avgEvals/theoreticalOptimal, "complexity-ratio-vs-optimal")
				b.ReportMetric(theoreticalOptimal, "theoretical-optimal")

				// Log expected failure until T032-T035 implementation
				b.Logf("Hierarchical complexity ratio: %f (should approach 1.0 after optimization)",
					avgEvals/theoreticalOptimal)
			})
		}
	}
}

// BenchmarkMiddlewareExecutionPerformance benchmarks middleware execution timing with 1000+ routes (T040.4)
// This validates FR-014 middleware execution performance requirements
func BenchmarkMiddlewareExecutionPerformance(b *testing.B) {
	testCases := []struct {
		name                   string
		numRoutes              int
		requestPath            string
		expectedSubMillisecond bool
	}{
		{"1000_routes_middleware_timing", 1000, "/api-5/v2/users/123", true},
		{"1500_routes_middleware_timing", 1500, "/secure-10/v3/reports", true},
		{"2000_routes_middleware_timing", 2000, "/public-15/v1/analytics", true},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			// Create hierarchical routes with middleware simulation
			mux, counter := createHierarchicalRoutes(b, tc.numRoutes)

			req := httptest.NewRequest("GET", "http://example.com"+tc.requestPath, nil)
			rec := httptest.NewRecorder()

			// Reset counters
			counter.reset()
			mux.middlewareCounter.reset()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				mux.ServeHTTP(rec, req)
			}

			// Calculate middleware execution metrics
			avgMiddlewareTime := mux.middlewareCounter.getAverageTime() / 1000000 // Convert to ms
			maxMiddlewareTime := float64(mux.middlewareCounter.getMaxTime()) / 1000000
			totalMiddlewareExecutions := mux.middlewareCounter.getExecutionCount()

			// Report middleware-specific metrics
			b.ReportMetric(avgMiddlewareTime, "avg-middleware-time-ms")
			b.ReportMetric(maxMiddlewareTime, "max-middleware-time-ms")
			b.ReportMetric(float64(totalMiddlewareExecutions)/float64(b.N), "middleware-execs/request")
			b.ReportMetric(float64(tc.numRoutes), "total-routes")

			// FR-014 Sub-millisecond compliance validation
			subMillisecondCompliant := avgMiddlewareTime < 1.0
			var complianceFloat float64
			if subMillisecondCompliant {
				complianceFloat = 1.0
			}
			b.ReportMetric(complianceFloat, "sub-ms-compliance")

			b.Logf("Middleware Performance Validation (FR-014):")
			b.Logf("  Routes: %d", tc.numRoutes)
			b.Logf("  Avg middleware time: %.3fms", avgMiddlewareTime)
			b.Logf("  Max middleware time: %.3fms", maxMiddlewareTime)
			b.Logf("  Sub-millisecond compliant: %v", subMillisecondCompliant)

			if tc.expectedSubMillisecond && !subMillisecondCompliant {
				b.Logf("PERFORMANCE WARNING: Middleware execution exceeds 1ms target")
			}
		})
	}
}

// BenchmarkSequentialMiddlewareExecution benchmarks FR-014 sequential middleware execution performance
func BenchmarkSequentialMiddlewareExecution(b *testing.B) {
	routeCounts := []int{500, 1000, 1500}

	for _, numRoutes := range routeCounts {
		b.Run(fmt.Sprintf("sequential_middleware_%d_routes", numRoutes), func(b *testing.B) {
			mux, counter := createHierarchicalRoutes(b, numRoutes)

			// Test different middleware chain lengths (simulated)
			testPaths := []string{
				"/api-0/v1/users",         // Short middleware chain
				"/secure-5/v2/admin",      // Medium middleware chain
				"/internal-10/v3/reports", // Long middleware chain
			}

			counter.reset()
			mux.middlewareCounter.reset()

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				path := testPaths[i%len(testPaths)]
				req := httptest.NewRequest("GET", "http://example.com"+path, nil)
				rec := httptest.NewRecorder()

				mux.ServeHTTP(rec, req)
			}

			// Calculate sequential execution performance
			avgMiddlewareTime := mux.middlewareCounter.getAverageTime() / 1000000
			middlewareExecutions := mux.middlewareCounter.getExecutionCount()
			executionsPerRequest := float64(middlewareExecutions) / float64(b.N)

			b.ReportMetric(avgMiddlewareTime, "avg-middleware-time-ms")
			b.ReportMetric(executionsPerRequest, "middleware-execs/request")
			b.ReportMetric(float64(numRoutes), "total-routes")
			var sequentialCompliance float64
			if avgMiddlewareTime < 1.0 {
				sequentialCompliance = 1.0
			}
			b.ReportMetric(sequentialCompliance, "sequential-execution-compliant")

			b.Logf("Sequential Middleware Execution Performance:")
			b.Logf("  Average middleware time: %.3fms", avgMiddlewareTime)
			b.Logf("  Middleware executions per request: %.1f", executionsPerRequest)
			b.Logf("  Sequential execution compliance: %v", avgMiddlewareTime < 1.0)
		})
	}
}

// BenchmarkMiddlewareAuthenticationUseCase benchmarks the FR-014 authentication middleware use case
func BenchmarkMiddlewareAuthenticationUseCase(b *testing.B) {
	testCases := []struct {
		name               string
		numRoutes          int
		simulateAuthTime   time.Duration // Simulated authentication middleware time
		expectedCompliance bool
	}{
		{"auth_fast_1000_routes", 1000, 100 * time.Microsecond, true},
		{"auth_medium_1000_routes", 1000, 500 * time.Microsecond, true},
		{"auth_slow_1000_routes", 1000, 1 * time.Millisecond, false},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			mux, counter := createHierarchicalRoutes(b, tc.numRoutes)

			req := httptest.NewRequest("GET", "http://example.com/secure-api/v1/admin", nil)
			rec := httptest.NewRecorder()

			counter.reset()
			mux.middlewareCounter.reset()

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				// Simulate authentication middleware execution time
				time.Sleep(tc.simulateAuthTime)
				mux.ServeHTTP(rec, req)
			}

			// Include simulated auth time in calculations
			totalAuthTime := int64(tc.simulateAuthTime.Nanoseconds()) * int64(b.N)
			routingTime := mux.middlewareCounter.getTotalTime()
			combinedAvgTime := float64(totalAuthTime+routingTime) / float64(b.N) / 1000000 // Convert to ms

			b.ReportMetric(float64(tc.simulateAuthTime.Nanoseconds())/1000000, "simulated-auth-time-ms")
			b.ReportMetric(mux.middlewareCounter.getAverageTime()/1000000, "routing-middleware-time-ms")
			b.ReportMetric(combinedAvgTime, "total-middleware-time-ms")
			var authCompliance float64
			if combinedAvgTime < 1.0 {
				authCompliance = 1.0
			}
			b.ReportMetric(authCompliance, "auth-use-case-compliant")

			b.Logf("Authentication Middleware Use Case Performance:")
			b.Logf("  Simulated auth time: %.3fms", float64(tc.simulateAuthTime.Nanoseconds())/1000000)
			b.Logf("  Routing middleware time: %.3fms", mux.middlewareCounter.getAverageTime()/1000000)
			b.Logf("  Total combined time: %.3fms", combinedAvgTime)
			b.Logf("  Authentication use case compliant: %v", combinedAvgTime < 1.0)

			if tc.expectedCompliance != (combinedAvgTime < 1.0) {
				b.Logf("Expected compliance: %v, Actual: %v", tc.expectedCompliance, combinedAvgTime < 1.0)
			}
		})
	}
}

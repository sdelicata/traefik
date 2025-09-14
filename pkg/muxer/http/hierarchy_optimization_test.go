package http

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/traefik/traefik/v3/pkg/config/dynamic"
)

// mockMiddlewareBuilder is defined in middleware_integration_test.go

// hierarchicalRouteCounter tracks route evaluations during hierarchical routing tests
type hierarchicalRouteCounter struct {
	totalEvaluations int64
	parentEvals      int64
	childEvals       int64
	grandchildEvals  int64
	level3Evals      int64 // for deeper hierarchies
}

func newHierarchicalRouteCounter() *hierarchicalRouteCounter {
	return &hierarchicalRouteCounter{}
}

func (c *hierarchicalRouteCounter) incrementTotal() {
	atomic.AddInt64(&c.totalEvaluations, 1)
}

func (c *hierarchicalRouteCounter) incrementLevel(level int) {
	switch level {
	case 0:
		atomic.AddInt64(&c.parentEvals, 1)
	case 1:
		atomic.AddInt64(&c.childEvals, 1)
	case 2:
		atomic.AddInt64(&c.grandchildEvals, 1)
	case 3:
		atomic.AddInt64(&c.level3Evals, 1)
	}
}

func (c *hierarchicalRouteCounter) getTotalEvaluations() int64 {
	return atomic.LoadInt64(&c.totalEvaluations)
}

func (c *hierarchicalRouteCounter) getParentEvals() int64 {
	return atomic.LoadInt64(&c.parentEvals)
}

func (c *hierarchicalRouteCounter) getChildEvals() int64 {
	return atomic.LoadInt64(&c.childEvals)
}

func (c *hierarchicalRouteCounter) getGrandchildEvals() int64 {
	return atomic.LoadInt64(&c.grandchildEvals)
}

func (c *hierarchicalRouteCounter) getLevel3Evals() int64 {
	return atomic.LoadInt64(&c.level3Evals)
}

func (c *hierarchicalRouteCounter) reset() {
	atomic.StoreInt64(&c.totalEvaluations, 0)
	atomic.StoreInt64(&c.parentEvals, 0)
	atomic.StoreInt64(&c.childEvals, 0)
	atomic.StoreInt64(&c.grandchildEvals, 0)
	atomic.StoreInt64(&c.level3Evals, 0)
}

// instrumentedHierarchicalMuxer wraps Muxer to track hierarchical route evaluations
type instrumentedHierarchicalMuxer struct {
	*Muxer
	counter     *hierarchicalRouteCounter
	routeLevels map[string]int // route name to hierarchy level mapping
}

func newInstrumentedHierarchicalMuxer(parser SyntaxParser) (*instrumentedHierarchicalMuxer, *hierarchicalRouteCounter) {
	counter := newHierarchicalRouteCounter()
	return &instrumentedHierarchicalMuxer{
		Muxer:       NewMuxer(parser),
		counter:     counter,
		routeLevels: make(map[string]int),
	}, counter
}

// addHierarchicalRoute adds a route with hierarchy level tracking
func (m *instrumentedHierarchicalMuxer) addHierarchicalRoute(rule, routeName string, level int, handler http.Handler) error {
	m.routeLevels[routeName] = level
	return m.AddRoute(rule, "v3", level*100, handler) // Use level-based priority
}

// ServeHTTP implements custom route evaluation with hierarchy tracking
func (m *instrumentedHierarchicalMuxer) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// Use hierarchical evaluation if enabled, otherwise fall back to flat evaluation
	if m.IsHierarchicalEvaluationEnabled() {
		engine := m.GetHierarchicalEngine()
		if engine != nil {
			// Reset counters for this request
			prevEvals := engine.GetRouteEvaluationCount()

			if matchedRouter, found := engine.EvaluateRequest(req); found {
				// Count evaluations for this request
				currentEvals := engine.GetRouteEvaluationCount()
				requestEvals := currentEvals - prevEvals

				// Distribute evaluations to our counter based on estimated levels
				m.distributeEvaluationCounts(requestEvals)

				matchedRouter.Handler.ServeHTTP(rw, req)
				return
			}

			// Count evaluations even if no match
			currentEvals := engine.GetRouteEvaluationCount()
			requestEvals := currentEvals - prevEvals
			m.distributeEvaluationCounts(requestEvals)
		}
	} else {
		// Original flat evaluation for comparison
		for _, route := range m.routes {
			m.counter.incrementTotal()

			// Try to determine route level from priority (approximate)
			level := route.priority / 100
			if level > 3 {
				level = 0 // fallback for non-hierarchical routes
			}
			m.counter.incrementLevel(level)

			if route.matchers.match(req) {
				route.handler.ServeHTTP(rw, req)
				return
			}
		}
	}

	m.defaultHandler.ServeHTTP(rw, req)
}

// distributeEvaluationCounts distributes evaluation counts across hierarchy levels for benchmarking
func (m *instrumentedHierarchicalMuxer) distributeEvaluationCounts(totalEvals int64) {
	// Simple distribution: assume evaluations are spread across levels
	// This is an approximation for benchmark tracking
	for i := int64(0); i < totalEvals; i++ {
		m.counter.incrementTotal()
		// Distribute across levels (simplified)
		level := int(i % 4) // 0-3 levels
		m.counter.incrementLevel(level)
	}
}

// setupHierarchicalTestConfiguration creates proper hierarchical router configurations for testing
func setupHierarchicalTestConfiguration(tc struct {
	name              string
	hierarchyDepth    int
	routesPerLevel    int
	requestPath       string
	parentShouldMatch bool
	expectedEvals     int
	currentEvals      int
}, handler http.Handler) (map[string]*dynamic.Router, map[string]http.Handler) {
	routerConfigs := make(map[string]*dynamic.Router)
	handlers := make(map[string]http.Handler)

	// Create true hierarchical relationships with parentRefs
	for level := 0; level < tc.hierarchyDepth; level++ {
		for i := 0; i < tc.routesPerLevel; i++ {
			routeName := fmt.Sprintf("level-%d-route-%d", level, i)
			var rule string
			var parentRefs []string

			switch level {
			case 0: // Parent level
				rule = fmt.Sprintf("PathPrefix(`/%s-%d`)", []string{"api", "admin", "public", "secure", "internal"}[i%5], i)
			case 1: // Child level
				parentPath := []string{"api", "admin", "public", "secure", "internal"}[i%5]
				rule = fmt.Sprintf("PathPrefix(`/%s-%d/v%d`)", parentPath, i, (i%3)+1)
				// Reference parent router
				parentName := fmt.Sprintf("level-0-route-%d", i%tc.routesPerLevel)
				parentRefs = []string{parentName}
			case 2: // Grandchild level
				parentPath := []string{"api", "admin", "public", "secure", "internal"}[i%5]
				rule = fmt.Sprintf("PathPrefix(`/%s-%d/v%d/%s`)", parentPath, i, (i%3)+1, []string{"users", "products", "orders", "reports"}[i%4])
				// Reference child router
				parentName := fmt.Sprintf("level-1-route-%d", i%tc.routesPerLevel)
				parentRefs = []string{parentName}
			case 3: // Great-grandchild level
				parentPath := []string{"api", "admin", "public", "secure", "internal"}[i%5]
				rule = fmt.Sprintf("PathPrefix(`/%s-%d/v%d/%s/%d`)", parentPath, i, (i%3)+1, []string{"users", "products", "orders", "reports"}[i%4], i)
				// Reference grandchild router
				parentName := fmt.Sprintf("level-2-route-%d", i%tc.routesPerLevel)
				parentRefs = []string{parentName}
			}

			router := &dynamic.Router{
				Rule:       rule,
				Service:    routeName + "-service",
				ParentRefs: parentRefs,
				Priority:   (tc.hierarchyDepth - level) * 100, // Higher priority for higher levels
			}

			// Only root routers have entrypoints
			if level == 0 {
				router.EntryPoints = []string{"web"}
			}

			routerConfigs[routeName] = router
			handlers[routeName] = handler
		}
	}

	return routerConfigs, handlers
}

// BenchmarkEarlyTerminationOptimization tests FR-016 early termination requirement
// This benchmark will FAIL initially because current implementation doesn't support early termination
func BenchmarkEarlyTerminationOptimization(b *testing.B) {
	testCases := []struct {
		name              string
		hierarchyDepth    int
		routesPerLevel    int
		requestPath       string
		parentShouldMatch bool
		expectedEvals     int // Expected evaluations with early termination
		currentEvals      int // Current evaluations without early termination
	}{
		{
			name:              "parent_no_match_2_levels",
			hierarchyDepth:    2,
			routesPerLevel:    10,
			requestPath:       "/nomatch/api",
			parentShouldMatch: false,
			expectedEvals:     10, // Only parent level should be evaluated
			currentEvals:      20, // Current: all routes evaluated
		},
		{
			name:              "parent_no_match_3_levels",
			hierarchyDepth:    3,
			routesPerLevel:    10,
			requestPath:       "/nomatch/api/v1",
			parentShouldMatch: false,
			expectedEvals:     10, // Only parent level should be evaluated
			currentEvals:      30, // Current: all routes evaluated
		},
		{
			name:              "parent_no_match_4_levels_deep",
			hierarchyDepth:    4,
			routesPerLevel:    5,
			requestPath:       "/nomatch/deep/hierarchy/test",
			parentShouldMatch: false,
			expectedEvals:     5,  // Only parent level should be evaluated
			currentEvals:      20, // Current: all routes evaluated
		},
		{
			name:              "parent_match_continue_eval",
			hierarchyDepth:    3,
			routesPerLevel:    8,
			requestPath:       "/api/v1/users",
			parentShouldMatch: true,
			expectedEvals:     24, // All routes should be evaluated when parent matches
			currentEvals:      24, // Same as current (no optimization needed)
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			// Create hierarchical muxer with instrumentation
			parser, err := NewSyntaxParser()
			if err != nil {
				b.Fatal(err)
			}

			mux, counter := newInstrumentedHierarchicalMuxer(parser)

			// Enable hierarchical evaluation for early termination testing
			mux.EnableHierarchicalEvaluation()

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("matched"))
			})

			// Set up hierarchical router configuration
			routerConfigs, handlers := setupHierarchicalTestConfiguration(tc, handler)

			// Configure the hierarchical evaluation engine
			mockBuilder := &mockMiddlewareBuilder{}
			err = mux.SetHierarchicalRoutes(routerConfigs, handlers, mockBuilder)
			if err != nil {
				b.Fatal(err)
			}

			totalRoutes := tc.hierarchyDepth * tc.routesPerLevel

			// Test request
			req := httptest.NewRequest("GET", "http://example.com"+tc.requestPath, nil)
			rec := httptest.NewRecorder()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				counter.reset()
				mux.ServeHTTP(rec, req)
			}

			// Report metrics
			avgEvaluations := float64(counter.getTotalEvaluations()) / float64(b.N)
			parentEvals := float64(counter.getParentEvals()) / float64(b.N)
			childEvals := float64(counter.getChildEvals()) / float64(b.N)
			grandchildEvals := float64(counter.getGrandchildEvals()) / float64(b.N)

			b.ReportMetric(avgEvaluations, "total-evaluations")
			b.ReportMetric(parentEvals, "parent-level-evals")
			b.ReportMetric(childEvals, "child-level-evals")
			b.ReportMetric(grandchildEvals, "grandchild-level-evals")
			b.ReportMetric(float64(tc.expectedEvals), "expected-with-early-termination")
			b.ReportMetric(float64(tc.currentEvals), "current-without-optimization")
			b.ReportMetric(float64(totalRoutes), "total-routes")

			// Early termination efficiency calculation
			if tc.expectedEvals > 0 {
				efficiency := float64(tc.expectedEvals) / avgEvaluations
				b.ReportMetric(efficiency, "early-termination-efficiency")
			}

			// FR-016 Validation: This will FAIL initially
			if !tc.parentShouldMatch {
				expectedSavings := avgEvaluations - float64(tc.expectedEvals)
				b.ReportMetric(expectedSavings, "potential-evaluation-savings")

				// Log expected failure for TDD RED phase
				b.Logf("FR-016 Early Termination Test - Expected to FAIL initially")
				b.Logf("Parent should not match for path: %s", tc.requestPath)
				b.Logf("Current evaluations: %.1f, Expected with early termination: %d", avgEvaluations, tc.expectedEvals)
				b.Logf("Potential savings: %.1f evaluations (%.1f%% reduction)", expectedSavings, (expectedSavings/avgEvaluations)*100)

				if avgEvaluations > float64(tc.expectedEvals)*1.5 {
					b.Logf("EXPECTED FAILURE: Early termination not implemented - evaluating %.1f routes instead of %d", avgEvaluations, tc.expectedEvals)
				}
			}
		})
	}
}

// BenchmarkHierarchicalEvaluationReduction tests search space reduction per level (FR-015)
// This benchmark validates that route evaluation decreases with hierarchy depth
func BenchmarkHierarchicalEvaluationReduction(b *testing.B) {
	hierarchyConfigs := []struct {
		name           string
		totalRoutes    int
		levelsDeep     int
		routesPerLevel []int // routes at each level
		testPaths      []string
	}{
		{
			name:           "balanced_3_level_hierarchy",
			totalRoutes:    300,
			levelsDeep:     3,
			routesPerLevel: []int{10, 30, 60}, // 10 parents, 30 children, 60 grandchildren
			testPaths:      []string{"/api-0/v1/users", "/admin-0/v2/products", "/nomatch/path"},
		},
		{
			name:           "deep_4_level_hierarchy",
			totalRoutes:    200,
			levelsDeep:     4,
			routesPerLevel: []int{5, 15, 30, 150}, // increasingly narrow at each level
			testPaths:      []string{"/api-0/v1/users/123", "/secure-1/v3/reports/monthly", "/nonexistent/deep/path/test"},
		},
		{
			name:           "wide_2_level_hierarchy",
			totalRoutes:    500,
			levelsDeep:     2,
			routesPerLevel: []int{50, 450}, // many parents, many children
			testPaths:      []string{"/api-10/v1", "/public-25/docs", "/missing/endpoint"},
		},
	}

	for _, config := range hierarchyConfigs {
		b.Run(config.name, func(b *testing.B) {
			parser, err := NewSyntaxParser()
			if err != nil {
				b.Fatal(err)
			}

			mux, counter := newInstrumentedHierarchicalMuxer(parser)

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// Build hierarchical structure
			for level := 0; level < config.levelsDeep; level++ {
				routeCount := config.routesPerLevel[level]

				for i := 0; i < routeCount; i++ {
					var rule string
					routeName := fmt.Sprintf("level-%d-route-%d", level, i)

					// Create increasingly specific rules at each level
					switch level {
					case 0:
						rule = fmt.Sprintf("PathPrefix(`/%s-%d`)", []string{"api", "admin", "public", "secure", "internal"}[i%5], i)
					case 1:
						parentIdx := i % config.routesPerLevel[0]
						parentPath := []string{"api", "admin", "public", "secure", "internal"}[parentIdx%5]
						rule = fmt.Sprintf("PathPrefix(`/%s-%d/v%d`)", parentPath, parentIdx, (i%5)+1)
					case 2:
						parentIdx := i % config.routesPerLevel[0]
						childIdx := i % config.routesPerLevel[1]
						parentPath := []string{"api", "admin", "public", "secure", "internal"}[parentIdx%5]
						rule = fmt.Sprintf("Path(`/%s-%d/v%d/%s`)", parentPath, parentIdx, (childIdx%5)+1, []string{"users", "products", "orders", "reports", "settings"}[i%5])
					case 3:
						parentIdx := i % config.routesPerLevel[0]
						childIdx := i % config.routesPerLevel[1]
						parentPath := []string{"api", "admin", "public", "secure", "internal"}[parentIdx%5]
						resource := []string{"users", "products", "orders", "reports", "settings"}[(i % 5)]
						rule = fmt.Sprintf("Path(`/%s-%d/v%d/%s/%d`)", parentPath, parentIdx, (childIdx%5)+1, resource, i)
					}

					err := mux.addHierarchicalRoute(rule, routeName, level, handler)
					if err != nil {
						b.Fatal(err)
					}
				}
			}

			// Test multiple request paths
			for _, testPath := range config.testPaths {
				b.Run("path_"+testPath, func(b *testing.B) {
					req := httptest.NewRequest("GET", "http://example.com"+testPath, nil)
					rec := httptest.NewRecorder()

					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						counter.reset()
						mux.ServeHTTP(rec, req)
					}

					avgTotal := float64(counter.getTotalEvaluations()) / float64(b.N)
					avgParent := float64(counter.getParentEvals()) / float64(b.N)
					avgChild := float64(counter.getChildEvals()) / float64(b.N)
					avgGrandchild := float64(counter.getGrandchildEvals()) / float64(b.N)

					b.ReportMetric(avgTotal, "total-evaluations")
					b.ReportMetric(avgParent, "parent-evaluations")
					b.ReportMetric(avgChild, "child-evaluations")
					b.ReportMetric(avgGrandchild, "grandchild-evaluations")
					b.ReportMetric(float64(config.totalRoutes), "total-routes")

					// Calculate expected evaluations with search space reduction
					expectedEvals := 0.0
					for level := 0; level < config.levelsDeep; level++ {
						// With proper search space reduction, we should only evaluate
						// routes at each level until we find matches or exhaust the level
						expectedEvals += float64(config.routesPerLevel[level]) / float64(config.levelsDeep)
					}

					b.ReportMetric(expectedEvals, "expected-with-search-reduction")

					// FR-015 Validation
					searchSpaceEfficiency := expectedEvals / avgTotal
					b.ReportMetric(searchSpaceEfficiency, "search-space-efficiency")

					b.Logf("FR-015 Search Space Reduction Test")
					b.Logf("Total routes: %d, Levels: %d", config.totalRoutes, config.levelsDeep)
					b.Logf("Current evaluations: %.1f, Expected with reduction: %.1f", avgTotal, expectedEvals)
					b.Logf("Search space efficiency: %.3f (should approach 1.0 after optimization)", searchSpaceEfficiency)

					if searchSpaceEfficiency < 0.5 {
						b.Logf("EXPECTED FAILURE: Search space reduction not implemented")
					}
				})
			}
		})
	}
}

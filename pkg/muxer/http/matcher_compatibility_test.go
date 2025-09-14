package http

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/traefik/traefik/v3/pkg/middlewares/requestdecorator"
	"github.com/traefik/traefik/v3/pkg/testhelpers"
)

// TestExistingMatcherFuncPreservation tests FR-018, FR-021: existing MatcherFunc implementations work unchanged
func TestExistingMatcherFuncPreservation(t *testing.T) {
	testCases := []struct {
		name    string
		matcher string
		url     string
		headers map[string]string
		want    bool
	}{
		// Host matchers
		{
			name:    "Host exact match",
			matcher: `Host("example.com")`,
			url:     "https://example.com/path",
			want:    true,
		},
		{
			name:    "Host no match",
			matcher: `Host("example.com")`,
			url:     "https://other.com/path",
			want:    false,
		},
		{
			name:    "HostRegexp match",
			matcher: `HostRegexp("^ex.*\\.com$")`,
			url:     "https://example.com/path",
			want:    true,
		},
		// Path matchers
		{
			name:    "Path exact match",
			matcher: `Path("/api/v1")`,
			url:     "https://example.com/api/v1",
			want:    true,
		},
		{
			name:    "PathPrefix match",
			matcher: `PathPrefix("/api")`,
			url:     "https://example.com/api/v1/users",
			want:    true,
		},
		{
			name:    "PathRegexp match",
			matcher: `PathRegexp("^/api/v[0-9]+")`,
			url:     "https://example.com/api/v2/users",
			want:    true,
		},
		// Method matchers
		{
			name:    "Method POST match",
			matcher: `Method("POST")`,
			url:     "POST:https://example.com/api",
			want:    true,
		},
		{
			name:    "Method GET no match",
			matcher: `Method("POST")`,
			url:     "GET:https://example.com/api",
			want:    false,
		},
		// Header matchers
		{
			name:    "Header exact match",
			matcher: `Header("Content-Type", "application/json")`,
			url:     "https://example.com/api",
			headers: map[string]string{"Content-Type": "application/json"},
			want:    true,
		},
		{
			name:    "HeaderRegexp match",
			matcher: `HeaderRegexp("Content-Type", "^application/.*")`,
			url:     "https://example.com/api",
			headers: map[string]string{"Content-Type": "application/xml"},
			want:    true,
		},
		// Query matchers
		{
			name:    "Query match",
			matcher: `Query("debug", "true")`,
			url:     "https://example.com/api?debug=true",
			want:    true,
		},
		{
			name:    "QueryRegexp match",
			matcher: `QueryRegexp("version", "^[0-9]+\\.[0-9]+$")`,
			url:     "https://example.com/api?version=1.2",
			want:    true,
		},
	}

	parser, err := NewSyntaxParser()
	require.NoError(t, err)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Test current matcher behavior
			muxer := NewMuxer(parser)
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			err := muxer.AddRoute(tc.matcher, "", 0, handler)
			require.NoError(t, err, "Failed to add route with matcher: %s", tc.matcher)

			// Create test request
			method := "GET"
			url := tc.url
			if len(tc.url) > 4 && tc.url[4] == ':' {
				method = tc.url[:4]
				url = tc.url[5:]
			}

			req := testhelpers.MustNewRequest(method, url, http.NoBody)
			for key, value := range tc.headers {
				req.Header.Set(key, value)
			}

			// Apply request decorator for host matching
			reqDecorator := requestdecorator.New(nil)
			w := httptest.NewRecorder()

			reqDecorator.ServeHTTP(w, req, muxer.ServeHTTP)

			got := w.Code == http.StatusOK
			assert.Equal(t, tc.want, got, "Matcher %s with URL %s should return %v but got %v", tc.matcher, tc.url, tc.want, got)
		})
	}
}

// TestMatchersTreeLogicPreservation tests FR-021: matchersTree.match() logic preserved
func TestMatchersTreeLogicPreservation(t *testing.T) {
	complexRules := []struct {
		name      string
		rule      string
		testCases map[string]bool // URL -> expected result
	}{
		{
			name: "AND combination",
			rule: `Host("example.com") && PathPrefix("/api")`,
			testCases: map[string]bool{
				"https://example.com/api/users": true,
				"https://example.com/web":       false,
				"https://other.com/api/users":   false,
			},
		},
		{
			name: "OR combination",
			rule: `Host("example.com") || Host("api.example.com")`,
			testCases: map[string]bool{
				"https://example.com/path":     true,
				"https://api.example.com/path": true,
				"https://other.com/path":       false,
			},
		},
		{
			name: "Complex nested logic",
			rule: `(Host("example.com") || Host("api.example.com")) && PathPrefix("/v1")`,
			testCases: map[string]bool{
				"https://example.com/v1/users":    true,
				"https://api.example.com/v1/data": true,
				"https://example.com/v2/users":    false,
				"https://other.com/v1/users":      false,
			},
		},
		{
			name: "NOT operator",
			rule: `!Host("example.com")`,
			testCases: map[string]bool{
				"https://example.com/path": false,
				"https://other.com/path":   true,
			},
		},
		{
			name: "Complex NOT with AND",
			rule: `!(Host("example.com") && PathPrefix("/admin"))`,
			testCases: map[string]bool{
				"https://example.com/admin/users": false,
				"https://example.com/public":      true,
				"https://other.com/admin/users":   true,
			},
		},
	}

	parser, err := NewSyntaxParser()
	require.NoError(t, err)

	for _, tc := range complexRules {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			muxer := NewMuxer(parser)
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			err := muxer.AddRoute(tc.rule, "", 0, handler)
			require.NoError(t, err, "Failed to add route with rule: %s", tc.rule)

			reqDecorator := requestdecorator.New(nil)

			for url, expected := range tc.testCases {
				t.Run(fmt.Sprintf("URL_%s", url), func(t *testing.T) {
					req := testhelpers.MustNewRequest(http.MethodGet, url, http.NoBody)
					w := httptest.NewRecorder()

					reqDecorator.ServeHTTP(w, req, muxer.ServeHTTP)

					got := w.Code == http.StatusOK
					assert.Equal(t, expected, got, "Rule %s with URL %s should return %v but got %v", tc.rule, url, expected, got)
				})
			}
		})
	}
}

// TestRouteMatchersCallPreservation tests that route.matchers.match() calls work identically
func TestRouteMatchersCallPreservation(t *testing.T) {
	testCases := []struct {
		name          string
		routes        []string // multiple routes to test priority and ordering
		testURL       string
		expectedMatch int // index of route that should match, -1 for no match
	}{
		{
			name: "Priority ordering preserved",
			routes: []string{
				`PathPrefix("/api/v1")`, // Longer rule, higher priority
				`PathPrefix("/api")`,    // Shorter rule, lower priority
			},
			testURL:       "https://example.com/api/v1/users",
			expectedMatch: 0, // First route should match due to higher priority (longer rule)
		},
		{
			name: "First match wins with same priority",
			routes: []string{
				`Host("example.com")`,
				`PathPrefix("/")`, // Would match but Host comes first
			},
			testURL:       "https://example.com/api",
			expectedMatch: 0, // First route should match
		},
		{
			name: "No match scenario",
			routes: []string{
				`Host("example.com")`,
				`PathPrefix("/api")`,
			},
			testURL:       "https://other.com/web",
			expectedMatch: -1, // No route should match
		},
	}

	parser, err := NewSyntaxParser()
	require.NoError(t, err)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			muxer := NewMuxer(parser)

			// Add routes with handlers that identify which route matched
			for i, rule := range tc.routes {
				handler := http.HandlerFunc(func(routeIndex int) func(w http.ResponseWriter, r *http.Request) {
					return func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Matched-Route", fmt.Sprintf("%d", routeIndex))
						w.WriteHeader(http.StatusOK)
					}
				}(i))

				err := muxer.AddRoute(rule, "", 0, handler)
				require.NoError(t, err, "Failed to add route %d: %s", i, rule)
			}

			req := testhelpers.MustNewRequest(http.MethodGet, tc.testURL, http.NoBody)
			reqDecorator := requestdecorator.New(nil)
			w := httptest.NewRecorder()

			reqDecorator.ServeHTTP(w, req, muxer.ServeHTTP)

			if tc.expectedMatch == -1 {
				assert.Equal(t, http.StatusNotFound, w.Code, "Expected no route to match")
			} else {
				assert.Equal(t, http.StatusOK, w.Code, "Expected route %d to match", tc.expectedMatch)
				matchedRoute := w.Header().Get("Matched-Route")
				assert.Equal(t, fmt.Sprintf("%d", tc.expectedMatch), matchedRoute, "Wrong route matched")
			}
		})
	}
}

// TestZeroModificationVerification tests FR-022: no changes to existing matcher code
func TestZeroModificationVerification(t *testing.T) {
	t.Run("MatcherFunc type unchanged", func(t *testing.T) {
		// Test that MatcherFunc signature is preserved
		var fn MatcherFunc = func(req *http.Request) bool { return true }

		// This should compile without issues
		req := &http.Request{}
		result := fn(req)
		assert.IsType(t, true, result, "MatcherFunc should return bool")
	})

	t.Run("matchersTree structure unchanged", func(t *testing.T) {
		tree := &matchersTree{}

		// Test that required fields exist
		rt := reflect.TypeOf(tree).Elem()

		// Check matcher field
		matcherField, found := rt.FieldByName("matcher")
		assert.True(t, found, "matchersTree should have matcher field")
		assert.Equal(t, "MatcherFunc", matcherField.Type.Name(), "matcher field should be MatcherFunc type")

		// Check operator field
		operatorField, found := rt.FieldByName("operator")
		assert.True(t, found, "matchersTree should have operator field")
		assert.Equal(t, "string", operatorField.Type.Name(), "operator field should be string type")

		// Check left and right fields
		leftField, found := rt.FieldByName("left")
		assert.True(t, found, "matchersTree should have left field")
		assert.Contains(t, leftField.Type.String(), "matchersTree", "left field should be *matchersTree type")

		rightField, found := rt.FieldByName("right")
		assert.True(t, found, "matchersTree should have right field")
		assert.Contains(t, rightField.Type.String(), "matchersTree", "right field should be *matchersTree type")
	})

	t.Run("matchersTree.match method preserved", func(t *testing.T) {
		tree := &matchersTree{
			matcher: func(req *http.Request) bool { return true },
		}

		// Test that match method exists and works
		req := &http.Request{}
		result := tree.match(req)
		assert.True(t, result, "matchersTree.match should work with MatcherFunc")

		// Test AND logic
		tree = &matchersTree{
			operator: "and",
			left:     &matchersTree{matcher: func(req *http.Request) bool { return true }},
			right:    &matchersTree{matcher: func(req *http.Request) bool { return false }},
		}
		result = tree.match(req)
		assert.False(t, result, "AND logic should work: true && false = false")

		// Test OR logic
		tree = &matchersTree{
			operator: "or",
			left:     &matchersTree{matcher: func(req *http.Request) bool { return true }},
			right:    &matchersTree{matcher: func(req *http.Request) bool { return false }},
		}
		result = tree.match(req)
		assert.True(t, result, "OR logic should work: true || false = true")
	})

	t.Run("httpFuncs registry unchanged", func(t *testing.T) {
		// Test that all expected matcher functions are still available
		expectedMatchers := []string{
			"ClientIP", "Method", "Host", "HostRegexp",
			"Path", "PathRegexp", "PathPrefix",
			"Header", "HeaderRegexp", "Query", "QueryRegexp",
		}

		for _, matcher := range expectedMatchers {
			_, exists := httpFuncs[matcher]
			assert.True(t, exists, "Matcher %s should exist in httpFuncs", matcher)
		}
	})
}

// TestHierarchicalCompatibilityPreservation tests that hierarchical routing preserves all existing matcher behavior
func TestHierarchicalCompatibilityPreservation(t *testing.T) {
	parser, err := NewSyntaxParser()
	require.NoError(t, err)

	t.Run("Hierarchical routing preserves matcher behavior", func(t *testing.T) {
		// Test that hierarchical evaluation produces identical results to flat routing
		testCases := []struct {
			name     string
			rules    []string // Rules for flat evaluation
			testURL  string
			headers  map[string]string
			expected bool // Expected result for flat routing
		}{
			{
				name:     "Host matcher preservation",
				rules:    []string{`Host("example.com")`},
				testURL:  "https://example.com/api",
				expected: true,
			},
			{
				name:     "PathPrefix matcher preservation",
				rules:    []string{`PathPrefix("/api")`},
				testURL:  "https://example.com/api/users",
				expected: true,
			},
			{
				name:     "Complex AND matcher preservation",
				rules:    []string{`Host("example.com") && PathPrefix("/api")`},
				testURL:  "https://example.com/api/users",
				expected: true,
			},
			{
				name:     "Complex OR matcher preservation",
				rules:    []string{`Host("example.com") || Host("api.example.com")`},
				testURL:  "https://api.example.com/users",
				expected: true,
			},
			{
				name:     "Method matcher preservation",
				rules:    []string{`Method("POST")`},
				testURL:  "POST:https://example.com/api",
				expected: true,
			},
			{
				name:     "Header matcher preservation",
				rules:    []string{`Header("Content-Type", "application/json")`},
				testURL:  "https://example.com/api",
				headers:  map[string]string{"Content-Type": "application/json"},
				expected: true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Test flat routing (existing behavior)
				flatMuxer := NewMuxer(parser)
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				})

				for _, rule := range tc.rules {
					err := flatMuxer.AddRoute(rule, "", 0, handler)
					require.NoError(t, err)
				}

				// Test hierarchical routing (should produce identical results)
				hierarchicalMuxer := NewMuxer(parser)
				hierarchicalMuxer.EnableHierarchicalEvaluation()

				for _, rule := range tc.rules {
					err := hierarchicalMuxer.AddRoute(rule, "", 0, handler)
					require.NoError(t, err)
				}

				// Create test request
				method := "GET"
				url := tc.testURL
				if len(tc.testURL) > 4 && tc.testURL[4] == ':' {
					method = tc.testURL[:4]
					url = tc.testURL[5:]
				}

				req := testhelpers.MustNewRequest(method, url, http.NoBody)
				for key, value := range tc.headers {
					req.Header.Set(key, value)
				}

				// Test flat routing
				reqDecorator := requestdecorator.New(nil)
				flatW := httptest.NewRecorder()
				reqDecorator.ServeHTTP(flatW, req, flatMuxer.ServeHTTP)
				flatResult := flatW.Code == http.StatusOK

				// Test hierarchical routing
				hierarchicalW := httptest.NewRecorder()
				reqDecorator.ServeHTTP(hierarchicalW, req, hierarchicalMuxer.ServeHTTP)
				hierarchicalResult := hierarchicalW.Code == http.StatusOK

				// Verify results are identical
				assert.Equal(t, tc.expected, flatResult, "Flat routing should match expected result")
				assert.Equal(t, flatResult, hierarchicalResult, "Hierarchical routing should preserve flat routing behavior")
			})
		}
	})

	t.Run("Early termination doesn't break matcher logic", func(t *testing.T) {
		// Test that hierarchical early termination doesn't interfere with individual matcher evaluation
		muxer := NewMuxer(parser)
		muxer.EnableHierarchicalEvaluation()

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// Add a rule that should be evaluated normally
		err := muxer.AddRoute(`Host("example.com") && PathPrefix("/api")`, "", 0, handler)
		require.NoError(t, err)

		// Test request that matches
		req := testhelpers.MustNewRequest("GET", "https://example.com/api/users", http.NoBody)
		reqDecorator := requestdecorator.New(nil)
		w := httptest.NewRecorder()

		reqDecorator.ServeHTTP(w, req, muxer.ServeHTTP)
		assert.Equal(t, http.StatusOK, w.Code, "Matcher evaluation should work correctly with hierarchical optimization")

		// Test request that doesn't match
		req2 := testhelpers.MustNewRequest("GET", "https://other.com/api/users", http.NoBody)
		w2 := httptest.NewRecorder()

		reqDecorator.ServeHTTP(w2, req2, muxer.ServeHTTP)
		assert.Equal(t, http.StatusNotFound, w2.Code, "Non-matching requests should still be correctly rejected")
	})

	t.Run("Search space optimization preserves MatcherFunc implementations", func(t *testing.T) {
		// Verify that search space optimization doesn't modify core matcher function behavior
		muxer := NewMuxer(parser)
		muxer.EnableHierarchicalEvaluation()

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// Add multiple routes to test search space optimization
		rules := []string{
			`Host("api1.example.com")`,
			`Host("api2.example.com")`,
			`PathPrefix("/v1")`,
			`PathPrefix("/v2")`,
			`Method("GET")`,
			`Method("POST")`,
		}

		for i, rule := range rules {
			err := muxer.AddRoute(rule, "", i, handler)
			require.NoError(t, err)
		}

		// Test various combinations to ensure MatcherFunc behavior is preserved
		testRequests := []struct {
			url      string
			method   string
			expected int // Expected status code
		}{
			{"https://api1.example.com/test", "GET", http.StatusOK},
			{"https://api2.example.com/test", "POST", http.StatusOK},
			{"https://other.com/v1", "GET", http.StatusOK},
			{"https://other.com/v2", "POST", http.StatusOK},
			{"https://unknown.com/unknown", "DELETE", http.StatusNotFound},
		}

		reqDecorator := requestdecorator.New(nil)
		for _, tr := range testRequests {
			req := testhelpers.MustNewRequest(tr.method, tr.url, http.NoBody)
			w := httptest.NewRecorder()

			reqDecorator.ServeHTTP(w, req, muxer.ServeHTTP)
			assert.Equal(t, tr.expected, w.Code, "Request %s %s should return %d", tr.method, tr.url, tr.expected)
		}
	})
}

// benchmarkCurrentMatcherPerformance provides baseline for performance regression testing
func BenchmarkCurrentMatcherPerformance(b *testing.B) {
	parser, err := NewSyntaxParser()
	if err != nil {
		b.Fatal(err)
	}

	muxer := NewMuxer(parser)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	// Add various types of matchers for comprehensive testing
	rules := []string{
		`Host("example.com")`,
		`PathPrefix("/api")`,
		`Host("api.example.com") && PathPrefix("/v1")`,
		`(Host("example.com") || Host("api.example.com")) && Method("GET")`,
		`Header("Content-Type", "application/json")`,
	}

	for i, rule := range rules {
		err := muxer.AddRoute(rule, "", i, handler)
		if err != nil {
			b.Fatal(err)
		}
	}

	req := testhelpers.MustNewRequest(http.MethodGet, "https://example.com/api/users", http.NoBody)
	req.Header.Set("Content-Type", "application/json")

	reqDecorator := requestdecorator.New(nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		reqDecorator.ServeHTTP(w, req, muxer.ServeHTTP)
	}
}

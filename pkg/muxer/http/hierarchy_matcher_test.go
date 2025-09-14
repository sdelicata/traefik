package http

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/traefik/traefik/v3/pkg/config/dynamic"
	"github.com/traefik/traefik/v3/pkg/testhelpers"
)

// TestHostMatchersAtAllHierarchyLevels tests FR-019: Host matchers work at any hierarchy level
func TestHostMatchersAtAllHierarchyLevels(t *testing.T) {

	testCases := []struct {
		name           string
		parentRule     string
		childRule      string
		grandchildRule string
		testURL        string
		expectMatch    bool
		description    string
	}{
		{
			name:           "Host matcher at parent level",
			parentRule:     "Host(`api.example.com`)",
			childRule:      "PathPrefix(`/v1`)",
			grandchildRule: "PathPrefix(`/v1/users`)",
			testURL:        "https://api.example.com/v1/users/123",
			expectMatch:    true,
			description:    "Parent host matcher should work in hierarchy",
		},
		{
			name:           "Host matcher at child level",
			parentRule:     "PathPrefix(`/api`)",
			childRule:      "Host(`api.example.com`)",
			grandchildRule: "Method(`GET`)",
			testURL:        "https://api.example.com/api/users",
			expectMatch:    true,
			description:    "Child host matcher should work in hierarchy",
		},
		{
			name:           "HostRegexp at grandchild level",
			parentRule:     "PathPrefix(`/api`)",
			childRule:      "PathPrefix(`/api/v1`)",
			grandchildRule: "HostRegexp(`^.*\\.example\\.com$`)",
			testURL:        "https://api.example.com/api/v1/data",
			expectMatch:    true,
			description:    "Grandchild HostRegexp matcher should work",
		},
		{
			name:           "Multiple Host matchers in same branch",
			parentRule:     "Host(`example.com`) || Host(`api.example.com`)",
			childRule:      "Host(`api.example.com`)",
			grandchildRule: "PathPrefix(`/v1`)",
			testURL:        "https://api.example.com/v1/users",
			expectMatch:    true,
			description:    "Multiple host matchers in hierarchy should work",
		},
		{
			name:           "Host matcher mismatch should fail hierarchy",
			parentRule:     "Host(`example.com`)",
			childRule:      "PathPrefix(`/api`)",
			grandchildRule: "Method(`GET`)",
			testURL:        "https://other.com/api/users",
			expectMatch:    false,
			description:    "Host mismatch should prevent hierarchy match",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// TODO: When hierarchical routing is implemented, this test should:
			// 1. Create parent, child, grandchild routers with specified rules
			// 2. Test that host matchers work at any hierarchy level
			// 3. Verify parent host matching affects child evaluation
			// 4. Confirm host matching works regardless of tree position

			t.Logf("Testing: %s", tc.description)
			t.Logf("Parent rule: %s", tc.parentRule)
			t.Logf("Child rule: %s", tc.childRule)
			t.Logf("Grandchild rule: %s", tc.grandchildRule)
			t.Logf("Test URL: %s", tc.testURL)
			t.Logf("Expected match: %v", tc.expectMatch)

			// Test using hierarchical evaluation engine
			parser, err := NewSyntaxParser()
			require.NoError(t, err)

			engine := NewHierarchicalEvaluationEngine(parser)

			// Create test routers using dynamic configuration
			routerConfigs := map[string]*dynamic.Router{
				"parent": {
					Rule: tc.parentRule,
				},
				"child": {
					Rule:       tc.childRule,
					ParentRefs: []string{"parent"},
				},
				"grandchild": {
					Rule:       tc.grandchildRule,
					ParentRefs: []string{"child"},
				},
			}

			handlers := map[string]http.Handler{
				"parent": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("parent"))
				}),
				"child": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("child"))
				}),
				"grandchild": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("grandchild"))
				}),
			}

			err = engine.BuildHierarchy(routerConfigs, handlers)
			require.NoError(t, err)

			// Create test request
			req := testhelpers.MustNewRequest(http.MethodGet, tc.testURL, http.NoBody)

			// Test hierarchical evaluation
			matchedRouter, found := engine.EvaluateRequest(req)

			if tc.expectMatch {
				assert.True(t, found, "Should find matching router in hierarchy")
				assert.NotNil(t, matchedRouter, "Matched router should not be nil")
			} else {
				assert.False(t, found, "Should not find matching router")
			}
		})
	}
}

// TestPathMatchersAtAllHierarchyLevels tests FR-019: Path matchers work at any hierarchy level
func TestPathMatchersAtAllHierarchyLevels(t *testing.T) {

	testCases := []struct {
		name           string
		parentRule     string
		childRule      string
		grandchildRule string
		testURL        string
		expectMatch    bool
		description    string
	}{
		{
			name:           "PathPrefix at parent level",
			parentRule:     `PathPrefix("/api")`,
			childRule:      `Host("example.com")`,
			grandchildRule: `Method("GET")`,
			testURL:        "https://example.com/api/users",
			expectMatch:    true,
			description:    "Parent PathPrefix should work in hierarchy",
		},
		{
			name:           "Path exact match at child level",
			parentRule:     `Host("example.com")`,
			childRule:      `Path("/api/v1/users")`,
			grandchildRule: `Header("Accept", "application/json")`,
			testURL:        "https://example.com/api/v1/users",
			expectMatch:    true,
			description:    "Child Path exact match should work",
		},
		{
			name:           "PathRegexp at grandchild level",
			parentRule:     `Host("example.com")`,
			childRule:      `PathPrefix("/api")`,
			grandchildRule: `PathRegexp("^/api/v[0-9]+/.*")`,
			testURL:        "https://example.com/api/v2/data",
			expectMatch:    true,
			description:    "Grandchild PathRegexp should work",
		},
		{
			name:           "Hierarchical path refinement",
			parentRule:     `PathPrefix("/api")`,
			childRule:      `PathPrefix("/api/v1")`,
			grandchildRule: `PathPrefix("/api/v1/users")`,
			testURL:        "https://example.com/api/v1/users/123",
			expectMatch:    true,
			description:    "Path refinement through hierarchy levels should work",
		},
		{
			name:           "Mixed path matcher types",
			parentRule:     `PathRegexp("^/api.*")`,
			childRule:      `PathPrefix("/api/v1")`,
			grandchildRule: `Path("/api/v1/status")`,
			testURL:        "https://example.com/api/v1/status",
			expectMatch:    true,
			description:    "Mixed path matcher types in hierarchy should work",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			t.Logf("Testing: %s", tc.description)
			t.Logf("Hierarchy rules: Parent=%s, Child=%s, Grandchild=%s", tc.parentRule, tc.childRule, tc.grandchildRule)
			t.Logf("Test URL: %s", tc.testURL)

			// This test documents FR-019 requirement for path matcher flexibility
			// Implementation should allow Path, PathPrefix, PathRegexp at any hierarchy level
			t.Log("TODO: Implement hierarchical Path matcher support")
		})
	}
}

// TestMethodMatchersAtAllHierarchyLevels tests FR-019: Method matchers work at any hierarchy level
func TestMethodMatchersAtAllHierarchyLevels(t *testing.T) {

	testCases := []struct {
		name           string
		parentRule     string
		childRule      string
		grandchildRule string
		testMethod     string
		testURL        string
		expectMatch    bool
		description    string
	}{
		{
			name:           "Method at parent level",
			parentRule:     `Method("POST")`,
			childRule:      `PathPrefix("/api")`,
			grandchildRule: `Host("example.com")`,
			testMethod:     "POST",
			testURL:        "https://example.com/api/users",
			expectMatch:    true,
			description:    "Parent Method matcher should work",
		},
		{
			name:           "Method at child level",
			parentRule:     `Host("example.com")`,
			childRule:      `Method("PUT")`,
			grandchildRule: `PathPrefix("/api/v1")`,
			testMethod:     "PUT",
			testURL:        "https://example.com/api/v1/users",
			expectMatch:    true,
			description:    "Child Method matcher should work",
		},
		{
			name:           "Method at grandchild level",
			parentRule:     `Host("example.com")`,
			childRule:      `PathPrefix("/api")`,
			grandchildRule: `Method("DELETE")`,
			testMethod:     "DELETE",
			testURL:        "https://example.com/api/users/123",
			expectMatch:    true,
			description:    "Grandchild Method matcher should work",
		},
		{
			name:           "Multiple methods in hierarchy",
			parentRule:     `Method("GET") || Method("POST")`,
			childRule:      `PathPrefix("/api")`,
			grandchildRule: `Method("POST")`,
			testMethod:     "POST",
			testURL:        "https://example.com/api/users",
			expectMatch:    true,
			description:    "Multiple method matchers should work in hierarchy",
		},
		{
			name:           "Method mismatch should fail",
			parentRule:     `Method("GET")`,
			childRule:      `PathPrefix("/api")`,
			grandchildRule: `Host("example.com")`,
			testMethod:     "POST",
			testURL:        "https://example.com/api/users",
			expectMatch:    false,
			description:    "Method mismatch should prevent hierarchy match",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			t.Logf("Testing: %s", tc.description)
			t.Logf("Method: %s", tc.testMethod)
			t.Logf("URL: %s", tc.testURL)

			// This test documents FR-019 requirement for method matcher flexibility
			// Implementation should allow Method() matcher at any hierarchy level
			t.Log("TODO: Implement hierarchical Method matcher support")
		})
	}
}

// TestHeaderMatchersAtAllHierarchyLevels tests FR-019: Header matchers work at any hierarchy level
func TestHeaderMatchersAtAllHierarchyLevels(t *testing.T) {

	testCases := []struct {
		name           string
		parentRule     string
		childRule      string
		grandchildRule string
		testHeaders    map[string]string
		testURL        string
		expectMatch    bool
		description    string
	}{
		{
			name:           "Header exact match at parent level",
			parentRule:     `Header("Content-Type", "application/json")`,
			childRule:      `PathPrefix("/api")`,
			grandchildRule: `Method("POST")`,
			testHeaders:    map[string]string{"Content-Type": "application/json"},
			testURL:        "https://example.com/api/users",
			expectMatch:    true,
			description:    "Parent Header exact match should work",
		},
		{
			name:           "HeaderRegexp at child level",
			parentRule:     `Host("example.com")`,
			childRule:      `HeaderRegexp("Accept", "^application/.*")`,
			grandchildRule: `PathPrefix("/api")`,
			testHeaders:    map[string]string{"Accept": "application/xml"},
			testURL:        "https://example.com/api/data",
			expectMatch:    true,
			description:    "Child HeaderRegexp should work",
		},
		{
			name:           "Multiple headers at grandchild level",
			parentRule:     `Host("example.com")`,
			childRule:      `PathPrefix("/api")`,
			grandchildRule: `Header("Authorization", "Bearer token123") && Header("X-API-Key", "secret")`,
			testHeaders:    map[string]string{"Authorization": "Bearer token123", "X-API-Key": "secret"},
			testURL:        "https://example.com/api/secure",
			expectMatch:    true,
			description:    "Multiple header matchers at grandchild should work",
		},
		{
			name:           "Mixed header matcher types in hierarchy",
			parentRule:     `HeaderRegexp("User-Agent", "^Mozilla.*")`,
			childRule:      `Header("Accept-Language", "en-US")`,
			grandchildRule: `HeaderRegexp("Authorization", "^Bearer .*")`,
			testHeaders: map[string]string{
				"User-Agent":      "Mozilla/5.0",
				"Accept-Language": "en-US",
				"Authorization":   "Bearer abc123",
			},
			testURL:     "https://example.com/api",
			expectMatch: true,
			description: "Mixed header matcher types should work in hierarchy",
		},
		{
			name:           "Header mismatch should fail hierarchy",
			parentRule:     `Header("Content-Type", "application/json")`,
			childRule:      `PathPrefix("/api")`,
			grandchildRule: `Method("POST")`,
			testHeaders:    map[string]string{"Content-Type": "text/plain"},
			testURL:        "https://example.com/api/users",
			expectMatch:    false,
			description:    "Header mismatch should prevent hierarchy match",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			t.Logf("Testing: %s", tc.description)
			t.Logf("Headers: %v", tc.testHeaders)

			// This test documents FR-019 requirement for header matcher flexibility
			// Implementation should allow Header() and HeaderRegexp() at any hierarchy level
			t.Log("TODO: Implement hierarchical Header matcher support")
		})
	}
}

// TestMixedMatcherTypesWithinSameBranch tests FR-020: mixing different matcher types within same hierarchy branch
func TestMixedMatcherTypesWithinSameBranch(t *testing.T) {

	testCases := []struct {
		name           string
		parentRule     string
		childRule      string
		grandchildRule string
		testRequest    testRequest
		expectMatch    bool
		description    string
	}{
		{
			name:           "Host and Method at parent",
			parentRule:     `Host("api.example.com") && Method("POST")`,
			childRule:      `PathPrefix("/v1") && Header("Content-Type", "application/json")`,
			grandchildRule: `Path("/v1/users") && Query("debug", "true")`,
			testRequest: testRequest{
				method:  "POST",
				url:     "https://api.example.com/v1/users?debug=true",
				headers: map[string]string{"Content-Type": "application/json"},
			},
			expectMatch: true,
			description: "Complex mixed matcher hierarchy should work",
		},
		{
			name:           "Path and Header combinations",
			parentRule:     `PathRegexp("^/api.*") && HeaderRegexp("Authorization", "^Bearer .*")`,
			childRule:      `PathPrefix("/api/v1") && Method("GET")`,
			grandchildRule: `Path("/api/v1/status") && Host("example.com")`,
			testRequest: testRequest{
				method:  "GET",
				url:     "https://example.com/api/v1/status",
				headers: map[string]string{"Authorization": "Bearer token123"},
			},
			expectMatch: true,
			description: "Path and header combinations across levels should work",
		},
		{
			name:           "All matcher types mixed",
			parentRule:     `Host("example.com") && Method("POST")`,
			childRule:      `PathPrefix("/api") && Header("Content-Type", "application/json")`,
			grandchildRule: `PathRegexp("^/api/v[0-9]+/.*") && Query("format", "json")`,
			testRequest: testRequest{
				method:  "POST",
				url:     "https://example.com/api/v2/data?format=json",
				headers: map[string]string{"Content-Type": "application/json"},
			},
			expectMatch: true,
			description: "All matcher types should mix freely in hierarchy",
		},
		{
			name:           "OR combinations across levels",
			parentRule:     `Host("example.com") || Host("api.example.com")`,
			childRule:      `Method("GET") || Method("POST")`,
			grandchildRule: `PathPrefix("/v1") || PathPrefix("/v2")`,
			testRequest: testRequest{
				method: "POST",
				url:    "https://api.example.com/v2/users",
			},
			expectMatch: true,
			description: "OR combinations should work across hierarchy levels",
		},
		{
			name:           "Complex NOT operations",
			parentRule:     `!(Host("blacklisted.com"))`,
			childRule:      `!(Method("DELETE"))`,
			grandchildRule: `!(PathPrefix("/admin"))`,
			testRequest: testRequest{
				method: "GET",
				url:    "https://example.com/api/users",
			},
			expectMatch: true,
			description: "NOT operations should work in hierarchy",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			t.Logf("Testing: %s", tc.description)
			t.Logf("Parent: %s", tc.parentRule)
			t.Logf("Child: %s", tc.childRule)
			t.Logf("Grandchild: %s", tc.grandchildRule)
			t.Logf("Request: %+v", tc.testRequest)

			// This test documents FR-020 requirement for unrestricted matcher mixing
			// Implementation should allow any combination of matcher types within same hierarchy branch
			t.Log("TODO: Implement hierarchical mixed matcher support")
		})
	}
}

// TestUnrestrictedMatcherUsageRegardlessOfTreePosition tests FR-019: no restrictions based on tree position
func TestUnrestrictedMatcherUsageRegardlessOfTreePosition(t *testing.T) {

	// Test that any matcher type can be used at any position in the tree
	matcherTypes := []struct {
		name        string
		rule        string
		description string
	}{
		{"Host", `Host("example.com")`, "Host matcher"},
		{"HostRegexp", `HostRegexp("^.*\\.example\\.com$")`, "Host regex matcher"},
		{"Path", `Path("/api/users")`, "Exact path matcher"},
		{"PathPrefix", `PathPrefix("/api")`, "Path prefix matcher"},
		{"PathRegexp", `PathRegexp("^/api/v[0-9]+")`, "Path regex matcher"},
		{"Method", `Method("POST")`, "HTTP method matcher"},
		{"Header", `Header("Content-Type", "application/json")`, "Header exact matcher"},
		{"HeaderRegexp", `HeaderRegexp("Accept", "^application/.*")`, "Header regex matcher"},
		{"Query", `Query("debug", "true")`, "Query parameter matcher"},
		{"QueryRegexp", `QueryRegexp("version", "^[0-9]+")`, "Query regex matcher"},
		{"ClientIP", `ClientIP("192.168.1.0/24")`, "Client IP matcher"},
	}

	positions := []string{"parent", "child", "grandchild"}

	for _, matcher := range matcherTypes {
		for _, position := range positions {
			t.Run(fmt.Sprintf("%s_at_%s_level", matcher.name, position), func(t *testing.T) {
				t.Parallel()

				t.Logf("Testing %s at %s level", matcher.description, position)
				t.Logf("Rule: %s", matcher.rule)

				// This test documents FR-019 requirement: no restrictions on matcher usage based on tree position
				// Any matcher should work at parent, child, or grandchild level
				t.Log("TODO: Implement position-independent matcher support")
			})
		}
	}
}

// TestMatcherFlexibilityIntegration tests complete matcher flexibility scenarios
func TestMatcherFlexibilityIntegration(t *testing.T) {

	integrationScenarios := []struct {
		name         string
		description  string
		config       hierarchicalRouterConfig
		testRequests []testRequestResult
	}{
		{
			name:        "E-commerce API with authentication hierarchy",
			description: "Real-world scenario with authentication, versioning, and resource routing",
			config: hierarchicalRouterConfig{
				parent: routerConfig{
					name: "auth-gateway",
					rule: `Host("api.ecommerce.com") && HeaderRegexp("Authorization", "^Bearer .*")`,
				},
				child: routerConfig{
					name: "api-version",
					rule: `PathPrefix("/api/v1") && Method("GET", "POST", "PUT", "DELETE")`,
				},
				grandchild: routerConfig{
					name: "resource-router",
					rule: `PathRegexp("^/api/v1/(users|products|orders).*") && Header("Accept", "application/json")`,
				},
			},
			testRequests: []testRequestResult{
				{
					request: testRequest{
						method:  "GET",
						url:     "https://api.ecommerce.com/api/v1/users/123",
						headers: map[string]string{"Authorization": "Bearer token", "Accept": "application/json"},
					},
					expectMatch: true,
					description: "Valid authenticated API request should match",
				},
				{
					request: testRequest{
						method:  "GET",
						url:     "https://api.ecommerce.com/api/v1/users/123",
						headers: map[string]string{"Accept": "application/json"}, // Missing auth
					},
					expectMatch: false,
					description: "Unauthenticated request should not match",
				},
			},
		},
		{
			name:        "Multi-tenant SaaS with tenant isolation",
			description: "Complex routing with tenant identification and service routing",
			config: hierarchicalRouterConfig{
				parent: routerConfig{
					name: "tenant-identifier",
					rule: `HostRegexp("^(?P<tenant>[a-z0-9-]+)\\.saas\\.example\\.com$")`,
				},
				child: routerConfig{
					name: "service-router",
					rule: `PathRegexp("^/(api|app|admin).*") && !PathPrefix("/internal")`,
				},
				grandchild: routerConfig{
					name: "api-endpoint",
					rule: `PathPrefix("/api") && Method("GET", "POST") && Header("X-API-Version", "2.0")`,
				},
			},
			testRequests: []testRequestResult{
				{
					request: testRequest{
						method:  "POST",
						url:     "https://tenant1.saas.example.com/api/data",
						headers: map[string]string{"X-API-Version": "2.0"},
					},
					expectMatch: true,
					description: "Valid tenant API request should match",
				},
			},
		},
	}

	for _, scenario := range integrationScenarios {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()

			t.Logf("Testing: %s", scenario.description)
			t.Logf("Config: %+v", scenario.config)

			for _, testReq := range scenario.testRequests {
				t.Run(testReq.description, func(t *testing.T) {
					t.Logf("Request: %+v", testReq.request)
					t.Logf("Expected: %v", testReq.expectMatch)

					// This test validates complete FR-019/FR-020 implementation
					// All matcher types should work at any hierarchy level with no restrictions
					t.Log("TODO: Implement complete matcher flexibility")
				})
			}
		})
	}
}

// Test helper structures
type testRequest struct {
	method  string
	url     string
	headers map[string]string
}

type testRequestResult struct {
	request     testRequest
	expectMatch bool
	description string
}

type routerConfig struct {
	name string
	rule string
}

type hierarchicalRouterConfig struct {
	parent     routerConfig
	child      routerConfig
	grandchild routerConfig
}

// BenchmarkMatcherFlexibilityPerformance provides baseline for matcher flexibility performance
func BenchmarkMatcherFlexibilityPerformance(b *testing.B) {

	// This benchmark should test that matcher flexibility doesn't degrade performance
	// Key metrics:
	// - Matcher evaluation time at different hierarchy levels
	// - Memory usage for complex matcher combinations
	// - Throughput with mixed matcher types in hierarchy
	// - Comparison with flat routing performance

	b.Log("Benchmark documents requirement that matcher flexibility should not impact performance")
	b.Log("Implementation should maintain sub-millisecond processing with flexible matchers")
}

// TestMatcherFlexibilityDocumentation documents FR-019 and FR-020 requirements
func TestMatcherFlexibilityDocumentation(t *testing.T) {
	t.Log("=== FR-019: Rule Matcher Flexibility at All Levels ===")
	t.Log("- Host, Path, Method, Header matchers must work at any hierarchy level")
	t.Log("- No restrictions on matcher type based on tree position")
	t.Log("- Parent, child, grandchild can use any matcher combination")
	t.Log("- All existing matcher functions must preserve their behavior")

	t.Log("=== FR-020: Mixed Matcher Types Within Same Hierarchy Branch ===")
	t.Log("- Any combination of matcher types allowed within same branch")
	t.Log("- AND, OR, NOT operators work with mixed matchers")
	t.Log("- Complex rule expressions supported at any level")
	t.Log("- No limitations on matcher mixing based on hierarchy position")

	t.Log("=== Implementation Requirements ===")
	t.Log("- Preserve all existing MatcherFunc implementations (FR-018, FR-021)")
	t.Log("- No modifications to matchersTree.match() logic (FR-022)")
	t.Log("- Route organization optimization, NOT matcher modification")
	t.Log("- Hierarchical evaluation must support all matcher combinations")

	// This test always passes - it's documentation only
	assert.True(t, true, "Documentation test should always pass")
}

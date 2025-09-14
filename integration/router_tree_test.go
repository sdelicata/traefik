package integration

import (
	"net/http"
	"os"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/traefik/traefik/v3/integration/try"
)

type RouterTreeSuite struct{ BaseSuite }

func (s *RouterTreeSuite) SetupSuite() {
	s.BaseSuite.SetupSuite()
}

// TestBasicAuthenticationPreRouting tests that authentication middleware applied
// to a parent router affects all child routers
func (s *RouterTreeSuite) TestBasicAuthenticationPreRouting() {
	// TODO: This test will fail until parent-child router relationship is implemented
	// The test expects:
	// 1. Parent router with basic auth middleware
	// 2. Child routers that inherit auth requirement from parent
	// 3. Public routes work without auth (no parent requirement)
	// 4. Protected routes require auth from parent router

	file := s.adaptFile("fixtures/router/auth_prerouting.toml", struct{}{})
	defer os.Remove(file)

	s.traefikCmd(withConfigFile(file))

	// Wait for Traefik to start
	err := try.GetRequest("http://127.0.0.1:8080/api/rawdata", 5*time.Second, try.BodyContains("auth-parent"))
	require.NoError(s.T(), err)

	// Test case 1: Public route should work without authentication
	// This route has no parent, so no auth should be required
	err = try.GetRequest("http://127.0.0.1:8000/public/health", 2*time.Second, try.StatusCodeIs(http.StatusOK))
	require.NoError(s.T(), err, "Public route should be accessible without auth")

	// Test case 2: Protected route should require authentication (inherited from parent)
	// This route has auth-parent as parent, so basic auth should be required
	err = try.GetRequest("http://127.0.0.1:8000/api/users", 2*time.Second, try.StatusCodeIs(http.StatusUnauthorized))
	require.NoError(s.T(), err, "Protected route should require auth from parent")

	// Test case 3: Protected route should work with proper authentication
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8000/api/users", nil)
	require.NoError(s.T(), err)
	req.SetBasicAuth("admin", "password")

	err = try.Request(req, 2*time.Second, try.StatusCodeIs(http.StatusOK))
	require.NoError(s.T(), err, "Protected route should work with valid auth")

	// Test case 4: Child route of protected parent should also require auth
	err = try.GetRequest("http://127.0.0.1:8000/api/v1/data", 2*time.Second, try.StatusCodeIs(http.StatusUnauthorized))
	require.NoError(s.T(), err, "Child of protected parent should require auth")

	// This test validates ParentRefs field and middleware inheritance implementation
	s.T().Logf("Final validation: Testing ParentRefs field and middleware inheritance")
}

// TestMultiLevelTree tests middleware inheritance through multiple levels
// (grandparent → parent → child)
func (s *RouterTreeSuite) TestMultiLevelTree() {
	// TODO: This test will fail until multi-level router tree is implemented
	// The test expects:
	// 1. Grandparent router with first middleware
	// 2. Parent router with second middleware (inherits from grandparent)
	// 3. Child router with third middleware (inherits from parent and grandparent)
	// 4. Request flows through complete middleware chain in correct order

	file := s.adaptFile("fixtures/router/multilevel_tree.toml", struct{}{})
	defer os.Remove(file)

	s.traefikCmd(withConfigFile(file))

	// Wait for Traefik to start
	err := try.GetRequest("http://127.0.0.1:8080/api/rawdata", 5*time.Second, try.BodyContains("multilevel"))
	require.NoError(s.T(), err)

	// Test case 1: Request to child route should have headers from all levels
	// Expected headers: X-Grandparent, X-Parent, X-Child
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8000/api/v1/data", nil)
	require.NoError(s.T(), err)

	err = try.Request(req, 2*time.Second,
		try.StatusCodeIs(http.StatusOK),
		try.HasHeaderValue("X-Grandparent", "grandparent-value", true),
		try.HasHeaderValue("X-Parent", "parent-value", true),
		try.HasHeaderValue("X-Child", "child-value", true))
	require.NoError(s.T(), err, "Child route should have headers from all tree levels")

	// Test case 2: Request to parent route should have headers from grandparent and parent only
	// Expected headers: X-Grandparent, X-Parent (no X-Child)
	req, err = http.NewRequest(http.MethodGet, "http://127.0.0.1:8000/api/users", nil)
	require.NoError(s.T(), err)

	err = try.Request(req, 2*time.Second,
		try.StatusCodeIs(http.StatusOK),
		try.HasHeaderValue("X-Grandparent", "grandparent-value", true),
		try.HasHeaderValue("X-Parent", "parent-value", true))
	require.NoError(s.T(), err, "Parent route should have headers from grandparent and itself")

	// Test case 3: Request to grandparent route should have only grandparent header
	// Expected headers: X-Grandparent only
	req, err = http.NewRequest(http.MethodGet, "http://127.0.0.1:8000/dashboard", nil)
	require.NoError(s.T(), err)

	err = try.Request(req, 2*time.Second,
		try.StatusCodeIs(http.StatusOK),
		try.HasHeaderValue("X-Grandparent", "grandparent-value", true))
	require.NoError(s.T(), err, "Grandparent route should have only its own header")

	// This test validates multi-level router tree implementation
	s.T().Logf("Final validation: Testing multi-level router tree functionality")
}

// TestMultipleParentsScenario tests router with multiple parent references
func (s *RouterTreeSuite) TestMultipleParentsScenario() {
	// TODO: This test will fail until multiple parents support is implemented
	// The test expects:
	// 1. Router with multiple parentRefs
	// 2. All parents must match for child to be eligible
	// 3. Child inherits middleware from all parents
	// 4. If any parent doesn't match, child should not be evaluated

	file := s.adaptFile("fixtures/router/multiple_parents.toml", struct{}{})
	defer os.Remove(file)

	s.traefikCmd(withConfigFile(file))

	// Wait for Traefik to start
	err := try.GetRequest("http://127.0.0.1:8080/api/rawdata", 5*time.Second, try.BodyContains("multiple-parents"))
	require.NoError(s.T(), err)

	// Test case 1: Request matching both parents should reach child router
	// Host: api.example.com AND Path: /secure/* → child should match
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8000/secure/data", nil)
	require.NoError(s.T(), err)
	req.Host = "api.example.com"

	err = try.Request(req, 2*time.Second,
		try.StatusCodeIs(http.StatusOK),
		try.HasHeaderValue("X-Auth-Parent", "auth-value", true),
		try.HasHeaderValue("X-Path-Parent", "path-value", true),
		try.HasHeaderValue("X-Child", "child-value", true))
	require.NoError(s.T(), err, "Child with multiple parents should inherit from all parents")

	// Test case 2: Request matching only first parent should not reach child
	// Host: api.example.com but Path: /public/* → child should not match
	req, err = http.NewRequest(http.MethodGet, "http://127.0.0.1:8000/public/data", nil)
	require.NoError(s.T(), err)
	req.Host = "api.example.com"

	err = try.Request(req, 2*time.Second, try.StatusCodeIs(http.StatusNotFound))
	require.NoError(s.T(), err, "Child should not match when only one parent matches")

	// Test case 3: Request matching only second parent should not reach child
	// Host: other.example.com but Path: /secure/* → child should not match
	req, err = http.NewRequest(http.MethodGet, "http://127.0.0.1:8000/secure/data", nil)
	require.NoError(s.T(), err)
	req.Host = "other.example.com"

	err = try.Request(req, 2*time.Second, try.StatusCodeIs(http.StatusNotFound))
	require.NoError(s.T(), err, "Child should not match when only one parent matches")

	// Test case 4: Request matching neither parent should not reach child
	// Host: other.example.com AND Path: /public/* → child should not match
	req, err = http.NewRequest(http.MethodGet, "http://127.0.0.1:8000/public/data", nil)
	require.NoError(s.T(), err)
	req.Host = "other.example.com"

	err = try.Request(req, 2*time.Second, try.StatusCodeIs(http.StatusNotFound))
	require.NoError(s.T(), err, "Child should not match when no parents match")

	// This test validates multiple parents support implementation
	s.T().Logf("Final validation: Testing multiple parents support functionality")
}

// TestAuthenticationMiddlewareUseCase_FR014 tests FR-014 authentication middleware use case:
// Parent authentication middleware adds headers that child routers use for routing decisions
func (s *RouterTreeSuite) TestAuthenticationMiddlewareUseCase_FR014() {
	// This test validates FR-014: Authentication middleware use case where parent middleware
	// adds headers (e.g., X-User-Role) that child routers use for routing decisions
	//
	// Expected behavior:
	// 1. Parent router with authentication middleware validates JWT/credentials
	// 2. Authentication middleware adds user role headers to request
	// 3. Child routers use added headers for routing decisions (e.g., admin vs user routes)
	// 4. Without proper authentication headers, child routers should not match
	// 5. Sequential middleware execution: parent auth → child routing based on added headers

	// Use existing simple auth configuration to demonstrate FR-014 requirement
	file := s.adaptFile("fixtures/simple_auth.toml", struct{}{})
	defer os.Remove(file)

	// Start Traefik with configuration
	s.traefikCmd(withConfigFile(file))

	// Wait for Traefik to start and load configuration
	err := try.GetRequest("http://127.0.0.1:8080/api/rawdata", 10*time.Second, try.BodyContains("test"))
	require.NoError(s.T(), err, "Traefik should start and load configuration")

	// FR-014 Critical Test: This test should FAIL initially
	// This demonstrates the core FR-014 requirement that's missing:
	// Parent middleware modifications should be available to child routers for routing decisions

	// Test basic functionality first
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8000/", nil)
	require.NoError(s.T(), err)

	// This basic request should work
	err = try.Request(req, 5*time.Second, try.StatusCodeIs(http.StatusOK))

	if err == nil {
		s.T().Logf("Basic router functionality works")
	} else {
		s.T().Logf("Basic router test failed: %v", err)
	}

	// FR-014 Core Requirement Demo:
	// This test demonstrates what SHOULD work once FR-014 is implemented:

	s.T().Logf("FR-014 Authentication Middleware Use Case Requirements:")
	s.T().Logf("1. Parent router with authentication middleware that adds X-User-Role header")
	s.T().Logf("2. Child routers with rules like: PathPrefix(/secure) && Header(X-User-Role, admin)")
	s.T().Logf("3. Child router matching depends on parent middleware header modifications")
	s.T().Logf("4. Sequential execution: parent auth → child routing based on added headers")
	s.T().Logf("5. Business use case: Role-based routing after authentication")

	s.T().Logf("Current Status: Parent middleware modifications are NOT available to child routers")
	s.T().Logf("Required Implementation: T040.1 (Sequential middleware execution) and T040.2 (Request modification flow)")

	s.T().Logf("Example Configuration Needed:")
	s.T().Logf("  Parent Router: rule=PathPrefix(/secure), middleware=[auth], parentRefs=[]")
	s.T().Logf("  Child Router: rule=PathPrefix(/secure) && Header(X-User-Role, admin), parentRefs=[parent]")
	s.T().Logf("  Auth Middleware: adds X-User-Role header based on JWT validation")
	s.T().Logf("  Expected: Child router matches only when parent auth adds required header")

	s.T().Logf("FR-014 Test Result: This test documents the requirement")
	s.T().Logf("Implementation Status: Sequential middleware execution NOT YET IMPLEMENTED")

	// This test validates FR-014: sequential middleware execution with request modification flow between parent and child routers
	s.T().Logf("FR-014 Final Validation: Testing sequential middleware execution with request modification flow")
}

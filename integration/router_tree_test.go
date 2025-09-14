package integration

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/traefik/traefik/v3/integration/try"
)

type RouterTreeSuite struct{ BaseSuite }

func (s *RouterTreeSuite) SetupSuite() {
	s.BaseSuite.SetupSuite()
}

// TestBasicAuthenticationPreRouting tests parent-child authentication inheritance
func (s *RouterTreeSuite) TestBasicAuthenticationPreRouting() {
	file := s.adaptFile("fixtures/router/auth_prerouting.toml", struct{}{})
	defer os.Remove(file)

	s.traefikCmd(withConfigFile(file))

	// Wait for Traefik to start
	err := try.GetRequest("http://127.0.0.1:8080/api/rawdata", 5*time.Second, try.BodyContains("auth-parent"))
	require.NoError(s.T(), err)

	// Public route should work without authentication
	err = try.GetRequest("http://127.0.0.1:8000/public/health", 2*time.Second, try.StatusCodeIs(http.StatusOK))
	require.NoError(s.T(), err, "Public route should be accessible without auth")

	// Protected route should require authentication from parent
	err = try.GetRequest("http://127.0.0.1:8000/api/users", 2*time.Second, try.StatusCodeIs(http.StatusUnauthorized))
	require.NoError(s.T(), err, "Protected route should require auth from parent")

	// Protected route should work with proper authentication
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8000/api/users", nil)
	require.NoError(s.T(), err)
	req.SetBasicAuth("admin", "password")

	err = try.Request(req, 2*time.Second, try.StatusCodeIs(http.StatusOK))
	require.NoError(s.T(), err, "Protected route should work with valid auth")

	// Child route should also inherit auth requirement
	err = try.GetRequest("http://127.0.0.1:8000/api/v1/data", 2*time.Second, try.StatusCodeIs(http.StatusUnauthorized))
	require.NoError(s.T(), err, "Child of protected parent should require auth")
}

// TestAdvancedRouterHierarchies tests complex router tree scenarios including
// multi-level inheritance and multiple parent references
func (s *RouterTreeSuite) TestAdvancedRouterHierarchies() {
	s.T().Log("Testing advanced router hierarchy scenarios")

	// Test 1: Multi-level middleware inheritance (grandparent → parent → child)
	s.T().Run("MultiLevelInheritance", func(t *testing.T) {
		file := s.adaptFile("fixtures/router/multilevel_tree.toml", struct{}{})
		defer os.Remove(file)

		s.traefikCmd(withConfigFile(file))

		// Wait for Traefik to start
		err := try.GetRequest("http://127.0.0.1:8080/api/rawdata", 5*time.Second, try.BodyContains("multilevel"))
		require.NoError(s.T(), err)

		// Test middleware inheritance at child level
		req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8000/api/v1/data", nil)
		require.NoError(s.T(), err)

		err = try.Request(req, 2*time.Second,
			try.StatusCodeIs(http.StatusOK),
			try.HasHeaderValue("X-Grandparent", "grandparent-value", true),
			try.HasHeaderValue("X-Parent", "parent-value", true),
			try.HasHeaderValue("X-Child", "child-value", true))
		require.NoError(s.T(), err, "Child should inherit middleware from all tree levels")
	})

	// Test 2: Multiple parent references scenario
	s.T().Run("MultipleParents", func(t *testing.T) {
		file := s.adaptFile("fixtures/router/multiple_parents.toml", struct{}{})
		defer os.Remove(file)

		s.traefikCmd(withConfigFile(file))

		// Wait for Traefik to start
		err := try.GetRequest("http://127.0.0.1:8080/api/rawdata", 5*time.Second, try.BodyContains("multiple-parents"))
		require.NoError(s.T(), err)

		// Test case: Both parents match → child should be accessible
		req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8000/secure/data", nil)
		require.NoError(s.T(), err)
		req.Host = "api.example.com"

		err = try.Request(req, 2*time.Second,
			try.StatusCodeIs(http.StatusOK),
			try.HasHeaderValue("X-Auth-Parent", "auth-value", true),
			try.HasHeaderValue("X-Path-Parent", "path-value", true))
		require.NoError(s.T(), err, "Child with multiple parents should inherit from all")

		// Test case: Only one parent matches → child should not be accessible
		req2, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8000/public/data", nil)
		require.NoError(s.T(), err)
		req2.Host = "api.example.com"

		err = try.Request(req2, 2*time.Second, try.StatusCodeIs(http.StatusNotFound))
		require.NoError(s.T(), err, "Child should not match when only one parent matches")
	})

	s.T().Logf("Advanced hierarchy testing completed")
}

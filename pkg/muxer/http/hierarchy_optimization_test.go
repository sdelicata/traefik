package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// BenchmarkBasicHierarchyPerformance benchmarks basic hierarchical routing performance
func BenchmarkBasicHierarchyPerformance(b *testing.B) {
	testCases := []struct {
		name        string
		routes      int
		requestPath string
	}{
		{
			name:        "small_hierarchy_10_routes",
			routes:      10,
			requestPath: "/api/v1/users",
		},
		{
			name:        "medium_hierarchy_100_routes",
			routes:      100,
			requestPath: "/api/v2/products",
		},
		{
			name:        "large_hierarchy_1000_routes",
			routes:      1000,
			requestPath: "/admin/v3/reports",
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			parser, err := NewSyntaxParser()
			if err != nil {
				b.Fatal(err)
			}

			mux := NewMuxer(parser)
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// Add test routes
			for i := 0; i < tc.routes; i++ {
				rule := "PathPrefix(`/test-" + string(rune('0'+i%10)) + "`)"
				err := mux.AddRoute(rule, "v3", i, handler)
				if err != nil {
					b.Fatal(err)
				}
			}

			req := httptest.NewRequest("GET", "http://example.com"+tc.requestPath, nil)
			rec := httptest.NewRecorder()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				mux.ServeHTTP(rec, req)
			}

			b.ReportMetric(float64(tc.routes), "total-routes")
		})
	}
}

// BenchmarkRouteEvaluationEfficiency benchmarks route evaluation efficiency
func BenchmarkRouteEvaluationEfficiency(b *testing.B) {
	parser, err := NewSyntaxParser()
	if err != nil {
		b.Fatal(err)
	}

	mux := NewMuxer(parser)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Add hierarchical-style routes
	routes := []string{
		"PathPrefix(`/api`)",
		"PathPrefix(`/api/v1`)",
		"PathPrefix(`/api/v2`)",
		"Path(`/api/v1/users`)",
		"Path(`/api/v2/products`)",
		"PathPrefix(`/admin`)",
		"Path(`/admin/reports`)",
	}

	for i, rule := range routes {
		err := mux.AddRoute(rule, "v3", i*100, handler)
		if err != nil {
			b.Fatal(err)
		}
	}

	testPaths := []string{
		"/api/v1/users",
		"/api/v2/products",
		"/admin/reports",
		"/nomatch/path",
	}

	for _, path := range testPaths {
		b.Run("path_"+path, func(b *testing.B) {
			req := httptest.NewRequest("GET", "http://example.com"+path, nil)
			rec := httptest.NewRecorder()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				mux.ServeHTTP(rec, req)
			}
		})
	}
}

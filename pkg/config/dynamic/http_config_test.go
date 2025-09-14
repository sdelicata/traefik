package dynamic

import (
	"encoding/json"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestRouterParentRefsSerialization(t *testing.T) {
	testCases := []struct {
		name     string
		router   Router
		expected map[string]interface{}
	}{
		{
			name: "Router with single parent reference",
			router: Router{
				Rule:       "Host(`test.example.com`)",
				ParentRefs: []string{"parent-router"},
			},
			expected: map[string]interface{}{
				"rule":       "Host(`test.example.com`)",
				"parentRefs": []interface{}{"parent-router"},
			},
		},
		{
			name: "Router with multiple parent references",
			router: Router{
				Rule:       "Host(`test.example.com`)",
				ParentRefs: []string{"parent-router-1", "parent-router-2"},
			},
			expected: map[string]interface{}{
				"rule":       "Host(`test.example.com`)",
				"parentRefs": []interface{}{"parent-router-1", "parent-router-2"},
			},
		},
		{
			name: "Router with empty parent references (should be omitted)",
			router: Router{
				Rule:       "Host(`test.example.com`)",
				ParentRefs: []string{},
			},
			expected: map[string]interface{}{
				"rule": "Host(`test.example.com`)",
			},
		},
		{
			name: "Router with nil parent references (should be omitted)",
			router: Router{
				Rule: "Host(`test.example.com`)",
			},
			expected: map[string]interface{}{
				"rule": "Host(`test.example.com`)",
			},
		},
	}

	t.Run("JSON Serialization", func(t *testing.T) {
		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// This test should fail initially because ParentRefs field doesn't exist yet
				jsonData, err := json.Marshal(tc.router)
				require.NoError(t, err)

				var result map[string]interface{}
				err = json.Unmarshal(jsonData, &result)
				require.NoError(t, err)

				// Remove fields that are not part of our test expectations
				for key := range result {
					if _, exists := tc.expected[key]; !exists && key != "parentRefs" {
						delete(result, key)
					}
				}

				assert.Equal(t, tc.expected, result)
			})
		}
	})

	t.Run("YAML Serialization", func(t *testing.T) {
		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// This test should fail initially because ParentRefs field doesn't exist yet
				yamlData, err := yaml.Marshal(tc.router)
				require.NoError(t, err)

				var result map[string]interface{}
				err = yaml.Unmarshal(yamlData, &result)
				require.NoError(t, err)

				// Remove fields that are not part of our test expectations
				for key := range result {
					if _, exists := tc.expected[key]; !exists && key != "parentRefs" {
						delete(result, key)
					}
				}

				assert.Equal(t, tc.expected, result)
			})
		}
	})

	t.Run("TOML Serialization", func(t *testing.T) {
		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// For TOML, we need to wrap the router in a struct since TOML requires a top-level table
				wrapper := struct {
					Router Router `toml:"router"`
				}{Router: tc.router}

				// This test should fail initially because ParentRefs field doesn't exist yet
				tomlData, err := toml.Marshal(wrapper)
				require.NoError(t, err)

				var result struct {
					Router map[string]interface{} `toml:"router"`
				}
				err = toml.Unmarshal(tomlData, &result)
				require.NoError(t, err)

				// Remove fields that are not part of our test expectations
				for key := range result.Router {
					if _, exists := tc.expected[key]; !exists && key != "parentRefs" {
						delete(result.Router, key)
					}
				}

				assert.Equal(t, tc.expected, result.Router)
			})
		}
	})
}

func TestRouterParentRefsDeserialization(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		format   string
		expected Router
	}{
		{
			name:   "JSON with single parent reference",
			format: "json",
			input:  `{"rule": "Host(` + "`test.example.com`" + `)", "parentRefs": ["parent-router"]}`,
			expected: Router{
				Rule:       "Host(`test.example.com`)",
				ParentRefs: []string{"parent-router"},
			},
		},
		{
			name:   "JSON with multiple parent references",
			format: "json",
			input:  `{"rule": "Host(` + "`test.example.com`" + `)", "parentRefs": ["parent-router-1", "parent-router-2"]}`,
			expected: Router{
				Rule:       "Host(`test.example.com`)",
				ParentRefs: []string{"parent-router-1", "parent-router-2"},
			},
		},
		{
			name:   "JSON without parent references",
			format: "json",
			input:  `{"rule": "Host(` + "`test.example.com`" + `)"}`,
			expected: Router{
				Rule: "Host(`test.example.com`)",
			},
		},
		{
			name:   "YAML with parent references",
			format: "yaml",
			input: `rule: "Host(` + "`test.example.com`" + `)"
parentRefs:
  - "parent-router-1"
  - "parent-router-2"`,
			expected: Router{
				Rule:       "Host(`test.example.com`)",
				ParentRefs: []string{"parent-router-1", "parent-router-2"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var router Router

			switch tc.format {
			case "json":
				// This test should fail initially because ParentRefs field doesn't exist yet
				err := json.Unmarshal([]byte(tc.input), &router)
				require.NoError(t, err)
			case "yaml":
				// This test should fail initially because ParentRefs field doesn't exist yet
				err := yaml.Unmarshal([]byte(tc.input), &router)
				require.NoError(t, err)
			}

			assert.Equal(t, tc.expected.Rule, router.Rule)
			assert.Equal(t, tc.expected.ParentRefs, router.ParentRefs)
		})
	}
}

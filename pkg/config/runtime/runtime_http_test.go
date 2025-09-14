package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/traefik/traefik/v3/pkg/config/dynamic"
)

func TestGetRoutersByEntryPoints(t *testing.T) {
	testCases := []struct {
		desc        string
		conf        dynamic.Configuration
		entryPoints []string
		expected    map[string]map[string]*RouterInfo
	}{
		{
			desc:        "Empty Configuration without entrypoint",
			conf:        dynamic.Configuration{},
			entryPoints: []string{""},
			expected:    map[string]map[string]*RouterInfo{},
		},
		{
			desc:        "Empty Configuration with unknown entrypoints",
			conf:        dynamic.Configuration{},
			entryPoints: []string{"foo"},
			expected:    map[string]map[string]*RouterInfo{},
		},
		{
			desc: "Valid configuration with an unknown entrypoint",
			conf: dynamic.Configuration{
				HTTP: &dynamic.HTTPConfiguration{
					Routers: map[string]*dynamic.Router{
						"foo": {
							EntryPoints: []string{"web"},
							Service:     "foo-service@myprovider",
							Rule:        "Host(`bar.foo`)",
						},
					},
				},
				TCP: &dynamic.TCPConfiguration{
					Routers: map[string]*dynamic.TCPRouter{
						"foo": {
							EntryPoints: []string{"web"},
							Service:     "foo-service@myprovider",
							Rule:        "HostSNI(`bar.foo`)",
						},
					},
				},
			},
			entryPoints: []string{"foo"},
			expected:    map[string]map[string]*RouterInfo{},
		},
		{
			desc: "Valid configuration with a known entrypoint",
			conf: dynamic.Configuration{
				HTTP: &dynamic.HTTPConfiguration{
					Routers: map[string]*dynamic.Router{
						"foo": {
							EntryPoints: []string{"web"},
							Service:     "foo-service@myprovider",
							Rule:        "Host(`bar.foo`)",
						},
						"bar": {
							EntryPoints: []string{"webs"},
							Service:     "bar-service@myprovider",
							Rule:        "Host(`foo.bar`)",
						},
						"foobar": {
							EntryPoints: []string{"web", "webs"},
							Service:     "foobar-service@myprovider",
							Rule:        "Host(`bar.foobar`)",
						},
					},
				},
				TCP: &dynamic.TCPConfiguration{
					Routers: map[string]*dynamic.TCPRouter{
						"foo": {
							EntryPoints: []string{"web"},
							Service:     "foo-service@myprovider",
							Rule:        "HostSNI(`bar.foo`)",
						},
						"bar": {
							EntryPoints: []string{"webs"},
							Service:     "bar-service@myprovider",
							Rule:        "HostSNI(`foo.bar`)",
						},
						"foobar": {
							EntryPoints: []string{"web", "webs"},
							Service:     "foobar-service@myprovider",
							Rule:        "HostSNI(`bar.foobar`)",
						},
					},
				},
			},
			entryPoints: []string{"web"},
			expected: map[string]map[string]*RouterInfo{
				"web": {
					"foo": {
						Router: &dynamic.Router{
							EntryPoints: []string{"web"},
							Service:     "foo-service@myprovider",
							Rule:        "Host(`bar.foo`)",
						},
						Status: "enabled",
						Using:  []string{"web"},
					},
					"foobar": {
						Router: &dynamic.Router{
							EntryPoints: []string{"web", "webs"},
							Service:     "foobar-service@myprovider",
							Rule:        "Host(`bar.foobar`)",
						},
						Status: "warning",
						Err:    []string{`entryPoint "webs" doesn't exist`},
						Using:  []string{"web"},
					},
				},
			},
		},
		{
			desc: "Valid configuration with multiple known entrypoints",
			conf: dynamic.Configuration{
				HTTP: &dynamic.HTTPConfiguration{
					Routers: map[string]*dynamic.Router{
						"foo": {
							EntryPoints: []string{"web"},
							Service:     "foo-service@myprovider",
							Rule:        "Host(`bar.foo`)",
						},
						"bar": {
							EntryPoints: []string{"webs"},
							Service:     "bar-service@myprovider",
							Rule:        "Host(`foo.bar`)",
						},
						"foobar": {
							EntryPoints: []string{"web", "webs"},
							Service:     "foobar-service@myprovider",
							Rule:        "Host(`bar.foobar`)",
						},
					},
				},
				TCP: &dynamic.TCPConfiguration{
					Routers: map[string]*dynamic.TCPRouter{
						"foo": {
							EntryPoints: []string{"web"},
							Service:     "foo-service@myprovider",
							Rule:        "HostSNI(`bar.foo`)",
						},
						"bar": {
							EntryPoints: []string{"webs"},
							Service:     "bar-service@myprovider",
							Rule:        "HostSNI(`foo.bar`)",
						},
						"foobar": {
							EntryPoints: []string{"web", "webs"},
							Service:     "foobar-service@myprovider",
							Rule:        "HostSNI(`bar.foobar`)",
						},
					},
				},
			},
			entryPoints: []string{"web", "webs"},
			expected: map[string]map[string]*RouterInfo{
				"web": {
					"foo": {
						Router: &dynamic.Router{
							EntryPoints: []string{"web"},
							Service:     "foo-service@myprovider",
							Rule:        "Host(`bar.foo`)",
						},
						Status: "enabled",
						Using:  []string{"web"},
					},
					"foobar": {
						Router: &dynamic.Router{
							EntryPoints: []string{"web", "webs"},
							Service:     "foobar-service@myprovider",
							Rule:        "Host(`bar.foobar`)",
						},
						Status: "enabled",
						Using:  []string{"web", "webs"},
					},
				},
				"webs": {
					"bar": {
						Router: &dynamic.Router{
							EntryPoints: []string{"webs"},
							Service:     "bar-service@myprovider",
							Rule:        "Host(`foo.bar`)",
						},
						Status: "enabled",
						Using:  []string{"webs"},
					},
					"foobar": {
						Router: &dynamic.Router{
							EntryPoints: []string{"web", "webs"},
							Service:     "foobar-service@myprovider",
							Rule:        "Host(`bar.foobar`)",
						},
						Status: "enabled",
						Using:  []string{"web", "webs"},
					},
				},
			},
		},
	}

	for _, test := range testCases {
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()
			runtimeConfig := NewConfig(test.conf)
			actual := runtimeConfig.GetRoutersByEntryPoints(t.Context(), test.entryPoints, false)
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestValidateRouterParentRefs(t *testing.T) {
	testCases := []struct {
		desc         string
		conf         dynamic.Configuration
		expectedErrs map[string][]string // router name -> list of expected errors
	}{
		{
			desc: "Valid parentRefs - single parent reference",
			conf: dynamic.Configuration{
				HTTP: &dynamic.HTTPConfiguration{
					Routers: map[string]*dynamic.Router{
						"parent-router": {
							EntryPoints: []string{"web"},
							Service:     "parent-service",
							Rule:        "Host(`parent.example.com`)",
						},
						"child-router": {
							EntryPoints: []string{"web"},
							Service:     "child-service",
							Rule:        "Host(`child.example.com`)",
							ParentRefs:  []string{"parent-router"},
						},
					},
				},
			},
			expectedErrs: map[string][]string{},
		},
		{
			desc: "Valid parentRefs - multiple parent references",
			conf: dynamic.Configuration{
				HTTP: &dynamic.HTTPConfiguration{
					Routers: map[string]*dynamic.Router{
						"parent-router-1": {
							EntryPoints: []string{"web"},
							Service:     "parent-service-1",
							Rule:        "Host(`parent1.example.com`)",
						},
						"parent-router-2": {
							EntryPoints: []string{"web"},
							Service:     "parent-service-2",
							Rule:        "Host(`parent2.example.com`)",
						},
						"child-router": {
							EntryPoints: []string{"web"},
							Service:     "child-service",
							Rule:        "Host(`child.example.com`)",
							ParentRefs:  []string{"parent-router-1", "parent-router-2"},
						},
					},
				},
			},
			expectedErrs: map[string][]string{},
		},
		{
			desc: "Invalid parentRefs - non-existent parent router",
			conf: dynamic.Configuration{
				HTTP: &dynamic.HTTPConfiguration{
					Routers: map[string]*dynamic.Router{
						"child-router": {
							EntryPoints: []string{"web"},
							Service:     "child-service",
							Rule:        "Host(`child.example.com`)",
							ParentRefs:  []string{"non-existent-parent"},
						},
					},
				},
			},
			expectedErrs: map[string][]string{
				"child-router": {`parent router "non-existent-parent" does not exist`},
			},
		},
		{
			desc: "Invalid parentRefs - self-reference",
			conf: dynamic.Configuration{
				HTTP: &dynamic.HTTPConfiguration{
					Routers: map[string]*dynamic.Router{
						"self-referencing-router": {
							EntryPoints: []string{"web"},
							Service:     "self-service",
							Rule:        "Host(`self.example.com`)",
							ParentRefs:  []string{"self-referencing-router"},
						},
					},
				},
			},
			expectedErrs: map[string][]string{
				"self-referencing-router": {"router cannot reference itself as parent"},
			},
		},
		{
			desc: "Invalid parentRefs - mixed valid and invalid references",
			conf: dynamic.Configuration{
				HTTP: &dynamic.HTTPConfiguration{
					Routers: map[string]*dynamic.Router{
						"parent-router": {
							EntryPoints: []string{"web"},
							Service:     "parent-service",
							Rule:        "Host(`parent.example.com`)",
						},
						"child-router": {
							EntryPoints: []string{"web"},
							Service:     "child-service",
							Rule:        "Host(`child.example.com`)",
							ParentRefs:  []string{"parent-router", "non-existent-parent", "child-router"},
						},
					},
				},
			},
			expectedErrs: map[string][]string{
				"child-router": {
					`parent router "non-existent-parent" does not exist`,
					"router cannot reference itself as parent",
				},
			},
		},
		{
			desc: "No parentRefs - should pass validation",
			conf: dynamic.Configuration{
				HTTP: &dynamic.HTTPConfiguration{
					Routers: map[string]*dynamic.Router{
						"simple-router": {
							EntryPoints: []string{"web"},
							Service:     "simple-service",
							Rule:        "Host(`simple.example.com`)",
						},
					},
				},
			},
			expectedErrs: map[string][]string{},
		},
		{
			desc: "Empty parentRefs - should pass validation",
			conf: dynamic.Configuration{
				HTTP: &dynamic.HTTPConfiguration{
					Routers: map[string]*dynamic.Router{
						"empty-parent-refs-router": {
							EntryPoints: []string{"web"},
							Service:     "empty-service",
							Rule:        "Host(`empty.example.com`)",
							ParentRefs:  []string{},
						},
					},
				},
			},
			expectedErrs: map[string][]string{},
		},
	}

	for _, test := range testCases {
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			// This test should fail initially because parentRefs validation doesn't exist yet
			runtimeConfig := NewConfig(test.conf)

			// Call the validation function that should be implemented in T014
			// This function doesn't exist yet, so the test should fail
			validateRouterParentRefs(runtimeConfig)

			// Check that expected errors are present
			for routerName, expectedErrors := range test.expectedErrs {
				router, exists := runtimeConfig.Routers[routerName]
				assert.True(t, exists, "Router %s should exist", routerName)

				if len(expectedErrors) == 0 {
					assert.Empty(t, router.Err, "Router %s should have no errors", routerName)
					assert.Equal(t, StatusEnabled, router.Status, "Router %s should be enabled", routerName)
				} else {
					assert.Len(t, router.Err, len(expectedErrors), "Router %s should have %d errors", routerName, len(expectedErrors))
					for i, expectedErr := range expectedErrors {
						assert.Contains(t, router.Err[i], expectedErr, "Router %s error %d should contain expected message", routerName, i)
					}
					assert.Equal(t, StatusWarning, router.Status, "Router %s should be in warning state", routerName)
				}
			}

			// Check that routers without expected errors have no errors
			for routerName := range runtimeConfig.Routers {
				if _, hasExpectedErrs := test.expectedErrs[routerName]; !hasExpectedErrs {
					router := runtimeConfig.Routers[routerName]
					assert.Empty(t, router.Err, "Router %s should have no errors", routerName)
					assert.Equal(t, StatusEnabled, router.Status, "Router %s should be enabled", routerName)
				}
			}
		})
	}
}

func TestValidateRouterParentRefsCircularDependency(t *testing.T) {
	testCases := []struct {
		desc         string
		conf         dynamic.Configuration
		expectedErrs map[string][]string // router name -> list of expected errors
	}{
		{
			desc: "No circular dependency - simple tree",
			conf: dynamic.Configuration{
				HTTP: &dynamic.HTTPConfiguration{
					Routers: map[string]*dynamic.Router{
						"grandparent": {
							EntryPoints: []string{"web"},
							Service:     "grandparent-service",
							Rule:        "Host(`grandparent.example.com`)",
						},
						"parent": {
							EntryPoints: []string{"web"},
							Service:     "parent-service",
							Rule:        "Host(`parent.example.com`)",
							ParentRefs:  []string{"grandparent"},
						},
						"child": {
							EntryPoints: []string{"web"},
							Service:     "child-service",
							Rule:        "Host(`child.example.com`)",
							ParentRefs:  []string{"parent"},
						},
					},
				},
			},
			expectedErrs: map[string][]string{},
		},
		{
			desc: "Circular dependency - 2 router cycle",
			conf: dynamic.Configuration{
				HTTP: &dynamic.HTTPConfiguration{
					Routers: map[string]*dynamic.Router{
						"router-a": {
							EntryPoints: []string{"web"},
							Service:     "service-a",
							Rule:        "Host(`a.example.com`)",
							ParentRefs:  []string{"router-b"},
						},
						"router-b": {
							EntryPoints: []string{"web"},
							Service:     "service-b",
							Rule:        "Host(`b.example.com`)",
							ParentRefs:  []string{"router-a"},
						},
					},
				},
			},
			expectedErrs: map[string][]string{
				"router-a": {"circular dependency detected involving routers: router-a, router-b"},
				"router-b": {"circular dependency detected involving routers: router-a, router-b"},
			},
		},
		{
			desc: "Circular dependency - 3 router cycle",
			conf: dynamic.Configuration{
				HTTP: &dynamic.HTTPConfiguration{
					Routers: map[string]*dynamic.Router{
						"router-a": {
							EntryPoints: []string{"web"},
							Service:     "service-a",
							Rule:        "Host(`a.example.com`)",
							ParentRefs:  []string{"router-c"},
						},
						"router-b": {
							EntryPoints: []string{"web"},
							Service:     "service-b",
							Rule:        "Host(`b.example.com`)",
							ParentRefs:  []string{"router-a"},
						},
						"router-c": {
							EntryPoints: []string{"web"},
							Service:     "service-c",
							Rule:        "Host(`c.example.com`)",
							ParentRefs:  []string{"router-b"},
						},
					},
				},
			},
			expectedErrs: map[string][]string{
				"router-a": {"circular dependency detected involving routers: router-a, router-b, router-c"},
				"router-b": {"circular dependency detected involving routers: router-a, router-b, router-c"},
				"router-c": {"circular dependency detected involving routers: router-a, router-b, router-c"},
			},
		},
		{
			desc: "Tree with maximum allowed depth (should pass)",
			conf: dynamic.Configuration{
				HTTP: &dynamic.HTTPConfiguration{
					Routers: map[string]*dynamic.Router{
						"level-0": {
							EntryPoints: []string{"web"},
							Service:     "service-0",
							Rule:        "Host(`level0.example.com`)",
						},
						"level-1": {
							EntryPoints: []string{"web"},
							Service:     "service-1",
							Rule:        "Host(`level1.example.com`)",
							ParentRefs:  []string{"level-0"},
						},
						"level-2": {
							EntryPoints: []string{"web"},
							Service:     "service-2",
							Rule:        "Host(`level2.example.com`)",
							ParentRefs:  []string{"level-1"},
						},
					},
				},
			},
			expectedErrs: map[string][]string{},
		},
		{
			desc: "4-router loop exceeding 3-loop limit",
			conf: dynamic.Configuration{
				HTTP: &dynamic.HTTPConfiguration{
					Routers: map[string]*dynamic.Router{
						"router-a": {
							EntryPoints: []string{"web"},
							Service:     "service-a",
							Rule:        "Host(`a.example.com`)",
							ParentRefs:  []string{"router-d"},
						},
						"router-b": {
							EntryPoints: []string{"web"},
							Service:     "service-b",
							Rule:        "Host(`b.example.com`)",
							ParentRefs:  []string{"router-a"},
						},
						"router-c": {
							EntryPoints: []string{"web"},
							Service:     "service-c",
							Rule:        "Host(`c.example.com`)",
							ParentRefs:  []string{"router-b"},
						},
						"router-d": {
							EntryPoints: []string{"web"},
							Service:     "service-d",
							Rule:        "Host(`d.example.com`)",
							ParentRefs:  []string{"router-c"},
						},
					},
				},
			},
			expectedErrs: map[string][]string{
				"router-a": {"router tree depth exceeds maximum allowed (3)"},
				"router-b": {"router tree depth exceeds maximum allowed (3)"},
				"router-c": {"router tree depth exceeds maximum allowed (3)"},
				"router-d": {"router tree depth exceeds maximum allowed (3)"},
			},
		},
	}

	for _, test := range testCases {
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			// This test should fail initially because circular dependency validation doesn't exist yet
			runtimeConfig := NewConfig(test.conf)

			// Call the validation function that should be implemented in T015
			// This function doesn't exist yet, so the test should fail
			validateRouterCircularDependencies(runtimeConfig)

			// Check that expected errors are present
			for routerName, expectedErrors := range test.expectedErrs {
				router, exists := runtimeConfig.Routers[routerName]
				assert.True(t, exists, "Router %s should exist", routerName)

				if len(expectedErrors) == 0 {
					assert.Empty(t, router.Err, "Router %s should have no errors", routerName)
					assert.Equal(t, StatusEnabled, router.Status, "Router %s should be enabled", routerName)
				} else {
					assert.Len(t, router.Err, len(expectedErrors), "Router %s should have %d errors", routerName, len(expectedErrors))
					for i, expectedErr := range expectedErrors {
						assert.Contains(t, router.Err[i], expectedErr, "Router %s error %d should contain expected message", routerName, i)
					}
					assert.Equal(t, StatusWarning, router.Status, "Router %s should be in warning state", routerName)
				}
			}

			// Check that routers without expected errors have no errors
			for routerName := range runtimeConfig.Routers {
				if _, hasExpectedErrs := test.expectedErrs[routerName]; !hasExpectedErrs {
					router := runtimeConfig.Routers[routerName]
					assert.Empty(t, router.Err, "Router %s should have no errors", routerName)
					assert.Equal(t, StatusEnabled, router.Status, "Router %s should be enabled", routerName)
				}
			}
		})
	}
}

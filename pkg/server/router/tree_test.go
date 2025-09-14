package router

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/traefik/traefik/v3/pkg/config/dynamic"
	"github.com/traefik/traefik/v3/pkg/config/runtime"
)

func TestPopulateTreeInfo(t *testing.T) {
	// Create test routers with tree structure
	dynamicRouters := map[string]*dynamic.Router{
		"parent": {
			Rule:        "Host(`parent.example.com`)",
			Middlewares: []string{"auth"},
		},
		"child": {
			Rule:        "Host(`child.example.com`)",
			ParentRefs:  []string{"parent"},
			Middlewares: []string{"ratelimit"},
		},
		"grandchild": {
			Rule:       "Host(`grandchild.example.com`)",
			ParentRefs: []string{"child"},
		},
	}

	// Create runtime configuration
	runtimeConfig := &runtime.Configuration{
		Routers: map[string]*runtime.RouterInfo{
			"parent": {
				Router: dynamicRouters["parent"],
				Status: runtime.StatusEnabled,
			},
			"child": {
				Router: dynamicRouters["child"],
				Status: runtime.StatusEnabled,
			},
			"grandchild": {
				Router: dynamicRouters["grandchild"],
				Status: runtime.StatusEnabled,
			},
		},
	}

	// Populate tree information
	PopulateTreeInfo(runtimeConfig, dynamicRouters)

	// Verify parent router
	parentInfo := runtimeConfig.Routers["parent"]
	assert.Empty(t, parentInfo.Parents, "Parent should have no parents")
	assert.Equal(t, []string{"child"}, parentInfo.Children, "Parent should have child as child")
	assert.Equal(t, 0, parentInfo.Depth, "Parent should have depth 0")
	assert.Equal(t, []string{"auth"}, parentInfo.EffectiveMiddlewares, "Parent should have its own middleware")

	// Verify child router
	childInfo := runtimeConfig.Routers["child"]
	assert.Equal(t, []string{"parent"}, childInfo.Parents, "Child should have parent as parent")
	assert.Equal(t, []string{"grandchild"}, childInfo.Children, "Child should have grandchild as child")
	assert.Equal(t, 1, childInfo.Depth, "Child should have depth 1")
	assert.Equal(t, []string{"auth", "ratelimit"}, childInfo.EffectiveMiddlewares, "Child should inherit parent middleware + own")

	// Verify grandchild router
	grandchildInfo := runtimeConfig.Routers["grandchild"]
	assert.Equal(t, []string{"child"}, grandchildInfo.Parents, "Grandchild should have child as parent")
	assert.Empty(t, grandchildInfo.Children, "Grandchild should have no children")
	assert.Equal(t, 2, grandchildInfo.Depth, "Grandchild should have depth 2")
	assert.Equal(t, []string{"auth", "ratelimit"}, grandchildInfo.EffectiveMiddlewares, "Grandchild should inherit all ancestor middleware")
}

func TestPopulateTreeInfo_MultipleParents(t *testing.T) {
	// Create test routers with multiple parents
	dynamicRouters := map[string]*dynamic.Router{
		"parent1": {
			Rule:        "Host(`parent1.example.com`)",
			Middlewares: []string{"auth"},
		},
		"parent2": {
			Rule:        "Host(`parent2.example.com`)",
			Middlewares: []string{"cors"},
		},
		"child": {
			Rule:        "Host(`child.example.com`)",
			ParentRefs:  []string{"parent1", "parent2"},
			Middlewares: []string{"ratelimit"},
		},
	}

	// Create runtime configuration
	runtimeConfig := &runtime.Configuration{
		Routers: map[string]*runtime.RouterInfo{
			"parent1": {
				Router: dynamicRouters["parent1"],
				Status: runtime.StatusEnabled,
			},
			"parent2": {
				Router: dynamicRouters["parent2"],
				Status: runtime.StatusEnabled,
			},
			"child": {
				Router: dynamicRouters["child"],
				Status: runtime.StatusEnabled,
			},
		},
	}

	// Populate tree information
	PopulateTreeInfo(runtimeConfig, dynamicRouters)

	// Verify child router with multiple parents
	childInfo := runtimeConfig.Routers["child"]
	assert.ElementsMatch(t, []string{"parent1", "parent2"}, childInfo.Parents, "Child should have both parents")
	assert.Empty(t, childInfo.Children, "Child should have no children")
	assert.Equal(t, 1, childInfo.Depth, "Child should have depth 1")
	// Note: Order might vary since we have multiple parents, but should contain all middleware
	assert.Contains(t, childInfo.EffectiveMiddlewares, "auth", "Child should inherit auth from parent1")
	assert.Contains(t, childInfo.EffectiveMiddlewares, "cors", "Child should inherit cors from parent2")
	assert.Contains(t, childInfo.EffectiveMiddlewares, "ratelimit", "Child should have its own middleware")
}

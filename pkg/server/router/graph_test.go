package router

import (
	"strings"
	"testing"

	"github.com/traefik/traefik/v3/pkg/config/dynamic"
)

func TestCircularDependencyDetection_TwoRouterLoop(t *testing.T) {
	// Test case: A -> B -> A (2-router circular dependency)
	graph := NewRouterGraph()

	routerA := &dynamic.Router{
		Rule:       "Host(`a.example.com`)",
		ParentRefs: []string{"routerB"}, // A depends on B
	}

	routerB := &dynamic.Router{
		Rule:       "Host(`b.example.com`)",
		ParentRefs: []string{"routerA"}, // B depends on A - creates circular dependency
	}

	err := graph.AddRouter("routerA", routerA)
	if err != nil {
		t.Fatalf("Failed to add routerA: %v", err)
	}

	err = graph.AddRouter("routerB", routerB)
	if err != nil {
		t.Fatalf("Failed to add routerB: %v", err)
	}

	// This should detect the circular dependency
	_, err = graph.DetectCircularDependencies()
	if err == nil {
		t.Fatal("Expected circular dependency error for 2-router loop, but got none")
	}

	expectedErr := "circular dependency detected: routerA -> routerB -> routerA"
	if err.Error() != expectedErr {
		t.Errorf("Expected error %q, got %q", expectedErr, err.Error())
	}
}

func TestCircularDependencyDetection_ThreeRouterLoop(t *testing.T) {
	// Test case: A -> B -> C -> A (3-router circular dependency)
	graph := NewRouterGraph()

	routerA := &dynamic.Router{
		Rule:       "Host(`a.example.com`)",
		ParentRefs: []string{"routerC"}, // A depends on C
	}

	routerB := &dynamic.Router{
		Rule:       "Host(`b.example.com`)",
		ParentRefs: []string{"routerA"}, // B depends on A
	}

	routerC := &dynamic.Router{
		Rule:       "Host(`c.example.com`)",
		ParentRefs: []string{"routerB"}, // C depends on B - completes the circle
	}

	err := graph.AddRouter("routerA", routerA)
	if err != nil {
		t.Fatalf("Failed to add routerA: %v", err)
	}

	err = graph.AddRouter("routerB", routerB)
	if err != nil {
		t.Fatalf("Failed to add routerB: %v", err)
	}

	err = graph.AddRouter("routerC", routerC)
	if err != nil {
		t.Fatalf("Failed to add routerC: %v", err)
	}

	// This should detect the circular dependency
	_, err = graph.DetectCircularDependencies()
	if err == nil {
		t.Fatal("Expected circular dependency error for 3-router loop, but got none")
	}

	// Check that error contains circular dependency message and involves all 3 routers
	errMsg := err.Error()
	if !strings.Contains(errMsg, "circular dependency detected") {
		t.Errorf("Expected error to contain 'circular dependency detected', got %q", errMsg)
	}
	// Verify all routers are involved in the cycle
	if !strings.Contains(errMsg, "routerA") || !strings.Contains(errMsg, "routerB") || !strings.Contains(errMsg, "routerC") {
		t.Errorf("Expected error to contain all routers (A, B, C), got %q", errMsg)
	}
}

func TestCircularDependencyDetection_FourRouterLoop_ShouldReject(t *testing.T) {
	// Test case: A -> B -> C -> D -> A (4-router loop, should be rejected due to 3-loop limit)
	graph := NewRouterGraph()

	routerA := &dynamic.Router{
		Rule:       "Host(`a.example.com`)",
		ParentRefs: []string{"routerD"}, // A depends on D
	}

	routerB := &dynamic.Router{
		Rule:       "Host(`b.example.com`)",
		ParentRefs: []string{"routerA"}, // B depends on A
	}

	routerC := &dynamic.Router{
		Rule:       "Host(`c.example.com`)",
		ParentRefs: []string{"routerB"}, // C depends on B
	}

	routerD := &dynamic.Router{
		Rule:       "Host(`d.example.com`)",
		ParentRefs: []string{"routerC"}, // D depends on C - completes the 4-router circle
	}

	err := graph.AddRouter("routerA", routerA)
	if err != nil {
		t.Fatalf("Failed to add routerA: %v", err)
	}

	err = graph.AddRouter("routerB", routerB)
	if err != nil {
		t.Fatalf("Failed to add routerB: %v", err)
	}

	err = graph.AddRouter("routerC", routerC)
	if err != nil {
		t.Fatalf("Failed to add routerC: %v", err)
	}

	err = graph.AddRouter("routerD", routerD)
	if err != nil {
		t.Fatalf("Failed to add routerD: %v", err)
	}

	// This should detect the circular dependency (4-router loop still counts as circular)
	_, err = graph.DetectCircularDependencies()
	if err == nil {
		t.Fatal("Expected circular dependency error for 4-router loop, but got none")
	}

	// Check that error contains circular dependency message and involves all 4 routers
	errMsg := err.Error()
	if !strings.Contains(errMsg, "circular dependency detected") {
		t.Errorf("Expected error to contain 'circular dependency detected', got %q", errMsg)
	}
	// Verify all routers are involved in the cycle
	if !strings.Contains(errMsg, "routerA") || !strings.Contains(errMsg, "routerB") ||
		!strings.Contains(errMsg, "routerC") || !strings.Contains(errMsg, "routerD") {
		t.Errorf("Expected error to contain all routers (A, B, C, D), got %q", errMsg)
	}
}

func TestCircularDependencyDetection_ValidTree_NoError(t *testing.T) {
	// Test case: Valid tree with no circular dependencies
	// A -> B -> C (linear dependency chain)
	graph := NewRouterGraph()

	routerA := &dynamic.Router{
		Rule: "Host(`a.example.com`)",
		// A has no parents - top level
	}

	routerB := &dynamic.Router{
		Rule:       "Host(`b.example.com`)",
		ParentRefs: []string{"routerA"}, // B depends on A
	}

	routerC := &dynamic.Router{
		Rule:       "Host(`c.example.com`)",
		ParentRefs: []string{"routerB"}, // C depends on B
	}

	err := graph.AddRouter("routerA", routerA)
	if err != nil {
		t.Fatalf("Failed to add routerA: %v", err)
	}

	err = graph.AddRouter("routerB", routerB)
	if err != nil {
		t.Fatalf("Failed to add routerB: %v", err)
	}

	err = graph.AddRouter("routerC", routerC)
	if err != nil {
		t.Fatalf("Failed to add routerC: %v", err)
	}

	// This should NOT detect any circular dependency
	_, err = graph.DetectCircularDependencies()
	if err != nil {
		t.Errorf("Expected no error for valid tree, but got: %v", err)
	}
}

func TestRouterGraphConstruction(t *testing.T) {
	testCases := []struct {
		desc             string
		routers          map[string]*dynamic.Router
		expectedNodes    []string
		expectedDepths   map[string]int
		expectedParents  map[string][]string
		expectedChildren map[string][]string
	}{
		{
			desc: "simple parent-child relationship",
			routers: map[string]*dynamic.Router{
				"parent": {
					Rule: "Host(`parent.example.com`)",
				},
				"child": {
					Rule:       "Host(`child.example.com`)",
					ParentRefs: []string{"parent"},
				},
			},
			expectedNodes: []string{"parent", "child"},
			expectedDepths: map[string]int{
				"parent": 0,
				"child":  1,
			},
			expectedParents: map[string][]string{
				"parent": {},
				"child":  {"parent"},
			},
			expectedChildren: map[string][]string{
				"parent": {"child"},
				"child":  {},
			},
		},
		{
			desc: "multi-level tree (grandparent -> parent -> child)",
			routers: map[string]*dynamic.Router{
				"grandparent": {
					Rule: "Host(`grandparent.example.com`)",
				},
				"parent": {
					Rule:       "Host(`parent.example.com`)",
					ParentRefs: []string{"grandparent"},
				},
				"child": {
					Rule:       "Host(`child.example.com`)",
					ParentRefs: []string{"parent"},
				},
			},
			expectedNodes: []string{"grandparent", "parent", "child"},
			expectedDepths: map[string]int{
				"grandparent": 0,
				"parent":      1,
				"child":       2,
			},
			expectedParents: map[string][]string{
				"grandparent": {},
				"parent":      {"grandparent"},
				"child":       {"parent"},
			},
			expectedChildren: map[string][]string{
				"grandparent": {"parent"},
				"parent":      {"child"},
				"child":       {},
			},
		},
		{
			desc: "router with multiple parents",
			routers: map[string]*dynamic.Router{
				"parent1": {
					Rule: "Host(`parent1.example.com`)",
				},
				"parent2": {
					Rule: "Host(`parent2.example.com`)",
				},
				"child": {
					Rule:       "Host(`child.example.com`)",
					ParentRefs: []string{"parent1", "parent2"},
				},
			},
			expectedNodes: []string{"parent1", "parent2", "child"},
			expectedDepths: map[string]int{
				"parent1": 0,
				"parent2": 0,
				"child":   1, // Depth should be max(parent depths) + 1
			},
			expectedParents: map[string][]string{
				"parent1": {},
				"parent2": {},
				"child":   {"parent1", "parent2"},
			},
			expectedChildren: map[string][]string{
				"parent1": {"child"},
				"parent2": {"child"},
				"child":   {},
			},
		},
	}

	for _, test := range testCases {
		t.Run(test.desc, func(t *testing.T) {
			graph := NewRouterGraph()

			// Add all routers to the graph
			for name, router := range test.routers {
				err := graph.AddRouter(name, router)
				if err != nil {
					t.Fatalf("Failed to add router %s: %v", name, err)
				}
			}

			// TODO: This test will fail until RouterGraph construction is implemented (T015)
			// The test expects:
			// 1. Graph construction from router configurations
			// 2. Parent/child relationship mapping
			// 3. Depth calculation for tree levels
			// 4. Proper node structure with references

			// Test graph structure - these assertions will fail until implementation is complete:

			// Verify all expected nodes exist
			for _, nodeName := range test.expectedNodes {
				if _, exists := graph.nodes[nodeName]; !exists {
					t.Errorf("Expected node %s not found in graph", nodeName)
				}
			}

			// Verify depth calculations
			for nodeName, expectedDepth := range test.expectedDepths {
				if node, exists := graph.nodes[nodeName]; exists {
					if node.depth != expectedDepth {
						t.Errorf("Node %s: expected depth %d, got %d", nodeName, expectedDepth, node.depth)
					}
				}
			}

			// Verify parent relationships
			for nodeName, expectedParents := range test.expectedParents {
				if node, exists := graph.nodes[nodeName]; exists {
					actualParentNames := make([]string, len(node.parents))
					for i, parent := range node.parents {
						// Need to find parent name by searching graph.nodes
						for name, n := range graph.nodes {
							if n == parent {
								actualParentNames[i] = name
								break
							}
						}
					}

					if len(actualParentNames) != len(expectedParents) {
						t.Errorf("Node %s: expected %d parents, got %d", nodeName, len(expectedParents), len(actualParentNames))
					}
				}
			}

		})
	}
}

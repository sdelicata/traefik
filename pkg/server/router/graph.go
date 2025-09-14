package router

import (
	"fmt"
	"slices"
	"strings"

	"github.com/traefik/traefik/v3/pkg/config/dynamic"
)

// RouterGraph manages router tree relationships and validates circular dependencies
type RouterGraph struct {
	nodes map[string]*RouterNode
}

// RouterNode represents a router in the dependency graph
type RouterNode struct {
	router   *dynamic.Router
	parents  []*RouterNode
	children []*RouterNode
	depth    int
}

// NewRouterGraph creates a new router graph
func NewRouterGraph() *RouterGraph {
	return &RouterGraph{
		nodes: make(map[string]*RouterNode),
	}
}

// AddRouter adds a router to the graph and builds relationships
func (rg *RouterGraph) AddRouter(name string, router *dynamic.Router) error {
	// Create new node for this router
	node := &RouterNode{
		router:   router,
		parents:  make([]*RouterNode, 0),
		children: make([]*RouterNode, 0),
		depth:    0,
	}

	// Store node in graph
	rg.nodes[name] = node

	// Build parent relationships and set up children references
	if router.ParentRefs != nil {
		for _, parentName := range router.ParentRefs {
			if parentNode, exists := rg.nodes[parentName]; exists {
				// Add parent reference to this node
				node.parents = append(node.parents, parentNode)
				// Add this node as child to parent
				parentNode.children = append(parentNode.children, node)
			}
			// If parent doesn't exist yet, we'll handle it when it's added later
		}
	}

	// Update existing nodes that reference this router as parent
	for _, existingNode := range rg.nodes {
		if existingNode.router.ParentRefs != nil {
			for _, parentRef := range existingNode.router.ParentRefs {
				if parentRef == name {
					// This existing node references the new router as parent
					existingNode.parents = append(existingNode.parents, node)
					node.children = append(node.children, existingNode)
				}
			}
		}
	}

	// Calculate depths for all nodes after adding this router
	rg.calculateDepths()

	return nil
}

// calculateDepths updates depth values for all nodes in the graph
func (rg *RouterGraph) calculateDepths() {
	// Reset all depths
	for _, node := range rg.nodes {
		node.depth = 0
	}

	// Calculate depths using topological traversal
	// Start with nodes that have no parents (root nodes)
	var toVisit []*RouterNode
	for _, node := range rg.nodes {
		if len(node.parents) == 0 {
			toVisit = append(toVisit, node)
		}
	}

	// Process nodes level by level
	for len(toVisit) > 0 {
		var nextLevel []*RouterNode

		for _, node := range toVisit {
			// Update children depths and add to next level
			for _, child := range node.children {
				// Child depth should be max of all parent depths + 1
				maxParentDepth := -1
				for _, parent := range child.parents {
					if parent.depth > maxParentDepth {
						maxParentDepth = parent.depth
					}
				}
				newDepth := maxParentDepth + 1
				if newDepth > child.depth {
					child.depth = newDepth
					// Add child to next level to process its children
					if !containsNode(nextLevel, child) {
						nextLevel = append(nextLevel, child)
					}
				}
			}
		}

		toVisit = nextLevel
	}
}

// containsNode checks if a slice contains a specific RouterNode
func containsNode(nodes []*RouterNode, target *RouterNode) bool {
	for _, node := range nodes {
		if node == target {
			return true
		}
	}
	return false
}

// DetectCircularDependencies checks for circular dependencies with a 3-loop limit
func (rg *RouterGraph) DetectCircularDependencies() ([]string, error) {
	// Use DFS to detect cycles
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	for nodeName := range rg.nodes {
		if !visited[nodeName] {
			if cycle := rg.dfsDetectCycle(nodeName, visited, recStack, []string{}); cycle != nil {
				// Format the cycle path for error message
				cyclePath := strings.Join(cycle, " -> ")
				// Return both the involved routers and the formatted error
				return cycle, fmt.Errorf("circular dependency detected: %s", cyclePath)
			}
		}
	}

	return nil, nil
}

// FindInvalidParentReferences checks if all parent references exist in the graph
// and returns a map of router names to their invalid parent references.
func (rg *RouterGraph) FindInvalidParentReferences() map[string][]string {
	invalidRefs := make(map[string][]string)

	for routerName, node := range rg.nodes {
		if node.router.ParentRefs == nil {
			continue
		}

		var invalidParents []string
		for _, parentRef := range node.router.ParentRefs {
			if _, exists := rg.nodes[parentRef]; !exists {
				invalidParents = append(invalidParents, parentRef)
			}
		}

		if len(invalidParents) > 0 {
			invalidRefs[routerName] = invalidParents
		}
	}

	return invalidRefs
}

// dfsDetectCycle performs DFS to detect cycles and return the cycle path if found
func (rg *RouterGraph) dfsDetectCycle(nodeName string, visited, recStack map[string]bool, path []string) []string {
	visited[nodeName] = true
	recStack[nodeName] = true
	path = append(path, nodeName)

	node := rg.nodes[nodeName]

	// Visit all parent nodes (dependencies)
	for _, parentNode := range node.parents {
		// Find parent name
		var parentName string
		for name, n := range rg.nodes {
			if n == parentNode {
				parentName = name
				break
			}
		}

		if !visited[parentName] {
			// Recursively visit parent
			if cycle := rg.dfsDetectCycle(parentName, visited, recStack, path); cycle != nil {
				return cycle
			}
		} else if recStack[parentName] {
			// Found a back edge - cycle detected
			// Find where the cycle starts in the path
			cycleStartIdx := slices.Index(path, parentName)
			if cycleStartIdx >= 0 {
				// Return the cycle path + the repeated node to show the loop
				cyclePath := append(path[cycleStartIdx:], parentName)
				return cyclePath
			}
		}
	}

	recStack[nodeName] = false
	return nil
}

// GetNode returns the RouterNode for a given router name
func (rg *RouterGraph) GetNode(name string) *RouterNode {
	return rg.nodes[name]
}

// GetNodeNames returns a map from RouterNode to its name for efficient lookups
func (rg *RouterGraph) GetNodeNames() map[*RouterNode]string {
	nameMap := make(map[*RouterNode]string)
	for name, node := range rg.nodes {
		nameMap[node] = name
	}
	return nameMap
}

// RouterNode methods for accessing properties
func (rn *RouterNode) GetRouter() *dynamic.Router {
	return rn.router
}

func (rn *RouterNode) GetParents() []*RouterNode {
	return rn.parents
}

func (rn *RouterNode) GetChildren() []*RouterNode {
	return rn.children
}

func (rn *RouterNode) GetDepth() int {
	return rn.depth
}

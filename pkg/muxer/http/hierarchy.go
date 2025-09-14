package http

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/containous/alice"
	"github.com/rs/zerolog/log"
	"github.com/traefik/traefik/v3/pkg/config/dynamic"
)

// HierarchicalRouter represents a router with its hierarchical relationships
type HierarchicalRouter struct {
	Name           string
	Router         *dynamic.Router
	Matchers       matchersTree
	Handler        http.Handler
	MiddlewareFunc func(http.Handler) (http.Handler, error) // Middleware function for request transformation
	Priority       int
	Parents        []*HierarchicalRouter
	Children       []*HierarchicalRouter
	Depth          int
	IsRoot         bool // true if router has no parents (entryPoints defined)
}

// MiddlewareBuilder interface for building middleware chains during hierarchy construction
type MiddlewareBuilder interface {
	BuildChain(ctx context.Context, middlewareNames []string) *alice.Chain
}

// HierarchicalEvaluationEngine provides simplified hierarchical route evaluation with middleware integration
type HierarchicalEvaluationEngine struct {
	routes            map[string]*HierarchicalRouter
	rootRoutes        []*HierarchicalRouter
	parser            SyntaxParser
	middlewareBuilder MiddlewareBuilder // For building middleware chains
	mu                sync.RWMutex

	// Simplified performance metrics
	evaluationCounter  int64 // Count of route evaluations
	terminationCounter int64 // Count of early terminations (key optimization)
	requestCount       int64 // Total requests processed

	// Configuration
	maxDepth               int  // Reasonable hierarchy depth limit
	enableEarlyTermination bool // Keep this key optimization
}

// NewHierarchicalEvaluationEngine creates a new simplified hierarchical evaluation engine
func NewHierarchicalEvaluationEngine(parser SyntaxParser, middlewareBuilder MiddlewareBuilder) *HierarchicalEvaluationEngine {
	return &HierarchicalEvaluationEngine{
		routes:                 make(map[string]*HierarchicalRouter),
		rootRoutes:             make([]*HierarchicalRouter, 0),
		parser:                 parser,
		middlewareBuilder:      middlewareBuilder,
		maxDepth:               10,   // Reasonable limit for hierarchy depth
		enableEarlyTermination: true, // Keep this key optimization
	}
}

// BuildHierarchy constructs the hierarchical router structure from a flat router map
func (h *HierarchicalEvaluationEngine) BuildHierarchy(routerConfigs map[string]*dynamic.Router, handlers map[string]http.Handler) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Clear existing hierarchy
	h.routes = make(map[string]*HierarchicalRouter)
	h.rootRoutes = make([]*HierarchicalRouter, 0)

	// First pass: Create all hierarchical routers without relationships
	for name, router := range routerConfigs {
		handler, exists := handlers[name]
		if !exists {
			continue // Skip routers without handlers
		}

		// Parse the router rule into matchers
		matchers, err := h.parseRouterRule(router.Rule)
		if err != nil {
			log.Error().Err(err).Str("router", name).Msg("Failed to parse router rule")
			continue
		}

		// Build middleware function for this router if it has middleware
		var middlewareFunc func(http.Handler) (http.Handler, error)
		if len(router.Middlewares) > 0 && h.middlewareBuilder != nil {
			// Build the middleware chain and convert to function
			chain := h.middlewareBuilder.BuildChain(context.TODO(), router.Middlewares)
			if chain != nil {
				middlewareFunc = func(next http.Handler) (http.Handler, error) {
					return chain.Then(next)
				}
			}
		}

		hierarchicalRouter := &HierarchicalRouter{
			Name:           name,
			Router:         router,
			Matchers:       matchers,
			Handler:        handler,
			MiddlewareFunc: middlewareFunc,
			Priority:       router.Priority,
			Parents:        make([]*HierarchicalRouter, 0),
			Children:       make([]*HierarchicalRouter, 0),
			IsRoot:         len(router.EntryPoints) > 0, // Routers with entryPoints are roots
		}

		h.routes[name] = hierarchicalRouter
	}

	// Second pass: Establish parent-child relationships
	for name, hierarchicalRouter := range h.routes {
		router := hierarchicalRouter.Router

		// Process parentRefs to establish relationships
		for _, parentName := range router.ParentRefs {
			parentRouter, exists := h.routes[parentName]
			if !exists {
				log.Warn().Str("router", name).Str("parent", parentName).Msg("Parent router not found")
				continue
			}

			// Add parent relationship
			hierarchicalRouter.Parents = append(hierarchicalRouter.Parents, parentRouter)
			parentRouter.Children = append(parentRouter.Children, hierarchicalRouter)

			// Child routers with parents are not root routers
			hierarchicalRouter.IsRoot = false
		}
	}

	// Third pass: Calculate depths and identify root routers
	for _, hierarchicalRouter := range h.routes {
		if hierarchicalRouter.IsRoot {
			h.rootRoutes = append(h.rootRoutes, hierarchicalRouter)
			h.calculateDepth(hierarchicalRouter, 0)
		}
	}

	// Sort root routers by priority for evaluation order
	sort.Slice(h.rootRoutes, func(i, j int) bool {
		return h.rootRoutes[i].Priority > h.rootRoutes[j].Priority
	})
	return nil
}

// calculateDepth recursively calculates and sets the depth for each router in the hierarchy
func (h *HierarchicalEvaluationEngine) calculateDepth(router *HierarchicalRouter, depth int) {
	if depth > h.maxDepth {
		log.Warn().Str("router", router.Name).Int("depth", depth).Msg("Router depth exceeds maximum")
		return
	}

	router.Depth = depth

	// Sort children by priority before calculating their depths
	sort.Slice(router.Children, func(i, j int) bool {
		return router.Children[i].Priority > router.Children[j].Priority
	})

	for _, child := range router.Children {
		h.calculateDepth(child, depth+1)
	}
}

// parseRouterRule converts a router rule string into a matchersTree
func (h *HierarchicalEvaluationEngine) parseRouterRule(rule string) (matchersTree, error) {
	if rule == "" {
		return matchersTree{}, nil
	}

	// Use the existing parser to create matchers tree
	// This preserves all existing matcher functionality
	return h.parser.parse("v3", rule)
}

// EvaluateRequest performs simplified hierarchical route evaluation
func (h *HierarchicalEvaluationEngine) EvaluateRequest(req *http.Request) (*HierarchicalRouter, bool) {
	atomic.AddInt64(&h.requestCount, 1)

	h.mu.RLock()
	defer h.mu.RUnlock()

	ctx := req.Context()

	// Simple hierarchical evaluation with middleware integration
	for _, rootRouter := range h.rootRoutes {
		if matchedRouter := h.evaluateRouterHierarchy(ctx, req, rootRouter); matchedRouter != nil {
			return matchedRouter, true
		}
	}

	return nil, false
}

// evaluateRouterHierarchy performs staged evaluation with middleware integration: parent middleware → child evaluation
func (h *HierarchicalEvaluationEngine) evaluateRouterHierarchy(ctx context.Context, req *http.Request, router *HierarchicalRouter) *HierarchicalRouter {
	// Increment evaluation counter for performance tracking
	atomic.AddInt64(&h.evaluationCounter, 1)

	// Stage 1: Check if current router matches the request
	routerMatches := h.evaluateRouterMatchers(req, router)

	// Optional debug logging for troubleshooting

	// Early Termination Optimization: If parent doesn't match, skip all children
	if !routerMatches && h.enableEarlyTermination {
		atomic.AddInt64(&h.terminationCounter, 1)
		return nil
	}

	// Stage 2: If router matches and has no children, it's a terminal route
	if len(router.Children) == 0 {
		if routerMatches && router.Router.Service != "" {
			return router
		}
		return nil
	}

	// Stage 3: If router matches, execute middleware and evaluate children with modified request
	if routerMatches {
		modifiedRequest := req // Start with original request

		// Execute parent middleware to transform request before evaluating children
		if router.MiddlewareFunc != nil {
			modifiedRequest = h.executeMiddlewareAndCaptureRequest(router.MiddlewareFunc, req)
		}

		// Evaluate children with the modified request
		for _, child := range router.Children {
			if matchedRouter := h.evaluateRouterHierarchy(ctx, modifiedRequest, child); matchedRouter != nil {
				return matchedRouter
			}
		}

		// If no children match but this router has a service, it can handle the request
		if router.Router.Service != "" {
			return router
		}
	}

	return nil
}

// executeMiddlewareAndCaptureRequest executes middleware and returns the modified request
func (h *HierarchicalEvaluationEngine) executeMiddlewareAndCaptureRequest(middlewareFunc func(http.Handler) (http.Handler, error), originalReq *http.Request) *http.Request {
	// Create a special handler that captures the modified request
	var capturedRequest *http.Request
	captureHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequest = r
	})

	// Build the middleware chain with our capture handler
	middlewareChain, err := middlewareFunc(captureHandler)
	if err != nil {
		log.Error().Err(err).Msg("Failed to build middleware chain for request transformation")
		return originalReq
	}

	// Create a custom response writer that doesn't interfere with request processing
	dummyWriter := &requestCaptureWriter{}

	// Execute the middleware chain with our capture handler
	// This will run all middleware and capture the final modified request
	middlewareChain.ServeHTTP(dummyWriter, originalReq)

	// If middleware executed successfully and we captured a request, return it
	if capturedRequest != nil {
		return capturedRequest
	}

	// Fallback to original request if capture failed
	return originalReq
}

// requestCaptureWriter is a minimal ResponseWriter implementation for request capture
type requestCaptureWriter struct{}

func (w *requestCaptureWriter) Header() http.Header {
	return make(http.Header)
}

func (w *requestCaptureWriter) Write([]byte) (int, error) {
	return 0, nil
}

func (w *requestCaptureWriter) WriteHeader(int) {}

// evaluateRouterMatchers evaluates router matchers while preserving existing behavior
func (h *HierarchicalEvaluationEngine) evaluateRouterMatchers(req *http.Request, router *HierarchicalRouter) bool {
	// Use existing matchersTree.match() logic to preserve all current matcher functionality
	return router.Matchers.match(req)
}

// GetPerformanceMetrics returns simplified performance metrics for monitoring
func (h *HierarchicalEvaluationEngine) GetPerformanceMetrics() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	evaluations := atomic.LoadInt64(&h.evaluationCounter)
	terminations := atomic.LoadInt64(&h.terminationCounter)
	requests := atomic.LoadInt64(&h.requestCount)

	var terminationRate float64
	if evaluations > 0 {
		terminationRate = float64(terminations) / float64(evaluations)
	}

	return map[string]interface{}{
		"total_routes":       len(h.routes),
		"root_routes":        len(h.rootRoutes),
		"evaluations":        evaluations,
		"terminations":       terminations,
		"requests_processed": requests,
		"termination_rate":   terminationRate,
		"early_termination":  h.enableEarlyTermination,
	}
}

// GetRouteEvaluationCount returns the number of route evaluations for monitoring
func (h *HierarchicalEvaluationEngine) GetRouteEvaluationCount() int64 {
	return atomic.LoadInt64(&h.evaluationCounter)
}

// GetTerminationCount returns the number of early terminations for monitoring
func (h *HierarchicalEvaluationEngine) GetTerminationCount() int64 {
	return atomic.LoadInt64(&h.terminationCounter)
}

// ResetPerformanceCounters resets basic performance counters for benchmarking
func (h *HierarchicalEvaluationEngine) ResetPerformanceCounters() {
	atomic.StoreInt64(&h.evaluationCounter, 0)
	atomic.StoreInt64(&h.terminationCounter, 0)
	atomic.StoreInt64(&h.requestCount, 0)
}

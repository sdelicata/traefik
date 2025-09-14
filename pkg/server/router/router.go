package router

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/containous/alice"
	"github.com/rs/zerolog/log"
	"github.com/traefik/traefik/v3/pkg/config/dynamic"
	"github.com/traefik/traefik/v3/pkg/config/runtime"
	"github.com/traefik/traefik/v3/pkg/logs"
	"github.com/traefik/traefik/v3/pkg/middlewares/accesslog"
	"github.com/traefik/traefik/v3/pkg/middlewares/denyrouterrecursion"
	metricsMiddle "github.com/traefik/traefik/v3/pkg/middlewares/metrics"
	"github.com/traefik/traefik/v3/pkg/middlewares/observability"
	"github.com/traefik/traefik/v3/pkg/middlewares/recovery"
	httpmuxer "github.com/traefik/traefik/v3/pkg/muxer/http"
	"github.com/traefik/traefik/v3/pkg/server/middleware"
	"github.com/traefik/traefik/v3/pkg/server/provider"
	"github.com/traefik/traefik/v3/pkg/tls"
)

const maxUserPriority = math.MaxInt - 1000

type middlewareBuilder interface {
	BuildChain(ctx context.Context, names []string) *alice.Chain
}

type serviceManager interface {
	BuildHTTP(rootCtx context.Context, serviceName string) (http.Handler, error)
	LaunchHealthCheck(ctx context.Context)
}

// Manager A route/router manager.
type Manager struct {
	routerHandlers     map[string]http.Handler
	serviceManager     serviceManager
	observabilityMgr   *middleware.ObservabilityMgr
	middlewaresBuilder middlewareBuilder
	conf               *runtime.Configuration
	tlsManager         *tls.Manager
	parser             httpmuxer.SyntaxParser
}

// NewManager creates a new Manager.
func NewManager(conf *runtime.Configuration, serviceManager serviceManager, middlewaresBuilder middlewareBuilder, observabilityMgr *middleware.ObservabilityMgr, tlsManager *tls.Manager, parser httpmuxer.SyntaxParser) *Manager {
	return &Manager{
		routerHandlers:     make(map[string]http.Handler),
		serviceManager:     serviceManager,
		observabilityMgr:   observabilityMgr,
		middlewaresBuilder: middlewaresBuilder,
		conf:               conf,
		tlsManager:         tlsManager,
		parser:             parser,
	}
}

func (m *Manager) getHTTPRouters(ctx context.Context, entryPoints []string, tls bool) map[string]map[string]*runtime.RouterInfo {
	if m.conf != nil {
		return m.conf.GetRoutersByEntryPoints(ctx, entryPoints, tls)
	}

	return make(map[string]map[string]*runtime.RouterInfo)
}

// BuildHandlers Builds handler for all entry points.
func (m *Manager) BuildHandlers(rootCtx context.Context, entryPoints []string, tls bool, precomputedProblematicRouters []string, precomputedRouterErrors map[string]error) map[string]http.Handler {
	entryPointHandlers := make(map[string]http.Handler)

	defaultObsConfig := dynamic.RouterObservabilityConfig{}
	defaultObsConfig.SetDefaults()

	var problematicRouters []string
	var routerErrors map[string]error
	var problematicRoutersMap map[string]bool

	// Use precomputed validation results if provided, otherwise compute them
	if precomputedProblematicRouters != nil && precomputedRouterErrors != nil {
		problematicRouters = precomputedProblematicRouters
		routerErrors = precomputedRouterErrors
	} else {
		// Validate router tree using RouterGraph - FR-013: graceful error handling
		problematicRouters, routerErrors = m.validateRouterTree()
	}

	problematicRoutersMap = make(map[string]bool)
	for _, routerName := range problematicRouters {
		problematicRoutersMap[routerName] = true
	}

	if len(problematicRouters) > 0 {
		logger := log.Ctx(rootCtx)
		// Log each problematic router individually for clear error reporting
		for _, routerName := range problematicRouters {
			if err, exists := routerErrors[routerName]; exists {
				logger.Error().Err(err).Str("router", routerName).Msg("Router excluded due to validation failure")
			}
		}
		logger.Info().Int("problematic_count", len(problematicRouters)).
			Int("total_routers", len(m.conf.Routers)).
			Msg("FR-013: Continuing with healthy routers, excluding problematic ones")
	}

	// T038: Detect if hierarchical optimization should be enabled
	useHierarchicalOptimization := m.hasHierarchicalRouters()
	if useHierarchicalOptimization {
		logger := log.Ctx(rootCtx)
		logger.Info().Msg("T038: Hierarchical routing detected, enabling performance optimization")
	}

	for entryPointName, routers := range m.getHTTPRouters(rootCtx, entryPoints, tls) {
		logger := log.Ctx(rootCtx).With().Str(logs.EntryPointName, entryPointName).Logger()
		ctx := logger.WithContext(rootCtx)

		// FR-013: Filter out problematic routers, keep only healthy ones
		healthyRouters := make(map[string]*runtime.RouterInfo)
		for routerName, routerInfo := range routers {
			if !problematicRoutersMap[routerName] {
				healthyRouters[routerName] = routerInfo
			}
		}

		// Skip building handler if no healthy routers remain for this entry point
		if len(healthyRouters) == 0 {
			logger.Info().Msg("No healthy routers for entry point, skipping handler creation")
			continue
		}

		// TODO: Improve this part. Relying on models is a shortcut to get the entrypoint observability configuration. Maybe we should pass down the static configuration.
		// When the entry point has no observability configuration no model is produced,
		// and we need to create the default configuration is this case.
		epObsConfig := defaultObsConfig
		if model, ok := m.conf.Models[entryPointName+"@internal"]; ok && model != nil {
			epObsConfig = model.Observability
		}

		// T038: Pass hierarchical optimization flag to entry point handler building
		handler, err := m.buildEntryPointHandlerWithOptimization(ctx, entryPointName, healthyRouters, epObsConfig, useHierarchicalOptimization)
		if err != nil {
			logger.Error().Err(err).Send()
			continue
		}

		entryPointHandlers[entryPointName] = handler
	}

	// Create default handlers.
	for _, entryPointName := range entryPoints {
		logger := log.Ctx(rootCtx).With().Str(logs.EntryPointName, entryPointName).Logger()
		ctx := logger.WithContext(rootCtx)

		handler, ok := entryPointHandlers[entryPointName]
		if ok || handler != nil {
			continue
		}

		// TODO: Improve this part. Relying on models is a shortcut to get the entrypoint observability configuration. Maybe we should pass down the static configuration.
		// When the entry point has no observability configuration no model is produced,
		// and we need to create the default configuration is this case.
		epObsConfig := defaultObsConfig
		if model, ok := m.conf.Models[entryPointName+"@internal"]; ok && model != nil {
			epObsConfig = model.Observability
		}

		defaultHandler, err := m.observabilityMgr.BuildEPChain(ctx, entryPointName, false, epObsConfig).Then(http.NotFoundHandler())
		if err != nil {
			logger.Error().Err(err).Send()
			continue
		}
		entryPointHandlers[entryPointName] = defaultHandler
	}

	return entryPointHandlers
}

// validateRouterTree validates the router tree using RouterGraph, returning per-router validation results
// Returns: (problematicRouters []string, routerErrors map[string]error) for FR-013 graceful error handling
func (m *Manager) validateRouterTree() ([]string, map[string]error) {
	return ValidateRouterTree(m.conf.Routers)
}

// hasHierarchicalRouters checks if any router in the configuration has ParentRefs defined.
// Returns true if hierarchical routing is needed, false for standard flat routing.
func (m *Manager) hasHierarchicalRouters() bool {
	for _, router := range m.conf.Routers {
		if len(router.ParentRefs) > 0 {
			return true
		}
	}
	return false
}

// ValidateRouterTree validates router tree configuration using RouterGraph, returning per-router validation results.
// This is a standalone function that doesn't require a Manager instance.
func ValidateRouterTree(routers map[string]*runtime.RouterInfo) ([]string, map[string]error) {
	problematicRouters := make([]string, 0)
	routerErrors := make(map[string]error)

	if routers == nil {
		return problematicRouters, routerErrors
	}

	// Create RouterGraph for validation
	graph := NewRouterGraph()

	// Phase 1: Add all routers to graph
	for routerName, routerInfo := range routers {
		if err := graph.AddRouter(routerName, routerInfo.Router); err != nil {
			problematicRouters = append(problematicRouters, routerName)
			routerErrors[routerName] = fmt.Errorf("failed to add router to graph: %w", err)
		}
	}

	// Phase 2: Validate parent references
	invalidParentRefs := graph.FindInvalidParentReferences()
	for routerName, invalidParents := range invalidParentRefs {
		if _, alreadyProblematic := routerErrors[routerName]; !alreadyProblematic {
			problematicRouters = append(problematicRouters, routerName)
			routerErrors[routerName] = fmt.Errorf("invalid parent reference(s): %s", strings.Join(invalidParents, ", "))
		}
	}

	// Phase 3: Detect circular dependencies
	cyclicRouters, cycleErr := graph.DetectCircularDependencies()
	if cycleErr != nil {
		for _, routerName := range cyclicRouters {
			if _, alreadyProblematic := routerErrors[routerName]; !alreadyProblematic {
				problematicRouters = append(problematicRouters, routerName)
				routerErrors[routerName] = fmt.Errorf("router involved in circular dependency: %w", cycleErr)
			}
		}
	}

	return problematicRouters, routerErrors
}

func (m *Manager) buildEntryPointHandler(ctx context.Context, entryPointName string, configs map[string]*runtime.RouterInfo, config dynamic.RouterObservabilityConfig) (http.Handler, error) {
	muxer := httpmuxer.NewMuxer(m.parser)

	defaultHandler, err := m.observabilityMgr.BuildEPChain(ctx, entryPointName, false, config).Then(http.NotFoundHandler())
	if err != nil {
		return nil, err
	}

	muxer.SetDefaultHandler(defaultHandler)

	for routerName, routerConfig := range configs {
		logger := log.Ctx(ctx).With().Str(logs.RouterName, routerName).Logger()
		ctxRouter := logger.WithContext(provider.AddInContext(ctx, routerName))

		if routerConfig.Priority == 0 {
			routerConfig.Priority = httpmuxer.GetRulePriority(routerConfig.Rule)
		}

		if routerConfig.Priority > maxUserPriority && !strings.HasSuffix(routerName, "@internal") {
			err = fmt.Errorf("the router priority %d exceeds the max user-defined priority %d", routerConfig.Priority, maxUserPriority)
			routerConfig.AddError(err, true)
			logger.Error().Err(err).Send()
			continue
		}

		handler, err := m.buildRouterHandler(ctxRouter, routerName, routerConfig)
		if err != nil {
			routerConfig.AddError(err, true)
			logger.Error().Err(err).Send()
			continue
		}

		if routerConfig.Observability != nil {
			config = *routerConfig.Observability
		}

		observabilityChain := m.observabilityMgr.BuildEPChain(ctxRouter, entryPointName, strings.HasSuffix(routerConfig.Service, "@internal"), config)
		handler, err = observabilityChain.Then(handler)
		if err != nil {
			routerConfig.AddError(err, true)
			logger.Error().Err(err).Send()
			continue
		}

		if err = muxer.AddRoute(routerConfig.Rule, routerConfig.RuleSyntax, routerConfig.Priority, handler); err != nil {
			routerConfig.AddError(err, true)
			logger.Error().Err(err).Send()
			continue
		}
	}

	chain := alice.New()
	chain = chain.Append(func(next http.Handler) (http.Handler, error) {
		return recovery.New(ctx, next)
	})

	return chain.Then(muxer)
}

// buildEntryPointHandlerWithOptimization builds the entry point handler with optional hierarchical optimization.
// T038: Integrates hierarchical optimization with router building when hierarchical routers are detected.
func (m *Manager) buildEntryPointHandlerWithOptimization(ctx context.Context, entryPointName string, configs map[string]*runtime.RouterInfo, config dynamic.RouterObservabilityConfig, useHierarchical bool) (http.Handler, error) {
	muxer := httpmuxer.NewMuxer(m.parser)

	// T038: Enable hierarchical evaluation if hierarchical routers are detected
	if useHierarchical {
		muxer.EnableHierarchicalEvaluation()
		logger := log.Ctx(ctx)
		logger.Debug().Str("entryPoint", entryPointName).Msg("T038: Hierarchical evaluation enabled for performance optimization")
	}

	defaultHandler, err := m.observabilityMgr.BuildEPChain(ctx, entryPointName, false, config).Then(http.NotFoundHandler())
	if err != nil {
		return nil, err
	}

	muxer.SetDefaultHandler(defaultHandler)

	// T038: Collect router configurations and handlers for hierarchical setup
	var routerConfigs map[string]*dynamic.Router
	var routerHandlers map[string]http.Handler

	if useHierarchical {
		routerConfigs = make(map[string]*dynamic.Router)
		routerHandlers = make(map[string]http.Handler)
	}

	for routerName, routerConfig := range configs {
		logger := log.Ctx(ctx).With().Str(logs.RouterName, routerName).Logger()
		ctxRouter := logger.WithContext(provider.AddInContext(ctx, routerName))

		if routerConfig.Priority == 0 {
			routerConfig.Priority = httpmuxer.GetRulePriority(routerConfig.Rule)
		}

		if routerConfig.Priority > maxUserPriority && !strings.HasSuffix(routerName, "@internal") {
			err = fmt.Errorf("the router priority %d exceeds the max user-defined priority %d", routerConfig.Priority, maxUserPriority)
			routerConfig.AddError(err, true)
			logger.Error().Err(err).Send()
			continue
		}

		handler, err := m.buildRouterHandler(ctxRouter, routerName, routerConfig)
		if err != nil {
			routerConfig.AddError(err, true)
			logger.Error().Err(err).Send()
			continue
		}

		if routerConfig.Observability != nil {
			config = *routerConfig.Observability
		}

		observabilityChain := m.observabilityMgr.BuildEPChain(ctxRouter, entryPointName, strings.HasSuffix(routerConfig.Service, "@internal"), config)
		handler, err = observabilityChain.Then(handler)
		if err != nil {
			routerConfig.AddError(err, true)
			logger.Error().Err(err).Send()
			continue
		}

		// T038: Store router configuration and handler for hierarchical setup
		if useHierarchical {
			// Get the original router config from manager's configuration
			if originalConfigInfo, exists := m.conf.Routers[routerName]; exists {
				routerConfigs[routerName] = originalConfigInfo.Router
				routerHandlers[routerName] = handler
			}
		}

		if err = muxer.AddRoute(routerConfig.Rule, routerConfig.RuleSyntax, routerConfig.Priority, handler); err != nil {
			routerConfig.AddError(err, true)
			logger.Error().Err(err).Send()
			continue
		}
	}

	// T038: Configure hierarchical routes after all handlers are built
	if useHierarchical && len(routerConfigs) > 0 {
		if err := muxer.SetHierarchicalRoutes(routerConfigs, routerHandlers); err != nil {
			logger := log.Ctx(ctx)
			logger.Error().Err(err).Str("entryPoint", entryPointName).Msg("T038: Failed to configure hierarchical routes, falling back to standard routing")
			// Don't return error - fall back to standard routing which is already configured
		} else {
			logger := log.Ctx(ctx)
			logger.Info().Str("entryPoint", entryPointName).Int("hierarchicalRouters", len(routerConfigs)).Msg("T038: Hierarchical optimization configured successfully")
		}
	}

	chain := alice.New()
	chain = chain.Append(func(next http.Handler) (http.Handler, error) {
		return recovery.New(ctx, next)
	})

	return chain.Then(muxer)
}

func (m *Manager) buildRouterHandler(ctx context.Context, routerName string, routerConfig *runtime.RouterInfo) (http.Handler, error) {
	if handler, ok := m.routerHandlers[routerName]; ok {
		return handler, nil
	}

	if routerConfig.TLS != nil {
		// Don't build the router if the TLSOptions configuration is invalid.
		tlsOptionsName := tls.DefaultTLSConfigName
		if len(routerConfig.TLS.Options) > 0 && routerConfig.TLS.Options != tls.DefaultTLSConfigName {
			tlsOptionsName = provider.GetQualifiedName(ctx, routerConfig.TLS.Options)
		}
		if _, err := m.tlsManager.Get(tls.DefaultTLSStoreName, tlsOptionsName); err != nil {
			return nil, fmt.Errorf("building router handler: %w", err)
		}
	}

	handler, err := m.buildHTTPHandler(ctx, routerConfig, routerName)
	if err != nil {
		return nil, err
	}

	m.routerHandlers[routerName] = handler
	return m.routerHandlers[routerName], nil
}

func (m *Manager) buildHTTPHandler(ctx context.Context, router *runtime.RouterInfo, routerName string) (http.Handler, error) {
	// T040.1: Check if this router has parent relationships for sequential middleware execution
	hasParents := router.ParentRefs != nil && len(router.ParentRefs) > 0

	if hasParents {
		// T040.1: For routers with parents, create a sequential middleware execution handler
		return m.buildSequentialMiddlewareHandler(ctx, router, routerName)
	}

	// Standard middleware handling for routers without parent relationships
	// Collect parent middleware first, then add current router's middleware
	var allMiddlewares []string

	// Collect parent middleware in tree order (from root to child)
	parentMiddleware := m.collectParentMiddleware(routerName, make(map[string]bool))
	allMiddlewares = append(allMiddlewares, parentMiddleware...)

	// Add this router's own middleware
	allMiddlewares = append(allMiddlewares, router.Middlewares...)

	// Qualify all middleware names
	var qualifiedNames []string
	for _, name := range allMiddlewares {
		qualifiedNames = append(qualifiedNames, provider.GetQualifiedName(ctx, name))
	}
	router.Middlewares = qualifiedNames

	if router.Service == "" {
		return nil, errors.New("the service is missing on the router")
	}

	qualifiedService := provider.GetQualifiedName(ctx, router.Service)

	chain := alice.New()

	if router.DefaultRule {
		chain = chain.Append(denyrouterrecursion.WrapHandler(routerName))
	}

	// Access logs, metrics, and tracing middlewares are idempotent if the associated signal is disabled.
	chain = chain.Append(observability.WrapRouterHandler(ctx, routerName, router.Rule, qualifiedService))
	metricsHandler := metricsMiddle.RouterMetricsHandler(ctx, m.observabilityMgr.MetricsRegistry(), routerName, qualifiedService)

	chain = chain.Append(observability.WrapMiddleware(ctx, metricsHandler))
	chain = chain.Append(func(next http.Handler) (http.Handler, error) {
		return accesslog.NewFieldHandler(next, accesslog.RouterName, routerName, nil), nil
	})

	mHandler := m.middlewaresBuilder.BuildChain(ctx, router.Middlewares)

	sHandler, err := m.serviceManager.BuildHTTP(ctx, qualifiedService)
	if err != nil {
		return nil, err
	}

	return chain.Extend(*mHandler).Then(sHandler)
}

// collectParentMiddleware recursively collects middleware from parent routers
// Returns middleware in tree order (from root to immediate parent)
func (m *Manager) collectParentMiddleware(routerName string, visited map[string]bool) []string {
	// Prevent infinite recursion in case of cycles
	if visited[routerName] {
		return nil
	}
	visited[routerName] = true

	router, exists := m.conf.Routers[routerName]
	if !exists {
		return nil
	}

	var allMiddleware []string

	// Process each parent router
	if router.ParentRefs != nil {
		for _, parentName := range router.ParentRefs {
			// Recursively collect middleware from parent's parents first
			parentParentMiddleware := m.collectParentMiddleware(parentName, visited)
			allMiddleware = append(allMiddleware, parentParentMiddleware...)

			// Then add the parent's own middleware
			parentRouter, parentExists := m.conf.Routers[parentName]
			if parentExists {
				allMiddleware = append(allMiddleware, parentRouter.Middlewares...)
			}
		}
	}

	return allMiddleware
}

// T040.1: buildSequentialMiddlewareHandler creates a handler that executes middleware sequentially by hierarchy level
// This implements FR-014 requirement for sequential middleware execution at each router level
func (m *Manager) buildSequentialMiddlewareHandler(ctx context.Context, router *runtime.RouterInfo, routerName string) (http.Handler, error) {
	log.Debug().Str("router", routerName).Msg("T040.1: Building sequential middleware handler")

	if router.Service == "" {
		return nil, errors.New("the service is missing on the router")
	}

	qualifiedService := provider.GetQualifiedName(ctx, router.Service)

	// Build the service handler (without middleware)
	serviceHandler, err := m.serviceManager.BuildHTTP(ctx, qualifiedService)
	if err != nil {
		return nil, err
	}

	// Create hierarchical middleware structure
	hierarchyLevels := m.buildMiddlewareHierarchy(routerName, make(map[string]bool))

	// Build sequential middleware execution handler
	handler := m.createSequentialExecutionHandler(ctx, hierarchyLevels, serviceHandler, routerName)

	// Add standard observability and access log middleware
	chain := alice.New()
	if router.DefaultRule {
		chain = chain.Append(denyrouterrecursion.WrapHandler(routerName))
	}

	chain = chain.Append(observability.WrapRouterHandler(ctx, routerName, router.Rule, qualifiedService))
	metricsHandler := metricsMiddle.RouterMetricsHandler(ctx, m.observabilityMgr.MetricsRegistry(), routerName, qualifiedService)
	chain = chain.Append(observability.WrapMiddleware(ctx, metricsHandler))
	chain = chain.Append(func(next http.Handler) (http.Handler, error) {
		return accesslog.NewFieldHandler(next, accesslog.RouterName, routerName, nil), nil
	})

	return chain.Then(handler)
}

// T040.1: buildMiddlewareHierarchy builds middleware hierarchy preserving router levels
func (m *Manager) buildMiddlewareHierarchy(routerName string, visited map[string]bool) [][]string {
	// Prevent infinite recursion
	if visited[routerName] {
		return nil
	}
	visited[routerName] = true

	router, exists := m.conf.Routers[routerName]
	if !exists {
		return nil
	}

	var hierarchyLevels [][]string

	// Process parent routers first (preserving hierarchy)
	if router.ParentRefs != nil {
		for _, parentName := range router.ParentRefs {
			parentHierarchy := m.buildMiddlewareHierarchy(parentName, visited)
			hierarchyLevels = append(hierarchyLevels, parentHierarchy...)
		}
	}

	// Add current router's middleware as final level
	if len(router.Middlewares) > 0 {
		hierarchyLevels = append(hierarchyLevels, router.Middlewares)
	}

	return hierarchyLevels
}

// T040.1: createSequentialExecutionHandler creates handler that executes middleware level by level
func (m *Manager) createSequentialExecutionHandler(ctx context.Context, hierarchyLevels [][]string, serviceHandler http.Handler, routerName string) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		log.Debug().Str("router", routerName).Int("levels", len(hierarchyLevels)).Msg("T040.1: Starting sequential middleware execution")

		currentReq := req

		// Execute middleware level by level
		for levelIndex, middlewareNames := range hierarchyLevels {
			log.Debug().Str("router", routerName).Int("level", levelIndex).Strs("middlewares", middlewareNames).Msg("T040.1: Executing middleware level")

			// Create middleware chain for this level
			var qualifiedNames []string
			for _, name := range middlewareNames {
				qualifiedNames = append(qualifiedNames, provider.GetQualifiedName(ctx, name))
			}

			// T040.1: Execute actual middleware chain for this level
			if len(qualifiedNames) > 0 {
				middlewareChain := m.middlewaresBuilder.BuildChain(ctx, qualifiedNames)

				// Create a temporary handler that captures the modified request
				var capturedReq *http.Request
				tempHandler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
					capturedReq = r
				})

				// Execute middleware chain and capture the modified request
				finalHandler, err := middlewareChain.Then(tempHandler)
				if err != nil {
					log.Error().Err(err).Str("router", routerName).Int("level", levelIndex).Msg("T040.1: Failed to build middleware chain")
					continue
				}

				// Execute middleware on current request
				finalHandler.ServeHTTP(httptest.NewRecorder(), currentReq)

				// Use the captured modified request for next level
				if capturedReq != nil {
					currentReq = capturedReq
				}
			}
		}

		// T040.1: Debug - Check what headers are present before calling service handler
		testHeaders := make([]string, 0)
		for name, values := range currentReq.Header {
			if strings.HasPrefix(name, "X-Test-") {
				for _, value := range values {
					testHeaders = append(testHeaders, name+": "+value)
				}
			}
		}
		log.Debug().Str("router", routerName).Strs("test_headers", testHeaders).Msg("T040.1: Completed sequential middleware execution, calling service handler")

		// Call final service handler with modified request
		serviceHandler.ServeHTTP(rw, currentReq)

		// T040.1: Debug - Check what headers are in the response after service handler
		responseHeaders := make([]string, 0)
		for name, values := range rw.Header() {
			if strings.HasPrefix(name, "X-Test-") {
				for _, value := range values {
					responseHeaders = append(responseHeaders, name+": "+value)
				}
			}
		}
		log.Debug().Str("router", routerName).Strs("response_headers", responseHeaders).Msg("T040.1: Response headers after service handler")
	})
}

// PopulateTreeInfo builds router tree data and populates it in the runtime configuration
func PopulateTreeInfo(runtimeConfig *runtime.Configuration, routers map[string]*dynamic.Router) {
	if runtimeConfig.Routers == nil || len(routers) == 0 {
		return
	}

	// Build router graph
	graph := NewRouterGraph()

	// Add all routers to the graph
	for name, r := range routers {
		if err := graph.AddRouter(name, r); err != nil {
			log.Error().Err(err).Msgf("Failed to add router %s to tree graph", name)
			continue
		}
	}

	// Check for circular dependencies (log error but don't fail)
	if _, err := graph.DetectCircularDependencies(); err != nil {
		log.Error().Err(err).Msg("Circular dependencies detected in router tree")
	}

	// Get node name mappings for efficient lookup
	nodeNames := graph.GetNodeNames()

	// Build tree data for each router
	treeData := make(map[string]runtime.TreeData)
	for name := range runtimeConfig.Routers {
		node := graph.GetNode(name)
		if node == nil {
			continue
		}

		data := runtime.TreeData{
			Parents:  getNodeNames(node.GetParents(), nodeNames),
			Children: getNodeNames(node.GetChildren(), nodeNames),
			Depth:    node.GetDepth(),
		}

		// Build effective middleware chain (parent middlewares + own middlewares)
		data.EffectiveMiddlewares = collectAllMiddlewares(node, nodeNames)
		treeData[name] = data
	}

	// Populate the runtime configuration
	runtime.PopulateRouterTreeInfo(runtimeConfig.Routers, treeData)
}

// getNodeNames extracts node names from RouterNode pointers using the name mapping
func getNodeNames(nodes []*RouterNode, nodeNames map[*RouterNode]string) []string {
	names := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if name, exists := nodeNames[node]; exists {
			names = append(names, name)
		}
	}
	return names
}

// collectAllMiddlewares recursively collects middlewares from all ancestors and the current node
func collectAllMiddlewares(node *RouterNode, nodeNames map[*RouterNode]string) []string {
	var allMiddlewares []string

	// Collect middlewares from all parents first (depth-first)
	for _, parent := range node.GetParents() {
		parentMiddlewares := collectAllMiddlewares(parent, nodeNames)
		allMiddlewares = append(allMiddlewares, parentMiddlewares...)
	}

	// Add this node's own middlewares
	if node.GetRouter().Middlewares != nil {
		allMiddlewares = append(allMiddlewares, node.GetRouter().Middlewares...)
	}

	return allMiddlewares
}

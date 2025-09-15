package router

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
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
	defaultObsConfig := dynamic.RouterObservabilityConfig{}
	defaultObsConfig.SetDefaults()

	// Prepare router validation results
	problematicRouters, routerErrors := m.prepareRouterValidation(precomputedProblematicRouters, precomputedRouterErrors)
	problematicRoutersMap := m.createProblematicRoutersMap(problematicRouters)
	m.logRouterValidationResults(rootCtx, problematicRouters, routerErrors)

	// Build entry point handlers for healthy routers
	entryPointHandlers := m.buildEntryPointHandlers(rootCtx, entryPoints, tls, problematicRoutersMap, defaultObsConfig)

	// Create default handlers for entry points without handlers
	m.createDefaultHandlers(rootCtx, entryPoints, entryPointHandlers, defaultObsConfig)

	return entryPointHandlers
}

// prepareRouterValidation handles router validation logic, using precomputed results if available
func (m *Manager) prepareRouterValidation(precomputedProblematicRouters []string, precomputedRouterErrors map[string]error) ([]string, map[string]error) {
	// Use precomputed validation results if provided, otherwise compute them
	if precomputedProblematicRouters != nil && precomputedRouterErrors != nil {
		return precomputedProblematicRouters, precomputedRouterErrors
	}

	// Validate router tree using RouterGraph - FR-013: graceful error handling
	return m.validateRouterTree()
}

// createProblematicRoutersMap converts a slice of problematic router names to a map for quick lookup
func (m *Manager) createProblematicRoutersMap(problematicRouters []string) map[string]bool {
	problematicRoutersMap := make(map[string]bool)
	for _, routerName := range problematicRouters {
		problematicRoutersMap[routerName] = true
	}
	return problematicRoutersMap
}

// logRouterValidationResults logs problematic routers and their validation errors
func (m *Manager) logRouterValidationResults(ctx context.Context, problematicRouters []string, routerErrors map[string]error) {
	if len(problematicRouters) == 0 {
		return
	}

	logger := log.Ctx(ctx)
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

// getObservabilityConfig retrieves the observability configuration for an entry point
func (m *Manager) getObservabilityConfig(entryPointName string, defaultObsConfig dynamic.RouterObservabilityConfig) dynamic.RouterObservabilityConfig {
	// TODO: Improve this part. Relying on models is a shortcut to get the entrypoint observability configuration. Maybe we should pass down the static configuration.
	// When the entry point has no observability configuration no model is produced,
	// and we need to create the default configuration is this case.
	epObsConfig := defaultObsConfig
	if model, ok := m.conf.Models[entryPointName+"@internal"]; ok && model != nil {
		epObsConfig = model.Observability
	}
	return epObsConfig
}

// filterHealthyRouters filters out problematic routers, keeping only healthy ones
func (m *Manager) filterHealthyRouters(routers map[string]*runtime.RouterInfo, problematicRoutersMap map[string]bool) map[string]*runtime.RouterInfo {
	healthyRouters := make(map[string]*runtime.RouterInfo)
	for routerName, routerInfo := range routers {
		if !problematicRoutersMap[routerName] {
			healthyRouters[routerName] = routerInfo
		}
	}
	return healthyRouters
}

// buildEntryPointHandlers builds handlers for each entry point with healthy routers
func (m *Manager) buildEntryPointHandlers(rootCtx context.Context, entryPoints []string, tls bool, problematicRoutersMap map[string]bool, defaultObsConfig dynamic.RouterObservabilityConfig) map[string]http.Handler {
	entryPointHandlers := make(map[string]http.Handler)

	for entryPointName, routers := range m.getHTTPRouters(rootCtx, entryPoints, tls) {
		logger := log.Ctx(rootCtx).With().Str(logs.EntryPointName, entryPointName).Logger()
		ctx := logger.WithContext(rootCtx)

		// FR-013: Filter out problematic routers, keep only healthy ones
		healthyRouters := m.filterHealthyRouters(routers, problematicRoutersMap)

		// Skip building handler if no healthy routers remain for this entry point
		if len(healthyRouters) == 0 {
			logger.Info().Msg("No healthy routers for entry point, skipping handler creation")
			continue
		}

		epObsConfig := m.getObservabilityConfig(entryPointName, defaultObsConfig)

		// Build entry point handler with automatic hierarchical optimization detection
		handler, err := m.buildEntryPointHandler(ctx, entryPointName, healthyRouters, epObsConfig)
		if err != nil {
			logger.Error().Err(err).Send()
			continue
		}

		entryPointHandlers[entryPointName] = handler
	}

	return entryPointHandlers
}

// createDefaultHandlers creates default handlers for entry points that don't have handlers yet
func (m *Manager) createDefaultHandlers(rootCtx context.Context, entryPoints []string, entryPointHandlers map[string]http.Handler, defaultObsConfig dynamic.RouterObservabilityConfig) {
	// Create default handlers.
	for _, entryPointName := range entryPoints {
		logger := log.Ctx(rootCtx).With().Str(logs.EntryPointName, entryPointName).Logger()
		ctx := logger.WithContext(rootCtx)

		handler, ok := entryPointHandlers[entryPointName]
		if ok || handler != nil {
			continue
		}

		epObsConfig := m.getObservabilityConfig(entryPointName, defaultObsConfig)

		defaultHandler, err := m.observabilityMgr.BuildEPChain(ctx, entryPointName, false, epObsConfig).Then(http.NotFoundHandler())
		if err != nil {
			logger.Error().Err(err).Send()
			continue
		}
		entryPointHandlers[entryPointName] = defaultHandler
	}
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

// hasHierarchicalRoutersInConfigs checks if any router in the provided configs has ParentRefs defined.
// Returns true if hierarchical routing is needed for this specific set of routers.
func (m *Manager) hasHierarchicalRoutersInConfigs(configs map[string]*runtime.RouterInfo) bool {
	for _, routerConfig := range configs {
		if len(routerConfig.ParentRefs) > 0 {
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

// buildEntryPointHandler builds the entry point handler with automatic hierarchical optimization detection.
// Hierarchical optimization is enabled when parent-child router relationships are detected.
func (m *Manager) buildEntryPointHandler(ctx context.Context, entryPointName string, configs map[string]*runtime.RouterInfo, config dynamic.RouterObservabilityConfig) (http.Handler, error) {
	muxer := httpmuxer.NewMuxer(m.parser)

	// Detect if hierarchical routing is needed by checking for ParentRefs
	useHierarchical := m.hasHierarchicalRoutersInConfigs(configs)
	if useHierarchical {
		muxer.EnableHierarchicalEvaluation()
		logger := log.Ctx(ctx)
		logger.Debug().Str("entryPoint", entryPointName).Msg("Hierarchical evaluation enabled for performance optimization")
	}

	defaultHandler, err := m.observabilityMgr.BuildEPChain(ctx, entryPointName, false, config).Then(http.NotFoundHandler())
	if err != nil {
		return nil, err
	}

	muxer.SetDefaultHandler(defaultHandler)

	// Collect router configurations and handlers for hierarchical setup
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

		// Store router configuration and handler for hierarchical setup
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

	// Configure hierarchical routes after all handlers are built
	if useHierarchical && len(routerConfigs) > 0 {
		if err := muxer.SetHierarchicalRoutes(routerConfigs, routerHandlers, m.middlewaresBuilder); err != nil {
			logger := log.Ctx(ctx)
			logger.Error().Err(err).Str("entryPoint", entryPointName).Msg("Failed to configure hierarchical routes, falling back to standard routing")
			// Don't return error - fall back to standard routing which is already configured
		} else {
			logger := log.Ctx(ctx)
			logger.Info().Str("entryPoint", entryPointName).Int("hierarchicalRouters", len(routerConfigs)).Msg("Hierarchical optimization configured successfully")
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
	// Simplified approach: Each router builds only its own middleware chain
	// The hierarchical engine now handles parent-child middleware execution during route evaluation

	// Qualify middleware names for this router only
	var qualifiedNames []string
	for _, name := range router.Middlewares {
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

// Removed buildSequentialMiddlewareHandler - hierarchical evaluation engine now handles middleware execution during route evaluation

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

// Removed createSequentialExecutionHandler - middleware execution now happens during hierarchical route evaluation

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

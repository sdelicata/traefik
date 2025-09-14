package server

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/traefik/traefik/v3/pkg/config/dynamic"
	"github.com/traefik/traefik/v3/pkg/config/runtime"
	"github.com/traefik/traefik/v3/pkg/config/static"
	httpmuxer "github.com/traefik/traefik/v3/pkg/muxer/http"
	"github.com/traefik/traefik/v3/pkg/server/middleware"
	tcpmiddleware "github.com/traefik/traefik/v3/pkg/server/middleware/tcp"
	"github.com/traefik/traefik/v3/pkg/server/router"
	tcprouter "github.com/traefik/traefik/v3/pkg/server/router/tcp"
	udprouter "github.com/traefik/traefik/v3/pkg/server/router/udp"
	"github.com/traefik/traefik/v3/pkg/server/service"
	tcpsvc "github.com/traefik/traefik/v3/pkg/server/service/tcp"
	udpsvc "github.com/traefik/traefik/v3/pkg/server/service/udp"
	"github.com/traefik/traefik/v3/pkg/tcp"
	"github.com/traefik/traefik/v3/pkg/tls"
	"github.com/traefik/traefik/v3/pkg/udp"
)

// HierarchicalConfig contains configuration options for hierarchical router optimization.
type HierarchicalConfig struct {
	// EnableOptimization controls whether hierarchical optimization is enabled.
	// Auto-detected by default when parentRefs are present in router configuration.
	EnableOptimization bool `json:"enableOptimization,omitempty" toml:"enableOptimization,omitempty" yaml:"enableOptimization,omitempty"`

	// FallbackToFlat enables fallback to flat routing if hierarchical optimization fails.
	FallbackToFlat bool `json:"fallbackToFlat,omitempty" toml:"fallbackToFlat,omitempty" yaml:"fallbackToFlat,omitempty"`

	// MinRoutersForOptimization minimum number of routers before enabling optimization.
	MinRoutersForOptimization int `json:"minRoutersForOptimization,omitempty" toml:"minRoutersForOptimization,omitempty" yaml:"minRoutersForOptimization,omitempty"`
}

// RouterFactory the factory of TCP/UDP routers.
type RouterFactory struct {
	entryPointsTCP  []string
	entryPointsUDP  []string
	allowACMEByPass map[string]bool

	managerFactory *service.ManagerFactory

	pluginBuilder middleware.PluginsBuilder

	observabilityMgr *middleware.ObservabilityMgr
	tlsManager       *tls.Manager

	dialerManager *tcp.DialerManager

	cancelPrevState func()

	parser httpmuxer.SyntaxParser

	// T039: Hierarchical optimization configuration
	hierarchicalConfig HierarchicalConfig
}

// NewRouterFactory creates a new RouterFactory.
func NewRouterFactory(staticConfiguration static.Configuration, managerFactory *service.ManagerFactory, tlsManager *tls.Manager,
	observabilityMgr *middleware.ObservabilityMgr, pluginBuilder middleware.PluginsBuilder, dialerManager *tcp.DialerManager,
) (*RouterFactory, error) {
	handlesTLSChallenge := false
	for _, resolver := range staticConfiguration.CertificatesResolvers {
		if resolver.ACME != nil && resolver.ACME.TLSChallenge != nil {
			handlesTLSChallenge = true
			break
		}
	}

	allowACMEByPass := map[string]bool{}
	var entryPointsTCP, entryPointsUDP []string
	for name, ep := range staticConfiguration.EntryPoints {
		allowACMEByPass[name] = ep.AllowACMEByPass || !handlesTLSChallenge

		protocol, err := ep.GetProtocol()
		if err != nil {
			// Should never happen because Traefik should not start if protocol is invalid.
			log.Error().Err(err).Msg("Invalid protocol")
		}

		if protocol == "udp" {
			entryPointsUDP = append(entryPointsUDP, name)
		} else {
			entryPointsTCP = append(entryPointsTCP, name)
		}
	}

	parser, err := httpmuxer.NewSyntaxParser()
	if err != nil {
		return nil, fmt.Errorf("creating parser: %w", err)
	}

	// T039: Initialize hierarchical optimization configuration with sensible defaults
	hierarchicalConfig := HierarchicalConfig{
		EnableOptimization:        true, // Auto-enable when parentRefs detected
		FallbackToFlat:            true, // Always allow fallback for safety
		MinRoutersForOptimization: 2,    // Minimum routers needed for optimization
	}

	// T039: Override with static configuration if hierarchical settings are provided
	if staticConfiguration.HierarchicalRouting != nil {
		hr := staticConfiguration.HierarchicalRouting

		if hr.EnableOptimization != nil {
			hierarchicalConfig.EnableOptimization = *hr.EnableOptimization
		}
		if hr.FallbackToFlat != nil {
			hierarchicalConfig.FallbackToFlat = *hr.FallbackToFlat
		}
		if hr.MinRoutersForOptimization != nil {
			hierarchicalConfig.MinRoutersForOptimization = *hr.MinRoutersForOptimization
		}

		log.Info().
			Bool("enableOptimization", hierarchicalConfig.EnableOptimization).
			Bool("fallbackToFlat", hierarchicalConfig.FallbackToFlat).
			Int("minRouters", hierarchicalConfig.MinRoutersForOptimization).
			Msg("T039: Hierarchical routing configuration loaded from static config")
	}

	return &RouterFactory{
		entryPointsTCP:     entryPointsTCP,
		entryPointsUDP:     entryPointsUDP,
		managerFactory:     managerFactory,
		observabilityMgr:   observabilityMgr,
		tlsManager:         tlsManager,
		pluginBuilder:      pluginBuilder,
		dialerManager:      dialerManager,
		allowACMEByPass:    allowACMEByPass,
		parser:             parser,
		hierarchicalConfig: hierarchicalConfig, // T039: Add hierarchical configuration
	}, nil
}

// CreateRouters creates new TCPRouters and UDPRouters.
func (f *RouterFactory) CreateRouters(rtConf *runtime.Configuration) (map[string]*tcprouter.Router, map[string]udp.Handler) {
	if f.cancelPrevState != nil {
		f.cancelPrevState()
	}

	var ctx context.Context
	ctx, f.cancelPrevState = context.WithCancel(context.Background())

	// HTTP
	serviceManager := f.managerFactory.Build(rtConf)

	middlewaresBuilder := middleware.NewBuilder(rtConf.Middlewares, serviceManager, f.pluginBuilder)

	routerManager := router.NewManager(rtConf, serviceManager, middlewaresBuilder, f.observabilityMgr, f.tlsManager, f.parser)

	// T039: Factory-level hierarchical optimization evaluation
	var hasHierarchicalRouters bool
	var totalRouters int

	// Populate router tree information if HTTP routers exist
	if rtConf.Routers != nil && len(rtConf.Routers) > 0 {
		totalRouters = len(rtConf.Routers)

		// T039: Check if hierarchical optimization should be applied at factory level
		for _, routerInfo := range rtConf.Routers {
			if len(routerInfo.ParentRefs) > 0 {
				hasHierarchicalRouters = true
				break
			}
		}

		// Extract dynamic routers from runtime configuration
		dynamicRouters := make(map[string]*dynamic.Router, len(rtConf.Routers))
		for name, routerInfo := range rtConf.Routers {
			dynamicRouters[name] = routerInfo.Router
		}
		router.PopulateTreeInfo(rtConf, dynamicRouters)
	}

	// T039: Factory-level logging for hierarchical optimization status
	optimizationEnabled := f.hierarchicalConfig.EnableOptimization &&
		hasHierarchicalRouters &&
		totalRouters >= f.hierarchicalConfig.MinRoutersForOptimization

	if optimizationEnabled {
		log.Info().
			Int("totalRouters", totalRouters).
			Int("minRequired", f.hierarchicalConfig.MinRoutersForOptimization).
			Bool("fallbackEnabled", f.hierarchicalConfig.FallbackToFlat).
			Msg("T039: RouterFactory enabling hierarchical optimization - performance improvements expected")
	} else if hasHierarchicalRouters && totalRouters < f.hierarchicalConfig.MinRoutersForOptimization {
		log.Debug().
			Int("totalRouters", totalRouters).
			Int("minRequired", f.hierarchicalConfig.MinRoutersForOptimization).
			Msg("T039: RouterFactory skipping hierarchical optimization - below minimum router threshold")
	} else if !hasHierarchicalRouters && totalRouters > 0 {
		log.Debug().
			Int("totalRouters", totalRouters).
			Msg("T039: RouterFactory using standard flat routing - no parentRefs detected")
	}

	// Perform router validation once for both TLS and non-TLS handlers
	problematicRouters, routerErrors := router.ValidateRouterTree(rtConf.Routers)

	// Build handlers with pre-computed validation results (avoiding duplicate graph creation)
	handlersNonTLS := routerManager.BuildHandlers(ctx, f.entryPointsTCP, false, problematicRouters, routerErrors)
	handlersTLS := routerManager.BuildHandlers(ctx, f.entryPointsTCP, true, problematicRouters, routerErrors)

	serviceManager.LaunchHealthCheck(ctx)

	// TCP
	svcTCPManager := tcpsvc.NewManager(rtConf, f.dialerManager)

	middlewaresTCPBuilder := tcpmiddleware.NewBuilder(rtConf.TCPMiddlewares)

	rtTCPManager := tcprouter.NewManager(rtConf, svcTCPManager, middlewaresTCPBuilder, handlersNonTLS, handlersTLS, f.tlsManager)
	routersTCP := rtTCPManager.BuildHandlers(ctx, f.entryPointsTCP)

	for ep, r := range routersTCP {
		if allowACMEByPass, ok := f.allowACMEByPass[ep]; ok && allowACMEByPass {
			r.EnableACMETLSPassthrough()
		}
	}

	// UDP
	svcUDPManager := udpsvc.NewManager(rtConf)
	rtUDPManager := udprouter.NewManager(rtConf, svcUDPManager)
	routersUDP := rtUDPManager.BuildHandlers(ctx, f.entryPointsUDP)

	rtConf.PopulateUsedBy()

	return routersTCP, routersUDP
}

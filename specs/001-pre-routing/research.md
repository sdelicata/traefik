# Research: Router Tree Implementation

## Overview
This document consolidates research findings for implementing router tree with parentRefs in Traefik.

## Current Router Implementation Analysis

### Decision: Extend existing Router struct
**Rationale**: The Router struct in `pkg/config/dynamic/http_config.go` is the central configuration point for all routers. Adding a `ParentRefs` field here ensures all providers and runtime components automatically support the feature.

**Alternatives considered**:
- Creating a separate "PreRouter" type - Rejected: Would require duplicating router logic
- Using middleware-only approach - Rejected: Cannot control router evaluation order

### Router Evaluation Flow

**Current flow**:
1. Manager builds handlers for entry points (`pkg/server/router/router.go`)
2. Routes added to muxer with priority
3. Muxer evaluates routes in priority order
4. First match wins and processes request

**Decision**: Extend muxer to support parent-aware routing
**Rationale**: The muxer already handles priority-based routing. Adding parent evaluation logic here keeps changes isolated.

**Implementation approach**:
1. Add `ParentRefs []string` field to Router struct
2. Build router dependency graph during configuration loading
3. Modify muxer to check parent match before evaluating child routers
4. Inherit middleware from parent routers in the chain

## Circular Dependency Detection

**Decision**: Implement 3-loop limit with detection during configuration validation
**Rationale**: Simple, effective, and matches specification requirements

**Implementation**:
- Add validation in `pkg/config/runtime/runtime_http.go`
- Track visited routers during graph traversal
- Return error if loop count exceeds 3

## Middleware Inheritance

**Current middleware chain building**:
- `buildHTTPHandler` in `pkg/server/router/router.go`
- Uses alice.Chain for composition
- Observability wraps middleware chain

**Decision**: Prepend parent middleware to child router's middleware chain
**Rationale**: Maintains existing middleware ordering semantics while enabling inheritance

**Implementation**:
1. When building child router handler, retrieve parent router(s)
2. Collect parent middleware in order (root to child)
3. Prepend to child's middleware list before building chain

## Provider Integration

**Current provider pattern**:
- Each provider builds dynamic configuration
- Configurations merged in `pkg/provider/configuration.go`
- Routers added via `AddRouter()`

**Decision**: No provider changes needed initially
**Rationale**: ParentRefs can be specified in configuration files first. Provider-specific support can be added incrementally.

**File provider example**:
```yaml
http:
  routers:
    parent-router:
      rule: "Host(`example.com`)"
      middlewares:
        - auth
    child-router:
      parentRefs:
        - parent-router
      rule: "Path(`/api`)"
      service: api-service
```

## Testing Strategy

### Integration Tests
**Location**: `integration/` directory

**Test scenarios**:
1. Basic parent-child routing
2. Multi-level tree (grandparent → parent → child)
3. Multiple parents for one child
4. Circular dependency detection
5. Middleware inheritance
6. Parent router not matching (child not evaluated)

### Unit Tests
**Locations**:
- `pkg/config/dynamic/` - Router struct validation
- `pkg/server/router/` - Handler building with parents
- `pkg/muxer/http/` - Parent-aware route matching

## Documentation Updates

**Files to update**:
- `docs/content/routing/routers/index.md` - Add parentRefs documentation
- `docs/content/reference/dynamic-configuration/` - Update configuration reference
- `docs/content/migration/` - Add migration guide for v3.x

**Make docs process**:
- Documentation is generated from Go structs using struct tags
- Add json/yaml/toml tags to ParentRefs field
- Run `make docs` to regenerate documentation

## Performance Considerations

**Decision**: Use caching for router dependency graph
**Rationale**: Avoid recalculating parent relationships on every request

**Implementation**:
- Build dependency graph once during configuration loading
- Cache parent-child relationships
- Invalidate cache on configuration reload

## Backward Compatibility

**Guaranteed compatibility**:
- Routers without parentRefs work exactly as before
- No changes to existing router behavior
- Configuration files without parentRefs remain valid
- All existing tests should continue passing

## Risk Assessment

**Low risk areas**:
- Adding field to Router struct
- Configuration validation
- Documentation updates

**Medium risk areas**:
- Muxer modifications for parent evaluation
- Middleware chain inheritance
- Circular dependency detection

**Mitigation**:
- Comprehensive test coverage before implementation
- Feature flag for gradual rollout (if needed)
- Extensive integration testing with real configurations
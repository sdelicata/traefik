# Implementation Plan: Hierarchical Router with Performance Optimization

**Branch**: `001-pre-routing` | **Date**: 2025-09-12 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/001-pre-routing/spec.md`

## Execution Flow (/plan command scope)
```
1. Load feature spec from Input path
   → If not found: ERROR "No feature spec at {path}"
2. Fill Technical Context (scan for NEEDS CLARIFICATION)
   → Detect Project Type from context (web=frontend+backend, mobile=app+api)
   → Set Structure Decision based on project type
3. Evaluate Constitution Check section below
   → If violations exist: Document in Complexity Tracking
   → If no justification possible: ERROR "Simplify approach first"
   → Update Progress Tracking: Initial Constitution Check
4. Execute Phase 0 → research.md
   → If NEEDS CLARIFICATION remain: ERROR "Resolve unknowns"
5. Execute Phase 1 → contracts, data-model.md, quickstart.md, CLAUDE.md for Claude Code
6. Re-evaluate Constitution Check section
   → If new violations: Refactor design, return to Phase 1
   → Update Progress Tracking: Post-Design Constitution Check
7. Plan Phase 2 → Describe task generation approach (DO NOT create tasks.md)
8. STOP - Ready for /tasks command
```

**IMPORTANT**: The /plan command STOPS at step 7. Phases 2-4 are executed by other commands:
- Phase 2: /tasks command creates tasks.md
- Phase 3-4: Implementation execution (manual or via tools)

## Summary
Implement a hierarchical router system for Traefik that organizes routes in parent-child relationships with staged evaluation for performance optimization. The system enables parent routers to execute middlewares sequentially and pass modified requests to child routers for evaluation, supporting use cases like authentication middleware adding headers that child routers use for routing decisions. Route matching complexity is reduced from O(n×p) to O(d×log n) through hierarchical evaluation and early termination. All existing matcher implementations (`MatcherFunc`, `matchersTree`, `route.matchers.match()`) are preserved unchanged - this is purely a route organization optimization.

## Technical Context
**Language/Version**: Go 1.24.0  
**Primary Dependencies**: Existing Traefik HTTP muxer, router graph system, middleware framework  
**Storage**: N/A (runtime route organization)  
**Testing**: Go testing framework, integration tests with Docker containers  
**Target Platform**: Linux server, containerized environments (Docker, Kubernetes)
**Project Type**: single (HTTP server enhancement)  
**Performance Goals**: Sub-millisecond request processing for 1000+ hierarchical routes, O(d×log n) complexity  
**Constraints**: <200ms p95 latency, maintain 100% backward compatibility, preserve existing matcher logic  
**Scale/Scope**: Support 1000+ routes with 3+ hierarchy levels, zero-downtime configuration updates

**Enhanced Requirements Context**:
- **Route Organization Optimization**: This changes evaluation sequence/grouping, NOT matcher implementation
- **Matcher Preservation**: All existing `MatcherFunc`, `matchersTree`, `route.matchers.match()` logic unchanged
- **Hierarchical Staging**: Parent routes evaluate first, child routes only if parent matches
- **Early Termination**: No parent match = skip entire child subtrees
- **Sequential Middleware Execution**: Each router level executes its middlewares sequentially, and the modified request is passed to the next level for evaluation
- **Request Modification Flow**: Parent middleware transformations (e.g., authentication headers) are available to child routers for routing decisions

## Constitution Check
*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

**Simplicity**:
- Projects: 1 (HTTP server enhancement within existing Traefik)
- Using framework directly? Yes - extending existing muxer/router system, no wrapper classes
- Single data model? Yes - extending existing router structures with parentRefs
- Avoiding patterns? Yes - reusing existing route matching patterns, no unnecessary abstractions

**Architecture**:
- EVERY feature as library? No - this is core HTTP routing enhancement
- Libraries listed: Extending pkg/muxer/http and pkg/server/router
- CLI per library: N/A - configuration-based feature
- Library docs: Will update existing documentation

**Testing (NON-NEGOTIABLE)**:
- RED-GREEN-Refactor cycle enforced? YES - performance tests must fail before optimization
- Git commits show tests before implementation? YES - benchmark and integration tests first
- Order: Contract→Integration→E2E→Unit strictly followed? YES
- Real dependencies used? YES - actual HTTP requests, real middleware chains
- Integration tests for: Hierarchical routing, performance benchmarks, error scenarios
- FORBIDDEN: Implementation before test, skipping RED phase

**Observability**:
- Structured logging included? YES - extending existing Traefik logging
- Frontend logs → backend? N/A - server-side feature
- Error context sufficient? YES - router hierarchy error reporting

**Versioning**:
- Version number assigned? 3.6.0 (following Traefik versioning)
- BUILD increments on every change? YES
- Breaking changes handled? NO breaking changes - 100% backward compatibility

## Project Structure

### Documentation (this feature)
```
specs/001-pre-routing/
├── plan.md              # This file (/plan command output)
├── research.md          # Phase 0 output (/plan command)
├── data-model.md        # Phase 1 output (/plan command)
├── quickstart.md        # Phase 1 output (/plan command)
├── contracts/           # Phase 1 output (/plan command)
└── tasks.md             # Phase 2 output (/tasks command - NOT created by /plan)
```

### Source Code (repository root)
```
pkg/
├── config/dynamic/
│   └── http_config.go           # Enhanced router config with parentRefs
├── server/router/
│   ├── graph.go                 # Router dependency graph (existing)
│   ├── router.go                # Hierarchical router implementation
│   └── hierarchy.go             # NEW: Hierarchical evaluation engine
├── muxer/http/
│   ├── mux.go                   # Enhanced muxer with staged evaluation
│   └── hierarchy_mux.go         # NEW: Hierarchical muxer implementation
└── middlewares/                 # Existing middleware framework (unchanged)

integration/
├── fixtures/router/
│   ├── hierarchy_basic.toml     # Basic parent-child scenarios
│   ├── hierarchy_complex.toml   # Multi-level hierarchies
│   └── performance_test.toml    # 1000+ route performance tests
└── hierarchy_test.go            # Integration tests for hierarchical routing
```

## Complexity Tracking
*Requirements that challenge simplicity - justify or eliminate*

**Justified Complexity**:
1. **Hierarchical Evaluation Engine**: Required for O(d×log n) performance improvement
   - Justification: Critical for 1000+ route performance (FR-014 to FR-017)
   - Mitigation: Reuses existing route matching logic, only changes evaluation order

2. **Parent-Child Graph Management**: Required for dependency tracking
   - Justification: Necessary for request modification flow and error isolation
   - Mitigation: Extends existing router graph system (pkg/server/router/graph.go)

3. **Sequential Middleware Execution and Request Modification**: Required for middleware execution at each router level
   - Justification: Core feature requirement - each level must execute middlewares sequentially and pass modified request to next level (e.g., authentication headers for child router routing)
   - Mitigation: Uses existing middleware framework, no new middleware patterns, extends existing request flow

**Eliminated Complexity**:
- Custom matcher implementations → Reuse existing `matchersTree.match()`
- New configuration formats → Extend existing router configuration
- Complex caching systems → Simple hierarchical evaluation order

## Phase Planning

### Phase 0: Research & Analysis
**Objectives**:
- Analyze current muxer performance bottlenecks
- Research hierarchical evaluation strategies
- Validate performance improvement potential
- Document preservation requirements for existing matchers

**Key Questions**:
- Current O(n×p) complexity measurement and bottleneck identification
- Hierarchical evaluation algorithms and data structures
- Router graph integration approach
- Performance benchmarking methodology

### Phase 1: Design & Architecture
**Objectives**:
- Design hierarchical evaluation engine preserving existing matchers
- Define router configuration extensions (parentRefs)
- Plan staged evaluation flow with sequential middleware execution at each level
- Design request modification flow ensuring parent middleware changes are available to child routers
- Design performance benchmarking framework

**Deliverables**:
- Data model for router hierarchy relationships
- API contracts for hierarchical evaluation
- Performance benchmark specifications
- Integration test scenarios

### Phase 2: Implementation Planning (Tasks)
**Approach**: Generate granular implementation tasks covering:
1. **Router Configuration Enhancement**: Add parentRefs support
2. **Hierarchical Evaluation Engine**: Staged evaluation implementation with sequential middleware execution at each level
3. **Sequential Middleware Flow**: Ensure middleware modifications are passed between router levels for child router evaluation
4. **Performance Optimization**: Early termination and search space reduction
5. **Integration Testing**: 1000+ route scenarios with performance validation and middleware modification validation
6. **Backward Compatibility**: Preserve all existing behaviors and APIs

**Success Criteria**:
- All 23 functional requirements implemented and tested (including FR-014: sequential middleware execution)
- Performance benchmarks show O(n×p) → O(d×log n) improvement
- 100% backward compatibility maintained
- Sub-millisecond processing for 1000+ hierarchical routes
- Middleware modifications (e.g., authentication headers) successfully passed between router levels and available for child router evaluation

## Progress Tracking
- [x] Initial Constitution Check passed
- [x] Feature specification loaded and analyzed  
- [x] Technical context established with performance focus
- [ ] Phase 0: Research completed
- [ ] Phase 1: Design artifacts generated
- [ ] Post-Design Constitution Check
- [ ] Phase 2: Ready for /tasks command

## Notes
This implementation preserves the core Traefik architecture while introducing performance-critical hierarchical evaluation. The key insight is that this is **route organization optimization**, not matcher modification - all existing route matching logic remains unchanged while dramatically improving evaluation efficiency through staged hierarchical processing.
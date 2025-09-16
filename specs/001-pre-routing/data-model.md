# Data Model: Router Tree

## Extended Router Entity

### Router (Extended)
**Location**: `pkg/config/dynamic/http_config.go`

**Fields** (new field in **bold**):
- `EntryPoints` ([]string): Entry points where router listens
- `Middlewares` ([]string): Middleware chain applied to requests  
- `Service` (string): Backend service name to route to
- `Rule` (string): Matching rule expression
- `Priority` (int): Router priority for conflict resolution
- `TLS` (*RouterTLSConfig): TLS configuration
- **`ParentRefs`** **([]string)**: References to parent router names
- `Observability` (*RouterObservabilityConfig): Observability settings

**Validation Rules**:
- ParentRefs must reference existing routers
- ParentRefs cannot create circular dependencies (max 3 loops)
- ParentRefs are optional (empty = top-level router)
- Router names in ParentRefs must be valid identifiers

**JSON/YAML Tags**:
```go
ParentRefs []string `json:"parentRefs,omitempty" toml:"parentRefs,omitempty" yaml:"parentRefs,omitempty" label:"allowEmpty" file:"allowEmpty" kv:"allowEmpty" export:"true"`
```

## Runtime Entities

### RouterInfo (Extended)
**Location**: `pkg/config/runtime/runtime.go`

**Additional Runtime Fields**:
- `Parents` ([]string): Resolved parent router names (validated)
- `Children` ([]string): List of child router names (computed)
- `Depth` (int): Depth in tree (0 = top-level)
- `EffectiveMiddlewares` ([]string): Combined middleware chain including parents

### RouterGraph
**Location**: `pkg/server/router/graph.go` (new file)

**Purpose**: Manages router parent-child relationships

**Fields**:
- `routers` (map[string]*RouterNode): All routers by name
- `topLevel` ([]string): Routers without parents
- `validated` (bool): Graph validation status

### RouterNode
**Location**: `pkg/server/router/graph.go` (new file)

**Fields**:
- `Name` (string): Router identifier
- `Router` (*dynamic.Router): Router configuration
- `Parents` ([]*RouterNode): Parent router nodes
- `Children` ([]*RouterNode): Child router nodes
- `Visited` (bool): For cycle detection
- `Depth` (int): Tree depth

## Configuration Examples

### YAML Configuration
```yaml
http:
  routers:
    # Top-level router (no parentRefs)
    auth-router:
      rule: "Host(`api.example.com`)"
      middlewares:
        - auth-middleware
      
    # Child router with single parent
    api-router:
      parentRefs:
        - auth-router
      rule: "PathPrefix(`/v1`)"
      service: api-service
      
    # Grandchild router
    users-router:
      parentRefs:
        - api-router
      rule: "Path(`/v1/users`)"
      service: users-service
```

### TOML Configuration
```toml
[http.routers.auth-router]
  rule = "Host(`api.example.com`)"
  middlewares = ["auth-middleware"]

[http.routers.api-router]
  parentRefs = ["auth-router"]
  rule = "PathPrefix(`/v1`)"
  service = "api-service"
```

### JSON Configuration
```json
{
  "http": {
    "routers": {
      "auth-router": {
        "rule": "Host(`api.example.com`)",
        "middlewares": ["auth-middleware"]
      },
      "api-router": {
        "parentRefs": ["auth-router"],
        "rule": "PathPrefix(`/v1`)",
        "service": "api-service"
      }
    }
  }
}
```

## State Transitions

### Router Evaluation States
1. **Unchecked**: Router not yet evaluated
2. **Parent Checking**: Evaluating parent routers
3. **Parent Matched**: At least one parent matched
4. **Rule Checking**: Evaluating router's own rule
5. **Matched**: Router matched and can handle request
6. **Not Matched**: Router or parents didn't match

### Configuration Loading States
1. **Parsing**: Reading configuration files
2. **Validating**: Checking parentRefs validity
3. **Graph Building**: Constructing router dependency graph
4. **Cycle Detection**: Checking for circular dependencies
5. **Ready**: Configuration loaded and validated

## Relationships

### Parent-Child Relationship
- **Type**: Many-to-Many (router can have multiple parents, parent can have multiple children)
- **Validation**: Parents must exist before children reference them
- **Cascade**: Parent middleware cascades to children
- **Evaluation**: Child only evaluated if parent matches

### Router-Service Relationship
- **Type**: Many-to-One (unchanged)
- **Validation**: Service must exist (unchanged)
- **Cascade**: No change from current behavior

### Router-Middleware Relationship  
- **Type**: Many-to-Many (unchanged)
- **Enhancement**: Child inherits parent's middleware chain
- **Order**: Parent middleware applied before child middleware

## Error Conditions

### Configuration Errors
- `RouterParentNotFound`: Referenced parent router doesn't exist
- `RouterCircularDependency`: Circular reference detected in parentRefs
- `RouterLoopLimitExceeded`: More than 3 loops in parent chain
- `RouterInvalidParentRef`: Parent reference is malformed

### Runtime Errors
- `RouterParentEvaluationFailed`: Error evaluating parent router
- `RouterGraphConstructionFailed`: Failed to build router dependency graph
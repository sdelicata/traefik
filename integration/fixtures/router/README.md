# Router Tree Test Fixtures

This directory contains test configuration files for the router tree feature (parentRefs).

## Valid Configurations

### `basic_tree.toml`
- **Purpose**: Basic parent-child router relationship
- **Scenario**: Parent router provides authentication, child router adds API headers
- **Tests**: Basic parentRefs functionality, middleware inheritance

### `auth_prerouting.toml`
- **Purpose**: Authentication pre-routing example from quickstart
- **Scenario**: Three-level tree (auth → api → admin)
- **Tests**: Multi-level middleware inheritance, real-world authentication use case

### `multilevel_tree.toml`
- **Purpose**: Complex four-level router tree
- **Scenario**: global → api → v1 → endpoints, plus admin branch
- **Tests**: Deep tree navigation, multiple child branches

### `multiple_parents.toml`
- **Purpose**: Routers with multiple parent references
- **Scenario**: Child routers requiring multiple parents to match
- **Tests**: Multiple parentRefs, AND logic for parent matching

## Invalid Configurations (Should Be Rejected)

### `circular_dependency_2_routers.toml`
- **Purpose**: Test 2-router circular dependency detection
- **Expected**: Configuration should be rejected
- **Tests**: Basic circular dependency validation

### `circular_dependency_3_routers.toml`
- **Purpose**: Test 3-router circular dependency (at the limit)
- **Expected**: Configuration should be rejected
- **Tests**: 3-loop limit enforcement

### `circular_dependency_4_routers.toml`
- **Purpose**: Test 4-router circular dependency (exceeds limit)
- **Expected**: Configuration should be rejected
- **Tests**: Loop limit exceeded validation

### `invalid_parent_ref.toml`
- **Purpose**: Test non-existent parent router references
- **Expected**: Configuration should be rejected
- **Tests**: Parent existence validation

## Usage in Tests

These fixtures are designed to be used in integration tests:

1. **Valid configurations**: Load and verify they work correctly
2. **Invalid configurations**: Load and verify they are properly rejected
3. **Behavior testing**: Use valid configurations to test request routing and middleware application

## Test Scenarios Covered

- ✅ Basic parent-child relationships
- ✅ Multi-level trees (up to 4 levels deep)
- ✅ Multiple parent references
- ✅ Middleware inheritance through tree
- ✅ Circular dependency detection (2, 3, 4+ routers)
- ✅ Invalid parent reference handling
- ✅ Real-world authentication use cases
- ✅ Complex routing scenarios

## Expected Middleware Chains

### `auth_prerouting.toml`
- `admin-router`: auth → api-headers → admin-only
- `api-router`: auth → api-headers
- `auth-router`: auth
- `public-router`: (no middleware)

### `multilevel_tree.toml`
- `users-router`: global-security → api-auth → v1-headers
- `admin-users-router`: global-security → api-auth → admin-auth
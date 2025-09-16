# Quickstart: Router Tree with ParentRefs

This guide demonstrates how to use the new router tree feature in Traefik v3.x.

## Prerequisites

- Traefik v3.x with parentRefs support
- Docker (for test services)
- Basic understanding of Traefik routing

## Basic Example: Authentication Pre-routing

This example shows how to require authentication for all API routes using parent routers.

### 1. Create the configuration file

Create `traefik.yml`:

```yaml
# Static configuration
entryPoints:
  web:
    address: ":80"
  websecure:
    address: ":443"

providers:
  file:
    filename: dynamic.yml
    watch: true

api:
  dashboard: true
  debug: true
```

Create `dynamic.yml`:

```yaml
http:
  # Middleware definitions
  middlewares:
    auth:
      basicAuth:
        users:
          - "admin:$2y$10$..."  # admin:password
    
    api-headers:
      headers:
        customRequestHeaders:
          X-API-Version: "v1"
    
    admin-only:
      headers:
        customRequestHeaders:
          X-Admin: "true"

  # Service definitions
  services:
    api-service:
      loadBalancer:
        servers:
          - url: "http://localhost:3000"
    
    admin-service:
      loadBalancer:
        servers:
          - url: "http://localhost:3001"
    
    public-service:
      loadBalancer:
        servers:
          - url: "http://localhost:3002"

  # Router tree
  routers:
    # Top-level router - applies auth to all children
    auth-router:
      entryPoints:
        - web
      rule: "Host(`api.example.com`)"
      middlewares:
        - auth
    
    # Child router - inherits auth, adds API headers
    api-router:
      parentRefs:
        - auth-router
      rule: "PathPrefix(`/api`)"
      middlewares:
        - api-headers
      service: api-service
    
    # Grandchild router - inherits auth and api-headers, adds admin-only
    admin-router:
      parentRefs:
        - api-router
      rule: "Path(`/api/admin`)"
      middlewares:
        - admin-only
      service: admin-service
    
    # Independent router - no auth required
    public-router:
      entryPoints:
        - web
      rule: "Host(`public.example.com`)"
      service: public-service
```

### 2. Start test services

Create `docker-compose.yml`:

```yaml
version: '3.8'

services:
  traefik:
    image: traefik:v3.x
    ports:
      - "80:80"
      - "443:443"
      - "8080:8080"
    volumes:
      - ./traefik.yml:/traefik.yml
      - ./dynamic.yml:/dynamic.yml
    
  api:
    image: traefik/whoami
    ports:
      - "3000:80"
    environment:
      - WHOAMI_NAME=API-Service
  
  admin:
    image: traefik/whoami
    ports:
      - "3001:80"
    environment:
      - WHOAMI_NAME=Admin-Service
  
  public:
    image: traefik/whoami
    ports:
      - "3002:80"
    environment:
      - WHOAMI_NAME=Public-Service
```

### 3. Test the tree

```bash
# Start services
docker-compose up -d

# Test public route (no auth required)
curl http://public.example.com
# Response: Public-Service

# Test API route without auth (should fail)
curl http://api.example.com/api/users
# Response: 401 Unauthorized

# Test API route with auth (should work)
curl -u admin:password http://api.example.com/api/users
# Response: API-Service with headers X-API-Version: v1

# Test admin route with auth (should work)
curl -u admin:password http://api.example.com/api/admin
# Response: Admin-Service with headers X-API-Version: v1, X-Admin: true

# Test non-matching parent (child not evaluated)
curl http://wrong.example.com/api/users
# Response: 404 Not Found
```

## Advanced Example: Multi-Domain Pre-routing

This example shows multiple parent routers for complex routing scenarios.

```yaml
http:
  routers:
    # CORS handler for API domains
    cors-router:
      entryPoints:
        - web
      rule: "Host(`api.example.com`) || Host(`api.staging.example.com`)"
      middlewares:
        - cors
    
    # Rate limiting for public endpoints
    rate-limit-router:
      entryPoints:
        - web
      rule: "Host(`api.example.com`)"
      middlewares:
        - rate-limit
    
    # Public API - requires both CORS and rate limiting
    public-api-router:
      parentRefs:
        - cors-router
        - rate-limit-router
      rule: "PathPrefix(`/public`)"
      service: public-api
    
    # Internal API - only requires CORS
    internal-api-router:
      parentRefs:
        - cors-router
      rule: "Host(`api.staging.example.com`) && PathPrefix(`/internal`)"
      service: internal-api
```

## Validation and Debugging

### Check router tree via API

```bash
# Get all routers with tree info
curl http://localhost:8080/api/http/routers | jq

# Get specific router details
curl http://localhost:8080/api/http/routers/admin-router | jq
```

Expected response for `admin-router`:

```json
{
  "name": "admin-router",
  "status": "enabled",
  "rule": "Path(`/api/admin`)",
  "parentRefs": ["api-router"],
  "parents": ["api-router"],
  "depth": 2,
  "middlewares": ["admin-only"],
  "effectiveMiddlewares": ["auth", "api-headers", "admin-only"],
  "service": "admin-service"
}
```

### Common Issues and Solutions

#### Issue: Child router not working

**Symptom**: Requests to child router return 404

**Solution**: Verify parent router rule matches the request:
```bash
# Check if parent matches
curl -v http://api.example.com/test 2>&1 | grep "Host:"
```

#### Issue: Circular dependency error

**Symptom**: Configuration fails with "RouterCircularDependency" error

**Solution**: Check router references don't create loops:
```yaml
# WRONG - creates a loop
routers:
  router-a:
    parentRefs: ["router-b"]
  router-b:
    parentRefs: ["router-a"]

# CORRECT - tree structure
routers:
  router-a:
    rule: "Host(`example.com`)"
  router-b:
    parentRefs: ["router-a"]
    rule: "PathPrefix(`/api`)"
```

#### Issue: Middleware not inherited

**Symptom**: Parent middleware not applied to child requests

**Solution**: Check `effectiveMiddlewares` in runtime API to verify inheritance:
```bash
curl http://localhost:8080/api/http/routers/child-router | jq .effectiveMiddlewares
```

## Testing Script

Save as `test-tree.sh`:

```bash
#!/bin/bash

echo "Testing Router Tree..."

# Test 1: Public route (no auth)
echo -n "Test 1 - Public route: "
if curl -s http://public.example.com | grep -q "Public-Service"; then
  echo "✓ PASS"
else
  echo "✗ FAIL"
fi

# Test 2: Protected route without auth
echo -n "Test 2 - Protected without auth: "
if curl -s -o /dev/null -w "%{http_code}" http://api.example.com/api/users | grep -q "401"; then
  echo "✓ PASS"
else
  echo "✗ FAIL"
fi

# Test 3: Protected route with auth
echo -n "Test 3 - Protected with auth: "
if curl -s -u admin:password http://api.example.com/api/users | grep -q "API-Service"; then
  echo "✓ PASS"
else
  echo "✗ FAIL"
fi

# Test 4: Inherited middleware
echo -n "Test 4 - Middleware inheritance: "
HEADERS=$(curl -s -u admin:password -I http://api.example.com/api/admin)
if echo "$HEADERS" | grep -q "X-API-Version: v1" && echo "$HEADERS" | grep -q "X-Admin: true"; then
  echo "✓ PASS"
else
  echo "✗ FAIL"
fi

# Test 5: Non-matching parent
echo -n "Test 5 - Non-matching parent: "
if curl -s -o /dev/null -w "%{http_code}" http://wrong.example.com/api/users | grep -q "404"; then
  echo "✓ PASS"
else
  echo "✗ FAIL"
fi

echo "Testing complete!"
```

Run with:
```bash
chmod +x test-tree.sh
./test-tree.sh
```

## Best Practices

1. **Keep tree shallow**: Deep nesting makes debugging difficult
2. **Use descriptive names**: Name routers by their purpose (auth-router, cors-router)
3. **Document parent relationships**: Add comments explaining why parentRefs are used
4. **Test incrementally**: Verify each level works before adding children
5. **Monitor effective middleware**: Use the API to verify middleware chains
6. **Avoid circular dependencies**: Plan tree structure top-down

## Migration from Middleware-Only Approach

If you previously duplicated middleware across routers:

**Before** (without parentRefs):
```yaml
routers:
  api-users:
    rule: "Host(`api.example.com`) && Path(`/users`)"
    middlewares: ["auth", "cors", "rate-limit", "api-headers"]
    service: users-service
  
  api-products:
    rule: "Host(`api.example.com`) && Path(`/products`)"
    middlewares: ["auth", "cors", "rate-limit", "api-headers"]
    service: products-service
```

**After** (with parentRefs):
```yaml
routers:
  api-base:
    rule: "Host(`api.example.com`)"
    middlewares: ["auth", "cors", "rate-limit", "api-headers"]
  
  api-users:
    parentRefs: ["api-base"]
    rule: "Path(`/users`)"
    service: users-service
  
  api-products:
    parentRefs: ["api-base"]
    rule: "Path(`/products`)"
    service: products-service
```

Benefits:
- Centralized middleware management
- Reduced configuration duplication
- Clearer routing tree structure
- Easier to modify common middleware
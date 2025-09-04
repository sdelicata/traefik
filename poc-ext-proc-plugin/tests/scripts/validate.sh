#!/bin/bash
# POC ext-proc Plugin Validation Script

set -e

echo "🧪 ext-proc Plugin POC Validation"
echo "================================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
TRAEFIK_URL="http://localhost:80"
TRAEFIK_API="http://localhost:8080"
TEST_HOST="whoami.localhost"

# Helper functions
log_info() {
    echo -e "${YELLOW}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

test_passed=0
test_failed=0

run_test() {
    local test_name="$1"
    local test_command="$2"
    local expected_result="$3"
    
    log_info "Running: $test_name"
    
    if eval "$test_command"; then
        log_success "$test_name"
        ((test_passed++))
    else
        log_error "$test_name"
        ((test_failed++))
    fi
    echo ""
}

# Test 1: Traefik API accessibility
log_info "Test 1: Traefik API Accessibility"
run_test "Traefik Ping" \
    "curl -s $TRAEFIK_API/ping | grep -q 'OK'" \
    "Should return OK"

# Test 2: Service connectivity
log_info "Test 2: Service Connectivity"
run_test "whoami service accessible" \
    "curl -s -H 'Host: $TEST_HOST' $TRAEFIK_URL/ | grep -q 'Hostname:'" \
    "Should return whoami response"

# Test 3: ext-proc middleware loaded
log_info "Test 3: ext-proc Middleware Status"
run_test "ext-proc middleware loaded in Traefik" \
    "curl -s $TRAEFIK_API/api/rawdata | jq -r '.http.middlewares | keys[]' | grep -q 'extproc'" \
    "Should find extproc middleware"

# Test 4: gRPC server connectivity (if available)
log_info "Test 4: gRPC Server Status"
if docker-compose ps | grep -q extproc-plugin; then
    run_test "ext-proc gRPC server health" \
        "docker-compose exec -T extproc-plugin grpc_health_probe -addr=:9001" \
        "Should return healthy status"
else
    log_error "ext-proc server container not found"
    ((test_failed++))
fi

# Test 5: Header processing (when ext-proc is working)
log_info "Test 5: Header Processing"
run_test "Request with X-Request-Header processed" \
    "curl -s -H 'X-Request-Header: test-value' -H 'Host: $TEST_HOST' -I $TRAEFIK_URL/ | grep -i 'x-response-header:'" \
    "Should add X-Response-Header to response"

# Test 6: Normal request without special header
log_info "Test 6: Normal Request Processing"
run_test "Request without X-Request-Header works normally" \
    "curl -s -H 'Host: $TEST_HOST' $TRAEFIK_URL/ | grep -q 'Hostname:'" \
    "Should work normally without special header"

# Test 7: Body processing with "stop" keyword
log_info "Test 7: Body Processing - Stop Detection"
run_test "Request body with 'stop' should return 503" \
    "curl -s -X POST -H 'Host: $TEST_HOST' -H 'Content-Type: application/json' -d '{\"action\": \"stop\"}' $TRAEFIK_URL/ -w '%{http_code}' -o /dev/null | grep -q '503'" \
    "Should return 503 when body contains 'stop'"

# Test 8: Body processing without "stop" keyword  
log_info "Test 8: Body Processing - Normal Processing"
run_test "Request body without 'stop' should continue normally" \
    "curl -s -X POST -H 'Host: $TEST_HOST' -H 'Content-Type: application/json' -d '{\"action\": \"continue\"}' $TRAEFIK_URL/ | grep -q 'Hostname:'" \
    "Should process normally when body doesn't contain 'stop'"

# Test 9: Multiple concurrent requests
log_info "Test 9: Concurrent Request Handling"
run_test "Multiple concurrent requests" \
    "for i in {1..5}; do curl -s -H 'X-Request-Header: concurrent-$i' -H 'Host: $TEST_HOST' $TRAEFIK_URL/ & done; wait" \
    "Should handle concurrent requests"

# Summary
echo "==============================="
echo "Validation Summary:"
echo "  Passed: $test_passed"
echo "  Failed: $test_failed"
echo "  Total:  $((test_passed + test_failed))"

if [ $test_failed -eq 0 ]; then
    log_success "All tests passed! POC is working correctly."
    exit 0
else
    log_error "$test_failed test(s) failed. Check the issues above."
    exit 1
fi
#!/bin/bash
# End-to-end test for security integration

set -e

echo "=== GraphDB Security Integration Test ==="
echo ""

# Start server in background
echo "Starting server with encryption enabled..."
ENCRYPTION_ENABLED=true PORT=9998 ./bin/server > /tmp/graphdb-test.log 2>&1 &
SERVER_PID=$!

# Wait for server to start
sleep 2

# Function to cleanup on exit
cleanup() {
    echo ""
    echo "Cleaning up..."
    kill $SERVER_PID 2>/dev/null || true
    rm -f /tmp/graphdb-test.log
}
trap cleanup EXIT

BASE_URL="http://localhost:9998"

echo "✓ Server started (PID: $SERVER_PID)"
echo ""

# Test 1: Health check
echo "Test 1: Health check"
curl -s "$BASE_URL/health" | grep -q "Community" && echo "✓ Health check passed" || echo "✗ Health check failed"
echo ""

# Test 2: Login to get JWT token
echo "Test 2: Authentication"
LOGIN_RESPONSE=$(curl -s -X POST "$BASE_URL/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123!"}')

TOKEN=$(echo "$LOGIN_RESPONSE" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)

if [ -n "$TOKEN" ]; then
    echo "✓ Authentication successful"
else
    echo "✗ Authentication failed"
    echo "Response: $LOGIN_RESPONSE"
    exit 1
fi
echo ""

# Test 3: Security health endpoint
echo "Test 3: Security health endpoint"
HEALTH_RESPONSE=$(curl -s -H "Authorization: Bearer $TOKEN" "$BASE_URL/api/v1/security/health")
echo "$HEALTH_RESPONSE" | grep -q "healthy" && echo "✓ Security health check passed" || echo "✗ Security health check failed"
echo ""

# Test 4: Encryption key info
echo "Test 4: Encryption key info"
KEY_INFO=$(curl -s -H "Authorization: Bearer $TOKEN" "$BASE_URL/api/v1/security/keys/info")
echo "$KEY_INFO" | grep -q "active_version" && echo "✓ Key info retrieved successfully" || echo "✗ Key info failed"
echo ""

# Test 5: Audit logs
echo "Test 5: Audit logs"
AUDIT_LOGS=$(curl -s -H "Authorization: Bearer $TOKEN" "$BASE_URL/api/v1/security/audit/logs")
echo "$AUDIT_LOGS" | grep -q "events" && echo "✓ Audit logs retrieved successfully" || echo "✗ Audit logs failed"
echo ""

# Test 6: Input validation (should reject malicious input)
echo "Test 6: Input validation (XSS protection)"
MALICIOUS_RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "$BASE_URL/query" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"query":"<script>alert(\"XSS\")</script>"}')

HTTP_CODE=$(echo "$MALICIOUS_RESPONSE" | tail -1)
if [ "$HTTP_CODE" = "400" ]; then
    echo "✓ XSS attack blocked successfully"
else
    echo "✗ XSS attack not blocked (HTTP $HTTP_CODE)"
fi
echo ""

# Test 7: Create a node (with authentication)
echo "Test 7: Create node with authentication"
NODE_RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "$BASE_URL/nodes" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"labels":["User"],"properties":{"name":"Alice","age":30}}')

HTTP_CODE=$(echo "$NODE_RESPONSE" | tail -1)
if [ "$HTTP_CODE" = "201" ]; then
    echo "✓ Node created successfully with authentication"
else
    echo "✗ Node creation failed (HTTP $HTTP_CODE)"
fi
echo ""

# Test 8: Try to access protected endpoint without auth
echo "Test 8: Unauthorized access protection"
UNAUTH_RESPONSE=$(curl -s -w "\n%{http_code}" -X GET "$BASE_URL/nodes")
HTTP_CODE=$(echo "$UNAUTH_RESPONSE" | tail -1)
if [ "$HTTP_CODE" = "401" ]; then
    echo "✓ Unauthorized access blocked successfully"
else
    echo "✗ Unauthorized access not blocked (HTTP $HTTP_CODE)"
fi
echo ""

# Test 9: Key rotation
echo "Test 9: Encryption key rotation"
ROTATE_RESPONSE=$(curl -s -X POST "$BASE_URL/api/v1/security/keys/rotate" \
  -H "Authorization: Bearer $TOKEN")
echo "$ROTATE_RESPONSE" | grep -q "new_version" && echo "✓ Key rotation successful" || echo "✗ Key rotation failed"
echo ""

# Test 10: Verify security headers
echo "Test 10: Security headers"
HEADERS=$(curl -s -I "$BASE_URL/health")
echo "$HEADERS" | grep -q "X-Frame-Options" && echo "✓ X-Frame-Options header present" || echo "✗ X-Frame-Options missing"
echo "$HEADERS" | grep -q "X-Content-Type-Options" && echo "✓ X-Content-Type-Options header present" || echo "✗ X-Content-Type-Options missing"
echo "$HEADERS" | grep -q "X-XSS-Protection" && echo "✓ X-XSS-Protection header present" || echo "✗ X-XSS-Protection missing"
echo ""

echo "=== All tests completed ==="
